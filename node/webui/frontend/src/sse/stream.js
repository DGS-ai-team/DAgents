import { AGENT_STREAM_EVENT_TYPES } from "./agentEvents.js";

const RECONNECT_MS = 5000;
// The server emits a named heartbeat every 15s. Three missed heartbeats are
// enough to identify a half-open EventSource without reconnecting on a brief
// scheduling/network delay.
const HEARTBEAT_TIMEOUT_MS = 45000;

/** Agent 流必须带当前 Agent 标识；全局流不做 Agent 过滤。 */
export function shouldIgnoreSSEForAgent(eventAgentId, currentAgentId) {
  const ev = String(eventAgentId || "").trim();
  const cur = String(currentAgentId || "").trim();
  return Boolean(cur && ev !== cur);
}

/**
 * 构造 /v1/streams URL。
 * - Agent 首连/重连：after_agent_seq（从 hydrate 水位补齐首连竞态）
 * - 无 Agent 游标的首连：live=1（只收增量）
 * - 全局重连：after_seq（供 Node 级订阅者使用）
 */
export function buildStreamURL({ agentId = "", live = true, afterSeq = 0, afterAgentSeq = 0 } = {}) {
  const params = new URLSearchParams();
  const aid = String(agentId || "").trim();
  if (aid) params.set("agent_id", aid);
  if (live) {
    params.set("live", "1");
  } else {
    const agentSeq = Number(afterAgentSeq) || 0;
    const seq = Number(afterSeq) || 0;
    if (aid) params.set("after_agent_seq", String(agentSeq));
    else if (!aid && seq > 0) params.set("after_seq", String(seq));
    else params.set("live", "1");
  }
  return `/v1/streams?${params}`;
}

/**
 * @param {object} opts
 * @param {() => string} [opts.getAgentId]
 * @param {(ev: object) => void} [opts.onEvent]
 * @param {(status: string) => void} [opts.onStatus]
 * @param {() => number} [opts.getAfterSeq] 重连时使用的水位
 * @param {() => number} [opts.getAfterAgentSeq] Agent 过滤流重连时使用的连续水位
 * @param {() => (void|Promise<void>)} [opts.onReconnect] 断线后再次 onopen 时回调（用于 hydrate 对账）
 */
export function connectStream({ getAgentId, onEvent, onStatus, getAfterSeq, getAfterAgentSeq, onReconnect }) {
  let es = null;
  let stopped = false;
  let reconnectTimer = null;
  let heartbeatTimer = null;
  let openCount = 0;
  let expectingReconnect = false;

  function clearHeartbeatTimer() {
    if (heartbeatTimer) {
      clearTimeout(heartbeatTimer);
      heartbeatTimer = null;
    }
  }

  function scheduleReconnect() {
    if (stopped || reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      open();
    }, RECONNECT_MS);
  }

  function noteHeartbeat() {
    clearHeartbeatTimer();
    if (!stopped) {
      heartbeatTimer = setTimeout(() => {
        heartbeatTimer = null;
        if (stopped) return;
        expectingReconnect = true;
        const current = es;
        current?.close();
        es = null;
        onStatus?.("disconnected");
        scheduleReconnect();
      }, HEARTBEAT_TIMEOUT_MS);
    }
  }

  function open() {
    if (stopped) return;
    const agentId = (getAgentId?.() ?? "").trim();
    const reconnecting = expectingReconnect || openCount > 0;
    // ChatView hydrates before opening its filtered stream. Starting from the
    // hydrate cursor on the very first connection closes the gap between the
    // snapshot and SSE registration; do not collapse agent_seq=0 to live=1.
    const resume = reconnecting || Boolean(agentId && getAfterAgentSeq);
    const afterSeq = resume ? Number(getAfterSeq?.() || 0) || 0 : 0;
    const afterAgentSeq = resume ? Number(getAfterAgentSeq?.() || 0) || 0 : 0;
    const url = buildStreamURL({
      agentId,
      live: !resume,
      afterSeq,
      afterAgentSeq,
    });
    const source = new EventSource(url);
    es = source;
    onStatus?.("connecting");
    noteHeartbeat();

    source.onopen = () => {
      if (stopped || es !== source) return;
      openCount += 1;
      onStatus?.("connected");
      if (reconnecting) {
        try {
          void onReconnect?.();
        } catch {
          /* best-effort */
        }
      }
      expectingReconnect = false;
    };

    source.onerror = () => {
      if (stopped || es !== source) return;
      onStatus?.("disconnected");
      clearHeartbeatTimer();
      source.close();
      es = null;
      expectingReconnect = true;
      scheduleReconnect();
    };

    // SSE comments are invisible to JavaScript. The server sends this named
    // event alongside the comment heartbeat so the watchdog can detect a
    // half-open connection.
    source.addEventListener("stream_heartbeat", () => {
      if (stopped || es !== source) return;
      noteHeartbeat();
    });

    AGENT_STREAM_EVENT_TYPES.forEach((type) => {
      source.addEventListener(type, (ev) => {
        if (stopped || es !== source) return;
        noteHeartbeat();
        const envelope = parseEventEnvelope(ev.data);
        const data = envelope.data && typeof envelope.data === "object" ? envelope.data : envelope;
        const seq = Number(envelope.seq ?? ev.lastEventId ?? 0);
        const agentSeq = Number(envelope.agent_seq ?? 0);
        const eventAgentId = String(envelope.agent_id || "").trim();
        onEvent({
          type,
          data,
          seq,
          agentSeq,
          epoch: String(envelope.stream_epoch || "").trim(),
          delivery: String(envelope.delivery || "").trim(),
          eventVersion: Number(envelope.event_version || 0),
          agentId: eventAgentId,
        });
      });
    });
  }

  open();

  return {
    close() {
      stopped = true;
      expectingReconnect = false;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      clearHeartbeatTimer();
      es?.close();
      es = null;
    },
  };
}
function parseEventEnvelope(raw) {
  try {
    return JSON.parse(raw || "{}");
  } catch {
    return {};
  }
}
