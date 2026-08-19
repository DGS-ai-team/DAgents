import { describe, expect, it, beforeEach } from "vitest";
import {
  resolveToolStepPhase,
  toolStepIsInProgress,
  toolStepIsPending,
  toolStepPurpose,
  toolStepStatusText,
  toolStepUserSummary,
} from "./toolUserLabel.js";
import { applyToolJobsSnapshot } from "../stores/toolJobs.js";

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

  it("shows browser_run_task goal", () => {
    const text = toolStepUserSummary({
      callEntry: {
        kind: "tool_call",
        data: { tool_name: "browser_run_task", arguments: { task: "打开 example.com 提取标题" } },
      },
    });
    expect(text).toBe("浏览器任务：打开 example.com 提取标题");
  });

  it("keeps browser_run_task goal and short status on result (summary left to citation)", () => {
    const text = toolStepUserSummary({
      callEntry: {
        kind: "tool_call",
        data: { tool_name: "browser_run_task", arguments: { task: "打开 example.com 提取标题" } },
      },
      resultEntry: {
        kind: "tool_result",
        data: {
          tool_name: "browser_run_task",
          content: JSON.stringify({
            ok: true,
            detail: { status: "completed", summary: "标题很长很长很长很长", success: true, steps: 4 },
          }),
        },
      },
    });
    expect(text).toBe("浏览器任务：打开 example.com 提取标题 · 已完成 · 4 步");
  });

  it("shows browser_task_status summary from result JSON", () => {
    const text = toolStepUserSummary({
      callEntry: {
        kind: "tool_call",
        data: { tool_name: "browser_task_status", arguments: { task_id: "btask-1" } },
      },
      resultEntry: {
        kind: "tool_result",
        data: {
          tool_name: "browser_task_status",
          content: JSON.stringify({
            ok: true,
            detail: { status: "completed", summary: "标题是 Example Domain", success: true },
          }),
        },
      },
    });
    expect(text).toBe("查询浏览器任务：标题是 Example Domain");
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
});

describe("toolStepStatusText", () => {
  beforeEach(() => {
    applyToolJobsSnapshot({ running: 0, background: 0, running_call_ids: [], background_call_ids: [] });
  });

  it("shows terminated for cancelled bash result", () => {
    expect(
      toolStepStatusText({
        resultEntry: {
          kind: "tool_result",
          data: { content: "[BASH_RESULT] status=CANCELLED\nshell_type=bash\n命令已被用户终止。" },
        },
      }),
    ).toBe("已终止");
  });

  it("shows interrupted for a tool result created by stream cancellation", () => {
    const result = {
      kind: "tool_result",
      data: { tool_name: "bash_run", content: "流式输出被用户中断。" },
    };
    expect(resolveToolStepPhase({ resultEntry: result })).toBe("interrupted");
    expect(toolStepStatusText({ resultEntry: result })).toBe("已中断");
  });

  it("shows running for parallel tool_call without result", () => {
    const call = { kind: "tool_call", data: { tool_name: "read_file", tool_call_id: "c2" } };
    expect(toolStepStatusText({ callEntry: call, resultEntry: null })).toBe("执行中");
    expect(toolStepIsPending({ callEntry: call, resultEntry: null })).toBe(false);
    expect(toolStepIsInProgress({ callEntry: call, resultEntry: null })).toBe(true);
  });

  it("treats a content-less partial tool call as generating", () => {
    const call = {
      kind: "tool_call",
      partial: true,
      data: { tool_name: "bash_run", tool_call_id: "c-partial-empty" },
    };
    expect(resolveToolStepPhase({ callEntry: call, resultEntry: null })).toBe("generating");
    expect(toolStepStatusText({ callEntry: call, resultEntry: null })).toBe("生成中");
    expect(toolStepIsInProgress({ callEntry: call, resultEntry: null })).toBe(true);
  });

  it("shows pending only when executionHint marks HITL-gated call", () => {
    const call = { kind: "tool_call", data: { tool_name: "bash_run", tool_call_id: "c-hitl" } };
    expect(toolStepStatusText({ callEntry: call, resultEntry: null, executionHint: "pending" })).toBe(
      "待执行",
    );
    expect(toolStepIsPending({ callEntry: call, resultEntry: null, executionHint: "pending" })).toBe(
      true,
    );
    expect(toolStepIsInProgress({ callEntry: call, resultEntry: null, executionHint: "pending" })).toBe(
      false,
    );
  });

  it("shows running when executionHint is active or call is in running list", () => {
    const call = { kind: "tool_call", data: { tool_name: "bash_run", tool_call_id: "c3" } };
    expect(
      toolStepStatusText({ callEntry: call, resultEntry: null, executionHint: "active" }),
    ).toBe("执行中");
    expect(toolStepIsInProgress({ callEntry: call, resultEntry: null, executionHint: "active" })).toBe(
      true,
    );

    applyToolJobsSnapshot({ running: 1, running_call_ids: ["c3"] });
    expect(resolveToolStepPhase({ callEntry: call, resultEntry: null })).toBe("running");
    expect(toolStepStatusText({ callEntry: call, resultEntry: null })).toBe("执行中");
  });

  it("shows background for running bash result still tracked", () => {
    const result = {
      kind: "tool_result",
      data: { tool_call_id: "c9", content: "[BASH_RESULT] status=RUNNING job_id=j1" },
    };
    applyToolJobsSnapshot({ background: 1, background_call_ids: ["c9"] });
    expect(toolStepStatusText({ resultEntry: result })).toBe("后台执行中");
  });

  it("prefers a real running job over a stale partial tool call", () => {
    const call = {
      kind: "tool_call",
      partial: true,
      data: { tool_name: "bash_run", tool_call_id: "c-partial-running" },
    };
    applyToolJobsSnapshot({ running: 1, running_call_ids: ["c-partial-running"] });
    expect(resolveToolStepPhase({ callEntry: call })).toBe("running");
    expect(toolStepStatusText({ callEntry: call })).toBe("执行中");
  });

  it("shows background execution for a partial call already detached", () => {
    const call = {
      kind: "tool_call",
      partial: true,
      data: { tool_name: "bash_run", tool_call_id: "c-partial-background" },
    };
    applyToolJobsSnapshot({ background: 1, background_call_ids: ["c-partial-background"] });
    expect(resolveToolStepPhase({ callEntry: call })).toBe("background");
    expect(toolStepStatusText({ callEntry: call })).toBe("后台执行中");
  });
});
