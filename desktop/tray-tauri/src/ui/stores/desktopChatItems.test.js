import { describe, expect, it } from "vitest";
import { createDesktopChatItemsStore } from "./desktopChatItems.js";

describe("desktop chat item store", () => {
  it("point-updates a stable draft without scanning or mapping the loaded transcript", () => {
    const store = createDesktopChatItemsStore();
    store.setScope("agent-a\0generation-1");
    const rows = Array.from({ length: 2000 }, (_, index) => ({
      id: `history-${index}`,
      item_id: `history-${index}`,
      item_version: 1,
      kind: "assistant",
      text: `message ${index}`,
      data: {},
    }));
    rows[1999] = {
      id: "tail-item",
      item_id: "tail-item",
      kind: "assistant",
      text: "token 1",
      streaming: true,
      partial: true,
      data: {},
    };
    const stableEntries = store.entries;
    store.replaceSnapshot(rows);
    const prefix = store.entries[0];
    const tail = store.entries.at(-1);
    const revision = store.revision.value;
    store.resetMetrics();

    for (let index = 2; index <= 8; index += 1) {
      store.upsertDraft({ ...tail, text: `token ${index}` });
    }

    expect(store.entries).toBe(stableEntries);
    expect(store.entries[0]).toBe(prefix);
    expect(store.entries.at(-1)).toBe(tail);
    expect(tail.text).toBe("token 8");
    expect(store.revision.value).toBe(revision);
    expect(store.metrics).toEqual({ snapshotRowsVisited: 0, mapperCalls: 0, pointUpdates: 7, inserts: 0 });
  });

  it("isolates identical item IDs across Agent generations", () => {
    const store = createDesktopChatItemsStore();
    store.setScope("agent-a\0generation-1");
    store.replaceSnapshot([{ id: "same", item_id: "same", item_version: 1, kind: "assistant", text: "A", data: {} }]);
    const first = store.entries[0];

    store.setScope("agent-b\0generation-1");
    store.replaceSnapshot([{ id: "same", item_id: "same", item_version: 1, kind: "assistant", text: "B", data: {} }]);

    expect(store.entries[0]).not.toBe(first);
    expect(store.entries[0].text).toBe("B");
  });

  it("skips an unchanged ordered snapshot while preserving same-version row updates", () => {
    const store = createDesktopChatItemsStore();
    store.setScope("agent-a\0generation-1");
    const first = {
      id: "stable-1",
      item_id: "stable-1",
      item_version: 1,
      kind: "assistant",
      text: "unchanged",
      data: { source: "node" },
    };
    const second = { ...first, id: "stable-2", item_id: "stable-2", text: "second" };
    store.replaceSnapshot([first, second]);
    const revision = store.revision.value;
    const firstRow = store.entries[0];
    store.resetMetrics();

    expect(store.replaceSnapshot([first, second])).toBe(revision);
    expect(store.entries[0]).toBe(firstRow);
    expect(store.metrics).toEqual({ snapshotRowsVisited: 0, mapperCalls: 0, pointUpdates: 0, inserts: 0 });

    const updated = { ...first, text: "same version, updated local projection" };
    expect(store.replaceSnapshot([updated, second])).toBe(revision + 1);
    expect(store.entries[0]).toBe(firstRow);
    expect(store.entries[0].text).toBe(updated.text);
    expect(store.metrics.pointUpdates).toBe(2);
  });

  it("keeps pending input state in the conversation store across hydration scopes", () => {
    const store = createDesktopChatItemsStore();
    const first = { id: 1, item_id: "agent-a/input/1", text: "first", queue_status: "sending" };
    const second = { id: 2, item_id: "agent-a/input/2", text: "second", queue_status: "queued" };

    store.setScope("agent-a\0generation-1");
    store.replacePendingInputs("agent-a", [first]);
    store.upsertPendingInput("agent-a", second);
    expect(store.pendingInputsForAgent("agent-a")).toEqual([first, second]);

    store.setScope("agent-a\0generation-2");
    expect(store.pendingInputsForAgent("agent-a")).toEqual([first, second]);
    store.removePendingInput("agent-a", second.item_id);
    expect(store.pendingInputsForAgent("agent-a")).toEqual([first]);
    expect(store.pendingInputsForAgent("agent-b")).toEqual([]);
  });

  it("applies durable transcript patches as point updates and ordered inserts", () => {
    const store = createDesktopChatItemsStore();
    store.setScope("agent-a\0generation-1");
    store.replaceSnapshot([
      { id: "one", item_id: "one", item_version: 1, conversation_ordinal: 1, kind: "user", text: "one", data: {} },
      { id: "two", item_id: "two", item_version: 1, conversation_ordinal: 2, kind: "assistant", text: "two", data: {} },
    ]);
    const first = store.entries[0];
    const second = store.entries[1];
    const revision = store.revision.value;
    store.resetMetrics();

    const result = store.applyTranscriptPatch([
      { id: "two", item_id: "two", item_version: 2, conversation_ordinal: 2, kind: "assistant", text: "updated", data: {} },
      { id: "three", item_id: "three", item_version: 1, conversation_ordinal: 3, kind: "assistant", text: "three", data: {} },
    ]);

    expect(result).toMatchObject({ structuralChanged: true, insertedItemIds: ["three"], changedItemIds: ["two"], revision: revision + 1 });
    expect(store.entries.map(row => row.item_id)).toEqual(["one", "two", "three"]);
    expect(store.entries[0]).toBe(first);
    expect(store.entries[1]).toBe(second);
    expect(second.text).toBe("updated");
    expect(store.metrics).toEqual({ snapshotRowsVisited: 0, mapperCalls: 0, pointUpdates: 1, inserts: 1 });
  });

  it("replaces a streaming draft with its authoritative transcript fields", () => {
    const store = createDesktopChatItemsStore();
    store.setScope("agent-a\0generation-1");
    store.replaceSnapshot([{
      id: "assistant-1",
      item_id: "assistant-1",
      kind: "assistant",
      text: "partial answer",
      streaming: true,
      partial: true,
      attempt_id: "attempt-1",
      chunk_seq: 4,
      agent_seq: 8,
      stream_seq: 12,
      data: {},
    }]);
    const row = store.entries[0];

    store.applyTranscriptPatch([{
      id: "assistant-1",
      item_id: "assistant-1",
      item_version: 1,
      kind: "assistant",
      text: "complete answer",
      data: {},
    }]);

    expect(store.entries[0]).toBe(row);
    expect(row).toMatchObject({ item_version: 1, text: "complete answer" });
    for (const key of ["streaming", "partial", "attempt_id", "chunk_seq", "agent_seq", "stream_seq"]) {
      expect(row).not.toHaveProperty(key);
    }
  });

  it("removes transient draft fields when a same-version snapshot replaces a row", () => {
    const store = createDesktopChatItemsStore();
    store.setScope("agent-a\0generation-1");
    store.replaceSnapshot([{
      id: "assistant-1",
      item_id: "assistant-1",
      item_version: 1,
      kind: "assistant",
      text: "complete answer",
      streaming: true,
      partial: true,
      attempt_id: "attempt-1",
      data: {},
    }]);
    const row = store.entries[0];

    store.replaceSnapshot([{
      id: "assistant-1",
      item_id: "assistant-1",
      item_version: 1,
      kind: "assistant",
      text: "complete answer",
      data: {},
    }]);

    expect(store.entries[0]).toBe(row);
    expect(row).not.toHaveProperty("streaming");
    expect(row).not.toHaveProperty("partial");
    expect(row).not.toHaveProperty("attempt_id");
  });
});
