function nonNegativeNumber(value) {
  return Math.max(0, Number(value) || 0);
}

/** Advance the UI-applied cursor only after a Node event handler succeeds. */
export function advanceAppliedAgentSeq(previousSequence, eventSequence) {
  const previous = nonNegativeNumber(previousSequence);
  const sequence = nonNegativeNumber(eventSequence);
  if (sequence <= 0) return { sequence: previous, gap: false, advanced: false };
  return {
    sequence: Math.max(previous, sequence),
    gap: sequence > previous + 1,
    advanced: sequence > previous,
  };
}

/** Decide whether an active Agent SSE event is contiguous, duplicate, or ahead of a gap. */
export function agentEventSequenceAction(previousSequence, eventSequence) {
  const previous = nonNegativeNumber(previousSequence);
  const sequence = nonNegativeNumber(eventSequence);
  if (sequence <= 0) return { action: "ephemeral", expected: previous + 1, sequence };
  if (sequence <= previous) return { action: "duplicate", expected: previous + 1, sequence };
  if (sequence === previous + 1) return { action: "apply", expected: previous + 1, sequence };
  return { action: "buffer", expected: previous + 1, sequence };
}

/** Decide whether a lightweight Node watermark needs existing UI recovery. */
export function conversationWatermarkAction({
  watermark = {},
  previous = null,
  active = false,
  epochChanged = false,
  sequenceGap = false,
  appliedAgentSeq = 0,
  probedAgentSeq = 0,
  localConversationRevision = 0,
} = {}) {
  const conversationRevision = nonNegativeNumber(watermark.conversationRevision);
  const latestAgentSeq = nonNegativeNumber(watermark.latestAgentSeq);
  const resultCommitStatus = String(watermark.resultCommitStatus || "unknown");
  const unseenAgentEvent = latestAgentSeq > nonNegativeNumber(appliedAgentSeq)
    && latestAgentSeq > nonNegativeNumber(probedAgentSeq);
  const revisionAdvanced = Boolean(previous && conversationRevision > nonNegativeNumber(previous.conversationRevision));
  const resultStatusChanged = Boolean(previous && resultCommitStatus !== String(previous.resultCommitStatus || "unknown"));
  const revisionAhead = active && conversationRevision > nonNegativeNumber(localConversationRevision);

  return {
    unseenAgentEvent,
    revisionAhead,
    shouldHydrateActive: active && (epochChanged || revisionAhead || unseenAgentEvent || sequenceGap || resultStatusChanged),
    shouldRefreshBackground: !active && (epochChanged || unseenAgentEvent || sequenceGap || revisionAdvanced || resultStatusChanged),
  };
}

/** Keep target observations monotonic within one Node epoch. */
export function mergeWatermarkObservation(previous, watermark, { sameEpoch = true } = {}) {
  const next = {
    conversationRevision: nonNegativeNumber(watermark?.conversationRevision),
    latestAgentSeq: nonNegativeNumber(watermark?.latestAgentSeq),
    resultCommitStatus: String(watermark?.resultCommitStatus || "unknown"),
  };
  if (!sameEpoch) previous = null;
  if (!previous) return next;
  return {
    conversationRevision: Math.max(nonNegativeNumber(previous.conversationRevision), next.conversationRevision),
    latestAgentSeq: Math.max(nonNegativeNumber(previous.latestAgentSeq), next.latestAgentSeq),
    resultCommitStatus: next.resultCommitStatus,
  };
}

/**
 * Reconcile a Node watermark response with the Desktop conversation store.
 * Callbacks keep transport scheduling in DesktopApp while this function owns
 * the production decision + cursor mutation path exercised by tests.
 */
export function reconcileAgentWatermarks(response, {
  activeAgentId = "",
  epochChanged = false,
  store,
  localConversationRevision = () => 0,
  hydrateActive = () => {},
  refreshBackground = () => {},
} = {}) {
  if (!store) throw new TypeError("conversation store is required");

  const activeId = String(activeAgentId || "");
  const requestedHydrates = new Set();
  let shouldRefreshBackground = false;
  const agents = Array.isArray(response?.agents) ? response.agents : [];

  for (const watermark of agents) {
    const id = String(watermark?.agent_id || "").trim();
    if (!id || String(watermark?.conversation_id || "") !== id) continue;

    const conversationRevision = nonNegativeNumber(watermark.conversation_revision);
    const latestAgentSeq = nonNegativeNumber(watermark.latest_agent_seq);
    const resultCommitStatus = String(watermark.result_commit_status || "unknown");
    const previous = store.observedAgentWatermark(id);
    const appliedAgentSeq = store.appliedAgentSequence(id);
    const probedAgentSeq = store.probedAgentSequence(id);
    const active = activeId === id;
    const sequenceGap = store.hasAgentSequenceGap(id);
    const action = conversationWatermarkAction({
      watermark: { conversationRevision, latestAgentSeq, resultCommitStatus },
      previous,
      active,
      epochChanged,
      sequenceGap,
      appliedAgentSeq,
      probedAgentSeq,
      localConversationRevision: active ? localConversationRevision(id) : 0,
    });

    if (action.shouldHydrateActive) {
      if (action.unseenAgentEvent) store.markAgentSequenceProbed(id, latestAgentSeq);
      store.clearAgentSequenceGap(id);
      hydrateActive(id, {
        conversation_revision: conversationRevision,
        agent_seq: latestAgentSeq,
      }, epochChanged || sequenceGap);
      requestedHydrates.add(id);
    } else if (action.shouldRefreshBackground) {
      shouldRefreshBackground = true;
      store.clearAgentSequenceGap(id);
      if (action.unseenAgentEvent) store.markAgentSequenceProbed(id, latestAgentSeq);
    }

    store.mergeAgentWatermark(
      id,
      { conversationRevision, latestAgentSeq, resultCommitStatus },
      { sameEpoch: !epochChanged },
    );
  }

  // An epoch change invalidates every process-local cursor. If the active
  // Agent was omitted from the response, still request one authoritative view.
  if (epochChanged && activeId && !requestedHydrates.has(activeId)) {
    hydrateActive(activeId, null, true);
    requestedHydrates.add(activeId);
  }
  if (shouldRefreshBackground) refreshBackground();

  return {
    hydratedAgentIds: [...requestedHydrates],
    refreshedBackground: shouldRefreshBackground,
  };
}
