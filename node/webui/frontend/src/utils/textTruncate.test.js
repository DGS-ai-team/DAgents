import { describe, expect, it } from "vitest";
import { truncateGraphemes } from "./textTruncate.js";

describe("truncateGraphemes", () => {
  it("keeps short text unchanged", () => {
    expect(truncateGraphemes("喝水提醒", 10)).toBe("喝水提醒");
  });

  it("truncates Chinese without splitting characters", () => {
    const text = "将创建定时触发器: 喝水提醒每天执行";
    const out = truncateGraphemes(text, 12);
    expect(out.endsWith("…")).toBe(true);
    expect(out).not.toMatch(/[\uD800-\uDFFF]/);
    expect("将创建定时触发器: 喝水提醒每天执行".startsWith(out.slice(0, -1))).toBe(true);
  });
});
