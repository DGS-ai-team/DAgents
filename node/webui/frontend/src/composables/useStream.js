import { computed } from "vue";

function shouldSkipEntry(entry) {
  if (!entry) return true;
  if (entry.kind === "reasoning" && !entry.text?.trim() && !entry.streaming) return true;
  if ((entry.kind === "assistant" || entry.kind === "user") && !entry.text?.trim() && !entry.streaming) return true;
  return false;
}

/** 合并同 blockId 的 tool_call + tool_result 为 tool_step（F-UI6）。 */
export function buildStream(entries, hitlQueue = []) {
  const items = [];
  for (let i = 0; i < entries.length; i += 1) {
    const entry = entries[i];
    if (shouldSkipEntry(entry)) continue;

    if (entry.kind === "tool_call") {
      const next = entries[i + 1];
      if (next?.kind === "tool_result" && next.blockId === entry.blockId) {
        items.push({
          key: `tool-step-${entry.blockId}`,
          kind: "tool_step",
          callEntry: entry,
          resultEntry: next,
        });
        i += 1;
        continue;
      }
      items.push({
        key: `tool-call-${entry.blockId || entry.id}`,
        kind: "tool_step",
        callEntry: entry,
        resultEntry: null,
      });
      continue;
    }

    if (entry.kind === "tool_result") {
      items.push({
        key: `tool-result-${entry.blockId || entry.id}`,
        kind: "tool_step",
        callEntry: null,
        resultEntry: entry,
      });
      continue;
    }

    items.push({ key: `e-${entry.id}`, kind: entry.kind, entry });
  }

  hitlQueue.forEach((hitl, idx) => {
    items.push({ key: `hitl-${idx}-${hitl.kind}`, kind: hitl.kind, hitl, hitlIndex: idx });
  });
  return items;
}

export function useStream(entriesRef, hitlQueueRef) {
  return computed(() => buildStream(entriesRef.value, hitlQueueRef.value));
}
