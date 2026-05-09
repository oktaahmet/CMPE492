import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  fetchWorkflowNodeOutputChunk,
  pullAssignment,
  registerWorker,
  submitResult,
  type Assignment,
} from "../api";
import { buildExecutionContext } from "../dependency-loader";
import { runWorkerOnce, type WasmWorkerResponse } from "../worker-loop";

vi.mock("../api", () => ({
  fetchWorkflowNodeOutputChunk: vi.fn(),
  pullAssignment: vi.fn(),
  registerWorker: vi.fn(),
  submitResult: vi.fn(),
}));

vi.mock("../dependency-loader", () => ({
  buildExecutionContext: vi.fn(),
}));

const mockedFetchChunk = vi.mocked(fetchWorkflowNodeOutputChunk);
const mockedPullAssignment = vi.mocked(pullAssignment);
const mockedRegisterWorker = vi.mocked(registerWorker);
const mockedSubmitResult = vi.mocked(submitResult);
const mockedBuildExecutionContext = vi.mocked(buildExecutionContext);

const workerID = "0x1234567890abcdef";

class FakeWorker {
  postedMessages: unknown[] = [];
  private readonly listeners = new Map<string, Set<(event: MessageEvent<WasmWorkerResponse>) => void>>();

  constructor(private readonly response: Omit<WasmWorkerResponse, "req_id">) {}

  addEventListener(type: string, listener: (event: MessageEvent<WasmWorkerResponse>) => void): void {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: (event: MessageEvent<WasmWorkerResponse>) => void): void {
    this.listeners.get(type)?.delete(listener);
  }

  postMessage(message: unknown): void {
    this.postedMessages.push(message);
    const reqID = typeof message === "object" && message ? String((message as { req_id?: unknown }).req_id) : "";
    queueMicrotask(() => {
      for (const listener of this.listeners.get("message") ?? []) {
        listener({ data: { req_id: reqID, ...this.response } } as MessageEvent<WasmWorkerResponse>);
      }
    });
  }
}

function assignment(overrides: Partial<Assignment> = {}): Assignment {
  return {
    job_id: "job-1",
    workflow_id: "wf",
    node_id: "node",
    wasm_url: "/node.wasm",
    args: [{ limit: 5 }],
    ...overrides,
  };
}

function deps(overrides: Partial<Parameters<typeof runWorkerOnce>[0]> = {}): Parameters<typeof runWorkerOnce>[0] {
  return {
    workerID,
    ensureWasmWorker: vi.fn(() => new FakeWorker({ output: { ok: true }, mode: "wasm", result_sig: "sig-1" }) as unknown as Worker),
    resetWasmWorker: vi.fn(),
    log: vi.fn(),
    setAssignmentText: vi.fn(),
    ...overrides,
  };
}

describe("runWorkerOnce", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("window", {
      setTimeout: globalThis.setTimeout,
      clearTimeout: globalThis.clearTimeout,
    });
    mockedRegisterWorker.mockResolvedValue({ ok: true });
    mockedSubmitResult.mockResolvedValue({ finalized: true });
    mockedBuildExecutionContext.mockResolvedValue({
      args: [],
      inputs: { upstream: { output: 42 } },
      artifacts: {},
    });
  });

  it("rejects invalid worker ids before touching the backend", async () => {
    await expect(runWorkerOnce(deps({ workerID: "not-a-wallet" }))).rejects.toThrow(
      "worker_id must be wallet address",
    );

    expect(mockedRegisterWorker).not.toHaveBeenCalled();
    expect(mockedPullAssignment).not.toHaveBeenCalled();
  });

  it("registers and exits quietly when no job is available", async () => {
    mockedPullAssignment.mockResolvedValueOnce(null);
    const runDeps = deps();

    await runWorkerOnce(runDeps);

    expect(mockedRegisterWorker).toHaveBeenCalledWith(workerID);
    expect(mockedPullAssignment).toHaveBeenCalledWith(workerID);
    expect(runDeps.log).toHaveBeenCalledWith("No job available");
    expect(runDeps.ensureWasmWorker).not.toHaveBeenCalled();
    expect(mockedSubmitResult).not.toHaveBeenCalled();
  });

  it("executes a wasm assignment with dependency context and submits the signed output payload", async () => {
    mockedPullAssignment.mockResolvedValueOnce(assignment({ dependencies: [{ workflow_id: "wf", node_id: "upstream" }] }));
    const fakeWorker = new FakeWorker({ output: { ok: true }, mode: "wasm:run_json", result_sig: "sig-1" });
    const runDeps = deps({
      ensureWasmWorker: vi.fn(() => fakeWorker as unknown as Worker),
    });

    await runWorkerOnce(runDeps);

    expect(mockedBuildExecutionContext).toHaveBeenCalledWith(expect.objectContaining({ job_id: "job-1" }), runDeps.log);
    expect(fakeWorker.postedMessages).toHaveLength(1);
    expect(fakeWorker.postedMessages[0]).toEqual(
      expect.objectContaining({
        wasm_url: "/node.wasm",
        args: [{ limit: 5 }],
        input_context: {
          args: [],
          inputs: { upstream: { output: 42 } },
          artifacts: {},
        },
      }),
    );
    expect(mockedSubmitResult).toHaveBeenCalledWith("job-1", workerID, "sig-1", {
      output: { ok: true },
      mode: "wasm:run_json",
    });
    expect(runDeps.resetWasmWorker).not.toHaveBeenCalled();
  });

  it("does not submit when the worker response is missing result_sig", async () => {
    mockedPullAssignment.mockResolvedValueOnce(assignment());
    const runDeps = deps({
      ensureWasmWorker: vi.fn(() => new FakeWorker({ output: { ok: true } }) as unknown as Worker),
    });

    await expect(runWorkerOnce(runDeps)).rejects.toThrow("worker result_sig missing");

    expect(mockedSubmitResult).not.toHaveBeenCalled();
  });

  it("fails synthetic chunk consumption when dependency offsets stop advancing", async () => {
    mockedPullAssignment.mockResolvedValueOnce(
      assignment({
        args: [{ _synthetic: "consume_big_array", chunk_limit: 64 }],
        dependencies: [{ workflow_id: "wf", node_id: "big-array" }],
      }),
    );
    mockedFetchChunk.mockResolvedValueOnce({
      mode: "array",
      offset: 0,
      limit: 64,
      next_offset: 0,
      done: false,
      items: [1, 2, 3],
    });
    const runDeps = deps();

    await expect(runWorkerOnce(runDeps)).rejects.toThrow("chunk offset did not advance");

    expect(mockedBuildExecutionContext).not.toHaveBeenCalled();
    expect(runDeps.ensureWasmWorker).not.toHaveBeenCalled();
    expect(mockedSubmitResult).not.toHaveBeenCalled();
  });
});
