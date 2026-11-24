import { describe, expect, it } from "vitest";
import { canManuallyRetryHydrate, hydrateSchedulePlan, hydrateSyncNotice, isHydrateReadError, mergeHydrateSnapshot, mergeTranscriptReadWindow, nextHydrateRetryDelay, restoreHydrateLifecycleFields, shouldReplaceHydrateTimer } from "./hydrateSnapshot.js";

describe("mergeHydrateSnapshot", () => {
  it("uses conversation revision rather than model history revision for transcript freshness", () => {
    const current = {
      conversation_revision: 8,
      history_revision: 21,
      transcript: [{ content: "current durable transcript" }],
    };
    const incoming = {
      conversation_revision: 9,
      history_revision: 20,
      transcript: [{ content: "new durable transcript" }],
    };

    const merged = mergeHydrateSnapshot(current, incoming, {
      conversationRevision: 8,
      historyRevision: 21,
    });

    expect(merged.transcriptAccepted).toBe(true);
    expect(merged.conversationRevision).toBe(9);
    expect(merged.snapshot.transcript).toEqual(incoming.transcript);
  });

  it("rejects a stale conversation revision even if its context history counter is higher", () => {
    const current = {
      conversation_revision: 12,
      history_revision: 18,
      transcript: [{ content: "latest durable transcript" }],
    };
    const incoming = {
      conversation_revision: 11,
      history_revision: 99,
      transcript: [{ content: "stale durable transcript" }],
    };

    const merged = mergeHydrateSnapshot(current, incoming, {
      conversationRevision: 12,
      historyRevision: 18,
    });

    expect(merged.transcriptAccepted).toBe(false);
    expect(merged.snapshot.conversation_revision).toBe(12);
    expect(merged.snapshot.transcript).toEqual(current.transcript);
  });

  it("keeps a newer transcript when the incoming lifecycle projection is stale", () => {
    const current = {
      history_revision: 4,
      transcript: [{ content: "old" }],
      turn_state: { phase: "model_generating", lifecycle_seq: 20 },
      pending_hitl: { request_id: "old-request" },
      has_active_turn: true,
    };
    const incoming = {
      history_revision: 5,
      transcript: [{ content: "committed result" }],
      turn_state: { phase: "queued", lifecycle_seq: 19 },
      pending_hitl: null,
      has_active_turn: false,
    };

    const merged = mergeHydrateSnapshot(current, incoming, { historyRevision: 4, lifecycleSeq: 20 });

    expect(merged.transcriptAccepted).toBe(true);
    expect(merged.lifecycleAccepted).toBe(false);
    expect(merged.snapshot.transcript).toEqual([{ content: "committed result" }]);
    expect(merged.snapshot.turn_state).toEqual(current.turn_state);
    expect(merged.snapshot.pending_hitl).toEqual(current.pending_hitl);
    expect(merged.snapshot.has_active_turn).toBe(true);
  });

  it("keeps the current transcript while accepting newer lifecycle state from an older history snapshot", () => {
    const current = {
      history_revision: 8,
      transcript: [{ content: "new" }],
      turn_state: { phase: "model_generating", lifecycle_seq: 20 },
      pending_hitl: { request_id: "old-request" },
      has_active_turn: true,
    };
    const incoming = {
      history_revision: 7,
      transcript: [{ content: "stale" }],
      turn_state: { phase: "waiting_user", lifecycle_seq: 21 },
      pending_hitl: { request_id: "new-request" },
      has_active_turn: false,
    };

    const merged = mergeHydrateSnapshot(current, incoming, { historyRevision: 8, lifecycleSeq: 20 });

    expect(merged.transcriptAccepted).toBe(false);
    expect(merged.lifecycleAccepted).toBe(true);
    expect(merged.snapshot.transcript).toEqual([{ content: "new" }]);
    expect(merged.snapshot.turn_state).toEqual(incoming.turn_state);
    expect(merged.snapshot.pending_hitl).toEqual(incoming.pending_hitl);
    expect(merged.snapshot.has_active_turn).toBe(false);
  });

  it("keeps history-window metadata with the accepted transcript watermark", () => {
    const current = { history_revision: 8, transcript: [{ content: "new" }], has_earlier_history: true };
    const incoming = { history_revision: 7, transcript: [{ content: "stale" }], has_earlier_history: false };

    const merged = mergeHydrateSnapshot(current, incoming, { historyRevision: 8 });

    expect(merged.snapshot.transcript).toEqual(current.transcript);
    expect(merged.snapshot.has_earlier_history).toBe(true);
  });

  it("preserves the bounded read slice while accepting the newest server window", () => {
    const oldRound = { item_id: "round-2/item", kind: "assistant", text: "being read", conversation_ordinal: 20, conversation_entry_index: 0, round_id: "round-2", round_revision_id: "rev-2" };
    const beforeAnchor = { item_id: "round-1/item", kind: "assistant", conversation_ordinal: 10, conversation_entry_index: 0, round_id: "round-1", round_revision_id: "rev-1" };
    const overlapping = { item_id: "round-3/item", kind: "assistant", text: "same item", conversation_ordinal: 30, conversation_entry_index: 0, round_id: "round-3", round_revision_id: "rev-3" };
    const latest = { item_id: "round-8/item", kind: "assistant", text: "latest", conversation_ordinal: 80, conversation_entry_index: 0, round_id: "round-8", round_revision_id: "rev-8" };

    const merged = mergeTranscriptReadWindow(
      [beforeAnchor, oldRound, overlapping],
      [overlapping, latest],
      { anchorOrdinal: 20, endOrdinal: 30, conversationEpoch: 4 },
      4,
    );

    expect(merged.map(entry => entry.item_id)).toEqual(["round-2/item", "round-3/item", "round-8/item"]);
    expect(merged[1]).toBe(overlapping);
  });

  it("drops a read-slice revision when the server selects its replacement", () => {
    const oldRevision = { item_id: "old-answer", kind: "assistant", conversation_ordinal: 50, round_id: "round-5", round_revision_id: "old" };
    const newRevision = { item_id: "new-answer", kind: "assistant", conversation_ordinal: 51, round_id: "round-5", round_revision_id: "new" };
    expect(mergeTranscriptReadWindow(
      [oldRevision],
      [newRevision],
      { anchorOrdinal: 50, endOrdinal: 50, conversationEpoch: 2 },
      2,
    )).toEqual([newRevision]);
  });

  it("does not preserve a read slice across conversation clear", () => {
    const oldEntry = { item_id: "old", conversation_ordinal: 3 };
    const newEntry = { item_id: "new", conversation_ordinal: 1 };
    expect(mergeTranscriptReadWindow(
      [oldEntry],
      [newEntry],
      { anchorOrdinal: 1, endOrdinal: 3, conversationEpoch: 1 },
      2,
    )).toEqual([newEntry]);
  });

  it("rolls back all lifecycle-owned fields after the turn store rejects a snapshot", () => {
    const current = {
      history_revision: 8,
      transcript: [{ content: "current" }],
      turn_state: { phase: "model_generating", lifecycle_seq: 22 },
      pending_hitl: { request_id: "current-request" },
      has_active_turn: true,
    };
    const incoming = {
      history_revision: 9,
      transcript: [{ content: "committed" }],
      turn_state: { phase: "queued", lifecycle_seq: 23 },
      pending_hitl: null,
      has_active_turn: false,
    };
    const merged = mergeHydrateSnapshot(current, incoming, { historyRevision: 8, lifecycleSeq: 22 });

    const restored = restoreHydrateLifecycleFields(merged.snapshot, current);

    expect(restored.transcript).toEqual([{ content: "committed" }]);
    expect(restored.turn_state).toEqual(current.turn_state);
    expect(restored.pending_hitl).toEqual(current.pending_hitl);
    expect(restored.has_active_turn).toBe(true);
  });

  it("keeps lifecycle fields accepted by the watermark when there is no prior hydrate snapshot", () => {
    const incoming = {
      history_revision: 1,
      transcript: [{ content: "first result" }],
      turn_state: { phase: "completed", lifecycle_seq: 24 },
      pending_hitl: null,
      has_active_turn: false,
    };
    const merged = mergeHydrateSnapshot(null, incoming, { historyRevision: -1, lifecycleSeq: 24 });

    const restored = restoreHydrateLifecycleFields(merged.snapshot, null);

    expect(merged.lifecycleAccepted).toBe(true);
    expect(restored.turn_state).toEqual(incoming.turn_state);
    expect(restored.pending_hitl).toBeNull();
    expect(restored.has_active_turn).toBe(false);
  });

  it("pulls an existing delayed hydrate forward for a terminal trigger", () => {
    const plan = hydrateSchedulePlan({ requestedDelay: 0, dirtyAt: 1000, now: 1100 });
    const existingDueAt = 1350;

    expect(shouldReplaceHydrateTimer(existingDueAt, 1100 + plan.delay)).toBe(true);
    expect(shouldReplaceHydrateTimer(existingDueAt, 1400)).toBe(false);
  });

  it("stops automatic retries after the five scheduled backoffs until the target changes", () => {
    expect([0, 1, 2, 3, 4].map(nextHydrateRetryDelay)).toEqual([500, 1000, 2000, 4000, 8000]);
    expect(nextHydrateRetryDelay(5)).toBeNull();
    expect(hydrateSchedulePlan({
      requestedDelay: 0,
      dirtyAt: 0,
      now: 1000,
      retryExhausted: true,
      targetChanged: false,
    }).allowed).toBe(false);
    expect(hydrateSchedulePlan({
      requestedDelay: 0,
      dirtyAt: 0,
      now: 1000,
      retryExhausted: true,
      targetChanged: true,
    }).allowed).toBe(true);
  });

  it("hides a short hydrate and exposes a retry action after retries are exhausted", () => {
    expect(hydrateSyncNotice({ phase: "pending", startedAt: 1000, now: 1599 })).toBe("");
    expect(hydrateSyncNotice({ phase: "pending", startedAt: 1000, now: 1600 })).toBe("syncing");
    expect(hydrateSyncNotice({ phase: "failed" })).toBe("failed");
    expect(canManuallyRetryHydrate("failed")).toBe(true);
    expect(canManuallyRetryHydrate("syncing")).toBe(false);
  });

  it("keeps a Node result commit failure distinct from a hydrate read failure", () => {
    expect(hydrateSyncNotice({ phase: "idle", resultCommitStatus: "failed" })).toBe("commit_failed");
  });

  it("clears only typed hydrate read errors, preserving unrelated operation errors", () => {
    expect(isHydrateReadError("结果暂未同步：连接超时")).toBe(true);
    expect(isHydrateReadError("无法上传附件：文件过大")).toBe(false);
  });
});
