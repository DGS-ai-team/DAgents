import { reactive } from "vue";
import { transcriptStore, noteSeq } from "./transcript.js";
import * as api from "../api/node.js";
import {
  applyHydrateTurnState as applyAuthoritativeHydrateTurnState,
  beginTurnSubmission,
  resetTurnState,
} from "./turnState.js";

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
	beginTurnSubmission();
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
  // Hub 序号是 Node 进程内的水位，Node 重启后会重新从 0 开始。
  // hydrate 返回较低水位时，说明前端仍持有上一进程的序号，必须切换纪元，
  // 否则新进程的所有 SSE 事件都会被 isDuplicateEvent 当成旧事件丢弃。
  if (hint < transcriptStore.lastSeq) {
    transcriptStore.lastSeq = hint;
  } else if (hint > 0) {
    noteSeq(hint);
  }
  agentStore.seqFence = hint > 0 ? hint : 0;
}

/** hydrate 后恢复权威 Turn 状态；旧调用点保留名称以兼容外部集成。 */
export function applyHydrateTurnState(data) {
	return applyAuthoritativeHydrateTurnState(data);
}

/** New lifecycle adapter used by ChatView; kept separate from transcript seq fencing. */
export function applyAuthoritativeTurnState(data, options) {
	return applyAuthoritativeHydrateTurnState(data, options);
}

export function resetAuthoritativeTurnState() {
	resetTurnState();
}
