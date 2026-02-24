export type JsonObject = Record<string, unknown>;

export type Assignment = {
  job_id: string;
  wasm_url: string;
  args?: unknown[];
  [key: string]: unknown;
};

export type Decision = {
  finalized: boolean;
  [key: string]: unknown;
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

export async function processPayments(): Promise<unknown> {
  const response = await fetch("/api/payments/process", { method: "POST" });
  if (!response.ok) {
    throw new Error(`payments/process failed: ${response.status}`);
  }
  return response.json();
}

export async function fetchStats(): Promise<unknown> {
  const response = await fetch("/api/stats");
  if (!response.ok) {
    throw new Error(`stats failed: ${response.status}`);
  }
  return response.json();
}

export async function fetchPayments(): Promise<unknown> {
  const response = await fetch("/api/payments");
  if (!response.ok) {
    throw new Error(`payments failed: ${response.status}`);
  }
  return response.json();
}
