import { describe, expect, it } from "vitest";
import { buildStream } from "../composables/useStream.js";
import {
  formatChildLifecycle,
  formatCompressionDetail,
  formatCompressionStart,
} from "./activityFormat.js";

describe("activityFormat", () => {
  it("formatChildLifecycle uses Chinese", () => {
    expect(formatChildLifecycle("temporary_agent_created", { purpose: "调研", child_session_id: "abc" })).toContain(
      "临时 Agent 已创建",
    );
  });

  it("formatCompressionDetail applied", () => {
    const line = formatCompressionDetail("blocking", { status: "applied", compressed_message_count: 3 });
    expect(line).toContain("上下文已压缩");
    expect(line).toContain("3");
  });

  it("formatCompressionStart", () => {
    expect(formatCompressionStart("silent", { compressed_message_count: 5 })).toContain("正在压缩上下文");
  });
});

describe("buildStream activity", () => {
  it("excludes system entries from main stream", () => {
    const items = buildStream(
      [
        { id: 1, kind: "user", text: "hi" },
        { id: 2, kind: "system", text: "上下文已压缩" },
      ],
      [],
    );
    expect(items.some((item) => item.kind === "system")).toBe(false);
    expect(items).toHaveLength(1);
  });

  it("merges tool_call and tool_result across skippable entries", () => {
    const items = buildStream(
      [
        {
          id: 1,
          kind: "tool_call",
          blockId: "call-1",
          data: { tool_name: "create_temporary_agent", arguments: { call_purpose: "子Agent1" } },
        },
        { id: 2, kind: "system", text: "临时 Agent 已创建" },
        { id: 3, kind: "reasoning", text: "" },
        {
          id: 4,
          kind: "tool_result",
          blockId: "call-1",
          data: { tool_name: "create_temporary_agent", content: "ok" },
        },
      ],
      [],
    );
    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe("tool_step");
    expect(items[0].callEntry?.blockId).toBe("call-1");
    expect(items[0].resultEntry?.blockId).toBe("call-1");
  });

  it("merges when blockId only exists on data.tool_call_id", () => {
    const items = buildStream(
      [
        { id: 1, kind: "tool_call", data: { tool_call_id: "call-x", tool_name: "read_file" } },
        { id: 2, kind: "tool_result", data: { tool_call_id: "call-x", tool_name: "read_file", content: "x" } },
      ],
      [],
    );
    expect(items).toHaveLength(1);
    expect(items[0].resultEntry).toBeTruthy();
  });

  it("merges parallel tool calls when results are interleaved", () => {
    const items = buildStream(
      [
        { id: 1, kind: "tool_call", blockId: "a", data: { tool_name: "glob_files" } },
        { id: 2, kind: "tool_call", blockId: "b", data: { tool_name: "read_file" } },
        { id: 3, kind: "tool_result", blockId: "a", data: { tool_name: "glob_files", content: "ok" } },
        { id: 4, kind: "tool_result", blockId: "b", data: { tool_name: "read_file", content: "ok" } },
      ],
      [],
    );
    expect(items).toHaveLength(2);
    expect(items.every((item) => item.kind === "tool_step" && item.resultEntry)).toBe(true);
    expect(items[0].callEntry.blockId).toBe("a");
    expect(items[1].callEntry.blockId).toBe("b");
  });

  it("skips date and async_tool user messages", () => {
    const items = buildStream(
      [
        { id: 1, kind: "user", text: "当天日期为：20260720" },
        { id: 2, kind: "user", name: "async_tool", text: "后台任务结果" },
        { id: 3, kind: "user", text: "真实问题" },
      ],
      [],
    );
    expect(items).toHaveLength(1);
    expect(items[0].entry.text).toBe("真实问题");
  });
});
