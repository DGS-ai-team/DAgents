function shouldSkipEntry(entry) {
  if (!entry) return true;
  // 思考内容仅流式时展示动态指示器，历史/落盘 reasoning 一律隐藏
  if (entry.kind === "reasoning") return !entry.streaming;
  if ((entry.kind === "assistant" || entry.kind === "user") && !entry.text?.trim() && !entry.streaming) return true;
  // 兜底：过滤注入型 user（日期 / 异步回灌等），与后端 hydrate 跳过对齐
  if (entry.kind === "user") {
    const name = String(entry.name || entry.data?.name || "").trim();
    if (["date", "async_tool", "compression", "compression_sidecar", "tool_vision"].includes(name)) {
      return true;
    }
    if (String(entry.text || "").startsWith("当天日期为：")) return true;
  }
  return false;
}

function entryBlockId(entry) {
  return String(entry?.blockId || entry?.data?.tool_call_id || entry?.data?.id || "").trim();
}

function isSkippableBetweenTools(entry) {
  if (!entry) return true;
  if (entry.kind === "system") return true;
  // 多工具并行时，hydrate 顺序常为 callA, callB, resultA, resultB；
  // 扫描匹配 result 时允许越过其它 tool_call。
  if (entry.kind === "tool_call") return true;
  return shouldSkipEntry(entry);
}

function findMatchingToolResult(entries, startIdx, blockId) {
  const bid = String(blockId || "").trim();
  if (!bid) return -1;
  for (let j = startIdx + 1; j < entries.length; j += 1) {
    const next = entries[j];
    if (isSkippableBetweenTools(next)) continue;
    if (next.kind === "tool_result" && entryBlockId(next) === bid) {
      return j;
    }
    // 遇到其它非可跳过内容则停止（例如真正的 user/assistant 气泡）
    if (next.kind !== "tool_result") {
      return -1;
    }
  }
  return -1;
}

/** 合并同 blockId 的 tool_call + tool_result 为 tool_step（F-UI6）。 */
export function buildStream(entries, hitlQueue = []) {
  const items = [];
  const mergedResultIndices = new Set();

  for (let i = 0; i < entries.length; i += 1) {
    if (mergedResultIndices.has(i)) continue;
    const entry = entries[i];
    if (shouldSkipEntry(entry)) continue;

    if (entry.kind === "tool_call") {
      const blockId = entryBlockId(entry);
      const resultIdx = findMatchingToolResult(entries, i, blockId);
      if (resultIdx >= 0) {
        mergedResultIndices.add(resultIdx);
        items.push({
          key: `tool-step-${blockId || entry.id}`,
          kind: "tool_step",
          callEntry: entry,
          resultEntry: entries[resultIdx],
        });
        continue;
      }
      items.push({
        key: `tool-call-${blockId || entry.id}`,
        kind: "tool_step",
        callEntry: entry,
        resultEntry: null,
      });
      continue;
    }

    if (entry.kind === "tool_result") {
      items.push({
        key: `tool-result-${entryBlockId(entry) || entry.id}`,
        kind: "tool_step",
        callEntry: null,
        resultEntry: entry,
      });
      continue;
    }

    if (entry.kind === "system") continue;

    items.push({ key: `e-${entry.id}`, kind: entry.kind, entry });
  }

  hitlQueue.forEach((hitl, idx) => {
    items.push({ key: `hitl-${idx}-${hitl.kind}`, kind: hitl.kind, hitl, hitlIndex: idx });
  });
  return items;
}
