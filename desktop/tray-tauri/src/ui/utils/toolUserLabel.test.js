import { describe, expect, it } from "vitest";
import {
  resolveToolStepPhase,
  toolStepPurpose,
  toolStepPurposeOrToolLabel,
  toolStepStatusText,
  toolStepToolLabel,
  toolStepToolLabelParts,
  toolStepUserSummary,
} from "./toolUserLabel.js";

describe("toolStepUserSummary", () => {
  it("uses purpose for bash_run", () => {
    const text = toolStepUserSummary({
      callEntry: {
        kind: "tool_call",
        data: { tool_name: "bash_run", arguments: { call_purpose: "查询东莞天气" } },
      },
      resultEntry: { kind: "tool_result", data: { tool_name: "bash_run", content: "ok" } },
    });
    expect(text).toBe("执行命令：查询东莞天气");
  });

  it("never shows bare tool()", () => {
    const text = toolStepUserSummary({
      resultEntry: { kind: "tool_result", data: { content: "raw" } },
    });
    expect(text).not.toMatch(/tool\(\)/);
    expect(text).toBe("助手执行了一步操作");
  });

  it("shows browser_call action", () => {
    const text = toolStepUserSummary({
      callEntry: {
        kind: "tool_call",
        data: { tool_name: "browser_call", arguments: { actions: [{ op: "navigate" }] } },
      },
    });
    expect(text).toBe("浏览器操作：navigate");
  });

  it("shows browser_call result status", () => {
    const text = toolStepUserSummary({
      callEntry: {
        kind: "tool_call",
        data: { tool_name: "browser_call", arguments: { actions: [{ op: "observe" }] } },
      },
      resultEntry: {
        kind: "tool_result",
        data: {
          tool_name: "browser_call",
          content: JSON.stringify({
            ok: true,
            detail: { status: "succeeded" },
          }),
        },
      },
    });
    expect(text).toBe("浏览器操作：已完成");
  });

  it("shows browser_evaluate result status", () => {
    const text = toolStepUserSummary({
      callEntry: {
        kind: "tool_call",
        data: { tool_name: "browser_evaluate", arguments: { script: "document.title" } },
      },
      resultEntry: {
        kind: "tool_result",
        data: {
          tool_name: "browser_evaluate",
          content: JSON.stringify({
            ok: true,
            detail: { status: "succeeded", value: "Example Domain" },
          }),
        },
      },
    });
    expect(text).toBe("执行浏览器脚本：已完成");
  });
});

describe("toolStepPurpose", () => {
  it("returns only call_purpose for the compact tool bubble", () => {
    expect(
      toolStepPurpose({
        callEntry: {
          kind: "tool_call",
          data: { tool_name: "bash_run", arguments: { call_purpose: "查询东莞天气" } },
        },
      }),
    ).toBe("查询东莞天气");
  });

  it("supports temporary agent purpose as a fallback", () => {
    expect(
      toolStepPurpose({
        callEntry: {
          kind: "tool_call",
          data: { tool_name: "create_temporary_agent", arguments: { purpose: "整理接口文档" } },
        },
      }),
    ).toBe("整理接口文档");
  });

  it("keeps call_purpose when a result entry has no arguments", () => {
    expect(toolStepPurpose({
      callEntry: { data: { tool_name: "bash_run", arguments: { call_purpose: "检查服务状态" } } },
      resultEntry: { data: { tool_name: "bash_run", status: "succeeded", content: "ok" } },
    })).toBe("检查服务状态");
  });

  it("ignores a whitespace-only call purpose and uses the result purpose", () => {
    expect(toolStepPurpose({
      callEntry: { data: { tool_name: "bash_run", arguments: { call_purpose: "   " } } },
      resultEntry: { data: { tool_name: "bash_run", arguments: { call_purpose: "检查完成结果" }, status: "succeeded" } },
    })).toBe("检查完成结果");
  });

  it("falls back to a friendly tool name when purpose is absent", () => {
    expect(toolStepPurposeOrToolLabel({
      callEntry: { data: { tool_name: "read_file", arguments: {} } },
    })).toBe("读取文件");
    expect(toolStepPurposeOrToolLabel({
      callEntry: { data: { tool_name: "unknown_tool", arguments: {} } },
    })).toBe("助手执行了一步操作");
  });
});

describe("toolStepToolLabel", () => {
  it("keeps the tool name and purpose in the compact row", () => {
    expect(
      toolStepToolLabel({
        callEntry: {
          kind: "tool_call",
          data: { tool_name: "bash_run", arguments: { call_purpose: "检查服务状态" } },
        },
      }),
    ).toBe("bash(检查服务状态)");
  });

  it("does not synthesize an empty parentheses suffix", () => {
    expect(
      toolStepToolLabel({
        callEntry: { kind: "tool_call", data: { tool_name: "terminal_command", arguments: {} } },
      }),
    ).toBe("terminal_command");
  });

  it("shares normalized name and purpose parts with animated headers", () => {
    expect(toolStepToolLabelParts({
      callEntry: { data: { tool_name: "bash_run", arguments: { call_purpose: "检查服务状态" } } },
    })).toEqual({ name: "bash", purpose: "检查服务状态" });
    expect(toolStepToolLabelParts({
      callEntry: { data: { tool_name: "read_file", arguments: {} } },
    })).toEqual({ name: "read_file", purpose: "" });
  });
});

describe("toolStepStatusText", () => {
  it("uses the structured status for a cancelled bash result", () => {
    expect(
      toolStepStatusText({
        resultEntry: {
          kind: "tool_result",
          data: { status: "cancelled", content: "命令已被用户终止。" },
        },
      }),
    ).toBe("已终止");
  });

  it("uses event status before inspecting result text", () => {
    const result = {
      kind: "tool_result",
      data: { tool_name: "terminal_command", status: "failed", content: "" },
    };
    expect(resolveToolStepPhase({ resultEntry: result })).toBe("failed");
    expect(toolStepStatusText({ resultEntry: result })).toBe("执行失败");
  });

  it("shows terminated for a tool result created by stream cancellation", () => {
    const result = {
      kind: "tool_result",
      data: { tool_name: "bash_run", status: "cancelled", content: "流式输出被用户中断。" },
    };
    expect(resolveToolStepPhase({ resultEntry: result })).toBe("cancelled");
    expect(toolStepStatusText({ resultEntry: result })).toBe("已终止");
  });

  it("shows terminated for a tool result created by turn cancellation", () => {
    const result = {
      kind: "tool_result",
      data: { tool_name: "bash_run", status: "cancelled", content: "用户需要补充信息，打断了工具执行。" },
    };
    expect(resolveToolStepPhase({ resultEntry: result })).toBe("cancelled");
    expect(toolStepStatusText({ resultEntry: result })).toBe("已终止");
  });

  it("shows running for parallel tool_call without result", () => {
    const call = { kind: "tool_call", data: { tool_name: "read_file", tool_call_id: "c2" } };
    expect(resolveToolStepPhase({ callEntry: call, resultEntry: null })).toBe("running");
    expect(toolStepStatusText({ callEntry: call, resultEntry: null })).toBe("执行中");
  });

  it("treats a content-less partial tool call as generating", () => {
    const call = {
      kind: "tool_call",
      partial: true,
      data: { tool_name: "bash_run", tool_call_id: "c-partial-empty" },
    };
    expect(resolveToolStepPhase({ callEntry: call, resultEntry: null })).toBe("generating");
    expect(toolStepStatusText({ callEntry: call, resultEntry: null })).toBe("生成中");
  });

  it("shows pending only when executionHint marks HITL-gated call", () => {
    const call = { kind: "tool_call", data: { tool_name: "bash_run", tool_call_id: "c-hitl" } };
    expect(resolveToolStepPhase({ callEntry: call, resultEntry: null, executionHint: "pending" })).toBe("pending");
    expect(toolStepStatusText({ callEntry: call, resultEntry: null, executionHint: "pending" })).toBe(
      "待执行",
    );
  });

  it("shows running from the authoritative lifecycle hint", () => {
    const call = { kind: "tool_call", data: { tool_name: "bash_run", tool_call_id: "c3" } };
    expect(
      toolStepStatusText({ callEntry: call, resultEntry: null, executionHint: "active" }),
    ).toBe("执行中");

    expect(resolveToolStepPhase({ callEntry: call, resultEntry: null, executionHint: "active" })).toBe("running");
    expect(toolStepStatusText({ callEntry: call, resultEntry: null, executionHint: "active" })).toBe("执行中");
  });

  it("shows background for an explicitly queued tool result", () => {
    const result = {
      kind: "tool_result",
      data: { tool_call_id: "c9", tool_name: "browser_call", status: "queued" },
    };
    expect(toolStepStatusText({ resultEntry: result })).toBe("后台执行中");
  });

  it("lets an explicit failed bash result win over a lagging running snapshot and active hint", () => {
    const callEntry = {
      kind: "tool_call",
      blockId: "bash-last",
      data: { tool_name: "bash_run", tool_call_id: "bash-last" },
    };
    const resultEntry = {
      kind: "tool_result",
      blockId: "bash-last",
      data: { tool_name: "bash_run", tool_call_id: "bash-last", status: "failed", exit_code: 1 },
    };
    const step = { callEntry, resultEntry, executionHint: "active" };
    expect(resolveToolStepPhase(step)).toBe("failed");
  });

  it("prefers an existing failed or queued result over a stale settled hint", () => {
    const callEntry = { kind: "tool_call", data: { tool_name: "bash_run", tool_call_id: "bash-result" } };
    const failedResult = { kind: "tool_result", data: { tool_name: "bash_run", tool_call_id: "bash-result", status: "failed" } };
    const queuedResult = { kind: "tool_result", data: { tool_name: "bash_run", tool_call_id: "bash-result", status: "queued" } };
    expect(resolveToolStepPhase({ callEntry, resultEntry: failedResult, executionHint: "settled" })).toBe("failed");
    expect(resolveToolStepPhase({ callEntry, resultEntry: queuedResult, executionHint: "settled" })).toBe("background");
  });

  it.each(["running", "queued"])("projects a nonterminal bash result to its active phase (%s)", (status) => {
    const callEntry = { kind: "tool_call", data: { tool_name: "bash_run", tool_call_id: "bash-live" } };
    const resultEntry = { kind: "tool_result", data: { tool_name: "bash_run", tool_call_id: "bash-live", status } };
    expect(resolveToolStepPhase({ callEntry, resultEntry, executionHint: "active" })).toBe(status === "queued" ? "background" : "running");
  });

  it("prefers the authoritative active lifecycle over a partial tool call", () => {
    const call = {
      kind: "tool_call",
      partial: true,
      data: { tool_name: "bash_run", tool_call_id: "c-partial-running" },
    };
    expect(resolveToolStepPhase({ callEntry: call, executionHint: "active" })).toBe("running");
    expect(toolStepStatusText({ callEntry: call, executionHint: "active" })).toBe("执行中");
  });

});
