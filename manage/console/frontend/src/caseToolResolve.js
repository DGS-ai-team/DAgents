export function buildToolCallMap(messages) {
  const map = new Map();
  for (const msg of messages) {
    const raw = msg.raw || msg;
    if (raw.role !== "assistant" || !Array.isArray(raw.tool_calls)) continue;
    for (const tc of raw.tool_calls) {
      const id = tc?.id;
      if (!id) continue;
      const fn = tc.function || {};
      map.set(id, {
        name: fn.name || tc.name || "",
        arguments: fn.arguments ?? "",
      });
    }
  }
  return map;
}

export function resolveToolName(raw, toolCallMap) {
  const name = String(raw.name || "").trim();
  if (name) return name;

  const callId = String(raw.tool_call_id || "").trim();

  if (callId && toolCallMap.has(callId)) {
    const matched = toolCallMap.get(callId).name;
    if (matched) return matched;
  }

  return null;
}

export function filterLinkedToolMessages(messages) {
  const toolCallMap = buildToolCallMap(messages);
  return messages.filter((msg) => {
    if (msg.role !== "tool") return true;
    return resolveToolName(msg.raw || {}, toolCallMap);
  });
}
