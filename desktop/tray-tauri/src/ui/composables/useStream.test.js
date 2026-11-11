import { beforeEach, describe, expect, it } from "vitest";
import { buildStream } from "./useStream.js";
import { resolveToolStepPhase, toolStepIsActiveInGroup } from "../utils/toolUserLabel.js";
import { applyTurnState, resetTurnState } from "../stores/turnState.js";

function entry(id, kind, extra = {}) {
  return { id, kind, ...extra };
}

beforeEach(() => resetTurnState());

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

  it("does not keep the final failed bash step active when a prior active hint lingers", () => {
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

    // Simulate a cached execution hint racing with the terminal result.
    steps[0].executionHint = "active";
    expect(toolStepIsActiveInGroup(steps[0])).toBe(false);
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
    expect(items[0].executionHint).toBeUndefined();
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
    ]);

    expect(step.kind).toBe("tool_step");
    expect(step.executionHint).toBe("active");
    expect(toolStepIsActiveInGroup(step)).toBe(true);
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
    ]);

    expect(step.resultEntry.data.status).toBe("succeeded");
    expect(step.lifecycleStatus).toBe("cancelled");
    expect(resolveToolStepPhase(step)).toBe("cancelled");
  });
});
