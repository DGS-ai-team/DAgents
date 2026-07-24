import { describe, expect, it } from "vitest";
import { toolStepStatusText, toolStepUserSummary } from "./toolUserLabel.js";

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

  it("shows background for running bash result", () => {
    expect(
      toolStepStatusText({
        resultEntry: {
          kind: "tool_result",
          data: { content: "[BASH_RESULT] status=RUNNING job_id=j1" },
        },
      }),
    ).toBe("后台执行中");
  });
});
