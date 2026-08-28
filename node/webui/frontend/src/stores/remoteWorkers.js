import { reactive } from "vue";
import * as api from "../api/node.js";
import { agentStore } from "./agent.js";

/** 父 Agent 下活跃临时子 Agent。 */
const entries = reactive(new Map());

export const remoteWorkerStore = reactive({
  tick: 0,
});

function intVal(value) {
  const n = Number(value);
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : 0;
}

function text(value) {
  return String(value || "").trim();
}

function progressFrom(item, fallback = {}) {
  const raw = item?.progress && typeof item.progress === "object" ? item.progress : item || {};
  return {
    status: text(raw.status || fallback.status || "active") || "active",
    phase: text(raw.phase || fallback.phase),
    turnCount: intVal(raw.turn_count ?? raw.turnCount ?? fallback.turn_count ?? fallback.turnCount),
    maxTurns: intVal(raw.max_turns ?? raw.maxTurns ?? fallback.max_turns ?? fallback.maxTurns),
    currentTool: text(raw.current_tool || raw.currentTool || fallback.current_tool || fallback.currentTool),
    currentToolCallId: text(raw.current_tool_call_id || raw.currentToolCallId || fallback.current_tool_call_id || fallback.currentToolCallId),
    currentToolStatus: text(raw.current_tool_status || raw.currentToolStatus || fallback.current_tool_status || fallback.currentToolStatus),
    lastOutputPreview: text(raw.last_output_preview || raw.lastOutputPreview || fallback.last_output_preview || fallback.lastOutputPreview),
    pendingApproval: raw.pending_approval === true || raw.pendingApproval === true || fallback.pending_approval === true || fallback.pendingApproval === true,
    summary: text(raw.summary || fallback.summary),
    error: text(raw.error || fallback.error),
    updatedAt: text(raw.updated_at || raw.updatedAt || fallback.updated_at || fallback.updatedAt),
    revision: intVal(raw.revision ?? fallback.revision),
  };
}

function entryFrom(item, previous = null) {
  let progress = progressFrom(item, previous?.progress);
  if (previous?.progress && progress.revision < previous.progress.revision) {
    progress = { ...previous.progress };
  }
  return {
    childAgentId: text(item?.child_agent_id || previous?.childAgentId),
    toolCallId: text(item?.tool_call_id || previous?.toolCallId),
    purpose: text(item?.purpose || previous?.purpose),
    awaitingApproval: progress.pendingApproval || previous?.awaitingApproval === true,
    progress,
  };
}

function bump() {
  remoteWorkerStore.tick += 1;
}

export function resetRemoteWorkers() {
  entries.clear();
  bump();
}

export function onChildCreated(data) {
  const id = String(data?.child_agent_id || "").trim();
  if (!id) return;
  entries.set(id, entryFrom(data, entries.get(id)));
  bump();
}

export function onChildProgress(data) {
  const id = text(data?.child_agent_id);
  if (!id) return;
  const previous = entries.get(id);
  const next = entryFrom(data, previous);
  if (previous && intVal(data?.revision) > 0 && intVal(data.revision) < previous.progress.revision) return;
  entries.set(id, next);
  bump();
}

export function onChildFinished(childOrData) {
  const id = text(typeof childOrData === "object" ? childOrData?.child_agent_id : childOrData);
  if (!id) return;
  entries.delete(id);
  bump();
}

export function setChildAwaitingApproval(childId, on) {
  const id = String(childId || "").trim();
  if (!id) return;
  let entry = entries.get(id);
  if (!entry) {
    if (!on) return;
    entry = entryFrom({ child_agent_id: id });
    entries.set(id, entry);
  }
  entry.awaitingApproval = !!on;
  entry.progress.pendingApproval = !!on;
  if (on) entry.progress.phase = "waiting_approval";
  bump();
}

export function replaceChildrenFromApi(items) {
  const incoming = new Set();
  for (const item of items || []) {
    const id = String(item?.child_agent_id || "").trim();
    if (!id) continue;
    incoming.add(id);
    entries.set(id, entryFrom(item, entries.get(id)));
  }
  // 保留刚由 SSE 观测到、但查询快照在请求发出时尚未包含的活跃 child；
  // 后续 completed/cancelled 事件会将其移除。
  for (const [id, entry] of entries) {
    if (incoming.has(id)) continue;
    if (entry.progress.revision > 0 && ["creating", "active"].includes(entry.progress.status)) continue;
    entries.delete(id);
  }
  bump();
}

export async function syncChildAgentsFromApi() {
  const sid = agentStore.agentId;
  if (!sid) return;
  try {
    const res = await api.listChildAgents(sid);
    replaceChildrenFromApi(res?.items || []);
  } catch {
    /* keep local SSE state */
  }
}

export function activeChildCount() {
  void remoteWorkerStore.tick;
  let count = 0;
  for (const entry of entries.values()) {
    if (["creating", "active"].includes(entry.progress.status)) count += 1;
  }
  return count;
}

export function workerStripText() {
  const childActive = activeChildCount();
  if (childActive > 0) return `子 Agent ${childActive} 工作中`;
  return "";
}

/** 返回与父工具调用关联的子 Agent 进度，供工具卡片展开区使用。 */
export function childProgressForTool(toolCallId, childAgentIds = []) {
  void remoteWorkerStore.tick;
  const callId = text(toolCallId);
  const ids = new Set((Array.isArray(childAgentIds) ? childAgentIds : []).map(text).filter(Boolean));
  const out = [];
  for (const entry of entries.values()) {
    if ((callId && entry.toolCallId === callId) || ids.has(entry.childAgentId)) {
      out.push(entry);
    }
  }
  return out;
}
