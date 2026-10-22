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

  it("merges a tool-generating step across an empty streaming thinking placeholder", () => {
    const a = step("tool-a");
    const b = step("tool-b");
    const emptyThinking = {
      kind: "reasoning",
      key: "reasoning-stream",
      entry: { streaming: true, text: "" },
    };

    const result = groupConsecutiveToolSteps([a, emptyThinking, b]);

    expect(result).toHaveLength(1);
    expect(result[0].kind).toBe("tool_group");
    expect(result[0].steps).toEqual([a, b]);
  });

  it("does not leave an empty thinking placeholder before the first tool step", () => {
    const tool = step("tool-1");
    const emptyThinking = {
      kind: "reasoning",
      key: "reasoning-stream",
      entry: { streaming: true, text: "" },
    };

    const result = groupConsecutiveToolSteps([emptyThinking, tool]);

    expect(result).toEqual([tool]);
  });

  it("keeps meaningful streaming thinking between tool steps", () => {
    const a = step("tool-a");
    const b = step("tool-b");
    const thinking = {
      kind: "reasoning",
      key: "reasoning-stream",
      entry: { streaming: true, text: "需要先确认上下文" },
    };

    const result = groupConsecutiveToolSteps([a, thinking, b]);

    expect(result.map((item) => item.kind)).toEqual(["tool_step", "reasoning", "tool_step"]);
  });
});
