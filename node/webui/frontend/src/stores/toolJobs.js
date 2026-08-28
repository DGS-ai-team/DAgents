import { reactive } from "vue";
import * as api from "../api/node.js";
import { patchBashResultStatus } from "./transcript.js";

export const toolJobsStore = reactive({
  running: 0,
  background: 0,
  runningCallIds: /** @type {string[]} */ ([]),
  backgroundCallIds: /** @type {string[]} */ ([]),
  busyCallIds: /** @type {Record<string, 'cancel' | 'background'>} */ ({}),
});

let pollTimer = null;

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
  const prevBackground = new Set(toolJobsStore.backgroundCallIds);
  const running = Number(data?.running);
  const background = Number(data?.background);
  toolJobsStore.running = Number.isFinite(running) && running > 0 ? Math.floor(running) : 0;
  toolJobsStore.background = Number.isFinite(background) && background > 0 ? Math.floor(background) : 0;
  toolJobsStore.runningCallIds = normalizeIDList(data?.running_call_ids);
  toolJobsStore.backgroundCallIds = normalizeIDList(data?.background_call_ids);

  // 后台任务已从队列消失：若气泡仍是 RUNNING，按完成态改写（终止路径会先写成 CANCELLED）。
  for (const id of prevBackground) {
    if (toolJobsStore.backgroundCallIds.includes(id)) continue;
    if (toolJobsStore.busyCallIds[id] === "cancel") continue;
    // patchBashResultStatus 仅替换仍为 RUNNING 的标记；已是 CANCELLED 则 no-op。
    patchBashResultStatus(id, "SUCCEEDED");
  }
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

/** 解析 bash_run 结果正文中的 status=… */
export function parseBashResultStatus(content) {
  const m = String(content || "").match(/\[BASH_RESULT\]\s+status=([A-Za-z_]+)/);
  return m ? m[1].toUpperCase() : "";
}

export function isBashBackgroundRunning(resultEntry) {
  return parseBashResultStatus(resultEntry?.data?.content) === "RUNNING";
}

/** 气泡仍显示 RUNNING，且 /tool-jobs 仍登记为后台时，才视为真正在后台跑。 */
export function isBashBackgroundActive({ callEntry, resultEntry } = {}) {
  if (!isBashBackgroundRunning(resultEntry)) return false;
  const id = toolCallIdFromEntry(callEntry) || toolCallIdFromEntry(resultEntry);
  if (!id) return true;
  return toolJobsStore.backgroundCallIds.includes(id);
}

/**
 * bash 控制模式：
 * - running：真正同步执行中（可终止 + 转后台）
 * - background：已转后台仍在跑（仅可终止）
 * - null：参数生成中 / 审批中 / 已结束 — 不展示按钮
 *
 * 仅依据 /tool-jobs 返回的 call id（真正进执行器后才登记），避免审批阶段误显。
 */
export function bashControlMode({ callEntry, resultEntry } = {}) {
  const name = toolNameFromEntry(callEntry) || toolNameFromEntry(resultEntry);
  if (name !== "bash_run") return null;
  const id = toolCallIdFromEntry(callEntry) || toolCallIdFromEntry(resultEntry);
  if (!id) return null;
  if (toolJobsStore.runningCallIds.includes(id)) return "running";
  if (toolJobsStore.backgroundCallIds.includes(id)) return "background";
  return null;
}

export function canControlBashTool(args) {
  return bashControlMode(args) != null;
}

export async function refreshToolJobs(agentId) {
  const id = String(agentId || "").trim();
  if (!id) {
    applyToolJobsSnapshot({ running: 0, background: 0, running_call_ids: [], background_call_ids: [] });
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
  const ACTIVE_MS = 1000;
  const IDLE_MS = 5000;
  const tick = async () => {
    const id = typeof getAgentId === "function" ? getAgentId() : getAgentId;
    await refreshToolJobs(id);
    const busy = toolJobsStore.running + toolJobsStore.background > 0;
    pollTimer = setTimeout(tick, busy ? ACTIVE_MS : IDLE_MS);
  };
  void tick();
}

export function stopToolJobsPolling() {
  if (pollTimer) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
}

export async function cancelBashToolCall(agentId, toolCallId) {
  const id = String(toolCallId || "").trim();
  if (!id) return;
  toolJobsStore.busyCallIds[id] = "cancel";
  try {
    const response = await api.cancelAgentToolCall(agentId, id);
    if (response?.cancelled !== true) {
      throw new Error("工具终止状态未确认，请稍后重试");
    }
    // 先改写气泡终态，再刷新队列，避免短暂仍显示「后台执行中」。
    patchBashResultStatus(id, "CANCELLED");
    await refreshToolJobs(agentId);
  } finally {
    delete toolJobsStore.busyCallIds[id];
  }
}
