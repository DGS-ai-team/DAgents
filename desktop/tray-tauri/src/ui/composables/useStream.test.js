import { beforeEach, describe, expect, it } from "vitest";
import { buildStream } from "./useStream.js";
import { resolveToolStepPhase } from "../utils/toolUserLabel.js";
import { createTurnStateStore } from "../stores/turnState.js";

function entry(id, kind, extra = {}) {
  return { id, kind, ...extra };
}

let turnLifecycle;
const applyTurnState = (...args) => turnLifecycle.applyTurnState(...args);

beforeEach(() => {
  turnLifecycle = createTurnStateStore();
});

describe("buildStream", () => {
  it("keeps a file-only user entry visible", () => {
    const items = buildStream([
      {
        id: 1,
        kind: "user",
        text: "",
        file_refs: [{ path: "D:\\work\\README.md", name: "README.md" }],
      },
    ]);
    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe("user");
  });

  it("keeps real user text that resembles an injected date notice", () => {
    const text = "当天日期为：我想了解这个缩写的来历";
    const items = buildStream([
      entry("user-prefix", "user", { text }),
    ]);

    expect(items).toHaveLength(1);
    expect(items[0].entry.text).toBe(text);
  });

  it("pairs parallel tool calls and results without rescanning the tail", () => {
    const items = buildStream([
      entry(1, "assistant", { text: "准备执行" }),
      entry(2, "tool_call", { blockId: "call-a", data: { arguments: "{}" } }),
      entry(3, "tool_call", { blockId: "call-b", data: { arguments: "{}" } }),
      entry(4, "tool_result", { blockId: "call-a", data: { content: "a" } }),
      entry(5, "tool_result", { blockId: "call-b", data: { content: "b" } }),
      entry(6, "assistant", { text: "完成" }),
    ]);

    expect(items.filter((item) => item.kind === "tool_step")).toHaveLength(2);
    expect(items.filter((item) => item.kind === "tool_step").every((item) => item.resultEntry)).toBe(true);
  });

  it("pairs repeated provider call IDs by the assistant record identity", () => {
    const items = buildStream([
      entry("call-record-1", "tool_call", {
        item_id: "tool-call-record-1",
        blockId: "reused-call",
        data: { tool_call_id: "reused-call", tool_name: "read_file" },
      }),
      entry("result-record-1", "tool_result", {
        blockId: "reused-call",
        data: { tool_call_id: "reused-call", tool_call_item_id: "tool-call-record-1", content: "first result" },
      }),
      entry("call-record-2", "tool_call", {
        item_id: "tool-call-record-2",
        blockId: "reused-call",
        data: { tool_call_id: "reused-call", tool_name: "read_file" },
      }),
      entry("result-record-2", "tool_result", {
        blockId: "reused-call",
        data: { tool_call_id: "reused-call", tool_call_item_id: "tool-call-record-2", content: "second result" },
      }),
    ]).filter((item) => item.kind === "tool_step");

    expect(items).toHaveLength(2);
    expect(items.map(item => item.key)).toEqual(["tool-step-tool-call-record-1", "tool-step-tool-call-record-2"]);
    expect(items.map(item => item.resultEntry.data.content)).toEqual(["first result", "second result"]);
  });

  it("keeps assistant and parallel tool rows in transcript order when results finish out of order", () => {
    const items = buildStream([
      entry("assistant-before", "assistant", { text: "开始检查。" }),
      entry("call-a", "tool_call", {
        blockId: "call-a",
        data: { tool_call_id: "call-a", tool_name: "read_file", arguments: '{"path":"a.txt"}' },
      }),
      entry("call-b", "tool_call", {
        blockId: "call-b",
        data: { tool_call_id: "call-b", tool_name: "read_file", arguments: '{"path":"b.txt"}' },
      }),
      // B finishes first, but the rendered tool rows stay in call order.
      entry("result-b", "tool_result", {
        blockId: "call-b",
        data: { tool_call_id: "call-b", status: "succeeded", content: "B done" },
      }),
      entry("result-a", "tool_result", {
        blockId: "call-a",
        data: { tool_call_id: "call-a", status: "succeeded", content: "A done" },
      }),
      entry("assistant-after", "assistant", { text: "检查完成。" }),
    ]);

    expect(items.map((item) => item.kind)).toEqual(["assistant", "tool_step", "tool_step", "assistant"]);
    expect(items[0].entry.id).toBe("assistant-before");
    expect(items.slice(1, 3).map((item) => item.callEntry.data.tool_call_id)).toEqual(["call-a", "call-b"]);
    expect(items.slice(1, 3).map((item) => item.resultEntry.data.content)).toEqual(["A done", "B done"]);
    expect(items[3].entry.id).toBe("assistant-after");
  });

  it("keeps a tool row key stable when its result arrives", () => {
    const call = entry(2, "tool_call", {
      blockId: "call-stable",
      data: { tool_call_id: "call-stable", tool_name: "read_file", arguments: "{}" },
    });
    const running = buildStream([call]).find((item) => item.kind === "tool_step");
    const settled = buildStream([
      call,
      entry(3, "tool_result", {
        blockId: "call-stable",
        data: { tool_call_id: "call-stable", tool_name: "read_file", status: "succeeded" },
      }),
    ]).find((item) => item.kind === "tool_step");

    expect(settled.key).toBe(running.key);
    expect(settled.resultEntry.data.status).toBe("succeeded");
  });

  it("projects a final failed bash step to one canonical phase", () => {
    const steps = buildStream([
      entry(1, "tool_call", {
        blockId: "bash-final",
        data: { tool_name: "bash_run", tool_call_id: "bash-final", arguments: '{"command":"exit 1"}' },
      }),
      entry(2, "tool_result", {
        blockId: "bash-final",
        data: { tool_name: "bash_run", tool_call_id: "bash-final", status: "failed", exit_code: 1 },
      }),
    ]);
    expect(steps).toHaveLength(1);
    expect(steps[0].resultEntry.data.status).toBe("failed");

    // buildStream projects the final result directly into one canonical phase.
    expect(steps[0].phase).toBe("failed");
    expect(steps[0]).not.toHaveProperty("executionHint");
    expect(resolveToolStepPhase(steps[0])).toBe("failed");
  });

  it("keys replacement HITL entries by request id", () => {
    const first = buildStream([
      entry(1, "assistant", { text: "等待确认" }),
    ], [{ kind: "approval", data: { request_id: "request-a" } }]);
    const second = buildStream([
      entry(1, "assistant", { text: "等待确认" }),
    ], [{ kind: "approval", data: { request_id: "request-b" } }]);

    expect(first.at(-1).key).not.toBe(second.at(-1).key);
  });

  it("keeps a content-less partial tool call visible as generating", () => {
    const items = buildStream([
      entry(1, "tool_call", {
        blockId: "mimo-call",
        partial: true,
        data: {
          tool_call_id: "mimo-call",
          tool_name: "bash_run",
          arguments: "",
        },
      }),
    ]);

    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe("tool_step");
    expect(items[0].callEntry.partial).toBe(true);
    expect(items[0].phase).toBe("generating");
  });

  it("restores a running tool from the hydrated execution projection", () => {
    applyTurnState({
      authority: "turn_coordinator",
      phase: "tool_executing",
      tool_executions: [{ tool_call_id: "call-hydrated", status: "running" }],
    }, { source: "hydrate" });

    const [step] = buildStream([
      entry(1, "tool_call", {
        blockId: "call-hydrated",
        data: { tool_call_id: "call-hydrated", tool_name: "bash_run", arguments: '{"command":"sleep 1"}' },
      }),
    ], [], turnLifecycle.state);

    expect(step.kind).toBe("tool_step");
    expect(step.phase).toBe("running");
    expect(resolveToolStepPhase(step)).toBe("running");
  });

  it("uses the explicitly supplied conversation state for tool execution status", () => {
    const authority = {
      authority: "turn_coordinator",
      toolExecutions: [{ toolCallId: "call-owned", status: "running" }],
    };

    const [step] = buildStream([
      entry(1, "tool_call", {
        blockId: "call-owned",
        data: { tool_call_id: "call-owned", tool_name: "bash_run", arguments: '{"command":"sleep 1"}' },
      }),
    ], [], authority);

    expect(step.kind).toBe("tool_step");
    expect(step.phase).toBe("running");
  });

  it("uses cancelled coordinator status when a persisted tool result says succeeded", () => {
    applyTurnState({
      authority: "turn_coordinator",
      phase: "cancelled",
      turn_id: "turn-cancelled",
      generation: 1,
      lifecycle_seq: 8,
      terminal: true,
      tool_executions: [{ tool_call_id: "write-cancelled", tool_name: "write_file", status: "cancelled" }],
    }, { source: "hydrate" });

    const [step] = buildStream([
      entry(1, "tool_call", {
        blockId: "write-cancelled",
        data: { tool_call_id: "write-cancelled", tool_name: "write_file", arguments: '{"path":"native-approval-cancel.md"}' },
      }),
      entry(2, "tool_result", {
        blockId: "write-cancelled",
        data: { tool_call_id: "write-cancelled", tool_name: "write_file", status: "succeeded", content: "工具调用已被用户取消。" },
      }),
    ], [], turnLifecycle.state);

    expect(step.resultEntry.data.status).toBe("succeeded");
    expect(step.phase).toBe("cancelled");
    expect(resolveToolStepPhase(step)).toBe("cancelled");
  });
});
