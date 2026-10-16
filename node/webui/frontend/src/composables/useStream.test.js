import { describe, expect, it } from "vitest";
import { buildStream } from "./useStream.js";
import { toolStepIsActiveInGroup } from "../utils/toolUserLabel.js";

function entry(id, kind, extra = {}) {
  return { id, kind, ...extra };
}

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
});
