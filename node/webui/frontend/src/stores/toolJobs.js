import { reactive } from "vue";
import * as api from "../api/node.js";
import { toolStepIsInProgress } from "../utils/toolUserLabel.js";

export const toolJobsStore = reactive({
  running: 0,
  background: 0,
  busyCallIds: /** @type {Record<string, 'cancel' | 'background'>} */ ({}),
});

let pollTimer = null;

export function applyToolJobsSnapshot(data) {
  const running = Number(data?.running);
  const background = Number(data?.background);
  toolJobsStore.running = Number.isFinite(running) && running > 0 ? Math.floor(running) : 0;
  toolJobsStore.background = Number.isFinite(background) && background > 0 ? Math.floor(background) : 0;
}

export function toolJobsStripText() {
  const parts = [];
  if (toolJobsStore.running > 0) parts.push(`${toolJobsStore.running} 执行中`);
  if (toolJobsStore.background > 0) parts.push(`${toolJobsStore.background} 后台`);
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

export function canControlBashTool({ callEntry, resultEntry } = {}) {
  if (!toolStepIsInProgress({ callEntry, resultEntry })) return false;
  if (callEntry?.partial) return false;
  const name = toolNameFromEntry(callEntry);
  if (name !== "bash_run") return false;
  return Boolean(toolCallIdFromEntry(callEntry));
}

export async function refreshToolJobs(agentId) {
  const id = String(agentId || "").trim();
  if (!id) {
    applyToolJobsSnapshot({ running: 0, background: 0 });
    return;
  }
  try {
    const data = await api.getAgentToolJobs(id);
    applyToolJobsSnapshot(data);
  } catch {
    /* ignore transient poll errors */
  }
}

export function startToolJobsPolling(getAgentId) {
  stopToolJobsPolling();
  const tick = () => {
    const id = typeof getAgentId === "function" ? getAgentId() : getAgentId;
    refreshToolJobs(id);
  };
  tick();
  pollTimer = setInterval(tick, 2000);
}

export function stopToolJobsPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

export async function cancelBashToolCall(agentId, toolCallId) {
  const id = String(toolCallId || "").trim();
  if (!id) return;
  toolJobsStore.busyCallIds[id] = "cancel";
  try {
    await api.cancelAgentToolCall(agentId, id);
    await refreshToolJobs(agentId);
  } finally {
    delete toolJobsStore.busyCallIds[id];
  }
}

export async function backgroundBashToolCall(agentId, toolCallId) {
  const id = String(toolCallId || "").trim();
  if (!id) return;
  toolJobsStore.busyCallIds[id] = "background";
  try {
    await api.backgroundAgentToolCall(agentId, id);
    await refreshToolJobs(agentId);
  } finally {
    delete toolJobsStore.busyCallIds[id];
  }
}
