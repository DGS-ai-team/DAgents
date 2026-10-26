import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import {
  transcriptStore,
  clearTranscript,
  upsertToolCallFromSSE,
  applyToolResult,
} from "./transcript.js";
import { resetToolStream, resolveToolBlockId } from "./toolStream.js";

/**
 * 复现：并行 write_file + bash_run 时，OpenAI 流式常先发带 id 的 delta，
 * 随后 args 分片 id 为空。旧逻辑会在空 id 时新建 partial-N 气泡，
 * tool_result 只结束真实 id，留下「生成中」僵死气泡。
 */
describe("parallel tool bubbles: empty-id arg chunks", () => {
  beforeEach(() => {
    vi.stubGlobal("requestAnimationFrame", vi.fn(() => 1));
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    clearTranscript();
    resetToolStream();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("resolveToolBlockId keeps index→id so empty-id chunks reuse the same bubble", () => {
    const first = resolveToolBlockId("call_write", 0, true);
    expect(first.blockId).toBe("call_write");

    const chunk = resolveToolBlockId("", 0, true);
    expect(chunk.blockId).toBe("call_write");
    expect(chunk.migrateFrom).toBe("");
  });

  it("reproduces stuck 生成中 bubble before fix (write_file + bash_run)", () => {
    // 1) write_file: id + name, empty args
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 0,
      tool_calls: [
        {
          id: "call_write",
          type: "function",
          function: { name: "write_file", arguments: "" },
        },
      ],
    });
    // 2) bash_run: id + full short args (often completes in one delta)
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 1,
      tool_calls: [
        {
          id: "call_bash",
          type: "function",
          function: {
            name: "bash_run",
            arguments: '{"command":"python test_extract.py"}',
          },
        },
      ],
    });
    // 3) write_file args chunks with empty id (long path typical for write_file)
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 0,
      tool_calls: [
        {
          id: "",
          type: "function",
          function: {
            name: "write_file",
            arguments: '{"path":"test_extract.py","content":"# fix',
          },
        },
      ],
    });
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 0,
      tool_calls: [
        {
          id: "",
          type: "function",
          function: {
            name: "write_file",
            arguments:
              '{"path":"test_extract.py","content":"# fix\\nprint(1)\\n"}',
          },
        },
      ],
    });

    const toolCalls = transcriptStore.entries.filter((e) => e.kind === "tool_call");
    const partialOrphans = toolCalls.filter((e) => String(e.blockId).startsWith("partial-"));
    expect(
      partialOrphans,
      "empty-id chunks must not create orphan partial-* bubbles",
    ).toHaveLength(0);
    expect(toolCalls.map((e) => e.blockId).sort()).toEqual(["call_bash", "call_write"]);

    // finals
    upsertToolCallFromSSE({
      partial: false,
      tool_index: 0,
      tool_calls: [
        {
          id: "call_write",
          type: "function",
          function: {
            name: "write_file",
            arguments:
              '{"path":"test_extract.py","content":"# fix\\nprint(1)\\n"}',
          },
        },
      ],
    });
    upsertToolCallFromSSE({
      partial: false,
      tool_index: 1,
      tool_calls: [
        {
          id: "call_bash",
          type: "function",
          function: {
            name: "bash_run",
            arguments: '{"command":"python test_extract.py"}',
          },
        },
      ],
    });

    applyToolResult({
      tool_call_id: "call_write",
      tool_name: "write_file",
      content: "ok",
      rejected: false,
    });
    applyToolResult({
      tool_call_id: "call_bash",
      tool_name: "bash_run",
      content: "ok",
      rejected: false,
    });

    const stillGenerating = transcriptStore.entries.filter(
      (e) => e.kind === "tool_call" && e.partial,
    );
    expect(stillGenerating, "no stuck 生成中 bubbles after tool_result").toHaveLength(0);
    expect(transcriptStore.entries.filter((e) => e.kind === "tool_result")).toHaveLength(2);
  });

  it("survives resetToolStream mid-stream via final tool_index pruning", () => {
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 0,
      tool_calls: [
        {
          id: "call_write",
          type: "function",
          function: { name: "write_file", arguments: "" },
        },
      ],
    });
    // Simulate page soft-reset clearing index map, then empty-id chunk creates orphan.
    resetToolStream();
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 0,
      tool_calls: [
        {
          id: "",
          type: "function",
          function: {
            name: "write_file",
            arguments: '{"path":"a.py","content":"x"}',
          },
        },
      ],
    });
    expect(transcriptStore.entries.some((e) => e.blockId === "partial-0")).toBe(true);

    // Final with tool_index should prune leftover partial-0.
    upsertToolCallFromSSE({
      partial: false,
      tool_index: 0,
      tool_calls: [
        {
          id: "call_write",
          type: "function",
          function: {
            name: "write_file",
            arguments: '{"path":"a.py","content":"x"}',
          },
        },
      ],
    });
    applyToolResult({
      tool_call_id: "call_write",
      tool_name: "write_file",
      content: "ok",
      rejected: false,
    });

    expect(transcriptStore.entries.some((e) => String(e.blockId).startsWith("partial-"))).toBe(
      false,
    );
    expect(transcriptStore.entries.some((e) => e.kind === "tool_call" && e.partial)).toBe(false);
  });
});
