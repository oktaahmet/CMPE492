import {
  type Assignment,
  type DependencyRef,
  type JsonObject,
  type WorkflowArtifact,
  fetchWorkflowNodeOutput,
  fetchWorkflowNodeOutputChunk,
} from "./api";

const MAX_INLINE_DEP_ARRAY_ITEMS = 10_000;
const MAX_INLINE_DEP_TEXT_CHARS = 200_000;
const CHUNKED_DEP_ARRAY_ITEMS = 4_096;
const CHUNKED_DEP_TEXT_CHARS = 16_384;

export type ExecutionContext = {
  args: unknown[];
  inputs: Record<string, JsonObject>;
  artifacts: Record<string, WorkflowArtifact>;
};

type DependencyChunkMode = "array" | "string" | "json" | "missing";

type DependencyLog = (message: string, obj?: unknown) => void;

async function fetchDependencyPayload(
  dep: DependencyRef,
  log: DependencyLog,
): Promise<JsonObject> {
  const firstChunk = await fetchWorkflowNodeOutputChunk(dep.workflow_id, dep.node_id, 0, 1);
  if (firstChunk.mode === "missing") {
    return {};
  }

  const isLargeArray =
    firstChunk.mode === "array" && (firstChunk.total_items ?? 0) > MAX_INLINE_DEP_ARRAY_ITEMS;
  const isLargeText =
    (firstChunk.mode === "string" || firstChunk.mode === "json") &&
    (firstChunk.total_chars ?? 0) > MAX_INLINE_DEP_TEXT_CHARS;
  if (!isLargeArray && !isLargeText) {
    return fetchWorkflowNodeOutput(dep.workflow_id, dep.node_id);
  }

  // Small dependency outputs stay on the simple endpoint. Large outputs switch
  // to chunks here, keeping the WASM node authoring model identical either way.
  log("Dependency output too large for inline fetch; switching to chunked reassembly", {
    workflow_id: dep.workflow_id,
    node_id: dep.node_id,
    mode: firstChunk.mode,
    total_items: firstChunk.total_items,
    total_chars: firstChunk.total_chars,
  });

  switch (firstChunk.mode) {
    case "array":
      return {
        output: await reassembleArrayDependency(dep.workflow_id, dep.node_id, firstChunk.mode),
      };
    case "string":
      return {
        output: await reassembleTextDependency(dep.workflow_id, dep.node_id, firstChunk.mode),
      };
    case "json":
      return {
        output: parseChunkedJSON(
          await reassembleTextDependency(dep.workflow_id, dep.node_id, firstChunk.mode),
          dep.workflow_id,
          dep.node_id,
        ),
      };
    default:
      throw new Error(`unsupported dependency chunk mode for ${dep.node_id}: ${String(firstChunk.mode)}`);
  }
}

async function reassembleArrayDependency(
  workflowID: string,
  nodeID: string,
  expectedMode: Extract<DependencyChunkMode, "array">,
): Promise<unknown[]> {
  const items: unknown[] = [];
  let offset = 0;

  for (;;) {
    const chunk = await fetchWorkflowNodeOutputChunk(workflowID, nodeID, offset, CHUNKED_DEP_ARRAY_ITEMS);
    if (chunk.mode !== expectedMode) {
      throw new Error(
        `dependency ${nodeID} changed mode during chunk fetch: expected ${expectedMode}, got ${chunk.mode}`,
      );
    }
    if (Array.isArray(chunk.items)) {
      items.push(...chunk.items);
    }
    if (chunk.done) {
      return items;
    }
    offset = typeof chunk.next_offset === "number" ? chunk.next_offset : offset + CHUNKED_DEP_ARRAY_ITEMS;
  }
}

async function reassembleTextDependency(
  workflowID: string,
  nodeID: string,
  expectedMode: Extract<DependencyChunkMode, "string" | "json">,
): Promise<string> {
  const parts: string[] = [];
  let offset = 0;

  for (;;) {
    const chunk = await fetchWorkflowNodeOutputChunk(workflowID, nodeID, offset, CHUNKED_DEP_TEXT_CHARS);
    if (chunk.mode !== expectedMode) {
      throw new Error(
        `dependency ${nodeID} changed mode during chunk fetch: expected ${expectedMode}, got ${chunk.mode}`,
      );
    }
    if (typeof chunk.data === "string" && chunk.data.length > 0) {
      parts.push(chunk.data);
    }
    if (chunk.done) {
      return parts.join("");
    }
    offset = typeof chunk.next_offset === "number" ? chunk.next_offset : offset + CHUNKED_DEP_TEXT_CHARS;
  }
}

function parseChunkedJSON(text: string, workflowID: string, nodeID: string): unknown {
  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(
      `dependency ${workflowID}/${nodeID} chunked json reassembly failed: ${String(error)}`,
    );
  }
}

export async function buildExecutionContext(
  assignment: Assignment,
  log: DependencyLog,
): Promise<ExecutionContext> {
  const args: unknown[] = Array.isArray(assignment.args) ? [...assignment.args] : [];
  const deps = Array.isArray(assignment.dependencies) ? assignment.dependencies : [];
  const inputPayloads: Record<string, JsonObject> = {};
  if (deps.length === 0) {
    return {
      args,
      inputs: inputPayloads,
      artifacts: assignmentArtifactsByID(assignment.artifacts),
    };
  }

  const fetched = await Promise.all(
    deps.map(async (dep) => {
      const payload = await fetchDependencyPayload(dep, log);
      return { dep, payload };
    }),
  );

  // The C++ ABI receives a single JSON context. Dependencies are keyed by node_id
  // so reducers can ask for inputs["upstream-node"].output without transport code.
  for (const item of fetched) {
    inputPayloads[item.dep.node_id] = item.payload;

    log("Dependency output loaded for execution context", {
      workflow_id: item.dep.workflow_id,
      node_id: item.dep.node_id,
    });
  }

  return {
    args,
    inputs: inputPayloads,
    artifacts: assignmentArtifactsByID(assignment.artifacts),
  };
}

function assignmentArtifactsByID(artifacts: Assignment["artifacts"]): Record<string, WorkflowArtifact> {
  const out: Record<string, WorkflowArtifact> = {};
  if (!Array.isArray(artifacts)) {
    return out;
  }
  for (const artifact of artifacts) {
    if (!artifact || typeof artifact.id !== "string" || artifact.id.trim() === "") {
      continue;
    }
    out[artifact.id] = artifact;
  }
  return out;
}
