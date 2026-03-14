import {
  type Assignment,
  fetchWorkflowNodeOutputChunk,
  fetchWorkflowNodeOutput,
  type JsonObject,
  pullAssignment,
  registerWorker,
  submitResult,
} from "./api";

export type WasmWorkerResponse = {
  req_id: string;
  output?: unknown;
  mode?: string;
  result_sig?: string;
  error?: string;
};

const WASM_WORKER_TIMEOUT_MS = 45_000;

type RunOnceDeps = {
  workerID: string;
  ensureWasmWorker: () => Worker;
  log: (message: string, obj?: unknown) => void;
  setAssignmentText: (value: string) => void;
};

type SyntheticMode = "emit_big_array" | "consume_big_array";

type ExecutionDependency = {
  workflow_id: string;
  node_id: string;
  payload: JsonObject;
};

type ExecutionContext = {
  args: unknown[];
  dependencies: ExecutionDependency[];
};

function compactValue(value: unknown, depth = 0): unknown {
  if (depth > 2) {
    return "[depth-limited]";
  }
  if (typeof value === "string") {
    if (value.length <= 200) {
      return value;
    }
    return `${value.slice(0, 120)}...[len=${value.length}]`;
  }
  if (Array.isArray(value)) {
    if (value.length <= 8) {
      return value.map((item) => compactValue(item, depth + 1));
    }
    return {
      kind: "array",
      length: value.length,
      head: value.slice(0, 3).map((item) => compactValue(item, depth + 1)),
      tail: value.slice(-2).map((item) => compactValue(item, depth + 1)),
    };
  }
  if (!value || typeof value !== "object") {
    return value;
  }

  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
    out[k] = compactValue(v, depth + 1);
  }
  return out;
}

function compactAssignmentForLog(assignment: Assignment): Record<string, unknown> {
  const copy: Record<string, unknown> = { ...assignment };
  if (Object.prototype.hasOwnProperty.call(copy, "args")) {
    copy.args = compactValue(copy.args);
  }
  if (Object.prototype.hasOwnProperty.call(copy, "dependencies")) {
    const deps = Array.isArray(copy.dependencies) ? copy.dependencies : [];
    copy.dependencies = { count: deps.length, items: compactValue(deps) };
  }
  return copy;
}

function compactWorkerResponseForLog(response: WasmWorkerResponse): Record<string, unknown> {
  return {
    req_id: response.req_id,
    mode: response.mode,
    result_sig: response.result_sig,
    output: compactValue(response.output),
    error: response.error,
  };
}

function compactDecisionForLog(decision: Record<string, unknown>): Record<string, unknown> {
  const copy: Record<string, unknown> = { ...decision };
  if (Object.prototype.hasOwnProperty.call(copy, "accepted_payload")) {
    copy.accepted_payload = compactValue(copy.accepted_payload);
  }
  return copy;
}

function executeWasmJob(
  ensureWasmWorker: () => Worker,
  assignment: Assignment,
  executionArgs: unknown[],
  executionContext: ExecutionContext,
): Promise<WasmWorkerResponse> {
  return new Promise<WasmWorkerResponse>((resolve, reject) => {
    const worker = ensureWasmWorker();
    const requestID = `${assignment.job_id}-${Date.now()}`;
    const timeoutID = window.setTimeout(
      () => reject(new Error("wasm worker timeout")),
      WASM_WORKER_TIMEOUT_MS,
    );

    const onMessage = (event: MessageEvent<WasmWorkerResponse>) => {
      const data = event.data;
      if (!data || data.req_id !== requestID) {
        return;
      }
      worker.removeEventListener("message", onMessage);
      window.clearTimeout(timeoutID);

      if (data.error) {
        reject(new Error(data.error));
        return;
      }
      resolve(data);
    };

    worker.addEventListener("message", onMessage);
    worker.postMessage({
      req_id: requestID,
      wasm_url: assignment.wasm_url,
      args: executionArgs,
      input_context: executionContext,
    });
  });
}

function parseSyntheticMode(args: unknown[] | undefined): { mode: SyntheticMode; cfg: Record<string, unknown> } | null {
  if (!Array.isArray(args)) {
    return null;
  }
  for (const arg of args) {
    if (!arg || typeof arg !== "object" || Array.isArray(arg)) {
      continue;
    }
    const cfg = arg as Record<string, unknown>;
    const raw = typeof cfg._synthetic === "string" ? cfg._synthetic.trim() : "";
    if (raw === "emit_big_array" || raw === "consume_big_array") {
      return { mode: raw, cfg };
    }
  }
  return null;
}

function toBoundedInt(value: unknown, fallback: number, min: number, max: number): number {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return fallback;
  }
  const out = Math.trunc(n);
  if (out < min) return min;
  if (out > max) return max;
  return out;
}

async function sha256Hex(text: string): Promise<string> {
  const bytes = new TextEncoder().encode(text);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const view = new Uint8Array(digest);
  let hex = "";
  for (let i = 0; i < view.length; i += 1) {
    hex += view[i].toString(16).padStart(2, "0");
  }
  return hex;
}

function makeSyntheticResult(jobID: string, output: unknown, mode: string): Promise<WasmWorkerResponse> {
  const reqID = `${jobID}-synthetic-${Date.now()}`;
  return sha256Hex(JSON.stringify({ output, mode })).then((resultSig) => ({
    req_id: reqID,
    output,
    mode,
    result_sig: resultSig,
  }));
}

async function maybeRunSyntheticJob(assignment: Assignment): Promise<WasmWorkerResponse | null> {
  const synthetic = parseSyntheticMode(assignment.args);
  if (!synthetic) {
    return null;
  }

  if (synthetic.mode === "emit_big_array") {
    const maxScan = toBoundedInt(synthetic.cfg.max_scan, 2_000_000, 10_000, 50_000_000);
    const wantCount = toBoundedInt(synthetic.cfg.count, 80_000, 100, 250_000);
    const out: number[] = [];
    for (let n = 1; n <= maxScan && out.length < wantCount; n += 1) {
      if (n % 17 === 0 || n % 19 === 0) {
        out.push(n);
      }
    }
    return makeSyntheticResult(assignment.job_id, out, "synthetic:emit_big_array");
  }

  const deps = Array.isArray(assignment.dependencies) ? assignment.dependencies : [];
  const limit = toBoundedInt(synthetic.cfg.chunk_limit, 512, 64, 5000);
  let total = 0;
  let count = 0;
  let first: number | null = null;
  let last: number | null = null;

  for (const dep of deps) {
    let offset = 0;
    for (;;) {
      const chunk = await fetchWorkflowNodeOutputChunk(dep.workflow_id, dep.node_id, offset, limit);
      const items = chunk.mode === "array" && Array.isArray(chunk.items) ? chunk.items : [];
      for (const item of items) {
        if (typeof item !== "number") {
          continue;
        }
        if (first === null) {
          first = item;
        }
        last = item;
        total += item;
        count += 1;
      }
      if (chunk.done) {
        break;
      }
      offset = typeof chunk.next_offset === "number" ? chunk.next_offset : offset + limit;
    }
  }

  return makeSyntheticResult(
    assignment.job_id,
    {
      count,
      first,
      last,
      sum: total,
      average: count > 0 ? total / count : 0,
    },
    "synthetic:consume_big_array",
  );
}

async function buildExecutionContext(
  assignment: Assignment,
  log: RunOnceDeps["log"],
): Promise<ExecutionContext> {
  const out: unknown[] = Array.isArray(assignment.args) ? [...assignment.args] : [];
  const deps = Array.isArray(assignment.dependencies) ? assignment.dependencies : [];
  const dependencyPayloads: ExecutionDependency[] = [];
  if (deps.length === 0) {
    return {
      args: out,
      dependencies: dependencyPayloads,
    };
  }

  const fetched = await Promise.all(
    deps.map(async (dep) => {
      const payload = await fetchWorkflowNodeOutput(dep.workflow_id, dep.node_id);
      return { dep, payload };
    }),
  );

  for (const item of fetched) {
    dependencyPayloads.push({
      workflow_id: item.dep.workflow_id,
      node_id: item.dep.node_id,
      payload: item.payload,
    });

    log("Dependency output loaded (kept as reference)", {
      workflow_id: item.dep.workflow_id,
      node_id: item.dep.node_id,
    });
  }

  return {
    args: out,
    dependencies: dependencyPayloads,
  };
}

export async function runWorkerOnce(deps: RunOnceDeps): Promise<void> {
  const workerAddress = deps.workerID.trim();
  if (!workerAddress.startsWith("0x") || workerAddress.length < 10) {
    throw new Error("worker_id must be wallet address (0x...)");
  }

  await registerWorker(workerAddress);

  const assignment = await pullAssignment(workerAddress);
  if (!assignment) {
    deps.log("No job available");
    return;
  }

  const assignmentForLog = compactAssignmentForLog(assignment);
  deps.setAssignmentText(JSON.stringify(assignmentForLog, null, 2));
  deps.log("Job assigned", assignmentForLog);

  const syntheticResult = await maybeRunSyntheticJob(assignment);
  let wasmResult: WasmWorkerResponse;
  if (syntheticResult) {
    wasmResult = syntheticResult;
  } else {
    const execution = await buildExecutionContext(assignment, deps.log);
    wasmResult = await executeWasmJob(deps.ensureWasmWorker, assignment, execution.args, execution);
  }
  deps.log("WASM executed", compactWorkerResponseForLog(wasmResult));

  if (!wasmResult.result_sig) {
    throw new Error("worker result_sig missing");
  }

  const resultPayload: JsonObject = {
    output: wasmResult.output ?? null,
  };
  if (typeof wasmResult.mode === "string" && wasmResult.mode.length > 0) {
    resultPayload.mode = wasmResult.mode;
  }
  const decision = await submitResult(assignment.job_id, workerAddress, wasmResult.result_sig, resultPayload);
  deps.log("Result submitted", compactDecisionForLog(decision as Record<string, unknown>));
}
