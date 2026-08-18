const RECONNECT_MS = 5000;

/** 切 Agent 后旧连接事件应丢弃：双方 id 都非空且不一致时忽略。 */
export function shouldIgnoreSSEForAgent(eventAgentId, currentAgentId) {
  const ev = String(eventAgentId || "").trim();
  const cur = String(currentAgentId || "").trim();
  return Boolean(ev && cur && ev !== cur);
}

/**
 * 构造 /v1/streams URL。
 * - 首连：live=1（只收增量，避免 replay 历史 done）
 * - 重连：after_seq（EventSource 无法设 Last-Event-ID header）从 hub 历史补洞
 */
export function buildStreamURL({ agentId = "", live = true, afterSeq = 0 } = {}) {
  const params = new URLSearchParams();
  const aid = String(agentId || "").trim();
  if (aid) params.set("agent_id", aid);
  if (live) {
    params.set("live", "1");
  } else {
    const seq = Number(afterSeq) || 0;
    if (seq > 0) params.set("after_seq", String(seq));
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
 * @param {() => (void|Promise<void>)} [opts.onReconnect] 断线后再次 onopen 时回调（用于 hydrate 对账）
 */
export function connectStream({ getAgentId, onEvent, onStatus, getAfterSeq, onReconnect }) {
  let es = null;
  let stopped = false;
  let reconnectTimer = null;
  let openCount = 0;
  let expectingReconnect = false;

  function open() {
    if (stopped) return;
    const agentId = (getAgentId?.() ?? "").trim();
    const resume = expectingReconnect;
    const afterSeq = resume ? Number(getAfterSeq?.() || 0) || 0 : 0;
    const url = buildStreamURL({
      agentId,
      live: !resume,
      afterSeq,
    });
    es = new EventSource(url);
    onStatus?.("connecting");

    es.onopen = () => {
      openCount += 1;
      onStatus?.("connected");
      if (resume || openCount > 1) {
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

    const types = [
      "assistant",
      "reasoning",
      "execution",
      "tool_call",
      "tool_result",
      "usage",
      "error",
      "done",
      "hitl_required",
      "temporary_agent_created",
      "temporary_agent_completed",
      "temporary_agent_cancelled",
      "context_compression_blocking",
      "context_compression_silent",
      "user_message_deferred",
      "side_effect_turn_start",
      "side_effect_applied",
      "side_effects_cleared",
      "terminal.opened",
      "terminal.updated",
      "terminal.closed",
    ];
    types.forEach((type) => {
      es.addEventListener(type, (ev) => {
        let envelope = {};
        try {
          envelope = JSON.parse(ev.data || "{}");
        } catch {
          envelope = {};
        }
        const data = envelope.data && typeof envelope.data === "object" ? envelope.data : envelope;
        const seq = Number(ev.lastEventId || envelope.seq || data.seq || 0);
        const eventAgentId = String(envelope.agent_id || data.agent_id || "").trim();
        onEvent({ type, data, seq, agentId: eventAgentId });
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
