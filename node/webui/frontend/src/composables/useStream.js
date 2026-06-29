import { computed } from "vue";

export function buildStream(entries, hitlQueue = []) {
  const items = [];
  for (const entry of entries) {
    if (entry.kind === "reasoning" && !entry.text?.trim() && !entry.streaming) continue;
    if ((entry.kind === "assistant" || entry.kind === "user") && !entry.text?.trim() && !entry.streaming) continue;
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
