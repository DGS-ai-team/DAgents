import { describe, expect, it } from "vitest";
import { advanceTextReveal, commonTextPrefixLength } from "./textReveal.js";

describe("buffered assistant text reveal", () => {
  it("reveals text at a stable time-based pace instead of jumping by provider chunk", () => {
    const text = "这是一段连续输出";
    let state = { visibleLength: 0, remainder: 0 };
    state = advanceTextReveal({ text, ...state, elapsedMs: 32 });
    expect(text.slice(0, state.visibleLength)).toBe("这");
    expect(state.done).toBe(false);

    state = advanceTextReveal({ text, ...state, elapsedMs: 32 });
    expect(text.slice(0, state.visibleLength)).toBe("这是一");
  });

  it("retains buffered text and accelerates only when the backlog grows", () => {
    const normal = advanceTextReveal({ text: "a".repeat(50), visibleLength: 0, elapsedMs: 32 });
    const text = "a".repeat(400);
    const buffered = advanceTextReveal({ text, visibleLength: 250, elapsedMs: 32 });
    expect(normal.visibleLength).toBe(1);
    expect(buffered.visibleLength - 250).toBeGreaterThan(normal.visibleLength);
    expect(buffered.visibleLength - 250).toBeLessThanOrEqual(4);
    expect(buffered.done).toBe(false);
  });

  it("never splits surrogate pairs and preserves fractional timing between frames", () => {
    const first = advanceTextReveal({ text: "😀a", visibleLength: 0, elapsedMs: 10 });
    expect(first.visibleLength).toBe(0);
    const second = advanceTextReveal({ text: "😀a", visibleLength: 0, remainder: first.remainder, elapsedMs: 20 });
    expect("😀a".slice(0, second.visibleLength)).toBe("😀");
  });

  it("finds a corrected prefix without splitting Unicode characters", () => {
    expect(commonTextPrefixLength("hello 😀old", "hello 😀new")).toBe("hello 😀".length);
  });
});
