import { describe, expect, it } from "vitest";
import { assistantDraftEntries, claimAssistantDraftGapRecovery, projectAssistantDrafts, reconcileAssistantDraftSnapshot, reconcileAssistantDrafts, reduceAssistantDraftDelta, retireAssistantDraftsForStep, seedAssistantDraftSnapshots } from "./assistantDrafts.js";
import { mergeHydrateSnapshot } from "./hydrateSnapshot.js";

function delta(agentId, itemId, attemptId, part, chunkSeq, content) {
  return {
    event_type: "conversation_delta",
    agent_id: agentId,
    agent_seq: chunkSeq,
    seq: chunkSeq,
    data: {
      display_type: part === "content" ? "delta" : "reasoning",
      item_id: itemId,
      attempt_id: attemptId,
      turn_id: "turn-1",
      part,
      chunk_seq: chunkSeq,
      content,
    },
  };
}

function apply(state, event, agentId = "agent-a", generation = 1) {
  return reduceAssistantDraftDelta(state, event, { agentId, generation });
}

describe("seedAssistantDraftSnapshots", () => {
  it("restores current content and reasoning drafts from a hydrate baseline", () => {
    const seeded = seedAssistantDraftSnapshots(new Map(), [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-1", part: "content", text: "partial answer", through_chunk_seq: 3, through_agent_seq: 3 },
      { item_id: "step-1/assistant/reasoning", attempt_id: "attempt-1", part: "reasoning", text: "thinking", through_chunk_seq: 2, through_agent_seq: 2 },
    ], { agentId: "agent-a", generation: 4 });
    expect(seeded.entries.map(entry => [entry.kind, entry.text, entry.chunk_seq])).toEqual([
      ["assistant", "partial answer", 3], ["reasoning", "thinking", 2],
    ]);
    expect(assistantDraftEntries(seeded.state, "agent-a", 4).map(entry => entry.item_id)).toEqual([
      "step-1/assistant/reasoning", "step-1/assistant/content",
    ]);
  });

  it("keeps a newer live draft when the hydrate response contains an older baseline", () => {
    const baseline = seedAssistantDraftSnapshots(new Map(), [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-1", part: "content", text: "partial", through_chunk_seq: 3, through_agent_seq: 3 },
    ], { agentId: "agent-a", generation: 1 }).state;
    const live = apply(baseline, delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 4, " later ")).state;
    const seeded = seedAssistantDraftSnapshots(live, [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-1", part: "content", text: "old baseline", through_chunk_seq: 3, through_agent_seq: 3 },
    ], { agentId: "agent-a", generation: 1 });
    expect(seeded.entries).toEqual([]);
    expect(assistantDraftEntries(seeded.state, "agent-a", 1)[0].text).toBe("partial later ");
  });

  it("retires drafts absent from the Node snapshot only through its covered cursor", () => {
    const coveredDraft = seedAssistantDraftSnapshots(new Map(), [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-1", part: "content", text: "covered", through_chunk_seq: 2, through_agent_seq: 2 },
    ], { agentId: "agent-a", generation: 1 }).state;
    const reconciled = reconcileAssistantDraftSnapshot(coveredDraft, [], {
      agentId: "agent-a", generation: 1, coveredAgentSeq: 2,
    });
    expect(assistantDraftEntries(reconciled.state, "agent-a", 1)).toEqual([]);
    expect(apply(reconciled.state, delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 3, "late")).accepted).toBe(false);

    const newerDraft = apply(coveredDraft, delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 3, "newer")).state;
    const newerReconciled = reconcileAssistantDraftSnapshot(newerDraft, [], {
      agentId: "agent-a", generation: 1, coveredAgentSeq: 2,
    });
    expect(assistantDraftEntries(newerReconciled.state, "agent-a", 1)[0].text).toBe("coverednewer");
  });

  it("uses the global stream cut to preserve newer ephemeral deltas and retire covered ones", () => {
    const event = delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 1, "A");
    event.agent_seq = 0;
    event.seq = 100;
    const current = apply(new Map(), event).state;
    const beforeDelta = reconcileAssistantDraftSnapshot(current, [], {
      agentId: "agent-a", generation: 1, coveredAgentSeq: 0, coveredStreamSeq: 99,
    });
    expect(assistantDraftEntries(beforeDelta.state, "agent-a", 1)[0].text).toBe("A");

    const covered = reconcileAssistantDraftSnapshot(current, [], {
      agentId: "agent-a", generation: 1, coveredAgentSeq: 0, coveredStreamSeq: 100,
    });
    expect(assistantDraftEntries(covered.state, "agent-a", 1)).toEqual([]);
    const late = delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 2, " late");
    expect(apply(covered.state, late).accepted).toBe(false);
  });

  it("closes a turn at terminal patch and rejects delayed ephemeral chunks", () => {
    const current = apply(new Map(), delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 1, "A"));
    const closed = reconcileAssistantDrafts(current.state, "agent-a", 1, [], {
      terminal: true,
      terminalTurnId: "turn-1",
    });
    expect(assistantDraftEntries(closed.state, "agent-a", 1)).toEqual([]);
    const late = delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 2, " late");
    expect(apply(closed.state, late).reason).toBe("retired_turn");
  });

  it("replaces a covered old attempt with the attempt represented by the snapshot", () => {
    const oldDraft = seedAssistantDraftSnapshots(new Map(), [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-1", part: "content", text: "old", through_chunk_seq: 2, through_agent_seq: 2 },
    ], { agentId: "agent-a", generation: 1 }).state;
    const reconciled = reconcileAssistantDraftSnapshot(oldDraft, [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-2", part: "content", text: "new", through_chunk_seq: 1, through_agent_seq: 4 },
    ], { agentId: "agent-a", generation: 1, coveredAgentSeq: 2 });
    expect(assistantDraftEntries(reconciled.state, "agent-a", 1)[0]).toMatchObject({ attempt_id: "attempt-2", text: "new" });
  });

  it("retires a failed attempt without allowing a delayed retry snapshot to revive it", () => {
    const current = apply(new Map(), delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 1, "partial"));
    const retired = retireAssistantDraftsForStep(current.state, "agent-a", 1, "step-1");
    const seeded = seedAssistantDraftSnapshots(retired.state, [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-1", part: "content", text: "old", through_chunk_seq: 1, through_agent_seq: 1 },
    ], { agentId: "agent-a", generation: 1 });
    expect(retired.removedItemIds).toEqual(["step-1/assistant/content"]);
    expect(seeded.entries).toEqual([]);
    expect(assistantDraftEntries(seeded.state, "agent-a", 1)).toEqual([]);
  });

  it("does not revive a retired attempt or replace a newer attempt already in the UI", () => {
    const first = apply(new Map(), delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 1, "old "));
    const second = apply(first.state, delta("agent-a", "step-1/assistant/content", "attempt-2", "content", 1, "new"));
    const retired = seedAssistantDraftSnapshots(second.state, [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-1", part: "content", text: "old answer", through_chunk_seq: 2 },
    ], { agentId: "agent-a", generation: 1 });
    const stale = seedAssistantDraftSnapshots(second.state, [
      { item_id: "step-1/assistant/content", attempt_id: "attempt-0", part: "content", text: "stale", through_chunk_seq: 2 },
    ], { agentId: "agent-a", generation: 1 });
    expect(retired.entries).toEqual([]);
    expect(stale.entries).toEqual([]);
    expect(assistantDraftEntries(second.state, "agent-a", 1)[0].text).toBe("new");
  });
});

describe("reduceAssistantDraftDelta", () => {
  it("reuses a stable transcript when there are no active drafts", () => {
    const transcript = [{ item_id: "stable-1", kind: "assistant", text: "already final" }];
    const state = new Map();

    expect(projectAssistantDrafts(transcript, state, "agent-a", 1)).toBe(transcript);
    expect(reconcileAssistantDrafts(state, "agent-a", 1, transcript).state).toBe(state);
  });

  it("tracks independent content and reasoning chunk sequences", () => {
    let state = new Map();
    state = apply(state, delta("agent-a", "item-1", "attempt-1", "content", 1, "A")).state;
    state = apply(state, delta("agent-a", "item-1", "attempt-1", "reasoning", 1, "R")).state;
    state = apply(state, delta("agent-a", "item-1", "attempt-1", "content", 2, "B")).state;
    state = apply(state, delta("agent-a", "item-1", "attempt-1", "reasoning", 2, "S")).state;

    expect(assistantDraftEntries(state, "agent-a", 1).map(entry => [entry.kind, entry.text])).toEqual([
      ["reasoning", "RS"],
      ["assistant", "AB"],
    ]);
  });

  it("drops repeated, reversed, malformed part, and invalid sequence events", () => {
    const first = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 1, "A"));
    const second = apply(first.state, delta("agent-a", "item-1", "attempt-1", "content", 2, "B"));
    expect(apply(second.state, delta("agent-a", "item-1", "attempt-1", "content", 2, "duplicate")).reason).toBe("out_of_order");
    expect(apply(second.state, delta("agent-a", "item-1", "attempt-1", "content", 1, "late")).reason).toBe("out_of_order");
    expect(apply(second.state, delta("agent-a", "item-1", "attempt-1", "wrong", 3, "bad")).accepted).toBe(false);
    expect(apply(second.state, delta("agent-a", "item-1", "attempt-1", "content", 3.5, "bad")).accepted).toBe(false);
    expect(assistantDraftEntries(second.state, "agent-a", 1)[0].text).toBe("AB");
  });

  it("reports the first missing chunk and an intermediate gap without advancing state", () => {
    const firstGap = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 2, "lost first"));
    expect(firstGap).toMatchObject({ accepted: false, reason: "missing_sequence", expectedChunkSeq: 1, receivedChunkSeq: 2 });
    expect(assistantDraftEntries(firstGap.state, "agent-a", 1)).toEqual([]);

    const first = apply(firstGap.state, delta("agent-a", "item-1", "attempt-1", "content", 1, "A"));
    const gap = apply(first.state, delta("agent-a", "item-1", "attempt-1", "content", 3, "C"));
    expect(gap).toMatchObject({ accepted: false, reason: "missing_sequence", expectedChunkSeq: 2, receivedChunkSeq: 3 });
    expect(assistantDraftEntries(gap.state, "agent-a", 1)[0].text).toBe("A");
    const recoveredOnce = claimAssistantDraftGapRecovery(new Set(), {
      agentId: "agent-a", generation: 1, itemId: gap.itemId, attemptId: gap.attemptId, part: gap.part,
    });
    const duplicateRecovery = claimAssistantDraftGapRecovery(recoveredOnce.recoveryKeys, {
      agentId: "agent-a", generation: 1, itemId: gap.itemId, attemptId: gap.attemptId, part: gap.part,
    });
    expect(recoveredOnce.shouldRecover).toBe(true);
    expect(duplicateRecovery.shouldRecover).toBe(false);
  });

  it("resets a draft on a new attempt and ignores late events from the retired attempt", () => {
    let state = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 1, "old")).state;
    state = apply(state, delta("agent-a", "item-1", "attempt-2", "reasoning", 1, "new reasoning")).state;
    expect(assistantDraftEntries(state, "agent-a", 1).map(entry => [entry.kind, entry.text])).toEqual([
      ["reasoning", "new reasoning"],
    ]);

    const retired = apply(state, delta("agent-a", "item-1", "attempt-1", "content", 2, " late"));
    expect(retired.accepted).toBe(false);
    expect(retired.reason).toBe("retired_attempt");

    state = apply(state, delta("agent-a", "item-1", "attempt-2", "content", 1, "new content")).state;
    expect(assistantDraftEntries(state, "agent-a", 1).map(entry => entry.text)).toEqual(["new reasoning", "new content"]);
  });

  it("isolates drafts by Agent and hydrate generation", () => {
    let state = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 1, "A"), "agent-a", 7).state;
    state = apply(state, delta("agent-b", "item-1", "attempt-1", "content", 1, "B"), "agent-b", 7).state;
    expect(assistantDraftEntries(state, "agent-a", 7)[0].text).toBe("A");
    expect(assistantDraftEntries(state, "agent-b", 7)[0].text).toBe("B");
    state = apply(state, delta("agent-a", "item-1", "attempt-1", "content", 1, "new generation"), "agent-a", 8).state;

    expect(assistantDraftEntries(state, "agent-b", 7)[0].text).toBe("B");
    expect(assistantDraftEntries(state, "agent-a", 8)[0].text).toBe("new generation");
  });

  it("overlays partial snapshots and lets a final hydrate item replace the draft", () => {
    let state = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 1, "streamed")).state;
    const partial = { id: "item-1", item_id: "item-1", item_version: 1, kind: "assistant", text: "stale prefix", partial: true, data: {} };
    expect(projectAssistantDrafts([partial], state, "agent-a", 1)[0].text).toBe("streamed");

    const final = { ...partial, text: "authoritative final", partial: false };
    const reconciled = reconcileAssistantDrafts(state, "agent-a", 1, [final]);
    expect(reconciled.removedItemIds).toEqual(["item-1"]);
    expect(projectAssistantDrafts([final], reconciled.state, "agent-a", 1)[0]).toBe(final);
  });

  it("projects content-first deltas in Node hydrate order through final reconciliation", () => {
    let state = new Map();
    state = apply(state, delta("agent-a", "step-1/assistant/content", "attempt-1", "content", 1, "answer")).state;
    state = apply(state, delta("agent-a", "step-1/assistant/reasoning", "attempt-1", "reasoning", 1, "think")).state;
    const draftOrder = assistantDraftEntries(state, "agent-a", 1).map(entry => entry.item_id);
    const finalTranscript = [
      { id: "step-1/assistant/reasoning", item_id: "step-1/assistant/reasoning", item_version: 1, kind: "reasoning", text: "think", data: {} },
      { id: "step-1/assistant/content", item_id: "step-1/assistant/content", item_version: 1, kind: "assistant", text: "answer", data: {} },
    ];
    const reconciled = reconcileAssistantDrafts(state, "agent-a", 1, finalTranscript, { terminal: true });
    const finalOrder = projectAssistantDrafts(finalTranscript, reconciled.state, "agent-a", 1, { terminal: true }).map(entry => entry.item_id);

    expect(draftOrder).toEqual(["step-1/assistant/reasoning", "step-1/assistant/content"]);
    expect(finalOrder).toEqual(draftOrder);
  });

  it("retires matching partial drafts on a terminal hydrate snapshot", () => {
    let state = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 1, "partial")).state;
    const partial = { item_id: "item-1", kind: "assistant", text: "partial", partial: true };
    const reconciled = reconcileAssistantDrafts(state, "agent-a", 1, [partial], { terminal: true });

    expect(reconciled.removedItemIds).toEqual(["item-1"]);
    expect(projectAssistantDrafts([partial], reconciled.state, "agent-a", 1, { terminal: true })).toEqual([partial]);
  });

  it("uses a recovered partial hydrate item as the authority after a chunk gap", () => {
    let state = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 1, "A")).state;
    const recovered = { item_id: "item-1", kind: "assistant", text: "A full recovered body", partial: true };
    const reconciled = reconcileAssistantDrafts(state, "agent-a", 1, [recovered], {
      authoritativeItemIds: new Set(["item-1"]),
    });

    expect(reconciled.removedItemIds).toEqual(["item-1"]);
    expect(projectAssistantDrafts([recovered], reconciled.state, "agent-a", 1)[0]).toBe(recovered);
  });

  it("does not let a stale terminal lifecycle clear a draft carried beside a fresh transcript", () => {
    let state = apply(new Map(), delta("agent-a", "item-1", "attempt-1", "content", 1, "streamed answer")).state;
    const current = {
      history_revision: 4,
      transcript: [],
      turn_state: { lifecycle_seq: 10, terminal: false },
    };
    const incoming = {
      history_revision: 5,
      transcript: [{ item_id: "item-1", kind: "assistant", text: "fresh partial", partial: true }],
      turn_state: { lifecycle_seq: 9, terminal: true },
    };
    const merged = mergeHydrateSnapshot(current, incoming, { historyRevision: 4, lifecycleSeq: 10 });
    const reconciled = reconcileAssistantDrafts(state, "agent-a", 1, merged.snapshot.transcript, {
      terminal: Boolean(merged.snapshot.turn_state?.terminal),
    });

    expect(merged.transcriptAccepted).toBe(true);
    expect(merged.lifecycleAccepted).toBe(false);
    expect(merged.snapshot.turn_state.terminal).toBe(false);
    expect(reconciled.removedItemIds).toEqual([]);
    expect(projectAssistantDrafts(merged.snapshot.transcript, reconciled.state, "agent-a", 1)[0].text).toBe("streamed answer");
  });
});
