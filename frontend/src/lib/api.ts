export type JsonObject = Record<string, unknown>;

export type WorkerAuthChallenge = {
  worker_id: string;
  nonce: string;
  message: string;
  expires_at: string;
};

export type WorkerAuthSession = {
  worker_id: string;
  token: string;
  expires_at: string;
};

export type DependencyRef = {
  workflow_id: string;
  node_id: string;
};

export type WorkflowArtifact = {
  id: string;
  file?: string;
  path?: string;
  url?: string;
  size?: number;
  sha256?: string;
};

export type Assignment = {
  job_id: string;
  workflow_id: string;
  node_id: string;
  wasm_url: string;
  args?: unknown[];
  dependencies?: DependencyRef[];
  artifacts?: WorkflowArtifact[];
  output_artifacts?: WorkflowArtifact[];
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
  program?: string;
  wasm_url: string;
  execution_target?: "browser_worker" | "server";
  reward_usdc: string;
  completed: boolean;
  enqueued: boolean;
  uses_artifacts?: string[];
  output_artifacts?: WorkflowArtifact[];
  replication_factor?: number;
  acceptance_policy?: "consensus" | "collect_all";
  traits?: string[];
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
  execution_target?: "browser_worker" | "server";
  assigned_workers?: string[];
  submitted_workers?: string[];
  finalized: boolean;
  accepted_workers: number;
  queue_index: number;
  required_replicas: number;
  acceptance_policy?: "consensus" | "collect_all";
  traits?: string[];
};

export type RuntimeSnapshot = {
  active_workflow_id?: string;
  loaded_workflow_id?: string;
  topology_mode?: string;
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

const workerAuthStorageKey = "x402.workerAuthSession";

let cachedWorkerAuthSession: WorkerAuthSession | null = loadWorkerAuthSessionFromStorage();

function loadWorkerAuthSessionFromStorage(): WorkerAuthSession | null {
  if (typeof window === "undefined") {
    return null;
  }
  const raw = window.localStorage.getItem(workerAuthStorageKey);
  if (!raw) {
    return null;
  }

  try {
    const parsed = JSON.parse(raw) as Partial<WorkerAuthSession>;
    if (
      typeof parsed.worker_id !== "string" ||
      typeof parsed.token !== "string" ||
      typeof parsed.expires_at !== "string"
    ) {
      window.localStorage.removeItem(workerAuthStorageKey);
      return null;
    }

    const expiresAt = new Date(parsed.expires_at);
    if (Number.isNaN(expiresAt.getTime()) || expiresAt.getTime() <= Date.now()) {
      window.localStorage.removeItem(workerAuthStorageKey);
      return null;
    }

    return {
      worker_id: parsed.worker_id,
      token: parsed.token,
      expires_at: parsed.expires_at,
    };
  } catch {
    window.localStorage.removeItem(workerAuthStorageKey);
    return null;
  }
}

function authHeaders(extraHeaders?: HeadersInit): HeadersInit {
  const headers = new Headers(extraHeaders);
  if (cachedWorkerAuthSession?.token) {
    headers.set("Authorization", `Bearer ${cachedWorkerAuthSession.token}`);
  }
  return headers;
}

export function getWorkerAuthSession(): WorkerAuthSession | null {
  cachedWorkerAuthSession = loadWorkerAuthSessionFromStorage();
  return cachedWorkerAuthSession;
}

export function persistWorkerAuthSession(session: WorkerAuthSession | null): void {
  cachedWorkerAuthSession = session;
  if (typeof window === "undefined") {
    return;
  }
  if (!session) {
    window.localStorage.removeItem(workerAuthStorageKey);
    return;
  }
  window.localStorage.setItem(workerAuthStorageKey, JSON.stringify(session));
}

export function clearWorkerAuthSession(): void {
  persistWorkerAuthSession(null);
}

export async function requestWorkerAuthChallenge(workerID: string): Promise<WorkerAuthChallenge> {
  const response = await fetch("/api/auth/challenge", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ worker_id: workerID }),
  });

  if (!response.ok) {
    const message = await response.text();
    throw new Error(`auth challenge failed: ${response.status} ${message}`);
  }

  return (await response.json()) as WorkerAuthChallenge;
}

export async function verifyWorkerAuthChallenge(
  workerID: string,
  nonce: string,
  signature: string,
): Promise<WorkerAuthSession> {
  const response = await fetch("/api/auth/verify", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      worker_id: workerID,
      nonce,
      signature,
    }),
  });

  if (!response.ok) {
    const message = await response.text();
    throw new Error(`auth verify failed: ${response.status} ${message}`);
  }

  const session = (await response.json()) as WorkerAuthSession;
  persistWorkerAuthSession(session);
  return session;
}

export async function registerWorker(workerID: string): Promise<unknown> {
  const response = await fetch("/api/workers/register", {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify({ worker_id: workerID }),
  });

  if (!response.ok) {
    const message = await response.text();
    throw new Error(`register failed: ${response.status} ${message}`);
  }
  return response.json();
}

export async function pullAssignment(workerID: string): Promise<Assignment | null> {
  const response = await fetch(`/api/pull?worker_id=${encodeURIComponent(workerID)}`, {
    headers: authHeaders(),
  });

  if (response.status === 204) {
    return null;
  }
  if (!response.ok) {
    const message = await response.text();
    throw new Error(`pull failed: ${response.status} ${message}`);
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
    headers: authHeaders({ "Content-Type": "application/json" }),
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

export async function fetchPayments(workerID: string): Promise<PaymentEvent[]> {
  const trimmedWorkerID = workerID.trim();
  if (trimmedWorkerID === "") {
    throw new Error("worker_id is required");
  }
  const qs = new URLSearchParams();
  qs.set("worker_id", trimmedWorkerID);
  const suffix = qs.toString();
  const response = await fetch(`/api/payments${suffix ? `?${suffix}` : ""}`, {
    headers: authHeaders(),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(`payments failed: ${response.status} ${message}`);
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
