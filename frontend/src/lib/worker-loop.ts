import {
  type Assignment,
  type JsonObject,
  processPayments,
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

type RunOnceDeps = {
  workerID: string;
  ensureWasmWorker: () => Worker;
  log: (message: string, obj?: unknown) => void;
  setAssignmentText: (value: string) => void;
};

function executeWasmJob(ensureWasmWorker: () => Worker, assignment: Assignment): Promise<WasmWorkerResponse> {
  return new Promise<WasmWorkerResponse>((resolve, reject) => {
    const worker = ensureWasmWorker();
    const requestID = `${assignment.job_id}-${Date.now()}`;
    const timeoutID = window.setTimeout(() => reject(new Error("wasm worker timeout")), 15000);

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
      args: assignment.args ?? [],
    });
  });
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

  deps.setAssignmentText(JSON.stringify(assignment, null, 2));
  deps.log("Job assigned", assignment);

  const wasmResult = await executeWasmJob(deps.ensureWasmWorker, assignment);
  deps.log("WASM executed", wasmResult);

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
  deps.log("Result submitted", decision);

  if (decision.finalized) {
    const paymentResult = await processPayments();
    deps.log("Payments processed", paymentResult);
  }
}
