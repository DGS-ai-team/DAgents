/** Return the conversation target while keeping Agent identity separate. */
export function conversationTarget(agentId, sessionId) {
  const dedicated = String(sessionId || "").trim();
  return dedicated || String(agentId || "").trim();
}

export function conversationChanged(previous, next) {
  return conversationTarget(previous?.agentId, previous?.sessionId) !==
    conversationTarget(next?.agentId, next?.sessionId);
}
