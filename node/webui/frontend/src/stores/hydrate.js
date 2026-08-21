import * as api from "../api/node.js";
import { clearHitl, enqueueHitlRequired } from "./hitl.js";
import { setChildAwaitingApproval } from "./remoteWorkers.js";
import {
  applyHydrateSeqHint,
  applyAuthoritativeTurnState,
  ackAgentAfterHydrate,
  ensureAgent,
  persistAgentId,
} from "./agent.js";
import { loadTranscriptFromHydrate, transcriptStore } from "./transcript.js";
import { applyToolJobsSnapshot } from "./toolJobs.js";
import { resetStatusLines, syncTurnStatus } from "./statusLines.js";
import { turnStateStore } from "./turnState.js";

// ChatView is KeepAlive-ed. A hydrate request can outlive the view that
// started it (for example while navigating to Agent settings). Do not let a
// response from that obsolete view overwrite a newer HITL/UI state.
let hydrationGeneration = 0;

export function invalidateHydration() {
  hydrationGeneration += 1;
}

/**
 * hydrate 后复位 UI 状态相位。
 * 模型生成时按已知输出通道展示模型生成中；思考内容到达后由 reasoning
 * 事件把展示细分切换为思考中。工具执行中只锁 composer，不挂模型气泡。
 */
export function syncStatusAfterHydrate(runTurnPhase = "") {
  resetStatusLines();
  if (turnStateStore.phase === "idle" && runTurnPhase) {
    // Compatibility with older nodes that do not return turn_state yet.
    const phase = String(runTurnPhase || "").trim();
    if (phase === "model_streaming") syncTurnStatus({ phase: "model_generating" });
    else if (phase === "awaiting_tool_execution") syncTurnStatus({ phase: "tool_executing" });
    return;
  }
  syncTurnStatus(turnStateStore);
}

function shouldApplyHydrateTranscript(data) {
  const incoming = Number(data?.history_revision) || 0;
  const current = Number(transcriptStore.historyRevision) || 0;
  if (!transcriptStore.historyDirty) {
    return incoming === 0 || incoming >= current;
  }

  // A locally streamed/user-visible history is newer than the last hydrate.
  // Do not replace it while a Turn is still active, even if an older or
  // partially committed snapshot reports a terminal lifecycle projection.
  const phase = String(turnStateStore.phase || "").trim();
  if (["posting", "accepted"].includes(turnStateStore.submitState)) {
    return false;
  }
  if (["queued", "model_generating", "tool_executing", "tool_waiting", "waiting_user"].includes(phase)) {
    return false;
  }
  return Boolean(data?.turn_state?.terminal) && incoming > current;
}

/** ensureAgent → GET /v1/agents/{id}/hydrate → 灌 transcript + pending HITL + SSE 水位。 */
export async function hydrateAgent() {
  const generation = ++hydrationGeneration;
  const agentId = await ensureAgent();
  const data = await api.getAgentHydrate(agentId);
  if (generation !== hydrationGeneration) return null;
  if (shouldApplyHydrateTranscript(data)) {
    loadTranscriptFromHydrate(data?.transcript, { historyRevision: data?.history_revision });
  }
  applyToolJobsSnapshot(data?.tool_jobs);
  clearHitl();
  const { approval } = enqueueHitlRequired(data?.pending_hitl);
  if (approval?.child_agent_id) {
    setChildAwaitingApproval(approval.child_agent_id, true);
  }
  applyHydrateSeqHint(data?.sse_seq_hint);
  ackAgentAfterHydrate(data?.notify_seq);
  applyAuthoritativeTurnState(data, { source: data?.turn_state ? "hydrate" : "hydrate_legacy" });
  syncStatusAfterHydrate(data?.run_turn_phase);
  return data;
}

/** 解析深链 ?agent=；兼容旧托盘 ?session=（实例 UUID 同源）。 */
export function consumeStartupURL() {
  if (typeof window === "undefined") return;
  const params = new URLSearchParams(window.location.search);
  const agent = params.get("agent")?.trim() || params.get("session")?.trim();
  if (agent) {
    persistAgentId(agent);
  }
}
