import { reactive } from "vue";
import * as api from "../api/node.js";
import { agentStore } from "./agent.js";

/** 父 Agent 下活跃临时子 Agent（对齐 Python ChildAgentTracker）。 */
const entries = reactive(new Map());

/** 进行中的 agent_invoke（对端 Agent 执行任务）。 */
let peerInvokeInflight = 0;

export const remoteWorkerStore = reactive({
  tick: 0,
});

function bump() {
  remoteWorkerStore.tick += 1;
}

export function resetRemoteWorkers() {
  entries.clear();
  peerInvokeInflight = 0;
  bump();
}

export function resetPeerInvokeInflight() {
  if (peerInvokeInflight === 0) return;
  peerInvokeInflight = 0;
  bump();
}

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

export function noteToolCallForWorkers(data) {
  if (data?.partial) return;
  const bumpNames = [];
  const calls = data?.tool_calls;
  if (Array.isArray(calls)) {
    for (const call of calls) {
      const name = String(call?.name || call?.function?.name || "").trim();
      if (name === "agent_invoke") bumpNames.push(name);
    }
  } else {
    const name = String(data?.name || data?.tool_name || "").trim();
    if (name === "agent_invoke") bumpNames.push(name);
  }
  if (!bumpNames.length) return;
  peerInvokeInflight += bumpNames.length;
  bump();
}

export function noteToolResultForWorkers(data) {
  const name = String(data?.tool_name || data?.name || "").trim();
  if (name === "agent_invoke" && peerInvokeInflight > 0) {
    peerInvokeInflight -= 1;
    bump();
  }
}

export function workerStripText() {
  void remoteWorkerStore.tick;
  const childActive = entries.size;
  const parts = [];
  if (childActive > 0) parts.push(`子 Agent ${childActive} 工作中`);
  if (peerInvokeInflight > 0) parts.push(`对端 Agent ${peerInvokeInflight} 工作中`);
  return parts.join(" · ");
}
