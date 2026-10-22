/** 流式 partial tool_call 块 ID 跟踪（对齐 Python _partial_tool_index_ids）。 */
const partialIndexIds = new Map();
const activeBlocks = new Set();

export function resetToolStream() {
  partialIndexIds.clear();
  activeBlocks.clear();
}

/**
 * 解析 tool_call 气泡 blockId。
 *
 * OpenAI 兼容流常先发带 id 的 delta，随后 args 分片 id 为空。
 * 必须在 partial 期间保持 toolIndex→真实 id 的映射，否则空 id 分片会另建
 * `partial-N` 气泡，tool_result 只结束真实 id，留下僵死「生成中」。
 */
export function resolveToolBlockId(callId, toolIndex, partial) {
  const id = String(callId || "").trim();
  let migrateFrom = "";
  if (id) {
    if (toolIndex >= 0 && partialIndexIds.has(toolIndex)) {
      const old = partialIndexIds.get(toolIndex);
      if (old && old !== id) migrateFrom = old;
    }
    if (partial && toolIndex >= 0) {
      // 流式期间记住真实 id，供后续空 id args 分片复用
      partialIndexIds.set(toolIndex, id);
    } else if (toolIndex >= 0) {
      partialIndexIds.delete(toolIndex);
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

/** 终态 tool_call / tool_result 时清理同 index 的占位气泡。 */
export function placeholderBlockIdForIndex(toolIndex) {
  if (toolIndex < 0) return "";
  return `partial-${toolIndex}`;
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
