function revisionOf(snapshot) {
  return Number(snapshot?.history_revision ?? snapshot?.turn_state?.history_revision) || 0;
}

function conversationRevisionOf(snapshot) {
  const value = Number(snapshot?.conversation_revision);
  return Number.isFinite(value) && value >= 0 ? value : -1;
}

function lifecycleSequenceOf(snapshot) {
  return Number(snapshot?.turn_state?.lifecycle_seq) || 0;
}

const LIFECYCLE_FIELDS = [
  "turn_state",
  "pending_hitl",
  "has_active_turn",
  "queue_pending",
  "child_agents",
  "notify_seq",
  "ack_seq",
  "has_unread",
];

const TRANSCRIPT_WINDOW_FIELDS = [
  "has_earlier_history",
  "window_start_round",
  "window_end_round",
];

/** Merge the transcript and lifecycle projections using their own watermarks. */
export function mergeHydrateSnapshot(current, incoming, { historyRevision = -1, conversationRevision = -1, lifecycleSeq = 0 } = {}) {
  const incomingHistoryRevision = revisionOf(incoming);
  const previousHistoryRevision = Math.max(historyRevision, revisionOf(current));
  const incomingConversationRevision = conversationRevisionOf(incoming);
  const previousConversationRevision = Math.max(conversationRevision, conversationRevisionOf(current));
  const hasConversationRevisionContract = incomingConversationRevision >= 0
    || previousConversationRevision >= 0;
  const transcriptAccepted = hasConversationRevisionContract
    ? incomingConversationRevision >= previousConversationRevision
    : incomingHistoryRevision >= previousHistoryRevision;
  const previousLifecycleSeq = Math.max(lifecycleSeq, lifecycleSequenceOf(current));
  const lifecycleAccepted = lifecycleSequenceOf(incoming) >= previousLifecycleSeq;
  const base = { ...(current || {}), ...incoming };
  if (!transcriptAccepted) {
    base.transcript = current?.transcript;
    base.history_revision = current?.history_revision ?? previousHistoryRevision;
    if (previousConversationRevision >= 0) base.conversation_revision = previousConversationRevision;
    else delete base.conversation_revision;
    for (const field of TRANSCRIPT_WINDOW_FIELDS) {
      if (current && Object.prototype.hasOwnProperty.call(current, field)) base[field] = current[field];
      else delete base[field];
    }
  }
  if (!lifecycleAccepted) {
    for (const field of LIFECYCLE_FIELDS) {
      if (current && Object.prototype.hasOwnProperty.call(current, field)) base[field] = current[field];
      else delete base[field];
    }
  }

  return {
    snapshot: base,
    historyRevision: transcriptAccepted ? Math.max(previousHistoryRevision, incomingHistoryRevision) : previousHistoryRevision,
    conversationRevision: transcriptAccepted && incomingConversationRevision >= 0
      ? Math.max(previousConversationRevision, incomingConversationRevision)
      : previousConversationRevision,
    transcriptAccepted,
    lifecycleAccepted,
  };
}

function transcriptWindowKey(entry, index) {
  if (typeof entry?.item_id === "string" && entry.item_id.trim()) return entry.item_id;
  const ordinal = Number(entry?.conversation_ordinal);
  if (Number.isFinite(ordinal)) return `ordinal:${ordinal}:${Number(entry?.conversation_entry_index) || index}:${entry?.kind || ""}`;
  return `incoming:${index}`;
}

/**
 * Keep the slice the user is reading while the server advances its bounded
 * latest-round window. Returning to the tail can discard this temporary read
 * slice and use the latest server window again.
 */
export function mergeTranscriptReadWindow(current = [], incoming = [], readWindow = null, conversationEpoch = 0) {
  const latest = Array.isArray(incoming) ? incoming : [];
  const previous = Array.isArray(current) ? current : [];
  if (!readWindow || Number(readWindow.conversationEpoch) !== Number(conversationEpoch)) return latest;
  const anchor = Number(readWindow.anchorOrdinal);
  const end = Number(readWindow.endOrdinal);
  if (!Number.isFinite(anchor) || !Number.isFinite(end) || end < anchor) return latest;

  const selectedRevisionByRound = new Map();
  for (const entry of latest) {
    const roundId = String(entry?.round_id || "");
    const revisionId = String(entry?.round_revision_id || "");
    if (roundId && revisionId) selectedRevisionByRound.set(roundId, revisionId);
  }

  const merged = new Map();
  previous.forEach((entry, index) => {
    const ordinal = Number(entry?.conversation_ordinal);
    if (!Number.isFinite(ordinal) || ordinal < anchor || ordinal > end) return;
    const roundId = String(entry?.round_id || "");
    const revisionId = String(entry?.round_revision_id || "");
    if (roundId && revisionId && selectedRevisionByRound.has(roundId) && selectedRevisionByRound.get(roundId) !== revisionId) return;
    merged.set(transcriptWindowKey(entry, index), entry);
  });
  latest.forEach((entry, index) => merged.set(transcriptWindowKey(entry, index), entry));
  return [...merged.values()].sort((left, right) => {
    const leftOrdinal = Number(left?.conversation_ordinal);
    const rightOrdinal = Number(right?.conversation_ordinal);
    if (Number.isFinite(leftOrdinal) && Number.isFinite(rightOrdinal) && leftOrdinal !== rightOrdinal) return leftOrdinal - rightOrdinal;
    const leftIndex = Number(left?.conversation_entry_index) || 0;
    const rightIndex = Number(right?.conversation_entry_index) || 0;
    return leftIndex - rightIndex;
  });
}

export function restoreHydrateLifecycleFields(snapshot, previous) {
  const restored = { ...(snapshot || {}) };
  if (!previous) return restored;
  for (const field of LIFECYCLE_FIELDS) {
    if (Object.prototype.hasOwnProperty.call(previous, field)) restored[field] = previous[field];
    else delete restored[field];
  }
  return restored;
}

export function hydrateSchedulePlan({ requestedDelay, dirtyAt, now, retryExhausted, targetChanged, force = false }) {
  const resetRetryBudget = Boolean(targetChanged || force);
  return {
    allowed: !retryExhausted || resetRetryBudget,
    resetRetryBudget,
    delay: Math.min(Math.max(0, requestedDelay), Math.max(0, 250 - (now - dirtyAt))),
  };
}

export function nextHydrateRetryDelay(retryCount) {
  return retryCount >= 5 ? null : Math.min(8000, 500 * (2 ** retryCount));
}

export function shouldReplaceHydrateTimer(existingDueAt, nextDueAt) {
  return !existingDueAt || nextDueAt < existingDueAt;
}

export function hydrateSyncNotice({ phase = "idle", startedAt = 0, now = Date.now(), resultCommitStatus = "" } = {}) {
  if (resultCommitStatus === "failed") return "commit_failed";
  if (phase === "failed") return "failed";
  if (["pending", "syncing"].includes(phase) && now - startedAt >= 600) return "syncing";
  return "";
}

export function canManuallyRetryHydrate(phase) {
  return phase === "failed";
}

export function isHydrateReadError(message) {
  return String(message || "").startsWith("结果暂未同步：");
}
