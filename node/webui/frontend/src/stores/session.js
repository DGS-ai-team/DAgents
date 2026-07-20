import { reactive } from "vue";
import { transcriptStore, noteSeq } from "./transcript.js";
import * as api from "../api/node.js";

const AGENT_KEY = "dagents_webui_agent_id";
const LEGACY_SESSION_KEY = "dagents_webui_session_id";

let pendingAckSeq = 0;
let lastAckedSeq = 0;
let ackInFlight = false;

function loadPersistedAgentId() {
  try {
    const cur = localStorage.getItem(AGENT_KEY);
    if (cur) return cur;
    const legacy = localStorage.getItem(LEGACY_SESSION_KEY);
    if (legacy) {
      localStorage.setItem(AGENT_KEY, legacy);
      localStorage.removeItem(LEGACY_SESSION_KEY);
      return legacy;
    }
  } catch {
    /* ignore */
  }
  return "";
}

function resetAckScheduler() {
  pendingAckSeq = 0;
  lastAckedSeq = 0;
  ackInFlight = false;
}

/** 对齐 Go session.ShouldBumpNotifySeq：仅完整消息/HITL 才推进 ack。 */
export function shouldAckSSEEvent(type, data) {
  switch (type) {
    case "hitl_required":
    case "approval_required":
    case "user_information_required":
      return true;
    case "done":
      return shouldAckOnDone(data);
    default:
      return false;
  }
}

function shouldAckOnDone(data) {
  if (!data || typeof data !== "object") return false;
  if (String(data.awaiting || "") === "hitl") return false;
  const finish = String(data.finish_reason || "");
  if (
    finish === "awaiting_hitl" ||
    finish === "awaiting_user_information" ||
    finish === "awaiting_tool_approval" ||
    finish === "error" ||
    finish === "cancelled"
  ) {
    return false;
  }
  if (typeof data.turn_complete === "boolean") return data.turn_complete;
  return finish === "stop" || finish === "";
}

function requestAck(seq) {
  const sseSeq = Number(seq) || 0;
  if (sseSeq <= 0) return;
  if (sseSeq > pendingAckSeq) pendingAckSeq = sseSeq;
  void flushAck();
}

async function flushAck() {
  const agentId = sessionStore.sessionId?.trim();
  const sseSeq = pendingAckSeq;
  if (!agentId || sseSeq <= 0 || sseSeq <= lastAckedSeq || ackInFlight) return;
  ackInFlight = true;
  try {
    await api.postSessionAck(agentId, sseSeq);
    lastAckedSeq = sseSeq;
  } catch {
    /* ignore transient ack failures; later events will reschedule */
  } finally {
    ackInFlight = false;
    if (pendingAckSeq > lastAckedSeq) void flushAck();
  }
}

export const sessionStore = reactive({
  /** 当前激活的 Agent 实例 id（1 Agent = 1 主对话；字段名保留兼容既有组件）。 */
  sessionId: loadPersistedAgentId(),
  awaitingTurn: false,
  turnContentSeen: false,
  seqFence: 0,
  statusLine: "",
  error: "",
});

export function persistAgentId(id) {
  sessionStore.sessionId = id || "";
  try {
    if (id) {
      localStorage.setItem(AGENT_KEY, id);
      localStorage.removeItem(LEGACY_SESSION_KEY);
    } else {
      localStorage.removeItem(AGENT_KEY);
      localStorage.removeItem(LEGACY_SESSION_KEY);
    }
  } catch {
    /* ignore */
  }
}

/** @deprecated 使用 persistAgentId */
export function persistSessionId(id) {
  persistAgentId(id);
}

/**
 * 激活已有 Agent：ensure runtime（按快照 CreateWithOptions）。
 * 无选中 Agent 时不自动新建（走模板向导）。
 */
export async function ensureAgent() {
  const existing = sessionStore.sessionId?.trim() || "";
  if (!existing) {
    throw new Error("请先创建或选择一个 Agent");
  }
  await api.ensureAgentRuntime(existing);
  persistAgentId(existing);
  return existing;
}

/** @deprecated 使用 ensureAgent */
export async function ensureSession() {
  return ensureAgent();
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
  return seq > 0 && seq <= transcriptStore.lastSeq;
}

/** 更新 SSE 去重水位；ack 仅在与 notify_seq 对齐的完整消息时发送。 */
export function markEventApplied(seq, { ack = false } = {}) {
  noteSeq(seq);
  if (ack) requestAck(seq);
}

export function ackSessionAfterHydrate() {
  pendingAckSeq = transcriptStore.lastSeq;
  void flushAck();
}

export function resetEventTracking() {
  sessionStore.seqFence = 0;
  resetAckScheduler();
}

/** hydrate 后设置 SSE 去重水位（F-H9）：忽略 seq <= hint 的 replay。 */
export function applyHydrateSeqHint(seq) {
  const hint = Number(seq) || 0;
  if (hint > 0) noteSeq(hint);
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
