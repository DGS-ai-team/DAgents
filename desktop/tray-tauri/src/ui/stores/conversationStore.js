import { computed, ref, shallowRef } from "vue";
import { createDesktopChatItemsStore } from "./desktopChatItems.js";
import { hitlQueueFromPending } from "./hitl.js";
import { createConversationEventSequencer } from "../utils/conversationEventSequencer.js";
import { mergeHydrateSnapshot } from "../utils/hydrateSnapshot.js";
import { advanceAppliedAgentSeq, mergeWatermarkObservation } from "../utils/conversationWatermarks.js";
import { desktopChatEntries } from "../utils/desktopChatEntries.js";
import {
  projectAssistantDrafts,
  reconcileAssistantDraftSnapshot,
  reconcileAssistantDrafts,
  reduceAssistantDraftDelta,
  claimAssistantDraftGapRecovery as claimDraftGapRecoveryKey,
  retireAssistantDraftsForStep,
} from "../utils/assistantDrafts.js";
import { mergeTranscriptReadWindow, restoreHydrateLifecycleFields } from "../utils/hydrateSnapshot.js";
import { mergeTranscriptReferenceWindow, replaceTranscriptReferenceWindow } from "../utils/transcriptIdentity.js";
import { createTurnStateStore } from "./turnState.js";

function transcriptOrder(left, right) {
  const leftOrdinal = Number(left?.conversation_ordinal);
  const rightOrdinal = Number(right?.conversation_ordinal);
  if (Number.isFinite(leftOrdinal) && Number.isFinite(rightOrdinal) && leftOrdinal !== rightOrdinal) return leftOrdinal - rightOrdinal;
  const leftEntryIndex = Number(left?.conversation_entry_index);
  const rightEntryIndex = Number(right?.conversation_entry_index);
  if (Number.isFinite(leftOrdinal) && Number.isFinite(rightOrdinal) && leftOrdinal === rightOrdinal
    && Number.isFinite(leftEntryIndex) && Number.isFinite(rightEntryIndex)) return leftEntryIndex - rightEntryIndex;
  return 0;
}

function mergeCommittedTranscript(current = [], upserts = [], removeIds = [], indexById = new Map()) {
  const removed = new Set((Array.isArray(removeIds) ? removeIds : []).map(value => String(value || "").trim()).filter(Boolean));
  let rows = Array.isArray(current) ? current : [];
  let rowsVisited = 0;
  if (removed.size) {
    const retained = [];
    for (const entry of rows) {
      rowsVisited += 1;
      const id = String(entry?.item_id || entry?.id || "").trim();
      if (removed.has(id) || removed.has(String(entry?.detail_item_id || ""))) continue;
      retained.push(entry);
    }
    rows = retained;
    indexById.clear();
    rows.forEach((entry, index) => {
      const id = String(entry?.item_id || entry?.id || "").trim();
      if (id) indexById.set(id, index);
    });
  }
  for (const entry of Array.isArray(upserts) ? upserts : []) {
    const id = String(entry?.item_id || entry?.id || "").trim();
    if (!id || removed.has(id)) continue;
    const index = indexById.get(id);
    if (Number.isInteger(index)) {
      const previous = rows[index];
      const priorVersion = Number(previous?.item_version);
      const nextVersion = Number(entry?.item_version);
      if (Number.isFinite(priorVersion) && Number.isFinite(nextVersion) && nextVersion <= priorVersion) continue;
      rows[index] = entry;
      continue;
    }

    let low = 0;
    let high = rows.length;
    let ordered = Number.isFinite(Number(entry?.conversation_ordinal));
    while (ordered && low < high) {
      const middle = (low + high) >>> 1;
      if (!Number.isFinite(Number(rows[middle]?.conversation_ordinal))) {
        ordered = false;
        break;
      }
      if (transcriptOrder(rows[middle], entry) <= 0) low = middle + 1;
      else high = middle;
    }
    const insertAt = ordered ? low : rows.length;
    rows.splice(insertAt, 0, entry);
    for (let rowIndex = insertAt; rowIndex < rows.length; rowIndex += 1) {
      rowsVisited += 1;
      const row = rows[rowIndex];
      const rowId = String(row?.item_id || row?.id || "").trim();
      if (rowId) indexById.set(rowId, rowIndex);
    }
  }
  return { rows, rowsVisited };
}

function supersededRevisionItemIds(current = [], upserts = []) {
  const selectedRevisionByRound = new Map();
  for (const entry of Array.isArray(upserts) ? upserts : []) {
    const roundId = String(entry?.round_id || "").trim();
    const revisionId = String(entry?.round_revision_id || "").trim();
    if (roundId && revisionId) selectedRevisionByRound.set(roundId, revisionId);
  }
  if (!selectedRevisionByRound.size) return [];
  const removeIds = new Set();
  for (const entry of Array.isArray(current) ? current : []) {
    const roundId = String(entry?.round_id || "").trim();
    const selectedRevision = selectedRevisionByRound.get(roundId);
    if (!selectedRevision || String(entry?.round_revision_id || "").trim() === selectedRevision) continue;
    for (const id of [entry?.item_id, entry?.id, entry?.detail_item_id]) {
      const value = String(id || "").trim();
      if (value) removeIds.add(value);
    }
  }
  return [...removeIds];
}

/** Own the active conversation projection and its per-Agent read cursors. */
export function createConversationStore() {
  const hydrate = ref(null);
  const pendingHitl = computed(() => hydrate.value?.pending_hitl || null);
  const hitlQueue = computed(() => hitlQueueFromPending(pendingHitl.value));
  const syncStatus = ref({ phase: "idle", startedAt: 0, resultCommitStatus: "" });
  const contentRevision = ref(-1);
  const conversationRevision = ref(-1);
  const latestHydrateTranscript = ref(null);
  const readingWindow = ref(null);
  const readingWindowsByAgent = new Map();
  let transcriptReferenceWindow = null;
  let latestTranscriptIndex = null;
  const assistantDraftState = shallowRef(new Map());
  const activeContentItemId = ref("");
  const entriesChange = ref({ revision: 0, kind: "snapshot" });
  const items = createDesktopChatItemsStore();
  const entries = items.entries;
  const itemsRevision = items.revision;
  const pendingInputsRevision = items.pendingInputsRevision;
  const eventSequencer = createConversationEventSequencer();
  const turnLifecycle = createTurnStateStore();
  let lastAppliedConversationPatch = null;
  let draftGapRecoveryKeys = new Set();
  const draftGapRecoveryItemIds = new Set();

  // These cursors are per Agent and intentionally survive active-Agent switches.
  const appliedAgentSeqById = new Map();
  const probedAgentSeqById = new Map();
  const observedAgentWatermarks = new Map();
  const agentWatermarkGapById = new Set();
  let activeScope = "";

  function ownsScope(agentId, generation) {
    const id = String(agentId || "").trim();
    const version = Number(generation) || 0;
    return Boolean(id) && activeScope === `${id}\0${version}`;
  }

  function mergeSnapshot(incoming, { lifecycleSeq = 0 } = {}) {
    const previousSnapshot = hydrate.value;
    let merged = mergeHydrateSnapshot(previousSnapshot, incoming, {
      historyRevision: contentRevision.value,
      conversationRevision: conversationRevision.value,
      lifecycleSeq,
    });
    const hasTurnState = incoming?.turn_state && typeof incoming.turn_state === "object";
    if (merged.lifecycleAccepted && hasTurnState && !turnLifecycle.canAcceptHydrateTurnState(incoming)) {
      merged = {
        ...merged,
        snapshot: restoreHydrateLifecycleFields(merged.snapshot, previousSnapshot),
        lifecycleAccepted: false,
      };
    }
    if (merged.transcriptAccepted || merged.lifecycleAccepted) {
      contentRevision.value = merged.historyRevision;
      conversationRevision.value = merged.conversationRevision;
    }
    return { ...merged, previousSnapshot };
  }

  function applyHydrateSnapshot(incoming, {
    agentId,
    generation,
    lifecycleSeq = 0,
    reconcileCoveredDrafts = false,
  } = {}) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation)) {
      return { accepted: false, stale: true, snapshot: hydrate.value, previousSnapshot: hydrate.value };
    }

    const merged = mergeSnapshot(incoming, { lifecycleSeq });
    let draftSnapshotReconciliation = null;
    if (reconcileCoveredDrafts && Array.isArray(incoming?.assistant_drafts)) {
      draftSnapshotReconciliation = reconcileDraftSnapshot(
        incoming.assistant_drafts,
        {
          agentId: id,
          generation,
          coveredAgentSeq: incoming.covered_agent_seq,
          coveredStreamSeq: incoming.covered_stream_seq,
        },
      );
    }
    const shouldApplySnapshot = merged.transcriptAccepted || merged.lifecycleAccepted;
    if (!shouldApplySnapshot) {
      return { ...merged, accepted: false, stale: false, snapshot: hydrate.value, draftSnapshotReconciliation };
    }
    if (merged.lifecycleAccepted && incoming?.turn_state && typeof incoming.turn_state === "object") {
      // Validate and apply the normalized lifecycle projection before exposing
      // any of its companion fields (HITL, queue, children) to the UI.
      turnLifecycle.applyHydrateTurnState(incoming);
    }
    if (turnLifecycle.state.phase === "model_generating") {
      const latestDraft = draftSnapshotReconciliation?.entries?.at(-1);
      if (latestDraft?.kind === "reasoning") turnLifecycle.setOutputChannel("reasoning");
      else if (latestDraft?.kind === "assistant") turnLifecycle.setOutputChannel("assistant");
    }

    const snapshot = merged.snapshot;
    let transcriptMerge = null;
    let draftReconciliation = null;
    if (merged.transcriptAccepted && Array.isArray(incoming?.transcript)) {
      transcriptMerge = mergeTranscriptReferences(id, generation, incoming.transcript);
      const conversationEpoch = Number(incoming?.conversation_epoch) || 0;
      if (readingWindow.value && Number(readingWindow.value.conversationEpoch) !== conversationEpoch) {
        setActiveReadingWindow(null);
      }
      setLatestHydrateTranscript(id, generation, conversationEpoch, transcriptMerge.transcript);
      const visibleTranscript = mergeTranscriptReadWindow(
        merged.previousSnapshot?.transcript,
        transcriptMerge.transcript,
        readingWindow.value,
        conversationEpoch,
      );
      replaceTranscriptReferences(id, generation, visibleTranscript);
      snapshot.transcript = visibleTranscript;
      draftReconciliation = reconcileDrafts(id, generation, visibleTranscript, {
        terminal: Boolean(snapshot.turn_state?.terminal),
        terminalTurnId: snapshot.turn_state?.turn_id,
        authoritativeItemIds: draftGapRecoveryItemIds,
      });
      for (const itemId of draftReconciliation.removedItemIds || []) draftGapRecoveryItemIds.delete(itemId);
    }

    hydrate.value = snapshot;
    return {
      ...merged,
      accepted: true,
      stale: false,
      snapshot,
      transcriptMerge,
      draftSnapshotReconciliation,
      draftReconciliation,
    };
  }

  function applyMediaTranscript(agentId, generation, {
    conversationEpoch = 0,
    historyRevision = -1,
    expectedConversationRevision = -1,
    transcript = [],
  } = {}) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation) || !hydrate.value) return false;
    if (Number(hydrate.value.conversation_epoch) !== Number(conversationEpoch)) return false;
    if (historyRevision >= 0 && Number(hydrate.value.history_revision) !== Number(historyRevision)) return false;
    if (expectedConversationRevision >= 0 && Number(hydrate.value.conversation_revision) !== Number(expectedConversationRevision)) return false;

    const latest = Array.isArray(transcript) ? transcript : [];
    setLatestHydrateTranscript(id, generation, conversationEpoch, latest);
    const visibleTranscript = mergeTranscriptReadWindow(
      hydrate.value.transcript,
      latest,
      readingWindow.value,
      conversationEpoch,
    );
    replaceTranscriptReferences(id, generation, visibleTranscript);
    hydrate.value = { ...hydrate.value, transcript: visibleTranscript };
    return true;
  }

  /** Apply the conversation-owned part of one active Node event in one place. */
  function applyActiveNodeEvent(event, { agentId, generation } = {}) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation) || event?.agent_id !== id) {
      return { kind: "ignored", accepted: false, reason: "scope" };
    }

    if (event.event_type === "conversation_patch") {
      return {
        kind: "patch",
        ...applyConversationPatch(event, { agentId: id, generation }),
      };
    }

    if (event.event_type === "conversation_delta") {
      const result = applyAssistantDraftDelta(event, { agentId: id, generation });
      return { kind: "draft", ...result };
    }

    if (event.event_type === "hitl_required") {
      return { kind: "hitl", ...applyHitlRequiredEvent(event) };
    }

    if (event.event_type !== "turn_state" && event.event_type !== "turn_finished") {
      return { kind: "unprojected", accepted: false, stale: false };
    }

    const turnStateAccepted = turnLifecycle.applyDesktopTurnEvent(event, id);
    let retiredDrafts = null;
    if (turnStateAccepted && event.event_type === "turn_state" && event.data?.command === "model_request_started") {
      retiredDrafts = retireDraftsForStep(id, generation, event.data?.step_id);
    }
    const stale = event.event_type === "turn_state" && !turnStateAccepted;
    return { kind: "lifecycle", accepted: true, stale, turnStateAccepted, retiredDrafts };
  }

  function applyHitlRequiredEvent(event) {
    const data = event?.data && typeof event.data === "object" ? event.data : {};
    const eventSeq = Number(data.event_seq);
    const hitlId = String(data.hitl_id || "").trim();
    const hitlItems = Array.isArray(data.items) ? data.items : [];
    if (!Number.isSafeInteger(eventSeq) || eventSeq <= 0) {
      return { accepted: false, stale: false, reason: "missing_sequence" };
    }
    if (!hitlId || hitlItems.length === 0) {
      return { accepted: false, stale: false, reason: "invalid_payload" };
    }

    const currentSeq = turnLifecycle.state.lifecycleSeq;
    if (eventSeq < currentSeq) {
      return { accepted: false, stale: true, reason: "stale_sequence" };
    }
    const currentHitlId = String(hydrate.value?.pending_hitl?.hitl_id || "").trim();
    if (eventSeq === currentSeq && currentHitlId && currentHitlId !== hitlId) {
      return { accepted: false, stale: true, reason: "sequence_conflict" };
    }

    if (eventSeq > currentSeq) turnLifecycle.advanceLifecycleSequence(eventSeq);
    const pendingHitl = {
      hitl_id: hitlId,
      message: String(data.message || ""),
      items: hitlItems,
      display_type: String(data.display_type || "normal_text"),
    };
    const current = hydrate.value || { transcript: [] };
    const turnState = current.turn_state && typeof current.turn_state === "object"
      ? { ...current.turn_state, lifecycle_seq: Math.max(Number(current.turn_state.lifecycle_seq) || 0, eventSeq) }
      : current.turn_state;
    hydrate.value = {
      ...current,
      ...(turnState ? { turn_state: turnState } : {}),
      pending_hitl: pendingHitl,
    };
    return { accepted: true, stale: false, pendingHitl, lifecycleSeq: eventSeq };
  }

  function applyConversationPatch(event, { agentId, generation } = {}) {
    const id = String(agentId || "").trim();
    const data = event?.data && typeof event.data === "object" ? event.data : {};
    if (!ownsScope(id, generation) || event?.agent_id !== id || String(data.conversation_id || "") !== id) {
      return { accepted: false, stale: true, structuralChanged: false };
    }
    const revision = Number(data.conversation_revision);
    const currentRevision = Number(conversationRevision.value);
    if (!Number.isSafeInteger(revision) || revision < 0 || (currentRevision >= 0 && revision < currentRevision)) {
      return { accepted: false, stale: true, structuralChanged: false };
    }

    const snapshot = hydrate.value || { transcript: [], conversation_epoch: 0 };
    const previousLatest = latestTranscriptFor(id, snapshot.conversation_epoch)?.transcript
      || (Array.isArray(snapshot.transcript) ? snapshot.transcript : []);
    const upsertItems = Array.isArray(data.upsert_items) ? data.upsert_items : [];
    const explicitRemoveIds = Array.isArray(data.remove_item_ids) ? data.remove_item_ids : [];
    const revisionRemoveIds = supersededRevisionItemIds(previousLatest, upsertItems);
    const removeItemIds = [...new Set([...explicitRemoveIds, ...revisionRemoveIds])];
    const indexById = transcriptIndexFor(id, generation, snapshot.conversation_epoch, previousLatest);
    const transcriptPatch = mergeCommittedTranscript(previousLatest, upsertItems, removeItemIds, indexById);
    const latest = transcriptPatch.rows;
    latestHydrateTranscript.value = {
      agentId: id,
      conversationEpoch: Math.max(0, Number(snapshot.conversation_epoch) || 0),
      transcript: latest,
    };
    latestTranscriptIndex = {
      agentId: id,
      generation: Number(generation) || 0,
      conversationEpoch: Math.max(0, Number(snapshot.conversation_epoch) || 0),
      byId: indexById,
    };
    patchTranscriptReferenceWindow(id, generation, latest, upsertItems, removeItemIds);

    const visible = readingWindow.value
      ? mergeTranscriptReadWindow(snapshot.transcript, latest, readingWindow.value, snapshot.conversation_epoch)
      : latest;
    let turnStateAccepted = false;
    if (data.turn_state && typeof data.turn_state === "object") {
      turnStateAccepted = turnLifecycle.applyDesktopTurnEvent({
        agent_id: id,
        event_type: "turn_state",
        data: data.turn_state,
      }, id);
    }

    const patchSnapshot = {
      ...snapshot,
      transcript: visible,
      conversation_revision: revision,
      history_revision: Math.max(Number(snapshot.history_revision) || 0, Number(data.history_revision) || 0),
      context_revision: Math.max(Number(snapshot.context_revision) || 0, Number(data.context_revision) || 0),
      result_commit_status: String(data.result_commit_status || snapshot.result_commit_status || ""),
    };
    if (turnStateAccepted && Object.prototype.hasOwnProperty.call(data, "pending_hitl")) {
      patchSnapshot.pending_hitl = data.pending_hitl;
    }
    if (data.input_updates_complete === true && Array.isArray(data.input_updates)) {
      patchSnapshot.pending_inputs = data.input_updates;
    }
    if (data.queue_state && typeof data.queue_state === "object") {
      if (Object.prototype.hasOwnProperty.call(data.queue_state, "pending_count")) {
        patchSnapshot.queue_pending = Math.max(0, Number(data.queue_state.pending_count) || 0);
      }
      patchSnapshot.queue_paused = Boolean(data.queue_state.paused);
      patchSnapshot.queue_control_revision = Math.max(0, Number(data.queue_state.control_revision) || 0);
    }
    if (turnStateAccepted) patchSnapshot.turn_state = data.turn_state;
    hydrate.value = patchSnapshot;

    const itemPatch = items.applyTranscriptPatch(upsertItems, removeItemIds);
    if (itemPatch.structuralChanged) {
      entriesChange.value = { revision: itemPatch.revision, kind: "patch", itemIds: [...itemPatch.insertedItemIds, ...itemPatch.removedItemIds] };
    }
    const draftReconciliation = reconcileDrafts(id, generation, upsertItems, {
      terminal: Boolean(patchSnapshot.turn_state?.terminal),
      terminalTurnId: patchSnapshot.turn_state?.turn_id,
    });
    contentRevision.value = Math.max(contentRevision.value, Number(patchSnapshot.history_revision) || 0);
    conversationRevision.value = Math.max(conversationRevision.value, revision);

    lastAppliedConversationPatch = {
      agentId: id,
      generation: Number(generation) || 0,
      agentSeq: Number(event?.agent_seq) || 0,
      conversationRevision: revision,
      lifecycleSeq: Number(patchSnapshot.turn_state?.lifecycle_seq) || 0,
      turnId: String(patchSnapshot.turn_state?.turn_id || ""),
      turnGeneration: Number(patchSnapshot.turn_state?.generation) || 0,
      terminal: Boolean(patchSnapshot.turn_state?.terminal),
    };
    return {
      accepted: true,
      stale: false,
      structuralChanged: itemPatch.structuralChanged,
      transcriptRowsVisited: transcriptPatch.rowsVisited,
      turnStateAccepted,
      draftReconciliation,
      snapshot: patchSnapshot,
    };
  }

  function conversationPatchCoversEvent(event, { agentId, generation } = {}) {
    const id = String(agentId || "").trim();
    const patch = lastAppliedConversationPatch;
    if (!ownsScope(id, generation) || !patch || patch.agentId !== id || patch.generation !== (Number(generation) || 0)) return false;
    if (event?.agent_id !== id) return false;
    if (event.event_type === "turn_state") {
      const data = event.data || {};
      return patch.lifecycleSeq > 0
        && Number(data.lifecycle_seq) <= patch.lifecycleSeq
        && String(data.turn_id || "") === patch.turnId
        && Number(data.generation) === patch.turnGeneration;
    }
    if (event.event_type === "turn_finished") {
      const data = event.data || {};
      return patch.terminal && String(data.turn_id || "") === patch.turnId
        && Number(data.turn_generation) === patch.turnGeneration;
    }
    return false;
  }

  function resetTurnLifecycle(options) {
    return turnLifecycle.resetTurnState(options);
  }

  function replaceHydrateSnapshot(snapshot) {
    hydrate.value = snapshot || null;
    return hydrate.value;
  }

  function beginSync(now = Date.now()) {
    syncStatus.value = { phase: "pending", startedAt: Number(now) || 0, resultCommitStatus: "" };
  }

  function markSyncing() {
    if (syncStatus.value.phase !== "pending") return false;
    syncStatus.value = { ...syncStatus.value, phase: "syncing" };
    return true;
  }

  function completeSync(resultCommitStatus = "") {
    syncStatus.value = { phase: "idle", startedAt: 0, resultCommitStatus: String(resultCommitStatus || "") };
  }

  function failSync() {
    syncStatus.value = { ...syncStatus.value, phase: "failed" };
  }

  function clearFailedSync() {
    if (syncStatus.value.phase !== "failed") return false;
    syncStatus.value = { phase: "idle", startedAt: 0, resultCommitStatus: "" };
    return true;
  }

  function setLatestHydrateTranscript(agentId, generation, conversationEpoch, transcript) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation)) return false;
    latestHydrateTranscript.value = {
      agentId: id,
      conversationEpoch: Math.max(0, Number(conversationEpoch) || 0),
      transcript: Array.isArray(transcript) ? transcript : [],
    };
    latestTranscriptIndex = null;
    transcriptIndexFor(id, generation, conversationEpoch, latestHydrateTranscript.value.transcript);
    return true;
  }

  function transcriptIndexFor(agentId, generation, conversationEpoch, transcript) {
    const id = String(agentId || "").trim();
    const version = Number(generation) || 0;
    const epoch = Math.max(0, Number(conversationEpoch) || 0);
    if (
      latestTranscriptIndex?.agentId === id
      && latestTranscriptIndex.generation === version
      && latestTranscriptIndex.conversationEpoch === epoch
    ) return latestTranscriptIndex.byId;

    const byId = new Map();
    for (let index = 0; index < (Array.isArray(transcript) ? transcript.length : 0); index += 1) {
      const itemId = String(transcript[index]?.item_id || transcript[index]?.id || "").trim();
      if (itemId) byId.set(itemId, index);
    }
    latestTranscriptIndex = { agentId: id, generation: version, conversationEpoch: epoch, byId };
    return byId;
  }

  function patchTranscriptReferenceWindow(agentId, generation, transcript, upserts = [], removeIds = []) {
    const id = String(agentId || "").trim();
    const version = Number(generation) || 0;
    if (!ownsScope(id, generation)) return false;
    if (transcriptReferenceWindow?.agentId !== id || transcriptReferenceWindow?.generation !== version) {
      transcriptReferenceWindow = replaceTranscriptReferenceWindow(id, version, transcript);
    }
    const stableItemsById = transcriptReferenceWindow.stableItemsById;
    const removed = new Set((Array.isArray(removeIds) ? removeIds : []).map(value => String(value || "").trim()).filter(Boolean));
    if (removed.size) {
      for (const [itemId, entry] of stableItemsById) {
        if (removed.has(itemId) || removed.has(String(entry?.detail_item_id || ""))) stableItemsById.delete(itemId);
      }
    }
    for (const entry of Array.isArray(upserts) ? upserts : []) {
      const itemId = typeof entry?.item_id === "string" ? entry.item_id.trim() : "";
      const nextVersion = Number(entry?.item_version);
      if (!itemId || !Number.isFinite(nextVersion)) continue;
      const previous = stableItemsById.get(itemId);
      const previousVersion = Number(previous?.item_version);
      if (Number.isFinite(previousVersion) && nextVersion <= previousVersion) continue;
      stableItemsById.set(itemId, entry);
    }
    return true;
  }

  function latestTranscriptFor(agentId, conversationEpoch) {
    const id = String(agentId || "").trim();
    const epoch = Math.max(0, Number(conversationEpoch) || 0);
    const latest = latestHydrateTranscript.value;
    return latest?.agentId === id && latest?.conversationEpoch === epoch ? latest : null;
  }

  function setActiveReadingWindow(window) {
    const next = window && typeof window === "object" ? window : null;
    readingWindow.value = next;
    const agentId = String(activeScope.split("\0", 1)[0] || "").trim();
    if (!agentId) return next;
    readingWindowsByAgent.delete(agentId);
    if (next) readingWindowsByAgent.set(agentId, next);
    while (readingWindowsByAgent.size > 64) {
      readingWindowsByAgent.delete(readingWindowsByAgent.keys().next().value);
    }
    return next;
  }

  function setReadingWindow(window) {
    return setActiveReadingWindow(window);
  }

  function applyReadingWindowChange(agentId, generation, state = {}, conversationEpoch = 0) {
    const id = String(agentId || "").trim();
    const epoch = Math.max(0, Number(conversationEpoch) || 0);
    if (!ownsScope(id, generation)) return { accepted: false, restoredLatest: false };

    if (state.following) {
      setActiveReadingWindow(null);
      const latest = latestTranscriptFor(id, epoch);
      if (!latest || !hydrate.value) return { accepted: true, restoredLatest: false };
      replaceTranscriptReferences(id, generation, latest.transcript);
      hydrate.value = { ...hydrate.value, transcript: latest.transcript };
      return { accepted: true, restoredLatest: true };
    }

    const anchorOrdinal = Number(state.anchorOrdinal);
    const stateEndOrdinal = Number(state.endOrdinal);
    if (!Number.isFinite(anchorOrdinal) || !Number.isFinite(stateEndOrdinal)) {
      return { accepted: false, restoredLatest: false };
    }
    const previous = readingWindow.value;
    const endOrdinal = previous?.conversationEpoch === epoch ? previous.endOrdinal : stateEndOrdinal;
    if (anchorOrdinal > endOrdinal) return { accepted: false, restoredLatest: false };
    const anchorItemKey = String(state.anchorItemKey || "").trim().slice(0, 512);
    const anchorOffset = Number(state.anchorOffset);
    setActiveReadingWindow({
      anchorOrdinal,
      endOrdinal,
      conversationEpoch: epoch,
      ...(anchorItemKey ? { anchorItemKey } : {}),
      ...(Number.isFinite(anchorOffset) ? { anchorOffset } : {}),
    });
    return { accepted: true, restoredLatest: false };
  }

  function appliedAgentSequence(agentId) {
    return appliedAgentSeqById.get(String(agentId || "").trim()) || 0;
  }

  function probedAgentSequence(agentId) {
    return probedAgentSeqById.get(String(agentId || "").trim()) || 0;
  }

  function hasAgentSequenceGap(agentId) {
    return agentWatermarkGapById.has(String(agentId || "").trim());
  }

  function noteAppliedAgentSequence(agentId, eventSequence) {
    const id = String(agentId || "").trim();
    if (!id) return { sequence: 0, gap: false, advanced: false, gapWasPresent: false };
    const previous = appliedAgentSequence(id);
    const cursor = advanceAppliedAgentSeq(previous, eventSequence);
    if (cursor.advanced) appliedAgentSeqById.set(id, cursor.sequence);
    const gapWasPresent = agentWatermarkGapById.has(id);
    if (cursor.gap) agentWatermarkGapById.add(id);
    return { ...cursor, gapWasPresent };
  }

  function markAgentSequenceProbed(agentId, sequence) {
    const id = String(agentId || "").trim();
    if (!id) return 0;
    const next = Math.max(probedAgentSequence(id), Math.max(0, Number(sequence) || 0));
    if (next) probedAgentSeqById.set(id, next);
    return next;
  }

  function clearAgentSequenceGap(agentId) {
    const id = String(agentId || "").trim();
    return id ? agentWatermarkGapById.delete(id) : false;
  }

  function applyAgentSequenceCoverage(agentId, coveredSequence) {
    const id = String(agentId || "").trim();
    const sequence = Number(coveredSequence);
    if (!id || !Number.isSafeInteger(sequence) || sequence < 0) return appliedAgentSequence(id);
    const current = appliedAgentSequence(id);
    if (sequence > current) appliedAgentSeqById.set(id, sequence);
    return Math.max(current, sequence);
  }

  function observedAgentWatermark(agentId) {
    return observedAgentWatermarks.get(String(agentId || "").trim()) || null;
  }

  function mergeAgentWatermark(agentId, watermark, { sameEpoch = true } = {}) {
    const id = String(agentId || "").trim();
    if (!id) return null;
    const merged = mergeWatermarkObservation(observedAgentWatermark(id), watermark, { sameEpoch });
    observedAgentWatermarks.set(id, merged);
    return merged;
  }

  function resetAgentSequences() {
    appliedAgentSeqById.clear();
    probedAgentSeqById.clear();
    observedAgentWatermarks.clear();
    agentWatermarkGapById.clear();
    eventSequencer.reset();
  }

  function offerAgentEvent(event, options) {
    return eventSequencer.offer(event, options);
  }

  function takeNextAgentEvent(options) {
    return eventSequencer.takeNext(options);
  }

  function takeDeferredAgentEvents(options) {
    return eventSequencer.takeAll(options);
  }

  function hasDeferredAgentEvents(agentId, generation) {
    return eventSequencer.hasPending(agentId, generation);
  }

  function mergeTranscriptReferences(agentId, generation, incoming = []) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation)) {
      return { transcript: Array.isArray(incoming) ? incoming : [], stableItemsById: new Map(), stale: true };
    }
    const merged = mergeTranscriptReferenceWindow(transcriptReferenceWindow, id, Number(generation) || 0, incoming);
    transcriptReferenceWindow = merged.window;
    return merged;
  }

  function replaceTranscriptReferences(agentId, generation, transcript = []) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation)) return false;
    transcriptReferenceWindow = replaceTranscriptReferenceWindow(id, Number(generation) || 0, transcript);
    return true;
  }

  function projectTranscript(transcript, agentId, generation, options) {
    if (!ownsScope(agentId, generation)) return Array.isArray(transcript) ? transcript : [];
    return projectAssistantDrafts(transcript, assistantDraftState.value, agentId, generation, options);
  }

  function replaceVisibleItems(agentId, generation) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation)) {
      return { accepted: false, stale: true, revision: items.revision.value };
    }

    const snapshot = hydrate.value;
    if (!snapshot) {
      const revision = items.replaceSnapshot([]);
      entriesChange.value = { revision, kind: "snapshot" };
      return { accepted: true, stale: false, revision, entries: items.entries };
    }

    const transcript = projectTranscript(
      snapshot.transcript || [],
      id,
      generation,
      { terminal: Boolean(snapshot.turn_state?.terminal) },
    );
    const pendingById = new Map();
    for (const queued of Array.isArray(snapshot.pending_inputs) ? snapshot.pending_inputs : []) {
      const inputId = String(queued?.input_id || "").trim();
      if (!inputId) continue;
      pendingById.set(inputId, {
        id: inputId,
        item_id: inputId,
        client_request_id: queued.client_request_id || "",
        kind: "user",
        text: queued.content || (queued.image_count ? `图片附件 × ${queued.image_count}` : ""),
        file_refs: Array.isArray(queued.file_refs) ? queued.file_refs : [],
        queue_status: queued.status || "queued",
        input_seq: queued.input_seq,
        input_revision: queued.input_revision,
      });
    }
    for (const pending of items.pendingInputsForAgent(id)) {
      const key = String(pending?.item_id || pending?.input_id || pending?.id || "").trim();
      if (key && !pendingById.has(key)) pendingById.set(key, pending);
    }

    const revision = items.replaceSnapshot(desktopChatEntries(id, transcript, [...pendingById.values()]));
    entriesChange.value = { revision, kind: "snapshot" };
    return { accepted: true, stale: false, revision, entries: items.entries };
  }

  function pendingInputsForAgent(agentId) {
    return items.pendingInputsForAgent(agentId);
  }

  function replacePendingInputs(agentId, pendingInputs) {
    return items.replacePendingInputs(agentId, pendingInputs);
  }

  function upsertPendingInput(agentId, input, identity) {
    return items.upsertPendingInput(agentId, input, identity);
  }

  function removePendingInput(agentId, identity) {
    return items.removePendingInput(agentId, identity);
  }

  function applyAssistantDraftDelta(event, { agentId, generation } = {}) {
    if (!ownsScope(agentId, generation)) return { state: assistantDraftState.value, accepted: false, reason: "scope" };
    const priorActiveContentItemId = activeContentItemId.value;
    const reduced = reduceAssistantDraftDelta(assistantDraftState.value, event, { agentId, generation });
    if (!reduced.accepted) return reduced;

    assistantDraftState.value = reduced.state;
    if (turnLifecycle.state.phase === "model_generating") {
      turnLifecycle.setOutputChannel(reduced.part === "reasoning" ? "reasoning" : "assistant");
    }
    const reasoningCounterpart = reduced.part === "reasoning"
      ? reduced.itemId.replace(/reasoning$/, "content")
      : "";
    const itemUpdate = items.upsertDraft(reduced.entry, { beforeId: reasoningCounterpart });
    if (itemUpdate.inserted) {
      entriesChange.value = {
        revision: itemUpdate.revision,
        kind: "insert",
        index: itemUpdate.index,
        itemId: reduced.itemId,
      };
    }

    if (reduced.attemptChanged && priorActiveContentItemId === reduced.itemId) activeContentItemId.value = "";
    if (reduced.part === "content") activeContentItemId.value = reduced.itemId;
    return { ...reduced, itemUpdate };
  }

  function assistantDraftText(agentId, generation, itemId = activeContentItemId.value) {
    if (!ownsScope(agentId, generation)) return "";
    const id = String(itemId || "").trim();
    if (!id) return "";
    return assistantDraftState.value.get(String(agentId).trim())?.itemsById?.get(id)?.parts?.get("content")?.text || "";
  }

  function refreshActiveContentItemId(agentId, generation) {
    if (!ownsScope(agentId, generation)) return false;
    const drafts = assistantDraftState.value.get(String(agentId).trim())?.itemsById;
    let latestId = "";
    if (drafts instanceof Map) {
      for (const [itemId, draft] of drafts) {
        if (draft?.parts?.has("content")) latestId = itemId;
      }
    }
    activeContentItemId.value = latestId;
    return true;
  }

  function hasAssistantDraftOutput(agentId, generation) {
    if (!ownsScope(agentId, generation)) return false;
    return Boolean(assistantDraftText(agentId, generation));
  }

  function reconcileDraftSnapshot(snapshots, options = {}) {
    if (!ownsScope(options.agentId, options.generation)) return { state: assistantDraftState.value, entries: [], stale: true };
    const result = reconcileAssistantDraftSnapshot(assistantDraftState.value, snapshots, options);
    assistantDraftState.value = result.state;
    refreshActiveContentItemId(options.agentId, options.generation);
    let inserted = [];
    for (const entry of result.entries || []) {
      const reasoningCounterpart = entry.kind === "reasoning" ? entry.item_id.replace(/reasoning$/, "content") : "";
      const updated = items.upsertDraft(entry, { beforeId: reasoningCounterpart });
      if (updated.inserted) inserted.push(entry.item_id);
    }
    if (inserted.length) {
      entriesChange.value = { revision: items.revision.value, kind: "insert", itemIds: inserted };
    }
    const transcriptIds = new Set((Array.isArray(hydrate.value?.transcript) ? hydrate.value.transcript : [])
      .map(entry => String(entry?.item_id || "").trim()).filter(Boolean));
    const orphaned = (result.removedItemIds || []).filter(itemId => !transcriptIds.has(itemId));
    if (result.removedItemIds.includes(activeContentItemId.value)) activeContentItemId.value = "";
    if (orphaned.length) {
      const itemPatch = items.applyTranscriptPatch([], orphaned);
      if (itemPatch.structuralChanged) {
        entriesChange.value = { revision: itemPatch.revision, kind: "remove", itemIds: itemPatch.removedItemIds };
      }
    }
    return result;
  }

  function reconcileDrafts(agentId, generation, transcript, options) {
    if (!ownsScope(agentId, generation)) return { state: assistantDraftState.value, removedItemIds: [], stale: true };
    const result = reconcileAssistantDrafts(assistantDraftState.value, agentId, generation, transcript, options);
    assistantDraftState.value = result.state;
    refreshActiveContentItemId(agentId, generation);
    const transcriptIds = new Set((Array.isArray(transcript) ? transcript : []).map(entry => String(entry?.item_id || "").trim()).filter(Boolean));
    const orphanedDraftIds = result.removedItemIds.filter(itemId => !transcriptIds.has(itemId));
    if (result.removedItemIds.includes(activeContentItemId.value)) activeContentItemId.value = "";
    if (orphanedDraftIds.length) {
      const itemPatch = items.applyTranscriptPatch([], orphanedDraftIds);
      if (itemPatch.structuralChanged) {
        entriesChange.value = { revision: itemPatch.revision, kind: "remove", itemIds: itemPatch.removedItemIds };
      }
    }
    return result;
  }

  function retireDraftsForStep(agentId, generation, stepId) {
    if (!ownsScope(agentId, generation)) return { state: assistantDraftState.value, removedItemIds: [], stale: true };
    const result = retireAssistantDraftsForStep(assistantDraftState.value, agentId, generation, stepId);
    assistantDraftState.value = result.state;
    if (result.removedItemIds.includes(activeContentItemId.value)) activeContentItemId.value = "";
    return result;
  }

  function claimDraftGapRecovery({ agentId, generation, itemId, attemptId, part } = {}) {
    const id = String(agentId || "").trim();
    if (!ownsScope(id, generation)) return { shouldRecover: false, reason: "scope" };
    const result = claimDraftGapRecoveryKey(draftGapRecoveryKeys, {
      agentId: id,
      generation,
      itemId,
      attemptId,
      part,
    });
    if (result.shouldRecover) {
      draftGapRecoveryKeys = result.recoveryKeys;
      draftGapRecoveryItemIds.add(String(itemId));
    }
    return { shouldRecover: result.shouldRecover };
  }

  function resetDrafts() {
    assistantDraftState.value = new Map();
    activeContentItemId.value = "";
    draftGapRecoveryKeys = new Set();
    draftGapRecoveryItemIds.clear();
  }

  function setActiveScope(agentId, generation) {
    const id = String(agentId || "").trim();
    const version = Number(generation) || 0;
    const nextScope = id ? `${id}\0${version}` : "";
    if (activeScope === nextScope) return false;

    const previousAgentId = String(activeScope.split("\0", 1)[0] || "").trim();
    if (previousAgentId && readingWindow.value) {
      readingWindowsByAgent.delete(previousAgentId);
      readingWindowsByAgent.set(previousAgentId, readingWindow.value);
      while (readingWindowsByAgent.size > 64) {
        readingWindowsByAgent.delete(readingWindowsByAgent.keys().next().value);
      }
    }
    activeScope = nextScope;
    items.setScope(nextScope);
    hydrate.value = null;
    syncStatus.value = { phase: "idle", startedAt: 0, resultCommitStatus: "" };
    contentRevision.value = -1;
    conversationRevision.value = -1;
    latestHydrateTranscript.value = null;
    const previousReadingWindow = id ? readingWindowsByAgent.get(id) || null : null;
    readingWindow.value = previousReadingWindow;
    if (id && previousReadingWindow) {
      readingWindowsByAgent.delete(id);
      readingWindowsByAgent.set(id, previousReadingWindow);
    }
    transcriptReferenceWindow = null;
    latestTranscriptIndex = null;
    lastAppliedConversationPatch = null;
    assistantDraftState.value = new Map();
    activeContentItemId.value = "";
    draftGapRecoveryKeys = new Set();
    draftGapRecoveryItemIds.clear();
    entriesChange.value = { revision: items.revision.value, kind: "snapshot" };
    eventSequencer.reset();
    return true;
  }

  return {
    hydrate,
    pendingHitl,
    hitlQueue,
    syncStatus,
    contentRevision,
    conversationRevision,
    latestHydrateTranscript,
    readingWindow,
    assistantDraftState,
    activeContentItemId,
    entriesChange,
    items,
    turnState: turnLifecycle.state,
    entries,
    itemsRevision,
    pendingInputsRevision,
    setActiveScope,
    mergeSnapshot,
    applyHydrateSnapshot,
    applyMediaTranscript,
    applyActiveNodeEvent,
    applyConversationPatch,
    conversationPatchCoversEvent,
    resetTurnState: resetTurnLifecycle,
    replaceHydrateSnapshot,
    beginSync,
    markSyncing,
    completeSync,
    failSync,
    clearFailedSync,
    setLatestHydrateTranscript,
    latestTranscriptFor,
    setReadingWindow,
    applyReadingWindowChange,
    appliedAgentSequence,
    probedAgentSequence,
    hasAgentSequenceGap,
    noteAppliedAgentSequence,
    markAgentSequenceProbed,
    clearAgentSequenceGap,
    applyAgentSequenceCoverage,
    observedAgentWatermark,
    mergeAgentWatermark,
    resetAgentSequences,
    offerAgentEvent,
    takeNextAgentEvent,
    takeDeferredAgentEvents,
    hasDeferredAgentEvents,
    mergeTranscriptReferences,
    replaceTranscriptReferences,
    replaceVisibleItems,
    pendingInputsForAgent,
    replacePendingInputs,
    upsertPendingInput,
    removePendingInput,
    applyAssistantDraftDelta,
    assistantDraftText,
    hasAssistantDraftOutput,
    reconcileDraftSnapshot,
    reconcileDrafts,
    retireDraftsForStep,
    claimDraftGapRecovery,
    resetDrafts,
  };
}
