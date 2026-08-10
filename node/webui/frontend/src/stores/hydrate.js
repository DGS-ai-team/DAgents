import * as api from "../api/node.js";
import { clearHitl, enqueueHitlRequired } from "./hitl.js";
import { setChildAwaitingApproval } from "./remoteWorkers.js";
import {
  applyHydrateSeqHint,
  applyHydrateTurnState,
  ackAgentAfterHydrate,
  agentStore,
  ensureAgent,
  finishTurn,
  persistAgentId,
} from "./agent.js";
import { loadTranscriptFromHydrate } from "./transcript.js";
import { applyToolJobsSnapshot } from "./toolJobs.js";
import { resetStatusLines, startStatus } from "./statusLines.js";

/**
 * hydrate 后复位 UI 状态相位。
 * model_streaming 时给 thinking 指示；工具执行中只锁 composer，不挂三点跳动。
 */
export function syncStatusAfterHydrate(runTurnPhase = "") {
  resetStatusLines();
  if (!agentStore.awaitingTurn) return;
  const phase = String(runTurnPhase || "").trim();
  if (phase === "awaiting_tool_execution") return;
  startStatus("thinking");
}

/** ensureAgent → GET /v1/agents/{id}/hydrate → 灌 transcript + pending HITL + SSE 水位。 */
export async function hydrateAgent() {
  const agentId = await ensureAgent();
  const data = await api.getAgentHydrate(agentId);
  loadTranscriptFromHydrate(data?.transcript);
  applyToolJobsSnapshot(data?.tool_jobs);
  clearHitl();
  const { approval } = enqueueHitlRequired(data?.pending_hitl);
  if (approval?.child_agent_id) {
    setChildAwaitingApproval(approval.child_agent_id, true);
  }
  applyHydrateSeqHint(data?.sse_seq_hint);
  ackAgentAfterHydrate(data?.notify_seq);
  applyHydrateTurnState({
    run_turn_phase: data?.run_turn_phase,
    has_active_turn: !!data?.has_active_turn,
    pending_hitl: data?.pending_hitl,
  });
  if (data?.pending_hitl?.items?.length) {
    finishTurn();
  }
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
