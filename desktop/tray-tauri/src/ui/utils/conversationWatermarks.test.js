import { describe, expect, it } from "vitest";
import { createConversationStore } from "../stores/conversationStore.js";
import { advanceAppliedAgentSeq, agentEventSequenceAction, conversationWatermarkAction, mergeWatermarkObservation, reconcileAgentWatermarks } from "./conversationWatermarks.js";

describe("advanceAppliedAgentSeq", () => {
  it("advances a contiguous applied cursor", () => {
    expect(advanceAppliedAgentSeq(4, 5)).toEqual({ sequence: 5, gap: false, advanced: true });
  });

  it("requests recovery when the first applied event starts after sequence one", () => {
    expect(advanceAppliedAgentSeq(0, 3)).toEqual({ sequence: 3, gap: true, advanced: true });
  });

  it("does not move backwards or report a gap for duplicate and delayed events", () => {
    expect(advanceAppliedAgentSeq(8, 7)).toEqual({ sequence: 8, gap: false, advanced: false });
    expect(advanceAppliedAgentSeq(8, 8)).toEqual({ sequence: 8, gap: false, advanced: false });
  });

  it("keeps the cursor unchanged for events without a replayable sequence", () => {
    expect(advanceAppliedAgentSeq(8, 0)).toEqual({ sequence: 8, gap: false, advanced: false });
  });
});

describe("agentEventSequenceAction", () => {
  it("applies only the next reliable sequence and buffers a future sequence", () => {
    expect(agentEventSequenceAction(4, 5)).toMatchObject({ action: "apply", expected: 5 });
    expect(agentEventSequenceAction(4, 7)).toMatchObject({ action: "buffer", expected: 5 });
  });

  it("drops duplicate reliable events and leaves ephemeral events unsequenced", () => {
    expect(agentEventSequenceAction(8, 7).action).toBe("duplicate");
    expect(agentEventSequenceAction(8, 0).action).toBe("ephemeral");
    expect(agentEventSequenceAction(8, undefined).action).toBe("ephemeral");
  });
});

describe("conversationWatermarkAction", () => {
  it("hydrates an active Agent when the durable conversation revision advances", () => {
    expect(conversationWatermarkAction({
      active: true,
      watermark: { conversationRevision: 9, latestAgentSeq: 4 },
      previous: { conversationRevision: 8, resultCommitStatus: "committed" },
      localConversationRevision: 8,
      appliedAgentSeq: 4,
      probedAgentSeq: 4,
    }).shouldHydrateActive).toBe(true);
  });

  it("detects a missing final event from the target cursor even when content revision is unchanged", () => {
    const action = conversationWatermarkAction({
      active: true,
      watermark: { conversationRevision: 3, latestAgentSeq: 11 },
      previous: { conversationRevision: 3, latestAgentSeq: 10, resultCommitStatus: "pending" },
      localConversationRevision: 3,
      appliedAgentSeq: 10,
      probedAgentSeq: 10,
    });
    expect(action.unseenAgentEvent).toBe(true);
    expect(action.shouldHydrateActive).toBe(true);
  });

  it("does not repeat work for an unchanged target that has already been probed", () => {
    expect(conversationWatermarkAction({
      active: true,
      watermark: { conversationRevision: 3, latestAgentSeq: 11, resultCommitStatus: "pending" },
      previous: { conversationRevision: 3, latestAgentSeq: 11, resultCommitStatus: "pending" },
      localConversationRevision: 3,
      appliedAgentSeq: 10,
      probedAgentSeq: 11,
    })).toMatchObject({ unseenAgentEvent: false, shouldHydrateActive: false });
  });

  it("refreshes background summaries without asking for a full hydrate", () => {
    const action = conversationWatermarkAction({
      active: false,
      watermark: { conversationRevision: 6, latestAgentSeq: 7 },
      previous: { conversationRevision: 5, latestAgentSeq: 7, resultCommitStatus: "committed" },
      appliedAgentSeq: 7,
    });
    expect(action).toMatchObject({ shouldHydrateActive: false, shouldRefreshBackground: true });
  });

  it("recovers an idle Auto Agent after every stream notification is missed", () => {
    // While the Agent is in the background, one lightweight watermark check
    // must be enough to mark its summary dirty even though there was no prior
    // watermark observation and the stream heartbeat itself stayed healthy.
    const background = conversationWatermarkAction({
      active: false,
      watermark: { conversationRevision: 9, latestAgentSeq: 21, resultCommitStatus: "committed" },
      previous: null,
      appliedAgentSeq: 0,
      probedAgentSeq: 0,
    });
    expect(background).toMatchObject({
      unseenAgentEvent: true,
      shouldHydrateActive: false,
      shouldRefreshBackground: true,
    });

    // The background summary refresh records the observed process-local event
    // target. On activation, the durable conversation revision still exposes
    // the missed Auto work and triggers one authoritative hydrate without
    // relying on that already-probed event sequence.
    const activated = conversationWatermarkAction({
      active: true,
      watermark: { conversationRevision: 9, latestAgentSeq: 21, resultCommitStatus: "committed" },
      previous: { conversationRevision: 9, latestAgentSeq: 21, resultCommitStatus: "committed" },
      localConversationRevision: 8,
      appliedAgentSeq: 0,
      probedAgentSeq: 21,
    });
    expect(activated).toMatchObject({
      unseenAgentEvent: false,
      revisionAhead: true,
      shouldHydrateActive: true,
    });
  });

  it("treats an epoch change as a fresh active sync target", () => {
    expect(conversationWatermarkAction({
      active: true,
      epochChanged: true,
      watermark: { conversationRevision: 0, latestAgentSeq: 1 },
      previous: { conversationRevision: 20, latestAgentSeq: 100, resultCommitStatus: "unknown" },
    }).shouldHydrateActive).toBe(true);
  });
});

describe("mergeWatermarkObservation", () => {
  it("does not let a delayed same-epoch target move revision or cursor backwards", () => {
    expect(mergeWatermarkObservation(
      { conversationRevision: 10, latestAgentSeq: 21, resultCommitStatus: "committed" },
      { conversationRevision: 9, latestAgentSeq: 19, resultCommitStatus: "pending" },
    )).toEqual({ conversationRevision: 10, latestAgentSeq: 21, resultCommitStatus: "pending" });
  });

  it("rebaselines the process-local SSE cursor after a Node epoch changes", () => {
    expect(mergeWatermarkObservation(
      { conversationRevision: 20, latestAgentSeq: 100, resultCommitStatus: "committed" },
      { conversationRevision: 20, latestAgentSeq: 1, resultCommitStatus: "unknown" },
      { sameEpoch: false },
    )).toEqual({ conversationRevision: 20, latestAgentSeq: 1, resultCommitStatus: "unknown" });
  });
});

describe("reconcileAgentWatermarks production orchestration", () => {
  const response = (agentId, conversationRevision, latestAgentSeq, resultCommitStatus = "committed") => ({
    stream_epoch: "node-epoch-1",
    agents: [{
      agent_id: agentId,
      conversation_id: agentId,
      conversation_revision: conversationRevision,
      latest_agent_seq: latestAgentSeq,
      result_commit_status: resultCommitStatus,
    }],
  });

  it("recovers a background Auto Agent whose every notification was missed when activated", () => {
    const store = createConversationStore();
    const hydrates = [];
    const backgroundRefreshes = [];
    const watermark = response("auto-a", 9, 21);

    const background = reconcileAgentWatermarks(watermark, {
      activeAgentId: "other-agent",
      store,
      hydrateActive: (...args) => hydrates.push(args),
      refreshBackground: () => backgroundRefreshes.push("refresh"),
    });
    expect(background).toEqual({ hydratedAgentIds: [], refreshedBackground: true });
    expect(backgroundRefreshes).toHaveLength(1);
    expect(store.probedAgentSequence("auto-a")).toBe(21);
    expect(store.appliedAgentSequence("auto-a")).toBe(0);

    const activationRecovery = reconcileAgentWatermarks(watermark, {
      activeAgentId: "auto-a",
      store,
      localConversationRevision: () => 8,
      hydrateActive: (...args) => hydrates.push(args),
      refreshBackground: () => backgroundRefreshes.push("unexpected"),
    });
    expect(activationRecovery).toEqual({ hydratedAgentIds: ["auto-a"], refreshedBackground: false });
    expect(hydrates).toEqual([["auto-a", { conversation_revision: 9, agent_seq: 21 }, false]]);

    const settled = reconcileAgentWatermarks(watermark, {
      activeAgentId: "auto-a",
      store,
      localConversationRevision: () => 9,
      hydrateActive: (...args) => hydrates.push(args),
      refreshBackground: () => backgroundRefreshes.push("unexpected"),
    });
    expect(settled).toEqual({ hydratedAgentIds: [], refreshedBackground: false });
    expect(hydrates).toHaveLength(1);
    expect(backgroundRefreshes).toHaveLength(1);
  });

  it("rebaselines process-local cursors and requests one hydrate after a Node epoch change", () => {
    const store = createConversationStore();
    store.noteAppliedAgentSequence("auto-a", 90);
    store.markAgentSequenceProbed("auto-a", 90);
    store.mergeAgentWatermark("auto-a", { conversationRevision: 10, latestAgentSeq: 90 });
    store.resetAgentSequences();
    const hydrates = [];

    const result = reconcileAgentWatermarks(response("auto-a", 10, 1), {
      activeAgentId: "auto-a",
      epochChanged: true,
      store,
      localConversationRevision: () => 10,
      hydrateActive: (...args) => hydrates.push(args),
    });

    expect(result).toEqual({ hydratedAgentIds: ["auto-a"], refreshedBackground: false });
    expect(hydrates).toEqual([["auto-a", { conversation_revision: 10, agent_seq: 1 }, true]]);
    expect(store.appliedAgentSequence("auto-a")).toBe(0);
    expect(store.probedAgentSequence("auto-a")).toBe(1);
    expect(store.observedAgentWatermark("auto-a")).toMatchObject({ conversationRevision: 10, latestAgentSeq: 1 });
  });

  it("forces an active snapshot on epoch change even when the active Agent is absent from the response", () => {
    const hydrates = [];
    const result = reconcileAgentWatermarks({ stream_epoch: "node-epoch-2", agents: [] }, {
      activeAgentId: "active-a",
      epochChanged: true,
      store: createConversationStore(),
      hydrateActive: (...args) => hydrates.push(args),
    });
    expect(result).toEqual({ hydratedAgentIds: ["active-a"], refreshedBackground: false });
    expect(hydrates).toEqual([["active-a", null, true]]);
  });

  it("preserves the sequence-gap force flag while clearing the recovered gap", () => {
    const store = createConversationStore();
    store.noteAppliedAgentSequence("active-a", 1);
    store.noteAppliedAgentSequence("active-a", 3);
    expect(store.hasAgentSequenceGap("active-a")).toBe(true);
    const hydrates = [];

    const result = reconcileAgentWatermarks(response("active-a", 4, 3), {
      activeAgentId: "active-a",
      store,
      localConversationRevision: () => 4,
      hydrateActive: (...args) => hydrates.push(args),
    });

    expect(result.hydratedAgentIds).toEqual(["active-a"]);
    expect(hydrates).toEqual([["active-a", { conversation_revision: 4, agent_seq: 3 }, true]]);
    expect(store.hasAgentSequenceGap("active-a")).toBe(false);
  });
});
