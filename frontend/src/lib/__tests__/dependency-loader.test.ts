import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  fetchWorkflowNodeOutput,
  fetchWorkflowNodeOutputChunk,
} from "../api";
import { buildExecutionContext } from "../dependency-loader";

vi.mock("../api", () => ({
  fetchWorkflowNodeOutput: vi.fn(),
  fetchWorkflowNodeOutputChunk: vi.fn(),
}));

const mockedFetchOutput = vi.mocked(fetchWorkflowNodeOutput);
const mockedFetchChunk = vi.mocked(fetchWorkflowNodeOutputChunk);

describe("buildExecutionContext", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps args and valid artifacts when there are no dependencies", async () => {
    const context = await buildExecutionContext(
      {
        job_id: "job-1",
        workflow_id: "wf",
        node_id: "node",
        wasm_url: "/node.wasm",
        args: [{ limit: 3 }],
        artifacts: [
          { id: "dataset", url: "/api/workflow/artifact?id=dataset" },
          { id: "", url: "/ignored" },
        ],
      },
      vi.fn(),
    );

    expect(context).toEqual({
      args: [{ limit: 3 }],
      inputs: {},
      artifacts: {
        dataset: { id: "dataset", url: "/api/workflow/artifact?id=dataset" },
      },
    });
    expect(mockedFetchChunk).not.toHaveBeenCalled();
    expect(mockedFetchOutput).not.toHaveBeenCalled();
  });

  it("uses the simple output endpoint for small dependencies", async () => {
    mockedFetchChunk.mockResolvedValueOnce({
      mode: "string",
      offset: 0,
      limit: 1,
      done: true,
      total_chars: 12,
    });
    mockedFetchOutput.mockResolvedValueOnce({ output: "small result" });

    const context = await buildExecutionContext(
      {
        job_id: "job-2",
        workflow_id: "wf",
        node_id: "consumer",
        wasm_url: "/consumer.wasm",
        dependencies: [{ workflow_id: "wf", node_id: "producer" }],
      },
      vi.fn(),
    );

    expect(mockedFetchOutput).toHaveBeenCalledWith("wf", "producer");
    expect(context.inputs).toEqual({
      producer: { output: "small result" },
    });
  });

  it("returns an empty payload for missing dependency output", async () => {
    mockedFetchChunk.mockResolvedValueOnce({
      mode: "missing",
      offset: 0,
      limit: 1,
      done: true,
    });

    const context = await buildExecutionContext(
      {
        job_id: "job-3",
        workflow_id: "wf",
        node_id: "consumer",
        wasm_url: "/consumer.wasm",
        dependencies: [{ workflow_id: "wf", node_id: "missing-node" }],
      },
      vi.fn(),
    );

    expect(mockedFetchOutput).not.toHaveBeenCalled();
    expect(context.inputs).toEqual({
      "missing-node": {},
    });
  });

  it("reassembles large string dependencies from chunks", async () => {
    const log = vi.fn();
    mockedFetchChunk
      .mockResolvedValueOnce({
        mode: "string",
        offset: 0,
        limit: 1,
        done: false,
        total_chars: 200_001,
      })
      .mockResolvedValueOnce({
        mode: "string",
        offset: 0,
        limit: 16_384,
        next_offset: 6,
        done: false,
        data: "hello ",
      })
      .mockResolvedValueOnce({
        mode: "string",
        offset: 6,
        limit: 16_384,
        done: true,
        data: "world",
      });

    const context = await buildExecutionContext(
      {
        job_id: "job-4",
        workflow_id: "wf",
        node_id: "consumer",
        wasm_url: "/consumer.wasm",
        dependencies: [{ workflow_id: "wf", node_id: "big-text" }],
      },
      log,
    );

    expect(mockedFetchOutput).not.toHaveBeenCalled();
    expect(mockedFetchChunk).toHaveBeenNthCalledWith(1, "wf", "big-text", 0, 1);
    expect(mockedFetchChunk).toHaveBeenNthCalledWith(2, "wf", "big-text", 0, 16_384);
    expect(mockedFetchChunk).toHaveBeenNthCalledWith(3, "wf", "big-text", 6, 16_384);
    expect(context.inputs["big-text"]).toEqual({ output: "hello world" });
    expect(log).toHaveBeenCalledWith(
      "Dependency output too large for inline fetch; switching to chunked reassembly",
      expect.objectContaining({ node_id: "big-text", mode: "string" }),
    );
  });

  it("reassembles large arrays from chunks", async () => {
    mockedFetchChunk
      .mockResolvedValueOnce({
        mode: "array",
        offset: 0,
        limit: 1,
        done: false,
        total_items: 10_001,
      })
      .mockResolvedValueOnce({
        mode: "array",
        offset: 0,
        limit: 4_096,
        next_offset: 2,
        done: false,
        items: ["a", "b"],
      })
      .mockResolvedValueOnce({
        mode: "array",
        offset: 2,
        limit: 4_096,
        done: true,
        items: ["c"],
      });

    const context = await buildExecutionContext(
      {
        job_id: "job-5",
        workflow_id: "wf",
        node_id: "consumer",
        wasm_url: "/consumer.wasm",
        dependencies: [{ workflow_id: "wf", node_id: "big-array" }],
      },
      vi.fn(),
    );

    expect(context.inputs["big-array"]).toEqual({ output: ["a", "b", "c"] });
  });

  it("parses chunked json dependencies after text reassembly", async () => {
    mockedFetchChunk
      .mockResolvedValueOnce({
        mode: "json",
        offset: 0,
        limit: 1,
        done: false,
        total_chars: 200_001,
      })
      .mockResolvedValueOnce({
        mode: "json",
        offset: 0,
        limit: 16_384,
        next_offset: 7,
        done: false,
        data: "{\"ok\":",
      })
      .mockResolvedValueOnce({
        mode: "json",
        offset: 7,
        limit: 16_384,
        done: true,
        data: "true}",
      });

    const context = await buildExecutionContext(
      {
        job_id: "job-6",
        workflow_id: "wf",
        node_id: "consumer",
        wasm_url: "/consumer.wasm",
        dependencies: [{ workflow_id: "wf", node_id: "big-json" }],
      },
      vi.fn(),
    );

    expect(context.inputs["big-json"]).toEqual({ output: { ok: true } });
  });

  it("fails when dependency chunk mode changes mid-stream", async () => {
    mockedFetchChunk
      .mockResolvedValueOnce({
        mode: "string",
        offset: 0,
        limit: 1,
        done: false,
        total_chars: 200_001,
      })
      .mockResolvedValueOnce({
        mode: "json",
        offset: 0,
        limit: 16_384,
        done: true,
        data: "{}",
      });

    await expect(
      buildExecutionContext(
        {
          job_id: "job-7",
          workflow_id: "wf",
          node_id: "consumer",
          wasm_url: "/consumer.wasm",
          dependencies: [{ workflow_id: "wf", node_id: "flaky-dep" }],
        },
        vi.fn(),
      ),
    ).rejects.toThrow("changed mode during chunk fetch");
  });

  it("fails when dependency chunk offsets do not advance", async () => {
    mockedFetchChunk
      .mockResolvedValueOnce({
        mode: "array",
        offset: 0,
        limit: 1,
        done: false,
        total_items: 10_001,
      })
      .mockResolvedValueOnce({
        mode: "array",
        offset: 0,
        limit: 4_096,
        next_offset: 0,
        done: false,
        items: ["a"],
      });

    await expect(
      buildExecutionContext(
        {
          job_id: "job-stuck",
          workflow_id: "wf",
          node_id: "consumer",
          wasm_url: "/consumer.wasm",
          dependencies: [{ workflow_id: "wf", node_id: "stuck-dep" }],
        },
        vi.fn(),
      ),
    ).rejects.toThrow("chunk offset did not advance");
  });

  it("fails loudly when chunked json is invalid", async () => {
    mockedFetchChunk
      .mockResolvedValueOnce({
        mode: "json",
        offset: 0,
        limit: 1,
        done: false,
        total_chars: 200_001,
      })
      .mockResolvedValueOnce({
        mode: "json",
        offset: 0,
        limit: 16_384,
        done: true,
        data: "{not-json",
      });

    await expect(
      buildExecutionContext(
        {
          job_id: "job-8",
          workflow_id: "wf",
          node_id: "consumer",
          wasm_url: "/consumer.wasm",
          dependencies: [{ workflow_id: "wf", node_id: "bad-json" }],
        },
        vi.fn(),
      ),
    ).rejects.toThrow("chunked json reassembly failed");
  });
});
