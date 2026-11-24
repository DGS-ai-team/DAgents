function stableItemId(entry) {
  return typeof entry?.item_id === "string" && entry.item_id.trim()
    ? entry.item_id
    : "";
}

function itemVersion(entry) {
  const version = entry?.item_version;
  return typeof version === "number" && Number.isFinite(version) ? version : null;
}

/**
 * Reuse only stable transcript entities whose version did not advance.
 * Incoming order is authoritative; entries without the Node identity contract
 * pass through untouched so legacy rendering keeps its existing adapter path.
 */
export function mergeTranscriptSnapshot(incoming = [], previousById = new Map()) {
  const incomingRows = Array.isArray(incoming) ? incoming : [];
  const prior = previousById instanceof Map ? previousById : new Map();
  const stableItemsById = new Map();
  const transcript = incomingRows.map((incomingEntry) => {
    const itemId = stableItemId(incomingEntry);
    const version = itemVersion(incomingEntry);
    if (!itemId || version === null) return incomingEntry;

    const previousEntry = prior.get(itemId);
    const previousVersion = itemVersion(previousEntry);
    const entry = previousVersion !== null && version <= previousVersion
      ? previousEntry
      : incomingEntry;
    stableItemsById.set(itemId, entry);
    return entry;
  });

  return { transcript, stableItemsById };
}

/** Keep only the active transcript window; older Agent generations cannot be reused. */
export function mergeTranscriptReferenceWindow(current, agentId, generation, incoming = []) {
  const previousById = current?.agentId === agentId && current?.generation === generation
    ? current.stableItemsById
    : new Map();
  const merged = mergeTranscriptSnapshot(incoming, previousById);
  return {
    ...merged,
    window: {
      agentId,
      generation,
      stableItemsById: merged.stableItemsById,
    },
  };
}

/** Record the published transcript references after a local media-only update. */
export function replaceTranscriptReferenceWindow(agentId, generation, transcript = []) {
  const stableItemsById = new Map();
  for (const entry of Array.isArray(transcript) ? transcript : []) {
    if (stableItemId(entry) && itemVersion(entry) !== null) {
      stableItemsById.set(entry.item_id, entry);
    }
  }
  return { agentId, generation, stableItemsById };
}
