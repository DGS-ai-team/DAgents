import { describe, expect, it } from "vitest";
import {
  USER_INFORMATION_TOOL,
  approvalItemDisplayName,
  approvalItemHintVisible,
  approvalItemReason,
  approvalItemToolLabel,
  extractToolCallsFromEvent,
  formatApprovalRawArguments,
  normalizeToolCallItem,
  parseToolArguments,
  resolveToolArgumentsFromData,
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

  it("formats generic tools with sorted args", () => {
    expect(toolDisplayName("glob_files", { pattern: "**/*.go", root: "/src" })).toBe(
      'glob_files(pattern="**/*.go", root="/src")',
    );
  });
});

describe("resolveToolArgumentsFromData", () => {
  it("reads nested function.arguments", () => {
    expect(
      resolveToolArgumentsFromData({
        tool_name: "write_file",
        function: { arguments: '{"path":"/tmp/a"}' },
      }),
    ).toEqual({ path: "/tmp/a" });
  });

  it("falls back to raw_arguments when arguments map is empty", () => {
    expect(
      resolveToolArgumentsFromData({
        tool_name: "read_file",
        arguments: {},
        raw_arguments: '{"path":"/tmp/a.txt"}',
      }),
    ).toEqual({ path: "/tmp/a.txt" });
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

describe("approvalItemHintVisible", () => {
  it("hides hint when reason already contains the command", () => {
    expect(
      approvalItemHintVisible({
        name: "bash_run",
        arguments: { command: "rm -rf /tmp/x" },
        reason: "策略要求审批：rm -rf /tmp/x",
      }),
    ).toBe(false);
  });

  it("shows hint when reason has no overlapping detail", () => {
    expect(
      approvalItemHintVisible({
        name: "bash_run",
        arguments: { command: "ls" },
        reason: "高风险 shell 需确认",
      }),
    ).toBe(true);
  });
});

describe("approval card presentation", () => {
  it("keeps tool title free of arguments and removes duplicated built-in detail", () => {
    const item = {
      name: "bash_run",
      arguments: { command: "rm -rf /tmp/x" },
      reason: "将执行 Shell 命令: rm -rf /tmp/x",
    };
    expect(approvalItemToolLabel(item)).toBe("bash");
    expect(approvalItemReason(item)).toBe("将执行 Shell 命令");
  });

  it("preserves custom approval explanations while removing only the repeated value", () => {
    expect(
      approvalItemReason({
        name: "write_file",
        arguments: { path: "/tmp/a.txt" },
        reason: "策略要求审批：/tmp/a.txt",
      }),
    ).toBe("策略要求审批");
    expect(
      approvalItemReason({
        name: "mcp__server__tool",
        arguments: { target: "production" },
        reason: "需要确认 production 的变更",
      }),
    ).toBe("需要确认 production 的变更");
  });

  it("formats raw approval arguments as optional readable JSON", () => {
    expect(formatApprovalRawArguments('{"command":"echo hi","timeout":3}')).toBe(
      '{\n  "command": "echo hi",\n  "timeout": 3\n}',
    );
    expect(formatApprovalRawArguments("{partial")).toBe("{partial");
    expect(formatApprovalRawArguments("", { path: "/tmp/a.txt" })).toContain('"path": "/tmp/a.txt"');
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
