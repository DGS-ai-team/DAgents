import { describe, expect, it } from "vitest";
import {
  buildContextMessageView,
  formatContextMessageContent,
} from "./contextMessagePreview.js";

describe("formatContextMessageContent", () => {
  it("formats temporary agent result JSON instead of raw payload", () => {
    const out = formatContextMessageContent(
      JSON.stringify({
        artifacts: [],
        child_session_id: "child-639e3690aa52",
        error: "",
        kind: "result",
        status: "completed",
        summary: "以下是 **2026年6月25日（周四）东莞** 的天气概况",
      }),
    );
    expect(out).toContain("临时 Agent 完成");
    expect(out).toContain("child-639e3690");
    expect(out).not.toContain('"artifacts"');
  });

  it("truncates long text without splitting graphemes", () => {
    const text = "将创建定时触发器: 喝水提醒每天执行" + "天".repeat(250);
    const out = formatContextMessageContent(text, 200);
    expect(out.endsWith("…")).toBe(true);
    expect(out).not.toContain("\uFFFD");
    expect(text.startsWith(out.slice(0, -1))).toBe(true);
  });
});

describe("buildContextMessageView", () => {
  it("marks long messages as expandable with distinct preview and full", () => {
    const text = "将创建定时触发器: 喝水提醒每天执行" + "天".repeat(250);
    const view = buildContextMessageView(text, 80);
    expect(view.expandable).toBe(true);
    expect(view.preview.endsWith("…")).toBe(true);
    expect(view.full).toBe(text);
    expect(view.full.length).toBeGreaterThan(view.preview.length);
  });

  it("does not mark short formatted results as expandable", () => {
    const view = buildContextMessageView(
      JSON.stringify({
        kind: "result",
        child_session_id: "child-abc",
        status: "completed",
        summary: "完成",
      }),
    );
    expect(view.expandable).toBe(false);
    expect(view.preview).toBe(view.full);
  });
});
