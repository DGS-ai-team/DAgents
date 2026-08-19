import { describe, expect, it } from "vitest";
import { groupConsecutiveToolSteps } from "./streamDisplay.js";

function step(key) {
  return { key, kind: "tool_step", callEntry: { id: key }, resultEntry: null };
}

describe("groupConsecutiveToolSteps", () => {
  it("keeps a single tool step unchanged", () => {
    const one = step("tool-1");
    const result = groupConsecutiveToolSteps([{ kind: "assistant", key: "assistant-1" }, one]);
    expect(result).toHaveLength(2);
    expect(result[1]).toBe(one);
  });

  it("groups only adjacent tool steps and preserves order", () => {
    const a = step("tool-a");
    const b = step("tool-b");
    const c = step("tool-c");
    const result = groupConsecutiveToolSteps([
      { kind: "assistant", key: "before" },
      a,
      b,
      { kind: "assistant", key: "between" },
      c,
    ]);

    expect(result.map((item) => item.kind)).toEqual(["assistant", "tool_group", "assistant", "tool_step"]);
    expect(result[1].steps).toEqual([a, b]);
    expect(result[3]).toBe(c);
  });
});
