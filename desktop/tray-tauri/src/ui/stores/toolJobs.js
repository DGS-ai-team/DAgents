import { reactive } from "vue";
import * as api from "../api/node.js";

export const toolJobsStore = reactive({
  running: 0,
  runningCallIds: /** @type {string[]} */ ([]),
  busyCallIds: /** @type {Record<string, 'cancel'>} */ ({}),
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
  const running = Number(data?.running);
  toolJobsStore.running = Number.isFinite(running) && running > 0 ? Math.floor(running) : 0;
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

export function toolJobsStripText() {
  const parts = [];
  if (toolJobsStore.running > 0) parts.push(`${toolJobsStore.running} 执行中`);
  return parts.join(" · ");
}

export function toolCallIdFromEntry(entry) {
  const data = entry?.data || {};
  return String(data.tool_call_id || data.id || entry?.blockId || "").trim();
}

export function toolNameFromEntry(entry) {
  const data = entry?.data || {};
  return String(data.tool_name || data.name || "").trim();
}

/**
 * bash 控制模式：running 表示同步执行中的 bash；其他阶段不可控制。
 *
 * 仅依据 /tool-jobs 返回的 call id（真正进执行器后才登记），避免审批阶段误显。
 */
export function bashControlMode({ callEntry, resultEntry } = {}) {
  const name = toolNameFromEntry(callEntry) || toolNameFromEntry(resultEntry);
  if (name !== "bash_run") return null;
  const id = toolCallIdFromEntry(callEntry) || toolCallIdFromEntry(resultEntry);
  if (!id) return null;
  if (toolJobsStore.runningCallIds.includes(id)) return "running";
  return null;
}

export function canControlBashTool(args) {
  return bashControlMode(args) != null;
}

export async function refreshToolJobs(agentId) {
  const id = String(agentId || "").trim();
  if (!id) {
    applyToolJobsSnapshot({ running: 0, running_call_ids: [] });
    return;
  }
  try {
    const data = await api.getAgentToolJobs(id);
    applyToolJobsSnapshot(data);
  } catch {
    /* ignore transient poll errors */
  }
}

export async function cancelBashToolCall(agentId, toolCallId) {
  const id = String(toolCallId || "").trim();
  if (!id) return;
  toolJobsStore.busyCallIds[id] = "cancel";
  try {
    const response = await api.cancelAgentToolCall(agentId, id);
    if (response?.cancelled !== true || response?.scope !== "tool_execution") {
      throw new Error("工具终止状态未确认，请稍后重试");
    }
    await refreshToolJobs(agentId);
  } finally {
    delete toolJobsStore.busyCallIds[id];
  }
}
