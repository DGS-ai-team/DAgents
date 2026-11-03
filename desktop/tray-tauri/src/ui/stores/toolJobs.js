import { reactive } from "vue";

export const toolJobsStore = reactive({
  runningCallIds: /** @type {string[]} */ ([]),
});

function normalizeIDList(raw) {
  if (!Array.isArray(raw)) return [];
  const out = [];
  const seen = new Set();
  for (const item of raw) {
    const id = String(item || "").trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push(id);
  }
  return out;
}
export function applyToolJobsSnapshot(data) {
  toolJobsStore.runningCallIds = normalizeIDList(data?.running_call_ids);
}

/**
 * Turn/SSE 是工具执行状态的主来源。工具任务接口只用于 hydrate 和显式
 * 取消后的对账，不再在前端持续轮询同一份状态。
 */
export function applyToolExecutions(executions) {
  const runningCallIds = normalizeIDList(
    (Array.isArray(executions) ? executions : [])
      .filter((item) => ["running", "executing", "in_progress", "started"].includes(String(item?.status || "").trim().toLowerCase()))
      .map((item) => item?.tool_call_id || item?.toolCallId || item?.id),
  );
  applyToolJobsSnapshot({ running: runningCallIds.length, running_call_ids: runningCallIds });
}

export function toolCallIdFromEntry(entry) {
  const data = entry?.data || {};
  return String(data.tool_call_id || data.id || entry?.blockId || "").trim();
}

export function toolNameFromEntry(entry) {
  const data = entry?.data || {};
  return String(data.tool_name || data.name || "").trim();
}
