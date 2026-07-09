import { describe, expect, it } from "vitest";
import { buildContextMessageView } from "./contextMessagePreview.js";

describe("buildContextMessageView", () => {
  it("formats temporary agent result JSON instead of raw payload", () => {
    const view = buildContextMessageView(
      JSON.stringify({
        artifacts: [],
        child_session_id: "child-639e3690aa52",
        error: "",
        kind: "result",
        status: "completed",
        summary: "以下是 **2026年6月25日（周四）东莞** 的天气概况",
      }),
    );
    expect(view.full).toContain("临时 Agent 完成");
    expect(view.full).toContain("child-639e3690");
    expect(view.full).not.toContain('"artifacts"');
  });

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
