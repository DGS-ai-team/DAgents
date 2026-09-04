import { reactive } from "vue";
import { transcriptStore, noteSeq } from "./transcript.js";
import * as api from "../api/node.js";
import {
  applyHydrateTurnState as applyAuthoritativeHydrateTurnState,
  beginTurnSubmission,
  resetTurnState,
} from "./turnState.js";

const AGENT_KEY = "dagents_webui_agent_id";

let pendingAckSeq = 0;
let lastAckedSeq = 0;
let ackInFlight = false;

function loadPersistedAgentId() {
  try {
    return localStorage.getItem(AGENT_KEY) || "";
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

/** 对齐运行时 ShouldBumpNotifySeq：仅 HITL/turn_finished 才推进 ack。 */
export function shouldAckSSEEvent(type, data) {
  switch (type) {
    case "hitl_required":
      return true;
    case "turn_finished": {
      const finishReason = String(data?.finish_reason || "").trim().toLowerCase();
      return finishReason !== "error" && finishReason !== "cancelled";
    }
    default:
      return false;
  }
}

function requestAck(seq) {
  const agentSeq = Number(seq) || 0;
  if (agentSeq <= 0) return;
  if (agentSeq > pendingAckSeq) pendingAckSeq = agentSeq;
  void flushAck();
}

async function flushAck() {
  const agentId = agentStore.agentId?.trim();
  const agentSeq = pendingAckSeq;
  if (!agentId || agentSeq <= 0 || agentSeq <= lastAckedSeq || ackInFlight) return;
  ackInFlight = true;
  try {
    await api.postAgentAck(agentId, agentSeq);
    lastAckedSeq = agentSeq;
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
  agentSeqFence: 0,
  error: "",
});

export function persistAgentId(id) {
  agentStore.agentId = id || "";
  try {
    if (id) {
      localStorage.setItem(AGENT_KEY, id);
    } else {
      localStorage.removeItem(AGENT_KEY);
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
  agentStore.agentSeqFence = transcriptStore.lastAgentSeq;
  agentStore.error = "";
}

/** 对齐 Go TurnGate.IsStale：优先使用 Agent 级连续游标。 */
export function isStaleEvent(seq, agentSeq = 0, epoch = "") {
  if (epoch && transcriptStore.streamEpoch && epoch !== transcriptStore.streamEpoch) return false;
  const cursor = Number(agentSeq) > 0 ? Number(agentSeq) : Number(seq);
  const fence = Number(agentSeq) > 0 ? agentStore.agentSeqFence : agentStore.seqFence;
  return cursor > 0 && cursor <= fence;
}

/** 同一游标的重复投递（双 SSE 连接等）。 */
export function isDuplicateEvent(seq, agentSeq = 0, epoch = "") {
  if (epoch && transcriptStore.streamEpoch && epoch !== transcriptStore.streamEpoch) return false;
  const cursor = Number(agentSeq) > 0 ? Number(agentSeq) : Number(seq);
  const last = Number(agentSeq) > 0 ? transcriptStore.lastAgentSeq : transcriptStore.lastSeq;
  return cursor > 0 && cursor <= last;
}

/** 更新 SSE 去重水位；ack 使用本 Agent 的连续游标。 */
export function markEventApplied(seq, { agentSeq = 0, epoch = "", ack = false } = {}) {
  noteSeq(Number(seq) || 0, Number(agentSeq) || 0, epoch);
  if (ack) requestAck(Number(agentSeq) > 0 ? agentSeq : seq);
}

export function ackAgentAfterHydrate(notifySeq) {
  // 用本 Agent 的 notify_seq 对齐未读游标，避免进程级 stream seq 把其他 Agent 流量算进 ack。
  const n = Number(notifySeq);
  if (Number.isFinite(n) && n > 0) {
    pendingAckSeq = n;
  } else {
    pendingAckSeq = transcriptStore.lastAgentSeq || transcriptStore.lastSeq;
  }
  void flushAck();
}

export function resetEventTracking() {
  agentStore.seqFence = 0;
  agentStore.agentSeqFence = 0;
  resetAckScheduler();
}

/**
 * 判断当前 Agent 游标是否出现不可解释的跳跃。ephemeral 事件不推进
 * agent_seq，因此它们不会制造假洞；调用方应 hydrate 修复真实断点。
 */
export function observeEventContinuity(agentSeq, epoch = "") {
  const next = Number(agentSeq) || 0;
  const currentEpoch = String(transcriptStore.streamEpoch || "");
  const incomingEpoch = String(epoch || "");
  if (incomingEpoch && currentEpoch && incomingEpoch !== currentEpoch) {
    return { epochChanged: true, gap: false };
  }
  if (next <= 0 || transcriptStore.lastAgentSeq <= 0) {
    return { epochChanged: false, gap: false };
  }
  return { epochChanged: false, gap: next > transcriptStore.lastAgentSeq + 1 };
}

/** hydrate 后设置 Node 事件纪元与 Agent 级重放水位。 */
export function applyHydrateSeqHint(data) {
  const payload = data || {};
  const epoch = String(payload.stream_epoch || "").trim();
  if (
    epoch &&
    ((transcriptStore.streamEpoch && epoch !== transcriptStore.streamEpoch) ||
      (!transcriptStore.streamEpoch && (transcriptStore.lastSeq > 0 || transcriptStore.lastAgentSeq > 0)))
  ) {
    transcriptStore.lastSeq = 0;
    transcriptStore.lastAgentSeq = 0;
    resetAckScheduler();
  }
  if (epoch) transcriptStore.streamEpoch = epoch;
  const streamHint = Number(payload.stream_seq_hint) || 0;
  const agentHint = Number(payload.agent_seq_hint) || 0;
  // A hydrate response may race with an already applied live/replayed event;
  // never move the cursor backwards within one epoch.
  if (streamHint > transcriptStore.lastSeq) transcriptStore.lastSeq = streamHint;
  if (agentHint > transcriptStore.lastAgentSeq) transcriptStore.lastAgentSeq = agentHint;
  agentStore.seqFence = streamHint > 0 ? streamHint : 0;
  agentStore.agentSeqFence = agentHint > 0 ? agentHint : 0;
}

/** hydrate 后恢复权威 Turn 状态。 */
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
