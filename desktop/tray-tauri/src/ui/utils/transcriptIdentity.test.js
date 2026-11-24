import { describe, expect, it } from "vitest";
import { mergeTranscriptReferenceWindow, mergeTranscriptSnapshot } from "./transcriptIdentity.js";

function item(itemId, version, text = `${itemId}@${version}`) {
  return { item_id: itemId, item_version: version, id: itemId, kind: "assistant", text, data: {} };
}

describe("mergeTranscriptSnapshot", () => {
  it("preserves the existing object reference when item versions match", () => {
    const current = item("reply-1", 3, "published");
    const merged = mergeTranscriptSnapshot([item("reply-1", 3, "same version payload")], new Map([["reply-1", current]]));

    expect(merged.transcript[0]).toBe(current);
    expect(merged.stableItemsById.get("reply-1")).toBe(current);
  });

  it("rejects an older item version without comparing or copying the payload", () => {
    const current = item("reply-1", 4, "newer");
    const merged = mergeTranscriptSnapshot([item("reply-1", 2, "stale")], new Map([["reply-1", current]]));

    expect(merged.transcript[0]).toBe(current);
  });

  it("replaces only the entity whose version advanced", () => {
    const stable = item("reply-a", 1);
    const changed = item("reply-b", 1, "old");
    const incomingChanged = item("reply-b", 2, "new");
    const merged = mergeTranscriptSnapshot(
      [item("reply-a", 1, "same"), incomingChanged],
      new Map([["reply-a", stable], ["reply-b", changed]]),
    );

    expect(merged.transcript[0]).toBe(stable);
    expect(merged.transcript[1]).toBe(incomingChanged);
  });

  it("follows incoming order while preserving references by stable ID", () => {
    const first = item("first", 2);
    const second = item("second", 5);
    const merged = mergeTranscriptSnapshot(
      [item("second", 5, "same"), item("first", 2, "same")],
      new Map([["first", first], ["second", second]]),
    );

    expect(merged.transcript).toEqual([second, first]);
    expect(merged.transcript[0]).toBe(second);
    expect(merged.transcript[1]).toBe(first);
  });

  it("passes legacy entries through without inventing stable identity", () => {
    const legacy = { kind: "assistant", text: "legacy" };
    const merged = mergeTranscriptSnapshot([legacy]);

    expect(merged.transcript[0]).toBe(legacy);
    expect(merged.stableItemsById.size).toBe(0);
  });
});

describe("mergeTranscriptReferenceWindow", () => {
  it("reuses references only for the active Agent generation", () => {
    const agentA = item("shared-id", 1, "agent A");
    const agentB = item("shared-id", 1, "agent B");
    const nextGeneration = item("shared-id", 1, "new generation");

    const first = mergeTranscriptReferenceWindow(null, "agent-a", 1, [agentA]);
    const second = mergeTranscriptReferenceWindow(first.window, "agent-b", 1, [agentB]);
    const third = mergeTranscriptReferenceWindow(second.window, "agent-a", 2, [nextGeneration]);

    expect(second.transcript[0]).toBe(agentB);
    expect(second.transcript[0]).not.toBe(agentA);
    expect(third.transcript[0]).toBe(nextGeneration);
    expect(third.transcript[0]).not.toBe(agentA);
    expect(second.window).not.toHaveProperty("windows");
  });
});
