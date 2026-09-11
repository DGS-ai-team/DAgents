import { describe, expect, it } from "vitest";
import { formatInputStripUsage, parseUsageFields } from "./usage.js";

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

});
