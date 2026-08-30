import { describe, expect, it } from "vitest";
import { buildToolCardModel } from "./toolResultPresentation.js";

function entry(kind, data) {
  return { kind, data };
}

describe("buildToolCardModel", () => {
  it("keeps bash input and presents structured shell result fields", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "bash_run",
        arguments: { command: "npm test", cwd: "D:/repo", timeout_seconds: 30 },
      }),
      resultEntry: entry("tool_result", {
        tool_name: "bash_run",
        duration_seconds: 1.2,
        content: [
          "[BASH_RESULT] exit=0",
          "status=SUCCEEDED",
          "target=local",
          "exit_code=0",
          "stdout_bytes=8",
          "stderr_bytes=0",
          "output_truncated: false",
          "--- STDOUT ---",
          "all good",
          "--- STDERR ---",
        ].join("\n"),
      }),
    });

    expect(model.inputFields).toEqual(
      [{ label: "命令", value: "npm test", kind: "code" }],
    );
    expect(model.resultFields).toEqual([]);
    expect(model.resultBlocks).toEqual([
      expect.objectContaining({ label: "stdout", content: "all good" }),
    ]);
    expect(model.resultBlocks.some((item) => item.content.includes("BASH_RESULT"))).toBe(false);
  });

  it("formats trigger frequency, gate command, task template and next fire time", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "trigger_create",
        arguments: {
          name: "每日检查",
          condition: { interval_seconds: 3600, cmd: "git diff --quiet" },
          task_template: "请检查项目状态",
        },
      }),
      resultEntry: entry("tool_result", {
        tool_name: "trigger_create",
        content: JSON.stringify({
          ok: true,
          trigger: {
            trigger_id: "trigger-123456789",
            name: "每日检查",
            condition: { interval_seconds: 3600, cmd: "git diff --quiet" },
            task_template: "请检查项目状态",
            enabled: true,
            next_fire_at: 1700000000,
          },
        }),
      }),
    });

    expect(model.inputFields).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ label: "触发频率", value: "每 1 小时" }),
        expect.objectContaining({ label: "触发前门控命令", value: "git diff --quiet", kind: "code" }),
        expect.objectContaining({ label: "任务模板", value: "请检查项目状态", kind: "multiline" }),
      ]),
    );
    expect(model.resultFields).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ label: "触发器 ID", value: "trigger-123456789" }),
        expect.objectContaining({ label: "状态", value: "已启用" }),
        expect.objectContaining({ label: "下次执行", value: expect.stringMatching(/^\d{4}-\d{2}-\d{2}/) }),
      ]),
    );
    expect(model.resultBlocks).toEqual([]);
    expect(model.resultBlocks.some((item) => item.content.includes('"trigger_id"'))).toBe(false);
  });

  it("turns trigger list JSON into compact readable entries", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "trigger_list",
        arguments: { include_disabled: false },
      }),
      resultEntry: entry("tool_result", {
        tool_name: "trigger_list",
        content: JSON.stringify({
          ok: true,
          triggers: [
            {
              name: "工作日提醒",
              condition: { schedule: { kind: "daily", hour: 9, minute: 5 } },
              enabled: true,
              next_fire_at: 1700000000,
            },
          ],
        }),
      }),
    });

    expect(model.resultFields).toContainEqual({ label: "任务数量", value: "1 个", kind: "text" });
    expect(model.resultBlocks).toContainEqual(
      expect.objectContaining({ label: "工作日提醒", content: expect.stringContaining("每天 09:05") }),
    );
    expect(model.resultBlocks[0].content).not.toContain("{");
  });

  it("extracts terminal JSON output without rendering the JSON envelope", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "terminal_command",
        arguments: { terminal_id: "term-1", command: "pwd", timeout: 5000 },
      }),
      resultEntry: entry("tool_result", {
        tool_name: "terminal_command",
        content: JSON.stringify({
          status: "succeeded",
          exit_code: 0,
          stdout: "/workspace\n",
          stderr: "",
          output_truncated: false,
        }),
      }),
    });

    expect(model.resultFields).toEqual([]);
    expect(model.resultBlocks).toContainEqual(expect.objectContaining({ label: "stdout", content: "/workspace" }));
    expect(model.resultBlocks.some((item) => item.content.includes('"stdout"'))).toBe(false);
  });

  it("shows only essential read_file metadata while leaving body to the file preview", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "read_file",
        arguments: { path: "README.md", line_offset: 1, line_limit: 100 },
      }),
      resultEntry: entry("tool_result", {
        tool_name: "read_file",
        content: [
          "文件编码: utf-8",
          "文件总行数: 200",
          "本页行区间: 1-100 / 200",
          "next_line_offset: 83",
          "后方是否还有未读取内容: 是",
          "本页内容是否因 token 上限截断: 是",
          "---",
          "正文",
        ].join("\n"),
      }),
    });

    expect(model.resultFields).toEqual([
      expect.objectContaining({ label: "文件编码", value: "utf-8" }),
      expect.objectContaining({ label: "文件总行数", value: "200" }),
    ]);
    expect(model.resultFields.some((item) => item.label === "下一行偏移")).toBe(false);
    expect(model.resultFields.some((item) => item.label === "Token 截断")).toBe(false);
    expect(model.resultBlocks).toHaveLength(0);
  });

  it("uses concise fields for an unknown structured result", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "custom_tool",
        arguments: { query: "status" },
      }),
      resultEntry: entry("tool_result", {
        tool_name: "custom_tool",
        content: JSON.stringify({ ok: true, status: "ready", count: 3, nested: { secret: "hidden" } }),
      }),
    });

    expect(model.resultFields).toEqual(
      [expect.objectContaining({ label: "count", value: "3" })],
    );
    expect(model.resultFields.some((item) => item.value.includes("secret"))).toBe(false);
  });

  it("does not repeat the call purpose in expanded card input", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "terminal_command",
        arguments: {
          call_purpose: "查看当前目录",
          terminal_id: "term-1",
          command: "pwd",
        },
      }),
    });

    expect(model.inputFields.some((item) => item.label === "执行目的")).toBe(false);
    expect(model.inputFields).toEqual([
      { label: "命令", value: "pwd", kind: "code" },
      { label: "终端", value: "term-1", kind: "text" },
    ]);
  });

  it("renders terminal input data as code instead of plain text", () => {
    const model = buildToolCardModel({
      callEntry: entry("tool_call", {
        tool_name: "terminal_input",
        arguments: {
          call_purpose: "在交互终端中执行命令",
          terminal_id: "local-terminal-1",
          data: "Get-Location\n",
        },
      }),
    });

    expect(model.inputFields).toEqual([
      { label: "终端", value: "local-terminal-1", kind: "mono" },
      { label: "输入内容", value: "Get-Location", kind: "code" },
    ]);
  });
});
