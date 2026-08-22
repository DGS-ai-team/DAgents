import { describe, expect, it } from "vitest";
import { formatInputStripUsage, parseUsageFields, parseUsageRound } from "./usage.js";

describe("usage cache observability", () => {
  it("keeps provider cache metrics unavailable when the response omits them", () => {
    const usage = parseUsageFields({ prompt_tokens: 100, completion_tokens: 20 });
    expect(usage.cacheObserved).toBe(false);
    expect(usage.rate).toBe(-1);
    expect(formatInputStripUsage(usage)).toBe("↑100 ↓20");
  });

  it("preserves explicit zero-hit metrics as observed", () => {
    const usage = parseUsageFields({
      prompt_tokens: 100,
      completion_tokens: 20,
      prompt_cache_available: true,
      prompt_cache_hit_tokens: 0,
      prompt_cache_miss_tokens: 100,
    });
    expect(usage.cacheObserved).toBe(true);
    expect(usage.rate).toBe(0);
  });

  it("maps round cache availability without changing the displayed strip", () => {
    const usage = parseUsageRound({
      round_prompt_tokens: 50,
      round_completion_tokens: 8,
      round_prompt_cache_available: true,
      round_prompt_cache_hit_tokens: 25,
      round_prompt_cache_hit_rate: 0.5,
    });
    expect(usage.cacheObserved).toBe(true);
    expect(usage.rate).toBe(0.5);
  });
});
