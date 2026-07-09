import { reactive } from "vue";
import { transcriptStore, noteSeq } from "./transcript.js";
import * as api from "../api/node.js";

const SESSION_KEY = "dagents_webui_session_id";

export const sessionStore = reactive({
  sessionId: localStorage.getItem(SESSION_KEY) || "",
  awaitingTurn: false,
  turnContentSeen: false,
  seqFence: 0,
  lastAppliedSeq: 0,
  statusLine: "",
  error: "",
});

export function persistSessionId(id) {
  sessionStore.sessionId = id || "";
  if (id) localStorage.setItem(SESSION_KEY, id);
  else localStorage.removeItem(SESSION_KEY);
}

/** 恢复或创建 session，并在 Node 内存中激活 consumer（重启 Node 后须 POST /v1/sessions 恢复）。 */
export async function ensureSession() {
  const existing = sessionStore.sessionId?.trim() || "";
  if (existing) {
    try {
      const res = await api.createSession(existing);
      persistSessionId(res.session_id);
      return res.session_id;
    } catch {
      persistSessionId("");
    }
  }
  const res = await api.createSession("");
  persistSessionId(res.session_id);
  return res.session_id;
}

export function beginSubmit() {
  beginTurnWait();
}

/** 被动续跑（side_effect_continue）：等同 beginSubmit 但不 POST user message。 */
export function beginImplicitTurn() {
  beginTurnWait();
}

function beginTurnWait() {
  sessionStore.awaitingTurn = true;
  sessionStore.turnContentSeen = false;
  sessionStore.seqFence = transcriptStore.lastSeq;
  sessionStore.error = "";
}

/** 对齐 Go TurnGate.IsStale：seq<=seqFence 的在途/回放事件一律忽略。 */
export function isStaleEvent(seq) {
  return seq > 0 && seq <= sessionStore.seqFence;
}

/** 同一 seq 的重复投递（双 SSE 连接等）。 */
export function isDuplicateEvent(seq) {
  return seq > 0 && seq <= sessionStore.lastAppliedSeq;
}

export function markEventApplied(seq) {
  if (seq > sessionStore.lastAppliedSeq) sessionStore.lastAppliedSeq = seq;
  void ackSessionRead(sessionStore.lastAppliedSeq);
}

async function ackSessionRead(seq) {
  const sessionId = sessionStore.sessionId?.trim();
  const sseSeq = Number(seq) || 0;
  if (!sessionId || sseSeq <= 0) return;
  try {
    await api.postSessionAck(sessionId, sseSeq);
  } catch {
    /* ignore transient ack failures */
  }
}

export function ackSessionAfterHydrate() {
  void ackSessionRead(sessionStore.lastAppliedSeq);
}

export function resetEventTracking() {
  sessionStore.lastAppliedSeq = transcriptStore.lastSeq;
  sessionStore.seqFence = 0;
}

/** hydrate 后设置 SSE 去重水位（F-H9）：忽略 seq <= hint 的 replay。 */
export function applyHydrateSeqHint(seq) {
  const hint = Number(seq) || 0;
  if (hint > 0) noteSeq(hint);
  sessionStore.lastAppliedSeq = hint > 0 ? hint : transcriptStore.lastSeq;
  sessionStore.seqFence = hint > 0 ? hint : 0;
}

/** 根据 hydrate 的 turn 状态恢复 awaitingTurn（F-H7）。 */
export function applyHydrateTurnState({ run_turn_phase, has_active_turn, pending_hitl, pending_a2a_relay }) {
  const phase = String(run_turn_phase || "").trim();
  const hasPending = pending_hitl && Array.isArray(pending_hitl.items) && pending_hitl.items.length > 0;
  const hasRelay = pending_a2a_relay && pending_a2a_relay.event_type && pending_a2a_relay.data;
  if (hasPending || hasRelay || phase === "awaiting_hitl") {
    sessionStore.awaitingTurn = false;
    sessionStore.turnContentSeen = false;
    sessionStore.error = "";
    return;
  }
  const activePhases = new Set(["model_streaming", "awaiting_tool_execution", "tool_loop", "open_batch", "other"]);
  if (has_active_turn && activePhases.has(phase)) {
    sessionStore.awaitingTurn = true;
    sessionStore.turnContentSeen = true;
    sessionStore.error = "";
    return;
  }
  sessionStore.awaitingTurn = false;
  sessionStore.turnContentSeen = false;
}

export function finishTurn() {
  sessionStore.awaitingTurn = false;
}

export function markTurnContent() {
  if (sessionStore.awaitingTurn) sessionStore.turnContentSeen = true;
}

export function shouldAcceptDone(seq) {
  if (!sessionStore.awaitingTurn) return false;
  return sessionStore.turnContentSeen || seq > sessionStore.seqFence;
}
