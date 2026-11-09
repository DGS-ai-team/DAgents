import { reactive } from "vue";

/** 父 Agent 下子 Agent 的实时/终态投影。 */
const entries = reactive(new Map());
let parentAgentId = "";
let remoteEventVersion = 0;

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

function recentToolsFrom(raw, fallback = []) {
  const items = Array.isArray(raw) ? raw : Array.isArray(fallback) ? fallback : [];
  return items
    .map((item) => {
      const toolName = text(item?.tool_name);
      if (!toolName) return null;
      return {
        toolCallId: text(item?.tool_call_id),
        toolName,
        status: text(item?.status || "running") || "running",
        inputSummary: text(item?.input_summary),
        outputPreview: text(item?.output_preview),
        startedAt: text(item?.started_at),
        finishedAt: text(item?.finished_at),
      };
    })
    .filter(Boolean);
}

function progressFrom(item, fallback = {}) {
  const raw = item?.progress && typeof item.progress === "object" ? item.progress : item || {};
  return {
    status: text(raw.status || fallback.status || "active") || "active",
    phase: text(raw.phase || fallback.phase),
    // Node retains the historical JSON field name; the value is a step_index count.
    stepCount: intVal(raw.turn_count ?? fallback.stepCount),
    currentTool: text(raw.current_tool || fallback.currentTool),
    currentToolCallId: text(raw.current_tool_call_id || fallback.currentToolCallId),
    currentToolStatus: text(raw.current_tool_status || fallback.currentToolStatus),
    lastOutputPreview: text(raw.last_output_preview || fallback.lastOutputPreview),
    recentTools: Array.isArray(raw.recent_tools)
      ? recentToolsFrom(raw.recent_tools)
      : Array.isArray(fallback.recentTools)
        ? fallback.recentTools
        : [],
    pendingApproval: raw.pending_approval === true || fallback.pendingApproval === true,
    pendingApprovalData: raw.pending_approval_data || fallback.pendingApprovalData || null,
    summary: text(raw.summary || fallback.summary),
    error: text(raw.error || fallback.error),
    updatedAt: text(raw.updated_at || fallback.updatedAt),
    revision: intVal(raw.revision ?? fallback.revision),
  };
}

function entryFrom(item, previous = null) {
  let progress = progressFrom(item, previous?.progress);
  if (previous?.progress && progress.revision < previous.progress.revision) {
    progress = { ...previous.progress };
  }
  const terminal = ["completed", "failed", "cancelled", "expired", "interrupted"].includes(progress.status);
  return {
    childAgentId: text(item?.child_agent_id || previous?.childAgentId),
    toolCallId: text(item?.tool_call_id || previous?.toolCallId),
    purpose: text(item?.purpose || previous?.purpose),
    remoteEventVersion: previous?.remoteEventVersion || 0,
    awaitingApproval: progress.pendingApproval || (!terminal && previous?.awaitingApproval === true),
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

/** Switch the owner scope before any new hydrate or event can be applied. */
export function setRemoteWorkerParent(agentId) {
  const next = text(agentId);
  if (next === parentAgentId) return;
  parentAgentId = next;
  remoteEventVersion += 1;
  entries.clear();
  bump();
}

export function onChildCreated(data) {
  const id = String(data?.child_agent_id || "").trim();
  if (!id) return;
  remoteEventVersion += 1;
  entries.set(id, { ...entryFrom(data, entries.get(id)), remoteEventVersion });
  bump();
}

export function onChildProgress(data) {
  const id = text(data?.child_agent_id);
  if (!id) return;
  const previous = entries.get(id);
  const next = entryFrom(data, previous);
  if (previous && intVal(data?.revision) > 0 && intVal(data.revision) < previous.progress.revision) return;
  remoteEventVersion += 1;
  entries.set(id, { ...next, remoteEventVersion });
  bump();
}

export function onChildFinished(childOrData) {
  const data = typeof childOrData === "object" ? childOrData : { child_agent_id: childOrData };
  const id = text(data?.child_agent_id);
  if (!id) return;
  const previous = entries.get(id);
  const status = text(data?.status || "completed");
  remoteEventVersion += 1;
  entries.set(id, {
    ...entryFrom({
      ...data,
      status,
      phase: data?.phase || status,
      summary: data?.summary,
      error: data?.error || data?.reason,
    }, previous),
    remoteEventVersion,
  });
  bump();
}

export function applyRemoteWorkerEvent(eventType, data) {
  switch (text(eventType)) {
    case "temporary_agent_created":
      onChildCreated(data);
      return true;
    case "temporary_agent_progress":
      onChildProgress(data);
      return true;
    case "temporary_agent_completed":
      onChildFinished({ ...data, status: data?.status || "completed" });
      return true;
    case "temporary_agent_cancelled":
      onChildFinished({ ...data, status: data?.status || "cancelled" });
      return true;
    default:
      return false;
  }
}

export function replaceChildrenFromHydrate(items, ownerAgentId = parentAgentId, eventVersionAtStart = remoteEventVersion) {
  if (text(ownerAgentId) !== parentAgentId) return;
  const incoming = new Set();
  for (const item of items || []) {
    const id = String(item?.child_agent_id || "").trim();
    if (!id) continue;
    incoming.add(id);
    const previous = entries.get(id);
    if (previous?.remoteEventVersion > eventVersionAtStart) continue;
    entries.set(id, entryFrom(item, previous));
  }
  // Hydrate is authoritative for rows that existed when it started. Preserve
  // only lifecycle events received after that point; the next hydrate reconciles them.
  for (const [id, entry] of entries) {
    if (incoming.has(id)) continue;
    // Keep only events newer than the snapshot request. Earlier active rows
    // omitted by hydrate are stale and must be removed.
    if (entry.remoteEventVersion > eventVersionAtStart) continue;
    entries.delete(id);
  }
  bump();
}

export function currentRemoteWorkerEventVersion() {
  return remoteEventVersion;
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
