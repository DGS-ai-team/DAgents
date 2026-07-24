import { describe, expect, it, beforeEach } from "vitest";
import {
  resolveToolStepPhase,
  toolStepIsInProgress,
  toolStepIsPending,
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

  it("backfills from result tool_name", () => {
    const text = toolStepUserSummary({
      resultEntry: {
        kind: "tool_result",
        data: { tool_name: "read_file", arguments: { path: "report.txt" }, content: "..." },
      },
    });
    expect(text).toBe("读取文件：report.txt");
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

  it("shows pending for queued tool_call without result", () => {
    const call = { kind: "tool_call", data: { tool_name: "read_file", tool_call_id: "c2" } };
    expect(toolStepStatusText({ callEntry: call, resultEntry: null })).toBe("待执行");
    expect(toolStepIsPending({ callEntry: call, resultEntry: null })).toBe(true);
    expect(toolStepIsInProgress({ callEntry: call, resultEntry: null })).toBe(false);
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
});
