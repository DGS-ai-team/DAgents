import { reactive } from "vue";
import * as api from "../api/node.js";
import { agentStore } from "./agent.js";

/** 父 Agent 下活跃临时子 Agent（对齐 Python ChildAgentTracker）。 */
const entries = reactive(new Map());

export const remoteWorkerStore = reactive({
  tick: 0,
});

function bump() {
  remoteWorkerStore.tick += 1;
}

export function resetRemoteWorkers() {
  entries.clear();
  bump();
}

/** @deprecated 已退役；保留空实现以免旧调用方报错。 */
export function resetPeerInvokeInflight() {}

export function onChildCreated(data) {
  const id = String(data?.child_agent_id || "").trim();
  if (!id) return;
  entries.set(id, {
    purpose: String(data?.purpose || "").trim(),
    awaitingApproval: false,
  });
  bump();
}

export function onChildFinished(childId) {
  const id = String(childId || "").trim();
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
    entry = { purpose: "", awaitingApproval: false };
    entries.set(id, entry);
  }
  entry.awaitingApproval = !!on;
  bump();
}

export function replaceChildrenFromApi(items) {
  entries.clear();
  for (const item of items || []) {
    const id = String(item?.child_agent_id || "").trim();
    if (!id) continue;
    entries.set(id, {
      purpose: String(item?.purpose || "").trim(),
      awaitingApproval: false,
    });
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

/** @deprecated 已退役。 */
export function noteToolCallForWorkers(_data) {}

/** @deprecated 已退役。 */
export function noteToolResultForWorkers(_data) {}

export function workerStripText() {
  void remoteWorkerStore.tick;
  const childActive = entries.size;
  if (childActive > 0) return `子 Agent ${childActive} 工作中`;
  return "";
}
