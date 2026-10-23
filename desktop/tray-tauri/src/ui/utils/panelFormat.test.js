import { describe, expect, it } from "vitest";
import {
  formatPolicyMode,
  formatTriggerCondition,
  formatUnixTime,
  triggerFireStatusMessage,
} from "./panelFormat.js";

describe("panelFormat", () => {
  it("labels policy modes in Chinese", () => {
    expect(formatPolicyMode("never")).toBe("自动允许");
    expect(formatPolicyMode("always")).toBe("需审批");
    expect(formatPolicyMode("deny")).toBe("禁止");
  });

  it("formats trigger interval condition", () => {
    expect(formatTriggerCondition({ interval_seconds: 300 })).toBe("每 300 秒");
  });

  it("formats unix timestamp", () => {
    const out = formatUnixTime(1700000000);
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
    expect(formatUnixTime(0)).toBe("—");
  });

  it("keeps fire status semantics explicit", () => {
    expect(triggerFireStatusMessage({ status: "queued" }, "日报")).toContain("已排队");
    expect(triggerFireStatusMessage({ status: "skipped", reason: "busy" }, "日报")).toContain("已跳过");
    expect(triggerFireStatusMessage({ status: "error", message: "拒绝" }, "日报")).toContain("投递失败");
  });
});
