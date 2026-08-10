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

/** 对齐运行时 ShouldBumpNotifySeq：仅完整消息/HITL 才推进 ack。 */
export function shouldAckSSEEvent(type, data) {
  switch (type) {
    case "hitl_required":
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
  const agentId = agentStore.agentId?.trim();
  const sseSeq = pendingAckSeq;
  if (!agentId || sseSeq <= 0 || sseSeq <= lastAckedSeq || ackInFlight) return;
  ackInFlight = true;
  try {
    await api.postAgentAck(agentId, sseSeq);
    lastAckedSeq = sseSeq;
  } catch {
    /* ignore transient ack failures; later events will reschedule */
  } finally {
    ackInFlight = false;
    if (pendingAckSeq > lastAckedSeq) void flushAck();
  }
}

export const agentStore = reactive({
  /** 当前激活的 Agent 实例 id（1 Agent = 1 主对话）。 */
  agentId: loadPersistedAgentId(),
  awaitingTurn: false,
  turnContentSeen: false,
  seqFence: 0,
  error: "",
});

export function persistAgentId(id) {
  agentStore.agentId = id || "";
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

/**
 * 激活已有 Agent：ensure runtime（按快照 CreateWithOptions）。
 * 无选中 Agent 时不自动新建（走模板向导）。
 */
export async function ensureAgent() {
  const existing = agentStore.agentId?.trim() || "";
  if (!existing) {
    throw new Error("请先创建或选择一个 Agent");
  }
  await api.ensureAgentRuntime(existing);
  persistAgentId(existing);
  return existing;
}

export function beginSubmit() {
  beginTurnWait();
}

/** 被动续跑（side_effect_continue）：等同 beginSubmit 但不 POST user message。 */
export function beginImplicitTurn() {
  beginTurnWait();
}

function beginTurnWait() {
  agentStore.awaitingTurn = true;
  agentStore.turnContentSeen = false;
  agentStore.seqFence = transcriptStore.lastSeq;
  agentStore.error = "";
}

/** 对齐 Go TurnGate.IsStale：seq<=seqFence 的在途/回放事件一律忽略。 */
export function isStaleEvent(seq) {
  return seq > 0 && seq <= agentStore.seqFence;
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

export function ackAgentAfterHydrate(notifySeq) {
  // 用本 Agent 的 notify_seq 对齐未读游标，避免 sse_seq_hint（全局 CurrentSeq）把他 Agent 流量算进 ack。
  const n = Number(notifySeq);
  if (Number.isFinite(n) && n > 0) {
    pendingAckSeq = n;
  } else {
    pendingAckSeq = transcriptStore.lastSeq;
  }
  void flushAck();
}

export function resetEventTracking() {
  agentStore.seqFence = 0;
  resetAckScheduler();
}

/** hydrate 后设置 SSE 去重水位（F-H9）：忽略 seq <= hint 的 replay。 */
export function applyHydrateSeqHint(seq) {
  const hint = Number(seq) || 0;
  if (hint > 0) noteSeq(hint);
  agentStore.seqFence = hint > 0 ? hint : 0;
}

/** 根据 hydrate 的 turn 状态恢复 awaitingTurn（F-H7）。 */
export function applyHydrateTurnState({ run_turn_phase, has_active_turn, pending_hitl }) {
  const phase = String(run_turn_phase || "").trim();
  const hasPending = pending_hitl && Array.isArray(pending_hitl.items) && pending_hitl.items.length > 0;
  if (hasPending || phase === "awaiting_hitl") {
    agentStore.awaitingTurn = false;
    agentStore.turnContentSeen = false;
    agentStore.error = "";
    return;
  }
  const activePhases = new Set(["model_streaming", "awaiting_tool_execution"]);
  if (has_active_turn && activePhases.has(phase)) {
    agentStore.awaitingTurn = true;
    agentStore.turnContentSeen = true;
    agentStore.error = "";
    return;
  }
  agentStore.awaitingTurn = false;
  agentStore.turnContentSeen = false;
}

export function finishTurn() {
  agentStore.awaitingTurn = false;
}

export function markTurnContent() {
  if (agentStore.awaitingTurn) agentStore.turnContentSeen = true;
}

export function shouldAcceptDone(seq) {
  if (!agentStore.awaitingTurn) return false;
  return agentStore.turnContentSeen || seq > agentStore.seqFence;
}
