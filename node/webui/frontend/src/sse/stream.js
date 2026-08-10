const RECONNECT_MS = 5000;

/** 切 Agent 后旧连接事件应丢弃：双方 id 都非空且不一致时忽略。 */
export function shouldIgnoreSSEForAgent(eventAgentId, currentAgentId) {
  const ev = String(eventAgentId || "").trim();
  const cur = String(currentAgentId || "").trim();
  return Boolean(ev && cur && ev !== cur);
}

export function connectStream({ getAgentId, onEvent, onStatus }) {
  let es = null;
  let stopped = false;
  let reconnectTimer = null;

  function open() {
    if (stopped) return;
    const agentId = (getAgentId?.() ?? "").trim();
    const params = new URLSearchParams({ live: "1" });
    if (agentId) {
      params.set("agent_id", agentId);
    }
    const url = `/v1/streams?${params}`;
    es = new EventSource(url);
    onStatus?.("connecting");

    es.onopen = () => onStatus?.("connected");

    es.onerror = () => {
      onStatus?.("disconnected");
      es?.close();
      es = null;
      if (!stopped) {
        reconnectTimer = setTimeout(open, RECONNECT_MS);
      }
    };

    const types = [
      "assistant",
      "reasoning",
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
        const agentId = String(envelope.agent_id || data.agent_id || "").trim();
        onEvent({ type, data, seq, agentId });
      });
    });
  }

  open();

  return {
    close() {
      stopped = true;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      es?.close();
      es = null;
    },
  };
}
