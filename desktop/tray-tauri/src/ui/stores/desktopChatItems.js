import { reactive, ref, shallowReactive } from "vue";

function entryId(entry) {
  return String(entry?.id || entry?.item_id || "");
}

function stableVersion(entry) {
  return entry?.item_id && Number.isSafeInteger(entry?.item_version)
    ? entry.item_version
    : null;
}

function cloneEntry(entry) {
  return reactive({ ...entry, data: entry?.data && typeof entry.data === "object" ? { ...entry.data } : {} });
}

function replaceEntry(target, source) {
  // Node rows are complete snapshots; clear draft-only fields omitted by the durable row.
  for (const key of Object.keys(target)) {
    if (!Object.prototype.hasOwnProperty.call(source, key)) delete target[key];
  }
  Object.assign(target, source, { data: source?.data && typeof source.data === "object" ? { ...source.data } : {} });
}

function compareTranscriptOrder(left, right) {
  const leftOrdinal = Number(left?.conversation_ordinal);
  const rightOrdinal = Number(right?.conversation_ordinal);
  if (Number.isFinite(leftOrdinal) && Number.isFinite(rightOrdinal) && leftOrdinal !== rightOrdinal) {
    return leftOrdinal - rightOrdinal;
  }
  const leftIndex = Number(left?.conversation_entry_index);
  const rightIndex = Number(right?.conversation_entry_index);
  if (Number.isFinite(leftOrdinal) && Number.isFinite(rightOrdinal) && leftOrdinal === rightOrdinal
    && Number.isFinite(leftIndex) && Number.isFinite(rightIndex) && leftIndex !== rightIndex) {
    return leftIndex - rightIndex;
  }
  return 0;
}

/**
 * Snapshot-backed ordered chat rows with point updates for streaming drafts.
 * Full scans are reserved for authoritative transcript/order replacement.
 */
export function createDesktopChatItemsStore() {
  const entries = shallowReactive([]);
  let byId = new Map();
  let indexById = new Map();
  let versionById = new Map();
  let sourceById = new Map();
  let scope = "";
  const structuralRevision = ref(0);
  const pendingInputsRevision = ref(0);
  const pendingInputsByAgent = shallowReactive(new Map());
  const metrics = { snapshotRowsVisited: 0, mapperCalls: 0, pointUpdates: 0, inserts: 0 };

  function pendingInputsForAgent(agentId) {
    const key = String(agentId || "").trim();
    return key ? pendingInputsByAgent.get(key) || [] : [];
  }

  function replacePendingInputs(agentId, inputs = []) {
    const key = String(agentId || "").trim();
    if (!key) return pendingInputsRevision.value;
    const next = Array.isArray(inputs) ? inputs : [];
    if (next.length) pendingInputsByAgent.set(key, next);
    else pendingInputsByAgent.delete(key);
    pendingInputsRevision.value += 1;
    return pendingInputsRevision.value;
  }

  function upsertPendingInput(agentId, input, identity = "") {
    const key = String(agentId || "").trim();
    if (!key || !input || typeof input !== "object") return pendingInputsRevision.value;
    const id = String(identity || input.item_id || input.input_id || input.id || "").trim();
    if (!id) return pendingInputsRevision.value;
    const current = pendingInputsForAgent(key);
    const index = current.findIndex(item => String(item?.item_id || item?.input_id || item?.id || "") === id);
    const next = current.slice();
    if (index >= 0) next[index] = input;
    else next.push(input);
    return replacePendingInputs(key, next);
  }

  function removePendingInput(agentId, identity) {
    const key = String(agentId || "").trim();
    const id = String(identity || "").trim();
    if (!key || !id) return pendingInputsRevision.value;
    const current = pendingInputsForAgent(key);
    const next = current.filter(item => String(item?.item_id || item?.input_id || item?.id || "") !== id);
    if (next.length === current.length) return pendingInputsRevision.value;
    return replacePendingInputs(key, next);
  }

  function isUnchangedStableSnapshot(rows) {
    if (rows.length !== entries.length || byId.size !== entries.length || sourceById.size !== entries.length) return false;
    for (let index = 0; index < rows.length; index += 1) {
      const source = rows[index];
      const id = entryId(source);
      const version = stableVersion(source);
      if (!id || version === null || entryId(entries[index]) !== id) return false;
      if (sourceById.get(id) !== source || versionById.get(id) !== version) return false;
    }
    return true;
  }

  function replaceSnapshot(rows = []) {
    const sources = Array.isArray(rows) ? rows : [];
    // Node item rows are immutable for an item_version. When an authoritative
    // hydrate reuses the same ordered references, avoid reactive assignments
    // and array replacement; point updates still handle live stream deltas.
    if (isUnchangedStableSnapshot(sources)) return structuralRevision.value;

    const nextById = new Map();
    const nextIndices = new Map();
    const nextVersions = new Map();
    const nextSources = new Map();
    const nextEntries = [];
    for (const source of sources) {
      metrics.snapshotRowsVisited += 1;
      metrics.mapperCalls += 1;
      const id = entryId(source);
      if (!id) continue;
      const version = stableVersion(source);
      const prior = byId.get(id);
      const priorVersion = versionById.get(id) ?? null;
      let row;
      if (prior && version !== null && priorVersion === version) {
        row = prior;
        replaceEntry(row, source);
        metrics.pointUpdates += 1;
      } else {
        row = cloneEntry(source);
      }
      nextById.set(id, row);
      nextIndices.set(id, nextEntries.length);
      nextVersions.set(id, version);
      nextSources.set(id, source);
      nextEntries.push(row);
    }
    byId = nextById;
    indexById = nextIndices;
    versionById = nextVersions;
    sourceById = nextSources;
    entries.splice(0, entries.length, ...nextEntries);
    structuralRevision.value += 1;
    return structuralRevision.value;
  }

  function upsertDraft(source, { beforeId = "" } = {}) {
    const id = entryId(source);
    if (!id) return { accepted: false, inserted: false, revision: structuralRevision.value };
    const prior = byId.get(id);
    if (prior) {
      replaceEntry(prior, source);
      metrics.pointUpdates += 1;
      versionById.set(id, stableVersion(source));
      return { accepted: true, inserted: false, entry: prior, revision: structuralRevision.value };
    }
    const row = cloneEntry(source);
    const before = beforeId ? byId.get(beforeId) : null;
    const index = before ? indexById.get(beforeId) : -1;
    const insertedIndex = index >= 0 ? index : entries.length;
    if (index >= 0) entries.splice(index, 0, row);
    else entries.push(row);
    byId.set(id, row);
    for (let cursor = insertedIndex; cursor < entries.length; cursor += 1) {
      indexById.set(entryId(entries[cursor]), cursor);
    }
    versionById.set(id, stableVersion(source));
    metrics.inserts += 1;
    structuralRevision.value += 1;
    return { accepted: true, inserted: true, entry: row, index: insertedIndex, revision: structuralRevision.value };
  }

  function applyTranscriptPatch(upsertRows = [], removeIds = []) {
    const removed = [];
    const removedSet = new Set((Array.isArray(removeIds) ? removeIds : []).map(value => String(value || "").trim()).filter(Boolean));
    if (removedSet.size) {
      for (let index = entries.length - 1; index >= 0; index -= 1) {
        const row = entries[index];
        const id = entryId(row);
        if (!removedSet.has(id) && !removedSet.has(String(row?.detail_item_id || ""))) continue;
        entries.splice(index, 1);
        byId.delete(id);
        indexById.delete(id);
        versionById.delete(id);
        sourceById.delete(id);
        removed.push(id);
      }
      for (let cursor = 0; cursor < entries.length; cursor += 1) indexById.set(entryId(entries[cursor]), cursor);
    }

    const inserted = [];
    const changed = [];
    for (const source of Array.isArray(upsertRows) ? upsertRows : []) {
      const id = entryId(source);
      if (!id || removedSet.has(id)) continue;
      const version = stableVersion(source);
      const prior = byId.get(id);
      const priorVersion = versionById.get(id) ?? null;
      if (prior) {
        if (version !== null && priorVersion !== null && version <= priorVersion) continue;
        replaceEntry(prior, source);
        versionById.set(id, version);
        sourceById.set(id, source);
        metrics.pointUpdates += 1;
        changed.push(id);
        continue;
      }

      const row = cloneEntry(source);
      let low = 0;
      let high = entries.length;
      let ordered = Number.isFinite(Number(source?.conversation_ordinal));
      while (ordered && low < high) {
        const mid = (low + high) >>> 1;
        const current = entries[mid];
        const currentOrdinal = Number(current?.conversation_ordinal);
        if (!Number.isFinite(currentOrdinal)) {
          ordered = false;
          break;
        }
        if (compareTranscriptOrder(current, source) <= 0) low = mid + 1;
        else high = mid;
      }
      const index = ordered ? low : entries.length;
      entries.splice(index, 0, row);
      byId.set(id, row);
      versionById.set(id, version);
      sourceById.set(id, source);
      for (let cursor = index; cursor < entries.length; cursor += 1) indexById.set(entryId(entries[cursor]), cursor);
      inserted.push(id);
      metrics.inserts += 1;
    }

    const structuralChanged = removed.length > 0 || inserted.length > 0;
    if (structuralChanged) structuralRevision.value += 1;
    return {
      structuralChanged,
      insertedItemIds: inserted,
      removedItemIds: removed,
      changedItemIds: changed,
      revision: structuralRevision.value,
    };
  }

  function clear() {
    replaceSnapshot([]);
  }

  function setScope(nextScope) {
    const normalized = String(nextScope || "");
    if (scope === normalized) return false;
    scope = normalized;
    byId = new Map();
    indexById = new Map();
    versionById = new Map();
    sourceById = new Map();
    entries.splice(0, entries.length);
    structuralRevision.value += 1;
    return true;
  }

  function resetMetrics() {
    metrics.snapshotRowsVisited = 0;
    metrics.mapperCalls = 0;
    metrics.pointUpdates = 0;
    metrics.inserts = 0;
  }

  return {
    entries,
    metrics,
    revision: structuralRevision,
    pendingInputsRevision,
    pendingInputsByAgent,
    pendingInputsForAgent,
    replacePendingInputs,
    upsertPendingInput,
    removePendingInput,
    replaceSnapshot,
    upsertDraft,
    applyTranscriptPatch,
    clear,
    setScope,
    resetMetrics,
  };
}
