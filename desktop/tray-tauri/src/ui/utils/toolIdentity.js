export function toolCallIdFromEntry(entry) {
  const data = entry?.data || {};
  return String(data.tool_call_id || data.id || entry?.blockId || "").trim();
}

export function toolNameFromEntry(entry) {
  const data = entry?.data || {};
  return String(data.tool_name || data.name || "").trim();
}
