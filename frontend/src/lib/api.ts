export type JsonObject = Record<string, unknown>;

export type DependencyRef = {
  workflow_id: string;
  node_id: string;
};

export type Assignment = {
  job_id: string;
  workflow_id: string;
  node_id: string;
  wasm_url: string;
  args?: unknown[];
  dependencies?: DependencyRef[];
  [key: string]: unknown;
};

export type Decision = {
  finalized: boolean;
  [key: string]: unknown;
};

export type PaymentEvent = {
  id: string;
  job_id: string;
  workflow_id: string;
  amount_usdc: string;
  worker_id: string;
  accepted_hash: string;
  status: string;
  attempts: number;
  last_error?: string;
  updated_at: string;
  tx_hash?: string;
  network?: string;
  payer?: string;
};

export type RuntimeStats = {
  queued_jobs: number;
  finalized_jobs: number;
  pending_payments: number;
  workers_online: number;
  total_jobs: number;
};

export type RuntimeWorkflowNode = {
  id: string;
  depends_on?: string[];
  priority?: number;
  wasm_url: string;
  reward_usdc: string;
  completed: boolean;
  enqueued: boolean;
};

export type RuntimeWorkflowSnapshot = {
  workflow_id: string;
  topological_order: string[];
  nodes: RuntimeWorkflowNode[];
};

export type RuntimeJobSnapshot = {
  job_id: string;
  workflow_id: string;
  node_id: string;
  assigned_workers?: string[];
  submitted_workers?: string[];
  finalized: boolean;
  accepted_workers: number;
  queue_index: number;
};

export type RuntimeSnapshot = {
  active_workflow_id?: string;
  loaded_workflow_id?: string;
  stats: RuntimeStats;
  workflow?: RuntimeWorkflowSnapshot;
  jobs?: RuntimeJobSnapshot[];
};

export type WorkflowNodeOutputChunk = {
  mode: "array" | "string" | "json" | "missing";
  offset: number;
  limit: number;
  next_offset?: number;
  done: boolean;
  total_items?: number;
  total_chars?: number;
  items?: unknown[];
  data?: string;
  value?: unknown;
};

export async function registerWorker(workerID: string): Promise<unknown> {
  const response = await fetch("/api/workers/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ worker_id: workerID }),
  });

  if (!response.ok) {
    throw new Error(`register failed: ${response.status}`);
  }
  return response.json();
}

export async function pullAssignment(workerID: string): Promise<Assignment | null> {
  const response = await fetch(`/api/pull?worker_id=${encodeURIComponent(workerID)}`);

  if (response.status === 204) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`pull failed: ${response.status}`);
  }
  return (await response.json()) as Assignment;
}

export async function fetchWorkflowNodeOutput(
  workflowID: string,
  nodeID: string,
): Promise<JsonObject> {
  const qs = new URLSearchParams({
    workflow_id: workflowID,
    node_id: nodeID,
  });
  const response = await fetch(`/api/workflow/node-output?${qs.toString()}`);
  if (!response.ok) {
    throw new Error(`workflow node output fetch failed: ${response.status}`);
  }
  return (await response.json()) as JsonObject;
}

export async function fetchWorkflowNodeOutputChunk(
  workflowID: string,
  nodeID: string,
  offset = 0,
  limit = 256,
): Promise<WorkflowNodeOutputChunk> {
  const qs = new URLSearchParams({
    workflow_id: workflowID,
    node_id: nodeID,
    offset: String(offset),
    limit: String(limit),
  });
  const response = await fetch(`/api/workflow/node-output/chunk?${qs.toString()}`);
  if (!response.ok) {
    throw new Error(`workflow node output chunk fetch failed: ${response.status}`);
  }
  return (await response.json()) as WorkflowNodeOutputChunk;
}

export async function submitResult(
  jobID: string,
  workerID: string,
  resultSig: string,
  resultPayload?: JsonObject,
): Promise<Decision> {
  const response = await fetch("/api/result", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      job_id: jobID,
      worker_id: workerID,
      result_sig: resultSig,
      result_payload: resultPayload ?? {},
    }),
  });

  if (!response.ok) {
    const message = await response.text();
    throw new Error(`submit failed: ${response.status} ${message}`);
  }
  return (await response.json()) as Decision;
}

export async function fetchStats(): Promise<unknown> {
  const response = await fetch("/api/stats");
  if (!response.ok) {
    throw new Error(`stats failed: ${response.status}`);
  }
  return response.json();
}

export async function fetchPayments(workerID?: string): Promise<PaymentEvent[]> {
  const qs = new URLSearchParams();
  if (workerID && workerID.trim() !== "") {
    qs.set("worker_id", workerID.trim());
  }
  const suffix = qs.toString();
  const response = await fetch(`/api/payments${suffix ? `?${suffix}` : ""}`);
  if (!response.ok) {
    throw new Error(`payments failed: ${response.status}`);
  }
  return (await response.json()) as PaymentEvent[];
}

export async function fetchRuntime(): Promise<RuntimeSnapshot> {
  const response = await fetch("/api/runtime");
  if (!response.ok) {
    throw new Error(`runtime failed: ${response.status}`);
  }
  return (await response.json()) as RuntimeSnapshot;
}
