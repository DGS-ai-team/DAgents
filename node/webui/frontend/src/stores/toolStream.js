/** 流式 partial tool_call 块 ID 跟踪（对齐 Python _partial_tool_index_ids）。 */
const partialIndexIds = new Map();
const activeBlocks = new Set();

export function resetToolStream() {
  partialIndexIds.clear();
  activeBlocks.clear();
}

export function resolveToolBlockId(callId, toolIndex, partial) {
  const id = String(callId || "").trim();
  let migrateFrom = "";
  if (id) {
    if (toolIndex >= 0 && partialIndexIds.has(toolIndex)) {
      const old = partialIndexIds.get(toolIndex);
      partialIndexIds.delete(toolIndex);
      if (old && old !== id) migrateFrom = old;
    }
    return { blockId: id, migrateFrom };
  }
  if (partial && toolIndex >= 0) {
    if (partialIndexIds.has(toolIndex)) {
      return { blockId: partialIndexIds.get(toolIndex), migrateFrom: "" };
    }
    const placeholder = `partial-${toolIndex}`;
    partialIndexIds.set(toolIndex, placeholder);
    return { blockId: placeholder, migrateFrom: "" };
  }
  return { blockId: "", migrateFrom: "" };
}

export function clearPartialToolIndex(toolIndex, partial) {
  if (!partial && toolIndex >= 0) partialIndexIds.delete(toolIndex);
}

export function markToolBlockActive(blockId) {
  const id = String(blockId || "").trim();
  if (id) activeBlocks.add(id);
}

export function forgetToolBlock(blockId) {
  const id = String(blockId || "").trim();
  if (id) activeBlocks.delete(id);
}

export function hasActiveToolBlock(blockId) {
  return activeBlocks.has(String(blockId || "").trim());
}
