import { describe, expect, it } from "vitest";
import { createConversationStore } from "./conversationStore.js";

describe("conversation store ownership", () => {
  it("reduces active Node conversation events through one state boundary", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);

    const patch = store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "conversation_patch",
      data: { conversation_id: "agent-a", conversation_revision: 1, upsert_items: [], remove_item_ids: [], turn_state: { lifecycle_seq: 1, turn_id: "turn-1", generation: 1, phase: "model_generating" } },
    }, { agentId: "agent-a", generation: 1 });
    expect(patch).toMatchObject({ kind: "patch", accepted: true });

    const delta = store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "conversation_delta",
      data: {
        display_type: "delta",
        item_id: "step-1/assistant/content",
        attempt_id: "attempt-1",
        turn_id: "turn-1",
        part: "content",
        chunk_seq: 1,
        content: "draft",
      },
    }, { agentId: "agent-a", generation: 1 });
    expect(delta).toMatchObject({ kind: "draft", accepted: true });
    expect(store.items.entries[0].text).toBe("draft");

    const lifecycle = store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: {
        command: "model_request_started",
        phase: "model_generating",
        turn_id: "turn-1",
        generation: 1,
        lifecycle_seq: 2,
        step_id: "step-1",
      },
    }, { agentId: "agent-a", generation: 1 });
    expect(lifecycle).toMatchObject({
      kind: "lifecycle",
      retiredDrafts: { removedItemIds: ["step-1/assistant/content"] },
    });
  });

  it("rejects stale active Node events before changing conversation state", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 2);

    expect(store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "conversation_delta",
      data: { display_type: "delta", item_id: "stale", attempt_id: "attempt", part: "content", chunk_seq: 1, content: "stale" },
    }, { agentId: "agent-a", generation: 1 })).toMatchObject({ kind: "ignored", reason: "scope" });
    expect(store.applyActiveNodeEvent({
      agent_id: "agent-b",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "wrong-agent", generation: 1, lifecycle_seq: 1 },
    }, { agentId: "agent-a", generation: 2 })).toMatchObject({ kind: "ignored", reason: "scope" });
    expect(store.turnState.phase).toBe("idle");
    expect(store.items.entries).toHaveLength(0);
  });

  it("keeps draft and lifecycle projections isolated through sustained Agent switching", () => {
    const store = createConversationStore();
    const agentCount = 20;
    const cycles = 10;
    let generation = 0;
    let previousScope = null;

    for (let cycle = 0; cycle < cycles; cycle += 1) {
      for (let index = 0; index < agentCount; index += 1) {
        const agentId = `background-agent-${index + 1}`;
        generation += 1;
        const scope = { agentId, generation };
        const turnId = `${agentId}-turn-${cycle + 1}`;
        const itemId = `${turnId}/assistant/content`;
        store.setActiveScope(agentId, generation);
        store.resetTurnState();
        expect(store.applyActiveNodeEvent({
          agent_id: agentId,
          event_type: "turn_state",
          data: { phase: "model_generating", turn_id: turnId, generation: cycle + 1, lifecycle_seq: 1 },
        }, scope)).toMatchObject({ kind: "lifecycle", turnStateAccepted: true });
        expect(store.applyAssistantDraftDelta({
          agent_id: agentId,
          event_type: "conversation_delta",
          agent_seq: 0,
          data: {
            display_type: "delta",
            item_id: itemId,
            attempt_id: `attempt-${cycle + 1}`,
            turn_id: turnId,
            part: "content",
            chunk_seq: 1,
            content: `${agentId} reply ${cycle + 1}`,
          },
        }, scope)).toMatchObject({ accepted: true });

        if (previousScope) {
          expect(store.applyActiveNodeEvent({
            agent_id: previousScope.agentId,
            event_type: "turn_state",
            data: {
              phase: "failed",
              turn_id: `${previousScope.agentId}-stale-turn`,
              generation: previousScope.generation,
              lifecycle_seq: 99,
            },
          }, previousScope)).toMatchObject({ kind: "ignored", reason: "scope" });
        }

        expect(store.turnState.turnId).toBe(turnId);
        expect(store.assistantDraftText(agentId, generation, itemId)).toBe(`${agentId} reply ${cycle + 1}`);
        expect(store.items.entries.map(entry => entry.item_id)).toEqual([itemId]);
        previousScope = scope;
      }
    }
  });

  it("reports non-projected events without changing lifecycle state", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    const before = { ...store.turnState };

    for (const event_type of ["tool_result", "error", "unknown_event"]) {
      expect(store.applyActiveNodeEvent({
        agent_id: "agent-a",
        event_type,
        data: { lifecycle_seq: 99, phase: "failed", turn_id: "unprojected" },
      }, { agentId: "agent-a", generation: 1 })).toEqual({
        kind: "unprojected",
        accepted: false,
        stale: false,
      });
    }

    expect(store.turnState).toEqual(before);
  });

  it("marks stale lifecycle events as consumed without retiring current drafts", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 4);
    store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "turn-current", generation: 8, lifecycle_seq: 20, step_id: "step-current" },
    }, { agentId: "agent-a", generation: 4 });
    store.applyAssistantDraftDelta({
      agent_id: "agent-a",
      event_type: "conversation_delta",
      data: {
        display_type: "delta",
        item_id: "step-current/assistant/content",
        attempt_id: "attempt-current",
        turn_id: "turn-current",
        part: "content",
        chunk_seq: 1,
        content: "keep this draft",
      },
    }, { agentId: "agent-a", generation: 4 });

    const stale = store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { command: "model_request_started", step_id: "step-current", turn_id: "turn-current", generation: 8, lifecycle_seq: 19 },
    }, { agentId: "agent-a", generation: 4 });

    expect(stale).toMatchObject({ kind: "lifecycle", accepted: true, stale: true, turnStateAccepted: false });
    expect(store.assistantDraftText("agent-a", 4, "step-current/assistant/content")).toBe("keep this draft");
    expect(store.turnState.lifecycleSeq).toBe(20);
  });

  it("owns draft-gap recovery deduplication and clears it with the active conversation", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    const recovery = { agentId: "agent-a", generation: 1, itemId: "item-1", attemptId: "attempt-1", part: "content" };

    expect(store.claimDraftGapRecovery(recovery)).toEqual({ shouldRecover: true });
    expect(store.claimDraftGapRecovery(recovery)).toEqual({ shouldRecover: false });
    store.resetDrafts();
    expect(store.claimDraftGapRecovery(recovery)).toEqual({ shouldRecover: true });
    store.setActiveScope("agent-a", 2);
    expect(store.claimDraftGapRecovery(recovery)).toEqual({ shouldRecover: false, reason: "scope" });
  });

  it("owns an isolated turn lifecycle as the store's single authority", () => {
    const store = createConversationStore();
    const otherStore = createConversationStore();
    expect(store.turnState).not.toBe(otherStore.turnState);
    expect(store.turnState).toMatchObject({ phase: "idle" });

    store.setActiveScope("agent-a", 1);
    store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "turn-owned", generation: 1, lifecycle_seq: 1 },
    }, { agentId: "agent-a", generation: 1 });
    expect(store.turnState.turnId).toBe("turn-owned");
    expect(otherStore.turnState.phase).toBe("idle");

    store.resetTurnState();
    expect(store.turnState.phase).toBe("idle");
  });

  it("preserves queue pending count for control-only conversation patches", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    store.replaceHydrateSnapshot({
      conversation_epoch: 1,
      conversation_revision: 1,
      transcript: [],
      queue_pending: 3,
      queue_paused: false,
      queue_control_revision: 1,
    });

    const result = store.applyConversationPatch({
      agent_id: "agent-a",
      event_type: "conversation_patch",
      data: {
        conversation_id: "agent-a",
        conversation_revision: 2,
        queue_state: { paused: true, control_revision: 2 },
      },
    }, { agentId: "agent-a", generation: 1 });

    expect(result.accepted).toBe(true);
    expect(store.hydrate.value.queue_pending).toBe(3);
    expect(store.hydrate.value.queue_paused).toBe(true);
    expect(store.hydrate.value.queue_control_revision).toBe(2);
  });

  it("removes the prior selected round revision when a replacement patch arrives before hydrate", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    const oldHuman = { item_id: "old-human", id: "old-human", detail_item_id: "old-human-record", round_id: "round-1", round_revision_id: "revision-1", conversation_ordinal: 1, kind: "user", text: "old input" };
    const oldAssistant = { item_id: "old-answer", id: "old-answer", detail_item_id: "old-answer-record", round_id: "round-1", round_revision_id: "revision-1", conversation_ordinal: 2, kind: "assistant", text: "old answer" };
    store.replaceHydrateSnapshot({ conversation_epoch: 1, conversation_revision: 3, history_revision: 3, transcript: [oldHuman, oldAssistant] });
    store.replaceTranscriptReferences("agent-a", 1, [oldHuman, oldAssistant]);
    store.items.replaceSnapshot([oldHuman, oldAssistant]);

    const replacement = { item_id: "new-human", id: "new-human", detail_item_id: "new-human-record", round_id: "round-1", round_revision_id: "revision-2", conversation_ordinal: 1, kind: "user", text: "edited input" };
    const result = store.applyConversationPatch({
      agent_id: "agent-a",
      event_type: "conversation_patch",
      data: { conversation_id: "agent-a", conversation_revision: 4, history_revision: 4, upsert_items: [replacement], remove_item_ids: [] },
    }, { agentId: "agent-a", generation: 1 });

    expect(result.accepted).toBe(true);
    expect(store.hydrate.value.transcript.map(item => item.item_id)).toEqual(["new-human"]);
    expect(store.items.entries.map(item => item.item_id)).toEqual(["new-human"]);
  });

  it("projects authoritative hydrate HITL into one ordered conversation queue", () => {
    const store = createConversationStore();
    expect(store.pendingHitl.value).toBeNull();
    expect(store.hitlQueue.value).toEqual([]);

    const pending = {
      hitl_id: "batch-1",
      message: "Review these requests",
      items: [
        { hitl_type: "user_information", content: "Which environment?", user_information_args: { question: "Which environment?" } },
        { hitl_type: "memory_conflict", id: "memory-1", content: "Resolve the conflict" },
        { hitl_type: "execute_tool", id: "call-1", function: { name: "read_file", arguments: "{}" } },
      ],
    };
    store.replaceHydrateSnapshot({ pending_hitl: pending });

    expect(store.pendingHitl.value).toEqual(pending);
    expect(store.hitlQueue.value.map((item) => item.kind)).toEqual([
      "user_information",
      "memory_conflict",
      "approval",
    ]);
    expect(store.hitlQueue.value[2].data).toMatchObject({
      approval_id: "batch-1",
      message: "Review these requests",
      approval_args: { tool_calls: [pending.items[2]] },
    });

    store.replaceHydrateSnapshot({ pending_hitl: null });
    expect(store.pendingHitl.value).toBeNull();
    expect(store.hitlQueue.value).toEqual([]);
  });

  it("resets active projection on Agent generation changes and preserves per-Agent cursors", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    store.hydrate.value = { transcript: [{ item_id: "item-a" }] };
    store.syncStatus.value = { phase: "syncing", startedAt: 10, resultCommitStatus: "pending" };
    store.contentRevision.value = 5;
    store.conversationRevision.value = 7;
    store.latestHydrateTranscript.value = { agentId: "agent-a", transcript: [] };
    store.readingWindow.value = { anchorOrdinal: 3 };
    store.assistantDraftState.value = new Map([["agent-a", { generation: 1, itemsById: new Map([["draft-a", { parts: new Map([["content", { text: "partial" }]]) }]]) }]]);
    store.noteAppliedAgentSequence("agent-a", 12);
    store.markAgentSequenceProbed("agent-a", 13);
    store.mergeAgentWatermark("agent-a", { latestAgentSeq: 13 });
    store.items.replacePendingInputs("agent-a", [{ item_id: "agent-a/input/1", text: "queued" }]);
    store.items.replaceSnapshot([{ id: "item-a", item_id: "item-a", item_version: 1, kind: "assistant", text: "visible", data: {} }]);
    store.offerAgentEvent({ agent_id: "agent-a", agent_seq: 2, seq: 2 }, {
      agentId: "agent-a",
      generation: 1,
      appliedSequence: 0,
    });

    expect(store.setActiveScope("agent-b", 1)).toBe(true);
    expect(store.hydrate.value).toBeNull();
    expect(store.syncStatus.value).toEqual({ phase: "idle", startedAt: 0, resultCommitStatus: "" });
    expect(store.contentRevision.value).toBe(-1);
    expect(store.conversationRevision.value).toBe(-1);
    expect(store.latestHydrateTranscript.value).toBeNull();
    expect(store.readingWindow.value).toBeNull();
    expect(store.assistantDraftState.value.size).toBe(0);
    expect(store.items.entries).toHaveLength(0);
    expect(store.hasDeferredAgentEvents("agent-a", 1)).toBe(false);
    expect(store.appliedAgentSequence("agent-a")).toBe(12);
    expect(store.probedAgentSequence("agent-a")).toBe(13);
    expect(store.observedAgentWatermark("agent-a")).toEqual({
      conversationRevision: 0,
      latestAgentSeq: 13,
      resultCommitStatus: "unknown",
    });
    expect(store.hasAgentSequenceGap("agent-a")).toBe(true);
    expect(store.items.pendingInputsForAgent("agent-a")).toEqual([{ item_id: "agent-a/input/1", text: "queued" }]);
  });

  it("keeps the active projection when asked to select the same scope", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 4);
    store.hydrate.value = { transcript: [] };
    store.contentRevision.value = 9;
    store.assistantDraftState.value = new Map([["draft", { text: "still active" }]]);

    expect(store.setActiveScope("agent-a", 4)).toBe(false);
    expect(store.hydrate.value).toEqual({ transcript: [] });
    expect(store.contentRevision.value).toBe(9);
    expect(store.assistantDraftState.value.get("draft")).toEqual({ text: "still active" });
  });

  it("owns independent transcript and lifecycle revision fences for hydrate snapshots", () => {
    const store = createConversationStore();
    const current = {
      conversation_revision: 8,
      history_revision: 21,
      transcript: [{ item_id: "current" }],
      turn_state: { lifecycle_seq: 30, phase: "completed" },
      pending_hitl: null,
    };
    store.replaceHydrateSnapshot(current);
    store.contentRevision.value = 21;
    store.conversationRevision.value = 8;

    const incoming = {
      conversation_revision: 7,
      history_revision: 99,
      transcript: [{ item_id: "stale" }],
      turn_state: { lifecycle_seq: 31, phase: "waiting_user" },
      pending_hitl: { request_id: "new-request" },
    };
    const merged = store.mergeSnapshot(incoming, { lifecycleSeq: 30 });

    expect(merged.transcriptAccepted).toBe(false);
    expect(merged.lifecycleAccepted).toBe(true);
    expect(merged.snapshot.transcript).toEqual(current.transcript);
    expect(merged.snapshot.pending_hitl).toEqual(incoming.pending_hitl);
    expect(merged.historyRevision).toBe(21);
    expect(merged.conversationRevision).toBe(8);
    expect(store.contentRevision.value).toBe(21);
    expect(store.conversationRevision.value).toBe(8);
    store.replaceHydrateSnapshot(merged.snapshot);

    const newerTranscript = store.mergeSnapshot({
      conversation_revision: 9,
      history_revision: 20,
      transcript: [{ item_id: "newer" }],
      turn_state: { lifecycle_seq: 29, phase: "queued" },
    }, { lifecycleSeq: 31 });
    expect(newerTranscript.transcriptAccepted).toBe(true);
    expect(newerTranscript.lifecycleAccepted).toBe(false);
    expect(newerTranscript.snapshot.transcript).toEqual([{ item_id: "newer" }]);
    expect(newerTranscript.snapshot.turn_state).toEqual(incoming.turn_state);
    expect(store.contentRevision.value).toBe(21);
    expect(store.conversationRevision.value).toBe(9);
  });

  it("applies hydrate transcript merging and the reading window as one scoped store update", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 5);
    const oldRows = [
      { item_id: "row-1", item_version: 1, conversation_ordinal: 1, kind: "assistant", text: "one", data: {} },
      { item_id: "row-2", item_version: 1, conversation_ordinal: 2, kind: "assistant", text: "two", data: {} },
      { item_id: "row-3", item_version: 1, conversation_ordinal: 3, kind: "assistant", text: "old three", data: {} },
    ];
    store.replaceHydrateSnapshot({
      conversation_epoch: 8,
      conversation_revision: 11,
      history_revision: 21,
      transcript: oldRows,
      turn_state: { lifecycle_seq: 30, terminal: true },
    });
    store.replaceTranscriptReferences("agent-a", 5, oldRows);
    store.setReadingWindow({ anchorOrdinal: 2, endOrdinal: 3, conversationEpoch: 8 });

    const incomingRows = [
      { item_id: "row-3", item_version: 2, conversation_ordinal: 3, kind: "assistant", text: "updated three", data: {} },
      { item_id: "row-4", item_version: 1, conversation_ordinal: 4, kind: "assistant", text: "four", data: {} },
    ];
    const result = store.applyHydrateSnapshot({
      conversation_epoch: 8,
      conversation_revision: 12,
      history_revision: 22,
      transcript: incomingRows,
      turn_state: { lifecycle_seq: 31, terminal: true },
    }, { agentId: "agent-a", generation: 5, lifecycleSeq: 30 });

    expect(result).toMatchObject({ accepted: true, stale: false, transcriptAccepted: true, lifecycleAccepted: true });
    expect(store.hydrate.value.transcript.map(row => row.item_id)).toEqual(["row-2", "row-3", "row-4"]);
    expect(store.hydrate.value.transcript[0]).toMatchObject(oldRows[1]);
    expect(store.hydrate.value.transcript[1]).toMatchObject(incomingRows[0]);
    expect(store.latestTranscriptFor("agent-a", 8).transcript).toEqual(incomingRows);
    expect(store.contentRevision.value).toBe(22);
    expect(store.conversationRevision.value).toBe(12);
  });

  it("reconciles a covered ephemeral draft even when the transcript snapshot is stale", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 5);
    store.applyHydrateSnapshot({
      conversation_revision: 4,
      history_revision: 9,
      transcript: [],
      turn_state: { lifecycle_seq: 10, phase: "model_generating", terminal: false },
    }, { agentId: "agent-a", generation: 5, lifecycleSeq: 10 });
    const delta = {
      agent_id: "agent-a", agent_seq: 0, seq: 20, event_type: "conversation_delta",
      data: {
        display_type: "delta", item_id: "step-1/assistant/content", attempt_id: "attempt-1",
        turn_id: "turn-1", part: "content", chunk_seq: 1, content: "first",
      },
    };
    store.applyAssistantDraftDelta(delta, { agentId: "agent-a", generation: 5 });
    const stableRow = store.items.entries[0];
    const result = store.applyHydrateSnapshot({
      conversation_revision: 3,
      history_revision: 8,
      transcript: [],
      turn_state: { lifecycle_seq: 9, phase: "queued", terminal: false },
      covered_agent_seq: 0,
      covered_stream_seq: 20,
      assistant_drafts: [{
        item_id: "step-1/assistant/content", attempt_id: "attempt-1", turn_id: "turn-1",
        part: "content", text: "full snapshot body", through_chunk_seq: 2,
        through_agent_seq: 0, through_stream_seq: 20,
      }],
    }, {
      agentId: "agent-a", generation: 5, lifecycleSeq: 10, reconcileCoveredDrafts: true,
    });

    expect(result).toMatchObject({ accepted: false, transcriptAccepted: false, lifecycleAccepted: false });
    expect(store.assistantDraftText("agent-a", 5, "step-1/assistant/content")).toBe("full snapshot body");
    expect(store.items.entries[0]).toBe(stableRow);
    expect(store.items.entries[0].text).toBe("full snapshot body");
  });

  it("owns active turn event routing and rejects a stale Agent generation", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 3);
    const event = {
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "turn-1", generation: 4, lifecycle_seq: 8 },
    };

    expect(store.applyActiveNodeEvent(event, { agentId: "agent-a", generation: 2 })).toMatchObject({ kind: "ignored", reason: "scope" });
    expect(store.turnState.phase).toBe("idle");
    expect(store.applyActiveNodeEvent(event, { agentId: "agent-a", generation: 3 })).toMatchObject({ kind: "lifecycle", turnStateAccepted: true });
    expect(store.turnState).toMatchObject({ phase: "model_generating", turnId: "turn-1", lifecycleSeq: 8 });
    expect(store.applyActiveNodeEvent({ ...event, agent_id: "agent-b" }, { agentId: "agent-a", generation: 3 })).toMatchObject({ kind: "ignored", reason: "scope" });
    expect(store.turnState.turnId).toBe("turn-1");
  });

  it("applies committed transcript patches by item version and suppresses covered terminal hydration", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 7);
    const first = { id: "row-1", item_id: "row-1", item_version: 1, conversation_ordinal: 1, kind: "user", text: "keep", data: {} };
    const priorAnswer = { id: "answer-1", item_id: "answer-1", item_version: 1, conversation_ordinal: 2, conversation_entry_index: 0, kind: "assistant", text: "draft answer", data: {} };
    store.replaceHydrateSnapshot({
      conversation_epoch: 3,
      conversation_revision: 4,
      history_revision: 9,
      transcript: [first, priorAnswer],
      turn_state: { phase: "model_generating", turn_id: "turn-1", generation: 2, lifecycle_seq: 7 },
      pending_hitl: null,
    });
    store.replaceTranscriptReferences("agent-a", 7, [first, priorAnswer]);
    store.items.replaceSnapshot([first, priorAnswer]);
    const stablePrefix = store.items.entries[0];
    const liveDraft = store.applyAssistantDraftDelta({
      agent_id: "agent-a", agent_seq: 0, seq: 12, event_type: "conversation_delta",
      data: {
        display_type: "delta", item_id: "answer-1", attempt_id: "turn-1/step-1/attempt/1",
        turn_id: "turn-1", part: "content", chunk_seq: 1, content: " streamed",
      },
    }, { agentId: "agent-a", generation: 7 });
    expect(liveDraft.accepted).toBe(true);
    const patch = {
      agent_id: "agent-a",
      agent_seq: 13,
      event_type: "conversation_patch",
      data: {
        conversation_id: "agent-a",
        conversation_revision: 5,
        history_revision: 10,
        context_revision: 3,
        result_commit_status: "committed",
        upsert_items: [
          { id: "answer-1", item_id: "answer-1", item_version: 2, conversation_ordinal: 2, conversation_entry_index: 0, kind: "assistant", text: "final answer", data: {} },
          { id: "row-3", item_id: "row-3", item_version: 1, conversation_ordinal: 3, conversation_entry_index: 0, kind: "assistant", text: "next", data: {} },
        ],
        remove_item_ids: [],
        input_updates_complete: true,
        input_updates: [],
        queue_state: { pending_count: 0, paused: false, control_revision: 4 },
        pending_hitl: null,
        turn_state: { phase: "completed", terminal: true, turn_id: "turn-1", generation: 2, lifecycle_seq: 10 },
      },
    };

    expect(store.applyConversationPatch(patch, { agentId: "agent-a", generation: 7 })).toMatchObject({ accepted: true, structuralChanged: true, draftReconciliation: { removedItemIds: ["answer-1"] } });
    expect(store.items.entries.map(row => row.item_id)).toEqual(["row-1", "answer-1", "row-3"]);
    expect(store.items.entries[0]).toBe(stablePrefix);
    expect(store.items.entries[1].text).toBe("final answer");
    expect(store.hydrate.value.transcript.map(row => row.item_id)).toEqual(["row-1", "answer-1", "row-3"]);
    expect(store.hydrate.value.conversation_revision).toBe(5);
    expect(store.hydrate.value.context_revision).toBe(3);
    expect(store.hydrate.value.pending_inputs).toEqual([]);
    expect(store.hydrate.value.queue_pending).toBe(0);
    expect(store.hydrate.value.queue_control_revision).toBe(4);
    expect(store.pendingHitl.value).toBeNull();
    expect(store.turnState).toMatchObject({ phase: "completed", turnId: "turn-1", lifecycleSeq: 10 });
    expect(store.assistantDraftState.value.get("agent-a").itemsById.size).toBe(0);
    expect(store.applyAssistantDraftDelta({
      agent_id: "agent-a", agent_seq: 0, seq: 14, event_type: "conversation_delta",
      data: {
        display_type: "delta", item_id: "answer-1", attempt_id: "turn-1/step-1/attempt/1",
        turn_id: "turn-1", part: "content", chunk_seq: 2, content: " too late",
      },
    }, { agentId: "agent-a", generation: 7 })).toMatchObject({ accepted: false, reason: "retired_turn" });
    expect(store.conversationPatchCoversEvent({
      agent_id: "agent-a", event_type: "turn_state",
      data: { turn_id: "turn-1", generation: 2, lifecycle_seq: 10, phase: "completed" },
    }, { agentId: "agent-a", generation: 7 })).toBe(true);
    expect(store.conversationPatchCoversEvent({
      agent_id: "agent-a", event_type: "turn_finished",
      data: { turn_id: "turn-1", turn_generation: 2, turn_complete: true },
    }, { agentId: "agent-a", generation: 7 })).toBe(true);
    expect(store.applyConversationPatch({ ...patch, data: { ...patch.data, conversation_revision: 4 } }, { agentId: "agent-a", generation: 7 }))
      .toMatchObject({ accepted: false, stale: true });
  });

  it("applies a newer transcript patch without regressing lifecycle or HITL state", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 8);
    const pending = {
      hitl_id: "pending-current",
      message: "Review the request",
      items: [{ hitl_type: "user_information", content: "Confirm this step" }],
    };
    const currentTurn = {
      phase: "tool_waiting",
      turn_id: "turn-current",
      generation: 4,
      lifecycle_seq: 20,
      interaction_kind: "approval",
    };
    store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: currentTurn,
    }, { agentId: "agent-a", generation: 8 });
    store.replaceHydrateSnapshot({
      conversation_revision: 4,
      history_revision: 6,
      transcript: [{ item_id: "old", kind: "assistant", text: "before", data: {} }],
      turn_state: currentTurn,
      pending_hitl: pending,
    });
    store.conversationRevision.value = 4;
    store.contentRevision.value = 6;

    const result = store.applyConversationPatch({
      agent_id: "agent-a",
      event_type: "conversation_patch",
      data: {
        conversation_id: "agent-a",
        conversation_revision: 5,
        history_revision: 7,
        upsert_items: [{ item_id: "new", id: "new", item_version: 1, conversation_ordinal: 2, kind: "assistant", text: "committed", data: {} }],
        remove_item_ids: [],
        pending_hitl: null,
        turn_state: {
          phase: "completed",
          terminal: true,
          turn_id: "turn-stale",
          generation: 3,
          lifecycle_seq: 19,
        },
      },
    }, { agentId: "agent-a", generation: 8 });

    expect(result).toMatchObject({ accepted: true, turnStateAccepted: false });
    expect(store.hydrate.value.transcript.map(item => item.item_id)).toEqual(["old", "new"]);
    expect(store.hydrate.value.turn_state).toEqual(currentTurn);
    expect(store.pendingHitl.value).toEqual(pending);
    expect(store.turnState).toMatchObject({ phase: "tool_waiting", turnId: "turn-current", lifecycleSeq: 20 });
  });

  it("applies a tail transcript patch without revisiting the stable prefix", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 5);
    const transcript = Array.from({ length: 10_000 }, (_, index) => ({
      item_id: `item-${index + 1}`,
      id: `item-${index + 1}`,
      item_version: 1,
      conversation_ordinal: index + 1,
      conversation_entry_index: 0,
      kind: "assistant",
      text: `stable ${index + 1}`,
      data: {},
    }));
    store.applyHydrateSnapshot({
      conversation_epoch: 1,
      conversation_revision: 1,
      history_revision: 1,
      transcript,
    }, { agentId: "agent-a", generation: 5 });
    const stablePrefix = store.hydrate.value.transcript[0];
    const liveDraft = store.applyAssistantDraftDelta({
      agent_id: "agent-a",
      event_type: "conversation_delta",
      data: {
        display_type: "delta",
        item_id: "live/assistant/content",
        attempt_id: "attempt-1",
        turn_id: "turn-1",
        part: "content",
        chunk_seq: 1,
        content: "in progress",
      },
    }, { agentId: "agent-a", generation: 5 });
    expect(liveDraft.accepted).toBe(true);
    const liveDraftRow = store.items.entries.find(entry => entry.item_id === "live/assistant/content");
    expect(liveDraftRow).toMatchObject({ streaming: true, partial: true, attempt_id: "attempt-1" });

    const result = store.applyConversationPatch({
      agent_id: "agent-a",
      event_type: "conversation_patch",
      data: {
        conversation_id: "agent-a",
        conversation_revision: 2,
        history_revision: 2,
        upsert_items: [{
          item_id: "item-10001",
          id: "item-10001",
          item_version: 1,
          conversation_ordinal: 10001,
          conversation_entry_index: 0,
          kind: "assistant",
          text: "new tail",
          data: {},
        }],
        remove_item_ids: [],
      },
    }, { agentId: "agent-a", generation: 5 });

    expect(result).toMatchObject({ accepted: true, transcriptRowsVisited: 1 });
    expect(store.hydrate.value.transcript).toHaveLength(10_001);
    expect(store.hydrate.value.transcript[0]).toBe(stablePrefix);
    expect(store.hydrate.value.transcript.at(-1).item_id).toBe("item-10001");

    const update = store.applyConversationPatch({
      agent_id: "agent-a",
      event_type: "conversation_patch",
      data: {
        conversation_id: "agent-a",
        conversation_revision: 3,
        history_revision: 3,
        upsert_items: [{
          item_id: "item-10001",
          id: "item-10001",
          item_version: 2,
          conversation_ordinal: 10001,
          conversation_entry_index: 0,
          kind: "assistant",
          text: "corrected tail",
          data: {},
        }],
        remove_item_ids: [],
      },
    }, { agentId: "agent-a", generation: 5 });
    expect(update).toMatchObject({ accepted: true, transcriptRowsVisited: 0 });
    expect(store.hydrate.value.transcript[0]).toBe(stablePrefix);
    expect(store.hydrate.value.transcript.at(-1).text).toBe("corrected tail");
    expect(store.assistantDraftText("agent-a", 5, "live/assistant/content")).toBe("in progress");

    store.applyConversationPatch({
      agent_id: "agent-a",
      event_type: "conversation_patch",
      data: {
        conversation_id: "agent-a",
        conversation_revision: 4,
        history_revision: 4,
        upsert_items: [{
          item_id: "live/assistant/content",
          id: "live/assistant/content",
          item_version: 1,
          conversation_ordinal: 10002,
          conversation_entry_index: 0,
          kind: "assistant",
          text: "completed draft",
          data: {},
        }],
        remove_item_ids: [],
      },
    }, { agentId: "agent-a", generation: 5 });
    expect(store.assistantDraftText("agent-a", 5, "live/assistant/content")).toBe("");
    expect(store.items.entries.find(entry => entry.item_id === "live/assistant/content")).toBe(liveDraftRow);
    for (const key of ["streaming", "partial", "attempt_id", "chunk_seq", "agent_seq", "stream_seq"]) {
      expect(liveDraftRow).not.toHaveProperty(key);
    }
  });

  it("projects valid HITL SSE immediately and fences older events and hydrates", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 2);
    store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "tool_waiting", turn_id: "turn-1", generation: 1, lifecycle_seq: 40 },
    }, { agentId: "agent-a", generation: 2 });
    const event = {
      agent_id: "agent-a",
      event_type: "hitl_required",
      data: {
        event_seq: 41,
        hitl_id: "hitl-41",
        message: "Please confirm",
        display_type: "normal_text",
        items: [{ hitl_type: "user_information", content: "Confirm this step" }],
      },
    };

    expect(store.applyActiveNodeEvent(event, { agentId: "agent-a", generation: 2 }))
      .toMatchObject({ kind: "hitl", accepted: true, lifecycleSeq: 41 });
    expect(store.pendingHitl.value).toMatchObject({ hitl_id: "hitl-41", message: "Please confirm" });
    expect(store.hitlQueue.value).toHaveLength(1);
    expect(store.turnState.lifecycleSeq).toBe(41);

    expect(store.applyActiveNodeEvent({
      ...event,
      data: { ...event.data, event_seq: 40, hitl_id: "hitl-old" },
    }, { agentId: "agent-a", generation: 2 })).toMatchObject({ accepted: false, stale: true, reason: "stale_sequence" });
    expect(store.pendingHitl.value.hitl_id).toBe("hitl-41");

    const staleHydrate = store.applyHydrateSnapshot({
      conversation_revision: 1,
      history_revision: 1,
      transcript: [],
      turn_state: { phase: "tool_waiting", turn_id: "turn-1", generation: 1, lifecycle_seq: 40 },
      pending_hitl: null,
    }, { agentId: "agent-a", generation: 2, lifecycleSeq: store.turnState.lifecycleSeq });
    expect(staleHydrate.lifecycleAccepted).toBe(false);
    expect(store.pendingHitl.value.hitl_id).toBe("hitl-41");
  });

  it("requests authoritative hydrate fallback for HITL SSE without a valid lifecycle sequence", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);

    expect(store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "hitl_required",
      data: { hitl_id: "hitl-missing-seq", items: [{ hitl_type: "user_information" }] },
    }, { agentId: "agent-a", generation: 1 })).toMatchObject({
      kind: "hitl",
      accepted: false,
      reason: "missing_sequence",
    });
    expect(store.pendingHitl.value).toBeNull();
  });

  it("derives the composer generation channel from accepted assistant deltas", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "turn-a", generation: 1, lifecycle_seq: 1 },
    }, { agentId: "agent-a", generation: 1 });

    expect(store.applyAssistantDraftDelta({
      agent_id: "agent-a", event_type: "conversation_delta", agent_seq: 0, seq: 1,
      data: {
        display_type: "reasoning", item_id: "step-1/assistant/reasoning", attempt_id: "attempt-1",
        turn_id: "turn-a", part: "reasoning", chunk_seq: 1, content: "先分析",
      },
    }, { agentId: "agent-a", generation: 1 }).accepted).toBe(true);
    expect(store.turnState.outputChannel).toBe("reasoning");

    expect(store.applyAssistantDraftDelta({
      agent_id: "agent-a", event_type: "conversation_delta", agent_seq: 0, seq: 2,
      data: {
        display_type: "delta", item_id: "step-1/assistant/content", attempt_id: "attempt-1",
        turn_id: "turn-a", part: "content", chunk_seq: 1, content: "答案",
      },
    }, { agentId: "agent-a", generation: 1 }).accepted).toBe(true);
    expect(store.turnState.outputChannel).toBe("assistant");
  });

  it("keeps newer transcript while rejecting a stale hydrate lifecycle in the same store update", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 6);
    store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "turn-current", generation: 9, lifecycle_seq: 20 },
    }, { agentId: "agent-a", generation: 6 });
    const previousSnapshot = {
      conversation_revision: 30,
      history_revision: 40,
      transcript: [{ item_id: "old", kind: "assistant", text: "old", data: {} }],
      turn_state: { phase: "model_generating", turn_id: "turn-current", generation: 9, lifecycle_seq: 20 },
      pending_hitl: null,
    };
    store.replaceHydrateSnapshot(previousSnapshot);
    store.contentRevision.value = 40;
    store.conversationRevision.value = 30;

    const incoming = {
      conversation_revision: 31,
      history_revision: 41,
      transcript: [{ item_id: "new", kind: "assistant", text: "new", data: {} }],
      turn_state: { phase: "completed", turn_id: "turn-stale", generation: 8, lifecycle_seq: 19 },
      pending_hitl: { request_id: "stale-request" },
    };
    const merged = store.applyHydrateSnapshot(incoming, {
      agentId: "agent-a",
      generation: 6,
      lifecycleSeq: store.turnState.lifecycleSeq,
    });
    expect(merged.transcriptAccepted).toBe(true);
    expect(merged.lifecycleAccepted).toBe(false);
    expect(store.hydrate.value.transcript).toEqual(incoming.transcript);
    expect(store.hydrate.value.turn_state).toEqual(previousSnapshot.turn_state);
    expect(store.hydrate.value.pending_hitl).toBeNull();
    expect(store.turnState.turnId).toBe("turn-current");
  });

  it("rejects a same-sequence conflicting turn identity before exposing lifecycle companion fields", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 3);
    store.applyActiveNodeEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "turn-current", generation: 4, lifecycle_seq: 20 },
    }, { agentId: "agent-a", generation: 3 });
    const current = {
      conversation_revision: 30,
      history_revision: 40,
      transcript: [{ item_id: "old", kind: "assistant", text: "stable", data: {} }],
      turn_state: { phase: "model_generating", turn_id: "turn-current", generation: 4, lifecycle_seq: 20 },
      pending_hitl: null,
      child_agents: [{ agent_id: "child-current" }],
    };
    store.replaceHydrateSnapshot(current);
    store.contentRevision.value = 40;
    store.conversationRevision.value = 30;

    const result = store.applyHydrateSnapshot({
      conversation_revision: 31,
      history_revision: 41,
      transcript: [{ item_id: "new", kind: "assistant", text: "new content", data: {} }],
      turn_state: { phase: "waiting_user", turn_id: "turn-other", generation: 5, lifecycle_seq: 20 },
      pending_hitl: { hitl_id: "stale-hitl", items: [{ hitl_type: "execute_tool" }] },
      child_agents: [{ agent_id: "child-stale" }],
    }, { agentId: "agent-a", generation: 3, lifecycleSeq: store.turnState.lifecycleSeq });

    expect(result).toMatchObject({ transcriptAccepted: true, lifecycleAccepted: false, accepted: true });
    expect(store.hydrate.value.transcript[0].item_id).toBe("new");
    expect(store.hydrate.value.turn_state).toEqual(current.turn_state);
    expect(store.hydrate.value.pending_hitl).toBeNull();
    expect(store.hydrate.value.child_agents).toEqual(current.child_agents);
    expect(store.turnState).toMatchObject({ turnId: "turn-current", generation: 4, lifecycleSeq: 20 });
  });

  it("rejects stale hydrate and media restoration updates without replacing the active transcript", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 2);
    const current = {
      conversation_epoch: 3,
      conversation_revision: 7,
      history_revision: 9,
      transcript: [{ item_id: "current", kind: "assistant", text: "current", data: {} }],
      turn_state: { lifecycle_seq: 4, terminal: true },
    };
    store.replaceHydrateSnapshot(current);

    expect(store.applyHydrateSnapshot({ transcript: [{ item_id: "stale" }] }, {
      agentId: "agent-a",
      generation: 1,
    })).toMatchObject({ accepted: false, stale: true });
    expect(store.applyMediaTranscript("agent-a", 2, {
      conversationEpoch: 3,
      historyRevision: 8,
      expectedConversationRevision: 7,
      transcript: [{ item_id: "old-media" }],
    })).toBe(false);
    expect(store.applyMediaTranscript("agent-a", 2, {
      conversationEpoch: 2,
      historyRevision: 9,
      expectedConversationRevision: 7,
      transcript: [{ item_id: "old-epoch" }],
    })).toBe(false);
    expect(store.hydrate.value).toEqual(current);
  });

  it("applies restored media to a committed patch while retaining the complete latest transcript", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 2);
    const first = { item_id: "user-1", kind: "user", text: "first", images: [], data: {} };
    const second = { item_id: "user-2", kind: "user", text: "second", images: ["dagents-media://media-2"], data: {} };
    store.replaceHydrateSnapshot({
      conversation_epoch: 3,
      conversation_revision: 7,
      history_revision: 9,
      transcript: [first, second],
    });
    store.setLatestHydrateTranscript("agent-a", 2, 3, [first, second]);

    const restored = { ...second, images: ["blob:desktop/media-2"] };
    const latest = store.latestTranscriptFor("agent-a", 3).transcript;
    expect(store.applyMediaTranscript("agent-a", 2, {
      conversationEpoch: 3,
      historyRevision: 9,
      expectedConversationRevision: 7,
      transcript: latest.map(entry => entry.item_id === "user-2" ? restored : entry),
    })).toBe(true);

    expect(store.hydrate.value.transcript.map(entry => entry.item_id)).toEqual(["user-1", "user-2"]);
    expect(store.hydrate.value.transcript[1].images).toEqual(["blob:desktop/media-2"]);
    expect(store.latestTranscriptFor("agent-a", 3).transcript).toHaveLength(2);
  });

  it("applies stream deltas and draft retirement through the conversation store", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    const delta = (chunkSeq, content, attemptId = "attempt-1") => ({
      agent_id: "agent-a",
      agent_seq: 0,
      seq: chunkSeq,
      event_type: "conversation_delta",
      data: {
        display_type: "delta",
        item_id: "step-1/assistant/content",
        attempt_id: attemptId,
        turn_id: "turn-1",
        part: "content",
        chunk_seq: chunkSeq,
        content,
      },
    });

    const first = store.applyAssistantDraftDelta(delta(1, "Hello"), { agentId: "agent-a", generation: 1 });
    const row = store.items.entries[0];
    expect(first.accepted).toBe(true);
    expect(first.itemUpdate.inserted).toBe(true);
    expect(store.hasAssistantDraftOutput("agent-a", 1)).toBe(true);
    expect(store.assistantDraftText("agent-a", 1)).toBe("Hello");

    const revision = store.items.revision.value;
    const second = store.applyAssistantDraftDelta(delta(2, " world"), { agentId: "agent-a", generation: 1 });
    expect(second.accepted).toBe(true);
    expect(second.itemUpdate.inserted).toBe(false);
    expect(store.items.entries[0]).toBe(row);
    expect(row.text).toBe("Hello world");
    expect(store.assistantDraftText("agent-a", 1)).toBe("Hello world");
    expect(store.items.revision.value).toBe(revision);

    const retry = store.applyAssistantDraftDelta(delta(1, "Retry", "attempt-2"), { agentId: "agent-a", generation: 1 });
    expect(retry.accepted).toBe(true);
    expect(row.text).toBe("Retry");
    expect(store.assistantDraftText("agent-a", 1)).toBe("Retry");

    const retired = store.reconcileDrafts("agent-a", 1, [
      { item_id: "step-1/assistant/content", partial: false, kind: "assistant", text: "Final" },
    ], { terminal: true });
    expect(retired.removedItemIds).toEqual(["step-1/assistant/content"]);
    expect(store.assistantDraftText("agent-a", 1)).toBe("");
    expect(store.hasAssistantDraftOutput("agent-a", 1)).toBe(false);
    expect(store.assistantDraftState.value.get("agent-a").itemsById.size).toBe(0);
  });

  it("rejects stale Agent and generation draft events before they reach visible items", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    store.setActiveScope("agent-a", 2);
    const staleEvent = {
      agent_id: "agent-a",
      agent_seq: 1,
      event_type: "conversation_delta",
      data: {
        display_type: "delta",
        item_id: "step-old/assistant/content",
        attempt_id: "attempt-old",
        part: "content",
        chunk_seq: 1,
        content: "must be ignored",
      },
    };

    const result = store.applyAssistantDraftDelta(staleEvent, { agentId: "agent-a", generation: 1 });
    expect(result).toMatchObject({ accepted: false, reason: "scope" });
    expect(store.items.entries).toHaveLength(0);
    expect(store.assistantDraftState.value.size).toBe(0);
  });

  it("owns per-Agent sequence advancement, gap recovery and snapshot coverage", () => {
    const store = createConversationStore();
    const first = store.noteAppliedAgentSequence("agent-a", 4);
    expect(first).toMatchObject({ sequence: 4, gap: true, advanced: true, gapWasPresent: false });
    expect(store.hasAgentSequenceGap("agent-a")).toBe(true);

    const contiguous = store.noteAppliedAgentSequence("agent-a", 5);
    expect(contiguous).toMatchObject({ sequence: 5, gap: false, advanced: true, gapWasPresent: true });
    // A later contiguous event must not clear an earlier gap; only reconciliation does.
    expect(store.hasAgentSequenceGap("agent-a")).toBe(true);
    store.clearAgentSequenceGap("agent-a");
    expect(store.hasAgentSequenceGap("agent-a")).toBe(false);

    expect(store.applyAgentSequenceCoverage("agent-a", 8)).toBe(8);
    expect(store.noteAppliedAgentSequence("agent-a", 7).sequence).toBe(8);
    expect(store.applyAgentSequenceCoverage("agent-a", 3)).toBe(8);
    expect(store.appliedAgentSequence("agent-a")).toBe(8);
  });

  it("keeps watermark observations monotonic within an epoch and resets all cursors on restart", () => {
    const store = createConversationStore();
    store.markAgentSequenceProbed("agent-a", 9);
    store.mergeAgentWatermark("agent-a", {
      conversationRevision: 7,
      latestAgentSeq: 9,
      resultCommitStatus: "pending",
    });
    store.mergeAgentWatermark("agent-a", {
      conversationRevision: 5,
      latestAgentSeq: 4,
      resultCommitStatus: "committed",
    });
    expect(store.observedAgentWatermark("agent-a")).toEqual({
      conversationRevision: 7,
      latestAgentSeq: 9,
      resultCommitStatus: "committed",
    });

    store.noteAppliedAgentSequence("agent-a", 12);
    store.markAgentSequenceProbed("agent-a", 13);
    store.mergeAgentWatermark("agent-a", { conversationRevision: 7, latestAgentSeq: 13 });
    store.offerAgentEvent({ agent_id: "agent-a", agent_seq: 15, seq: 15 }, {
      agentId: "agent-a",
      generation: 1,
      appliedSequence: 12,
    });
    expect(store.hasDeferredAgentEvents("agent-a", 1)).toBe(true);

    store.resetAgentSequences();
    expect(store.appliedAgentSequence("agent-a")).toBe(0);
    expect(store.probedAgentSequence("agent-a")).toBe(0);
    expect(store.observedAgentWatermark("agent-a")).toBeNull();
    expect(store.hasAgentSequenceGap("agent-a")).toBe(false);
    expect(store.hasDeferredAgentEvents("agent-a", 1)).toBe(false);
    expect(store.mergeAgentWatermark("agent-a", { conversationRevision: 1, latestAgentSeq: 2 }, { sameEpoch: false })).toEqual({
      conversationRevision: 1,
      latestAgentSeq: 2,
      resultCommitStatus: "unknown",
    });
  });

  it("keeps transcript reference reuse scoped to the active Agent generation", () => {
    const store = createConversationStore();
    const row = (text) => ({ item_id: "same-id", item_version: 1, id: "same-id", text, data: {} });
    const first = row("agent-a generation 1");
    store.setActiveScope("agent-a", 1);
    store.mergeTranscriptReferences("agent-a", 1, [first]);

    store.setActiveScope("agent-b", 2);
    const otherAgent = row("agent-b");
    const second = store.mergeTranscriptReferences("agent-b", 2, [otherAgent]);
    expect(second.transcript[0]).toBe(otherAgent);
    expect(second.transcript[0]).not.toBe(first);

    store.setActiveScope("agent-a", 3);
    const nextGeneration = row("agent-a generation 3");
    const third = store.mergeTranscriptReferences("agent-a", 3, [nextGeneration]);
    expect(third.transcript[0]).toBe(nextGeneration);
    expect(store.replaceTranscriptReferences("agent-b", 2, [otherAgent])).toBe(false);
  });

  it("owns hydrate status transitions and fences latest transcript by active scope", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    store.beginSync(123);
    expect(store.syncStatus.value).toEqual({ phase: "pending", startedAt: 123, resultCommitStatus: "" });
    expect(store.markSyncing()).toBe(true);
    expect(store.markSyncing()).toBe(false);
    store.failSync();
    expect(store.syncStatus.value.phase).toBe("failed");
    expect(store.clearFailedSync()).toBe(true);
    expect(store.syncStatus.value).toEqual({ phase: "idle", startedAt: 0, resultCommitStatus: "" });
    store.completeSync("committed");
    expect(store.syncStatus.value).toEqual({ phase: "idle", startedAt: 0, resultCommitStatus: "committed" });

    const transcript = [{ item_id: "latest" }];
    expect(store.setLatestHydrateTranscript("agent-a", 1, 4, transcript)).toBe(true);
    expect(store.latestTranscriptFor("agent-a", 4)).toEqual({
      agentId: "agent-a",
      conversationEpoch: 4,
      transcript,
    });
    expect(store.latestTranscriptFor("agent-a", 3)).toBeNull();
    expect(store.setLatestHydrateTranscript("agent-a", 0, 5, [])).toBe(false);
    store.setActiveScope("agent-b", 2);
    expect(store.latestTranscriptFor("agent-a", 4)).toBeNull();
    expect(store.setReadingWindow({ anchorOrdinal: 8 })).toEqual({ anchorOrdinal: 8 });
    expect(store.setReadingWindow(null)).toBeNull();
  });

  it("owns reading-window movement and restores the latest transcript when following the tail", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 4);
    const latest = [{ item_id: "latest-row", conversation_ordinal: 8, kind: "assistant", text: "latest", data: {} }];
    store.replaceHydrateSnapshot({ conversation_epoch: 6, transcript: [{ item_id: "older-row" }] });
    store.setLatestHydrateTranscript("agent-a", 4, 6, latest);

    expect(store.applyReadingWindowChange("agent-a", 4, {
      following: false,
      anchorOrdinal: 5,
      endOrdinal: 8,
    }, 6)).toMatchObject({ accepted: true, restoredLatest: false });
    expect(store.readingWindow.value).toEqual({ anchorOrdinal: 5, endOrdinal: 8, conversationEpoch: 6 });

    expect(store.applyReadingWindowChange("agent-a", 4, { following: true }, 6)).toMatchObject({
      accepted: true,
      restoredLatest: true,
    });
    expect(store.readingWindow.value).toBeNull();
    expect(store.hydrate.value.transcript).toEqual(latest);
    expect(store.latestTranscriptFor("agent-a", 6).transcript).toEqual(latest);
    expect(store.applyReadingWindowChange("agent-a", 3, { following: true }, 6)).toMatchObject({
      accepted: false,
      restoredLatest: false,
    });
  });

  it("restores each Agent's stable reading anchor when switching back", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    const anchor = {
      anchorOrdinal: 5,
      endOrdinal: 8,
      conversationEpoch: 6,
      anchorItemKey: "tool-step-call-9",
      anchorOffset: 42.5,
    };
    store.setReadingWindow(anchor);

    store.setActiveScope("agent-b", 1);
    expect(store.readingWindow.value).toBeNull();
    store.setReadingWindow({ anchorOrdinal: 2, endOrdinal: 4, conversationEpoch: 3, anchorItemKey: "e-b", anchorOffset: 18 });

    store.setActiveScope("agent-a", 2);
    expect(store.readingWindow.value).toEqual(anchor);
    store.setActiveScope("agent-b", 2);
    expect(store.readingWindow.value).toEqual({ anchorOrdinal: 2, endOrdinal: 4, conversationEpoch: 3, anchorItemKey: "e-b", anchorOffset: 18 });
    store.setActiveScope("agent-a", 3);
    store.applyHydrateSnapshot({ conversation_epoch: 7, conversation_revision: 1, transcript: [] }, { agentId: "agent-a", generation: 3 });
    expect(store.readingWindow.value).toBeNull();
  });

  it("rebuilds visible transcript and queued rows as one scoped store projection", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    store.replaceHydrateSnapshot({
      transcript: [{ item_id: "human-1", item_version: 1, id: "human-1", kind: "user", text: "Question", data: {} }],
      pending_inputs: [{ input_id: "agent-a/input/2", content: "Durable queued", status: "queued", input_seq: 2, input_revision: 1 }],
      turn_state: { terminal: false },
    });
    store.items.replacePendingInputs("agent-a", [
      { id: "agent-a/input/2", item_id: "agent-a/input/2", kind: "user", text: "Optimistic duplicate" },
      { id: "pending:request-local", client_request_id: "request-local", kind: "user", text: "Local pending" },
    ]);

    const projection = store.replaceVisibleItems("agent-a", 1);
    expect(projection).toMatchObject({ accepted: true, stale: false });
    expect(projection.entries.map(entry => entry.id)).toEqual([
      "human-1",
      "agent-a/input/2",
      "pending:request-local",
    ]);
    expect(projection.entries[1]).toMatchObject({ text: "Durable queued", queue_status: "queued", input_seq: 2 });
    expect(projection.entries[2]).toMatchObject({ text: "Local pending", client_request_id: "request-local" });
    expect(store.entriesChange.value).toMatchObject({ kind: "snapshot", revision: projection.revision });
  });

  it("does not let a delayed projection from an old Agent clear the active rows", () => {
    const store = createConversationStore();
    store.setActiveScope("agent-a", 1);
    store.replaceHydrateSnapshot({ transcript: [{ item_id: "a", kind: "assistant", text: "A", data: {} }] });
    store.replaceVisibleItems("agent-a", 1);

    store.setActiveScope("agent-b", 2);
    store.replaceHydrateSnapshot({ transcript: [{ item_id: "b", kind: "assistant", text: "B", data: {} }] });
    store.replaceVisibleItems("agent-b", 2);
    const activeRow = store.items.entries[0];

    expect(store.replaceVisibleItems("agent-a", 1)).toMatchObject({ accepted: false, stale: true });
    expect(store.items.entries).toHaveLength(1);
    expect(store.items.entries[0]).toBe(activeRow);
    expect(store.items.entries[0].item_id).toBe("b");
  });
});
