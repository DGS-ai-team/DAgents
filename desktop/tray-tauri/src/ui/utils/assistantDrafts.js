function identityOf(event) {
  const data = event?.data;
  if (!data || typeof data !== "object") return null;
  const itemId = typeof data.item_id === "string" ? data.item_id.trim() : "";
  const attemptId = typeof data.attempt_id === "string" ? data.attempt_id.trim() : "";
  const part = data.part;
  const chunkSeq = data.chunk_seq;
  const conversationDelta = event.event_type === "conversation_delta";
  if (!itemId || !attemptId || !["content", "reasoning"].includes(part)) return null;
  if (!Number.isSafeInteger(chunkSeq) || chunkSeq < 1) return null;
  if (part === "content" && !(conversationDelta && data.display_type === "delta")) return null;
  if (part === "reasoning" && !(conversationDelta && data.display_type === "reasoning")) return null;
  if (typeof data.content !== "string") return null;
  return {
    itemId,
    attemptId,
    part,
    chunkSeq,
    agentSeq: Number(event?.agent_seq) || 0,
    streamSeq: Number.isSafeInteger(event?.seq) ? event.seq : 0,
    turnId: typeof data.turn_id === "string" ? data.turn_id.trim() : "",
    delta: data.content,
  };
}

function windowFor(state, agentId, generation) {
  const current = state instanceof Map ? state.get(agentId) : null;
  return current?.generation === generation ? current.itemsById : new Map();
}

function stateWithWindow(state, agentId, generation, itemsById) {
  const next = state instanceof Map ? new Map(state) : new Map();
  const prior = next.get(agentId);
  const closedTurns = prior?.generation === generation ? prior.closedTurns : new Set();
  next.set(agentId, { generation, itemsById, closedTurns: new Set(closedTurns || []) });
  return next;
}

function makeEntry(itemId, attemptId, part, chunkSeq, text, agentSeq = 0, streamSeq = 0, turnId = "") {
  return {
    id: itemId,
    item_id: itemId,
    attempt_id: attemptId,
    kind: part === "content" ? "assistant" : "reasoning",
    text,
    streaming: true,
    partial: true,
    data: {},
    chunk_seq: chunkSeq,
    agent_seq: agentSeq,
    stream_seq: streamSeq,
    turn_id: turnId,
  };
}

/** Seed the open model drafts captured with a Node hydrate cursor. */
export function seedAssistantDraftSnapshots(state, snapshots, { agentId, generation } = {}) {
  if (!agentId || !Number.isFinite(generation)) return { state, entries: [] };
  const currentItems = windowFor(state, agentId, generation);
  const currentScope = state instanceof Map ? state.get(agentId) : null;
  if (!Array.isArray(snapshots) || snapshots.length === 0) return { state, entries: [] };
  const nextItems = new Map(currentItems);
  const entries = [];

  for (const snapshot of snapshots) {
    const itemId = typeof snapshot?.item_id === "string" ? snapshot.item_id.trim() : "";
    const attemptId = typeof snapshot?.attempt_id === "string" ? snapshot.attempt_id.trim() : "";
    const part = snapshot?.part;
    const chunkSeq = snapshot?.through_chunk_seq;
    const turnId = typeof snapshot?.turn_id === "string" ? snapshot.turn_id.trim() : "";
    if (!itemId || !attemptId || !["content", "reasoning"].includes(part)
      || !Number.isSafeInteger(chunkSeq) || chunkSeq < 1 || typeof snapshot?.text !== "string") continue;
    if (turnId && currentScope?.generation === generation && currentScope.closedTurns?.has(turnId)) continue;

    const prior = nextItems.get(itemId);
    if (prior?.retiredAttempts.has(attemptId)) continue;
    if (prior && prior.attemptId !== attemptId && prior.parts.size > 0) continue;
    const currentPart = prior?.parts.get(part);
    if (currentPart && currentPart.chunkSeq >= chunkSeq) continue;

    const retiredAttempts = new Set(prior?.retiredAttempts || []);
    if (prior?.attemptId && prior.attemptId !== attemptId) retiredAttempts.add(prior.attemptId);
    const draft = prior?.attemptId === attemptId
      ? prior
      : { attemptId, retiredAttempts, parts: new Map() };
    const agentSeq = Math.max(0, Number(snapshot.through_agent_seq) || 0);
    const streamSeq = Math.max(0, Number(snapshot.through_stream_seq) || 0);
    const entry = makeEntry(itemId, attemptId, part, chunkSeq, snapshot.text, agentSeq, streamSeq, turnId);
    const parts = new Map(draft.parts);
    parts.set(part, { chunkSeq, agentSeq, streamSeq, text: snapshot.text, entry });
    nextItems.set(itemId, { ...draft, parts });
    entries.push(entry);
  }

  return entries.length === 0
    ? { state, entries }
    : { state: stateWithWindow(state, agentId, generation, nextItems), entries };
}

/** Reconcile local drafts against the drafts covered by one Node snapshot. */
export function reconcileAssistantDraftSnapshot(state, snapshots, { agentId, generation, coveredAgentSeq = 0, coveredStreamSeq = 0 } = {}) {
  const currentItems = windowFor(state, agentId, generation);
  if (currentItems.size === 0) return { ...seedAssistantDraftSnapshots(state, snapshots, { agentId, generation }), removedItemIds: [] };
  const snapshotByPart = new Map();
  for (const snapshot of Array.isArray(snapshots) ? snapshots : []) {
    if (typeof snapshot?.item_id !== "string" || typeof snapshot?.part !== "string") continue;
    snapshotByPart.set(`${snapshot.item_id}\0${snapshot.part}`, snapshot);
  }

  const nextItems = new Map();
  const removedItemIds = [];
  for (const [itemId, draft] of currentItems) {
    const parts = new Map(draft.parts);
    const retiredAttempts = new Set(draft.retiredAttempts || []);
    for (const [partName, currentPart] of parts) {
      const snapshot = snapshotByPart.get(`${itemId}\0${partName}`);
      const snapshotCoversPart = snapshot
        && snapshot.attempt_id === draft.attemptId
        && Number(snapshot.through_chunk_seq) >= currentPart.chunkSeq;
      const hasStreamCoverage = Number.isSafeInteger(Number(coveredStreamSeq)) && Number(coveredStreamSeq) > 0 && currentPart.streamSeq > 0;
      const coveredByCut = hasStreamCoverage
        ? currentPart.streamSeq <= Number(coveredStreamSeq)
        : currentPart.agentSeq <= Number(coveredAgentSeq);
      if (coveredByCut && !snapshotCoversPart) {
        parts.delete(partName);
        retiredAttempts.add(draft.attemptId);
      }
    }
    if (draft.parts.size > 0 && parts.size === 0) removedItemIds.push(itemId);
    nextItems.set(itemId, { ...draft, retiredAttempts, parts });
  }

  const reconciled = stateWithWindow(state, agentId, generation, nextItems);
  return { ...seedAssistantDraftSnapshots(reconciled, snapshots, { agentId, generation }), removedItemIds };
}

/** Retire the previous streamed attempt when Node starts a retry for a Step. */
export function retireAssistantDraftsForStep(state, agentId, generation, stepId) {
  const currentItems = windowFor(state, agentId, generation);
  const normalizedStepId = typeof stepId === "string" ? stepId.trim() : "";
  if (!normalizedStepId || currentItems.size === 0) return { state, removedItemIds: [] };
  const prefix = `${normalizedStepId}/assistant/`;
  let nextItems = null;
  const removedItemIds = [];
  for (const [itemId, draft] of currentItems) {
    if (!itemId.startsWith(prefix) || draft.parts.size === 0) continue;
    nextItems ||= new Map(currentItems);
    const retiredAttempts = new Set(draft.retiredAttempts || []);
    if (draft.attemptId) retiredAttempts.add(draft.attemptId);
    nextItems.set(itemId, { ...draft, retiredAttempts, parts: new Map() });
    removedItemIds.push(itemId);
  }
  return nextItems ? { state: stateWithWindow(state, agentId, generation, nextItems), removedItemIds } : { state, removedItemIds };
}

/** Apply one identity-bearing assistant or reasoning delta without mutating prior state. */
export function reduceAssistantDraftDelta(state, event, { agentId, generation } = {}) {
  if (!agentId || !Number.isFinite(generation) || (event?.agent_id && event.agent_id !== agentId)) {
    return { state, accepted: false, reason: "scope" };
  }
  const identity = identityOf(event);
  if (!identity) return { state, accepted: false, reason: "invalid_delta" };

  const priorItems = windowFor(state, agentId, generation);
  const scope = state instanceof Map ? state.get(agentId) : null;
  if (identity.turnId && scope?.generation === generation && scope.closedTurns?.has(identity.turnId)) {
    return { state, accepted: false, reason: "retired_turn", itemId: identity.itemId, attemptId: identity.attemptId };
  }
  const prior = priorItems.get(identity.itemId);
  if (prior?.retiredAttempts.has(identity.attemptId)) {
    return { state, accepted: false, reason: "retired_attempt" };
  }

  let draft = prior;
  const attemptChanged = !draft || draft.attemptId !== identity.attemptId;
  if (attemptChanged) {
    const retiredAttempts = new Set(draft?.retiredAttempts || []);
    if (draft?.attemptId) retiredAttempts.add(draft.attemptId);
    draft = {
      attemptId: identity.attemptId,
      retiredAttempts,
      parts: new Map(),
    };
  }

  const previousPart = draft.parts.get(identity.part);
  const expectedChunkSeq = previousPart ? previousPart.chunkSeq + 1 : 1;
  if (identity.chunkSeq !== expectedChunkSeq) {
    return {
      state,
      accepted: false,
      reason: identity.chunkSeq > expectedChunkSeq ? "missing_sequence" : "out_of_order",
      itemId: identity.itemId,
      attemptId: identity.attemptId,
      part: identity.part,
      expectedChunkSeq,
      receivedChunkSeq: identity.chunkSeq,
    };
  }
  const text = `${previousPart?.text || ""}${identity.delta}`;
  const entry = makeEntry(identity.itemId, identity.attemptId, identity.part, identity.chunkSeq, text, identity.agentSeq, identity.streamSeq, identity.turnId);
  const parts = new Map(draft.parts);
  parts.set(identity.part, { chunkSeq: identity.chunkSeq, agentSeq: identity.agentSeq, streamSeq: identity.streamSeq, text, entry });
  const nextDraft = { ...draft, parts };
  const nextItems = new Map(priorItems);
  nextItems.set(identity.itemId, nextDraft);
  const nextState = stateWithWindow(state, agentId, generation, nextItems);
  return { state: nextState, accepted: true, entry, itemId: identity.itemId, part: identity.part, attemptChanged };
}

export function assistantDraftEntries(state, agentId, generation) {
  const items = windowFor(state, agentId, generation);
  const groups = new Map();
  for (const draft of items.values()) {
    for (const part of draft.parts.values()) {
      const entry = part.entry;
      const match = String(entry.item_id).match(/^(.*)\/assistant\/(reasoning|content)$/);
      const groupId = match?.[1] || entry.item_id;
      let group = groups.get(groupId);
      if (!group) {
        group = { reasoning: [], content: [], other: [] };
        groups.set(groupId, group);
      }
      if (match?.[2] === "reasoning" || entry.kind === "reasoning") group.reasoning.push(entry);
      else if (match?.[2] === "content" || entry.kind === "assistant") group.content.push(entry);
      else group.other.push(entry);
    }
  }
  return [...groups.values()].flatMap(group => [...group.reasoning, ...group.content, ...group.other]);
}

/**
 * A partial snapshot is projected in place with the newer draft. A terminal or
 * complete snapshot retires a matching draft so the persisted item takes over.
 */
export function reconcileAssistantDrafts(state, agentId, generation, transcript = [], { terminal = false, terminalTurnId = "", authoritativeItemIds = new Set() } = {}) {
  const items = windowFor(state, agentId, generation);
  const currentScope = state instanceof Map ? state.get(agentId) : null;
  if (items.size === 0 && !terminalTurnId) return { state, removedItemIds: [] };
  const nextItems = new Map(items);
  const removedItemIds = [];
  const closedTurns = new Set(currentScope?.generation === generation ? currentScope.closedTurns || [] : []);
  const normalizedTerminalTurnId = String(terminalTurnId || "").trim();
  if (terminal && normalizedTerminalTurnId) closedTurns.add(normalizedTerminalTurnId);
  while (closedTurns.size > 64) closedTurns.delete(closedTurns.values().next().value);
  const authoritativeIds = authoritativeItemIds instanceof Set ? authoritativeItemIds : new Set(authoritativeItemIds || []);
  if (terminal) {
    for (const [itemId, draft] of nextItems) {
      if (draft.parts.size) removedItemIds.push(itemId);
      nextItems.delete(itemId);
    }
  } else {
    for (const entry of Array.isArray(transcript) ? transcript : []) {
      const itemId = typeof entry?.item_id === "string" ? entry.item_id : "";
      if (!itemId || !nextItems.has(itemId)) continue;
      if (!entry.partial || authoritativeIds.has(itemId)) {
        const draft = nextItems.get(itemId);
        const retiredAttempts = new Set(draft?.retiredAttempts || []);
        if (draft?.attemptId) retiredAttempts.add(draft.attemptId);
        nextItems.set(itemId, { ...draft, retiredAttempts, parts: new Map() });
        removedItemIds.push(itemId);
      }
    }
  }
  let nextState = stateWithWindow(state, agentId, generation, nextItems);
  if (closedTurns.size) {
    nextState = new Map(nextState);
    nextState.set(agentId, { ...nextState.get(agentId), closedTurns });
  }
  return { state: nextState, removedItemIds };
}

export function projectAssistantDrafts(transcript = [], state, agentId, generation, { terminal = false } = {}) {
  const rows = Array.isArray(transcript) ? transcript : [];
  const activeDrafts = windowFor(state, agentId, generation);
  if (activeDrafts.size === 0) return rows;
  const drafts = new Map(assistantDraftEntries(state, agentId, generation).map(entry => [entry.item_id, entry]));
  const seen = new Set();
  const projected = rows.map(entry => {
    const itemId = typeof entry?.item_id === "string" ? entry.item_id : "";
    if (!itemId) return entry;
    seen.add(itemId);
    const draft = drafts.get(itemId);
    if (!draft) return entry;
    return !terminal && entry.partial ? draft : entry;
  });
  for (const [itemId, entry] of drafts) {
    if (!seen.has(itemId)) projected.push(entry);
  }
  return projected;
}

/** Claim one hydrate recovery per missing chunk stream scope. */
export function claimAssistantDraftGapRecovery(recoveryKeys, { agentId, generation, itemId, attemptId, part } = {}) {
  const current = recoveryKeys instanceof Set ? recoveryKeys : new Set();
  const key = [agentId, generation, itemId, attemptId, part].join("\0");
  if (!agentId || !itemId || !attemptId || !["content", "reasoning"].includes(part) || current.has(key)) {
    return { recoveryKeys: current, shouldRecover: false };
  }
  const next = new Set(current);
  next.add(key);
  return { recoveryKeys: next, shouldRecover: true };
}
