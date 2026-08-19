import { describe, expect, it } from "vitest";
import { countNewStreamItems } from "./streamUnread.js";

describe("countNewStreamItems", () => {
  it("counts only items with keys that were not in the previous render", () => {
    expect(
      countNewStreamItems(
        [{ key: "user-1" }, { key: "assistant-1" }, { key: "tool-2" }],
        [{ key: "user-1" }, { key: "tool-1" }],
      ),
    ).toBe(2);
  });

  it("does not count content updates to an existing message", () => {
    expect(
      countNewStreamItems(
        [{ key: "assistant-1", text: "longer" }],
        [{ key: "assistant-1", text: "short" }],
      ),
    ).toBe(0);
  });
});
