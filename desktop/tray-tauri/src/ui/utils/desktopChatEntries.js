/** Normalize the Node transcript and optimistic inputs into stable chat rows. */
export function desktopChatEntries(agentId, transcript = [], pendingUser = null) {
  const rows = Array.isArray(transcript) ? transcript : [];
  const pendingUsers = Array.isArray(pendingUser) ? pendingUser : pendingUser ? [pendingUser] : [];
  const pendingIds = new Set();
  const pendingRequestIds = new Set();
  for (const pending of pendingUsers) {
    const itemId = String(pending?.item_id || pending?.input_id || "").trim();
    if (itemId) pendingIds.add(itemId);
    const requestId = String(pending?.client_request_id || "").trim();
    if (requestId) pendingRequestIds.add(requestId);
  }
  const persistedItemIds = new Set();
  const persistedRequestIds = new Set();
  const entries = rows.map(entry => {
    const itemId = String(entry?.item_id || entry?.id || "").trim();
    if (!itemId) throw new Error(`Node transcript item for ${agentId || "active Agent"} is missing its stable identity.`);
    if (itemId && pendingIds.has(itemId)) persistedItemIds.add(itemId);
    const requestId = String(entry?.client_request_id || "").trim();
    if (requestId && pendingRequestIds.has(requestId)) persistedRequestIds.add(requestId);
    const normalizedData = entry?.data && typeof entry.data === "object" && !Array.isArray(entry.data);
    if (itemId && entry.id === itemId && normalizedData) return entry;
    return {
      ...entry,
      id: itemId,
      item_id: itemId,
      data: normalizedData ? entry.data : {},
    };
  });
  for (const pending of pendingUsers) {
    const itemId = String(pending?.item_id || pending?.input_id || "").trim();
    const requestId = String(pending?.client_request_id || "").trim();
    const persisted = (itemId && persistedItemIds.has(itemId))
      || (requestId && persistedRequestIds.has(requestId));
    if (!pending || persisted) continue;
    const entry = { ...pending };
    const pendingId = itemId || String(entry.id || "").trim();
    if (!pendingId) throw new Error("Pending input is missing its stable identity.");
    entries.push({
      ...entry,
      id: pendingId,
      item_id: itemId || entry.item_id,
      data: {},
    });
  }
  return entries;
}
