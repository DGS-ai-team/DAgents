import { describe, expect, it } from "vitest";
import {
  USER_INFORMATION_TOOL,
  approvalItemDisplayName,
  extractToolCallsFromEvent,
  normalizeToolCallItem,
  parseToolArguments,
  toolDisplayName,
  toolCallParts,
} from "./toolCalls.js";

describe("parseToolArguments", () => {
  it("parses JSON string", () => {
    expect(parseToolArguments('{"command":"echo hi"}')).toEqual({ command: "echo hi" });
  });

  it("returns object as-is", () => {
    expect(parseToolArguments({ path: "/tmp/a" })).toEqual({ path: "/tmp/a" });
  });

  it("returns empty object on invalid JSON", () => {
    expect(parseToolArguments("{bad")).toEqual({});
  });
});

describe("toolDisplayName", () => {
  it("maps ask_user_information to Agent 询问", () => {
    expect(toolDisplayName(USER_INFORMATION_TOOL, {})).toBe("Agent 询问");
  });

  it("shows call_purpose for bash_run", () => {
    expect(toolDisplayName("bash_run", { call_purpose: "list files", command: "ls" })).toBe(
      "bash(list files)",
    );
  });

  it("truncates long bash command", () => {
    const cmd = "x".repeat(60);
    expect(toolDisplayName("bash_run", { command: cmd })).toBe(`bash(${"x".repeat(47)}…)`);
  });

  it("formats create_temporary_agent with purpose", () => {
    expect(toolDisplayName("create_temporary_agent", { purpose: "research", wait: true })).toBe(
      "创建临时 Agent · research (wait)",
    );
  });

  it("formats write_file with path", () => {
    expect(toolDisplayName("write_file", { path: "/tmp/out.txt" })).toBe("write_file(/tmp/out.txt)");
  });
});

describe("approvalItemDisplayName", () => {
  it("uses parsed arguments from rawArgs", () => {
    const name = approvalItemDisplayName({
      name: "bash_run",
      rawArgs: '{"call_purpose":"deploy"}',
    });
    expect(name).toBe("bash(deploy)");
  });
});

describe("extractToolCallsFromEvent", () => {
  it("normalizes tool_calls array", () => {
    const calls = extractToolCallsFromEvent({
      tool_calls: [{ id: "1", function: { name: "read_file", arguments: '{"path":"a"}' } }],
    });
    expect(calls).toHaveLength(1);
    expect(calls[0].name).toBe("read_file");
    expect(calls[0].arguments).toEqual({ path: "a" });
  });
});

describe("normalizeToolCallItem", () => {
  it("reads function.name and function.arguments", () => {
    const item = normalizeToolCallItem({
      id: "tc1",
      function: { name: "trigger_create", arguments: '{"name":"daily"}' },
    });
    expect(item.name).toBe("trigger_create");
    expect(item.arguments).toEqual({ name: "daily" });
  });
});

describe("toolCallParts", () => {
  it("returns bash command preview", () => {
    const parts = toolCallParts("bash_run", { command: "echo ok" });
    expect(parts.summary).toBe("bash(echo ok)");
    expect(parts.codePreview).toBe("echo ok");
  });
});
