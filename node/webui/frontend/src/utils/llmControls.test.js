import { describe, expect, it } from "vitest";
import { canToggleThinking, getThinkingControl, hasThinkingSecondaryControl } from "./llmControls.js";

describe("llm thinking controls", () => {
  it("uses explicit provider/model capability metadata", () => {
    expect(getThinkingControl({ thinking_control: "budget" })).toBe("budget");
    expect(getThinkingControl({ thinking_control: "fixed" })).toBe("fixed");
    expect(canToggleThinking({ thinking_control: "toggle" })).toBe(true);
    expect(canToggleThinking({ thinking_control: "fixed" })).toBe(false);
  });

  it("keeps compatibility with older nodes", () => {
    expect(getThinkingControl({ reasoning_effort_supported: false })).toBe("toggle");
    expect(getThinkingControl({ reasoning_effort_supported: true })).toBe("effort");
  });

  it("only exposes the secondary control for effort/budget modes", () => {
    expect(hasThinkingSecondaryControl({ thinking_control: "effort" })).toBe(true);
    expect(hasThinkingSecondaryControl({ thinking_control: "budget" })).toBe(true);
    expect(hasThinkingSecondaryControl({ thinking_control: "toggle" })).toBe(false);
    expect(hasThinkingSecondaryControl({ thinking_control: "fixed" })).toBe(false);
    expect(hasThinkingSecondaryControl({ thinking_control: "effort", reasoning_effort_supported: false })).toBe(false);
  });
});
