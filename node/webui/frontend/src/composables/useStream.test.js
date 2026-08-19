import { describe, expect, it } from "vitest";
import { buildStream } from "./useStream.js";

function entry(id, kind, extra = {}) {
  return { id, kind, ...extra };
}

describe("buildStream", () => {
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
