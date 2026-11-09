import { describe, expect, it } from "vitest";
import { groupConsecutiveToolSteps } from "./streamDisplay.js";

function step(key) {
  return { key, kind: "tool_step", callEntry: { id: key }, resultEntry: null };
}

describe("groupConsecutiveToolSteps", () => {
  it("wraps a single tool step in the same stable group used for batches", () => {
    const one = step("tool-1");
    const result = groupConsecutiveToolSteps([{ kind: "assistant", key: "assistant-1" }, one]);
    expect(result).toHaveLength(2);
    expect(result[1]).toEqual({ key: "tool-group-tool-1", kind: "tool_group", steps: [one] });
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

    expect(result.map((item) => item.kind)).toEqual(["assistant", "tool_group", "assistant", "tool_group"]);
    expect(result[1].steps).toEqual([a, b]);
    expect(result[3].key).toBe("tool-group-tool-c");
    expect(result[3].steps).toEqual([c]);
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

    expect(result).toEqual([{ key: "tool-group-tool-1", kind: "tool_group", steps: [tool] }]);
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

    expect(result.map((item) => item.kind)).toEqual(["tool_group", "reasoning", "tool_group"]);
    expect(result[0].steps).toEqual([a]);
    expect(result[2].steps).toEqual([b]);
  });
});
