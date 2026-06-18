import { reactive } from "vue";
import { transcriptStore } from "./transcript.js";
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
}

export function resetEventTracking() {
  sessionStore.lastAppliedSeq = transcriptStore.lastSeq;
  sessionStore.seqFence = 0;
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
