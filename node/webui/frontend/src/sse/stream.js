import { AGENT_STREAM_EVENT_TYPES } from "./agentEvents.js";

const RECONNECT_MS = 5000;

/** 切 Agent 后旧连接事件应丢弃：双方 id 都非空且不一致时忽略。 */
export function shouldIgnoreSSEForAgent(eventAgentId, currentAgentId) {
  const ev = String(eventAgentId || "").trim();
  const cur = String(currentAgentId || "").trim();
  return Boolean(ev && cur && ev !== cur);
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
  let openCount = 0;
  let expectingReconnect = false;

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
    es = new EventSource(url);
    onStatus?.("connecting");

    es.onopen = () => {
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

    es.onerror = () => {
      onStatus?.("disconnected");
      es?.close();
      es = null;
      expectingReconnect = true;
      if (!stopped) {
        reconnectTimer = setTimeout(open, RECONNECT_MS);
      }
    };

    AGENT_STREAM_EVENT_TYPES.forEach((type) => {
      es.addEventListener(type, (ev) => {
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
