import * as api from "../api/node.js";
import { clearHitl, enqueueA2ARelayPending, enqueueHitlRequired } from "./hitl.js";
import { setChildAwaitingApproval } from "./remoteWorkers.js";
import {
  applyHydrateSeqHint,
  applyHydrateTurnState,
  ackSessionAfterHydrate,
  ensureAgent,
  finishTurn,
  persistAgentId,
  sessionStore,
} from "./session.js";
import { loadTranscriptFromHydrate } from "./transcript.js";

/** ensureAgent → GET /v1/agents/{id}/hydrate → 灌 transcript + pending HITL + SSE 水位。 */
export async function hydrateSession() {
  const agentId = await ensureAgent();
  const data = await api.getAgentHydrate(agentId);
  loadTranscriptFromHydrate(data?.transcript);
  clearHitl();
  const { approval } = enqueueHitlRequired(data?.pending_hitl);
  enqueueA2ARelayPending(data?.pending_a2a_relay);
  if (approval?.child_session_id) {
    setChildAwaitingApproval(approval.child_session_id, true);
  }
  applyHydrateSeqHint(data?.sse_seq_hint);
  ackSessionAfterHydrate();
  applyHydrateTurnState({
    run_turn_phase: data?.run_turn_phase,
    has_active_turn: !!data?.has_active_turn,
    pending_hitl: data?.pending_hitl,
    pending_a2a_relay: data?.pending_a2a_relay,
  });
  if (data?.pending_hitl?.items?.length || data?.pending_a2a_relay?.event_type) {
    finishTurn();
  }
  return data;
}

/** 解析深链 ?agent= 或旧 ?session=。 */
export function consumeStartupURL() {
  if (typeof window === "undefined") return;
  const params = new URLSearchParams(window.location.search);
  const agent = params.get("agent")?.trim() || params.get("session")?.trim();
  if (agent) {
    persistAgentId(agent);
  }
}
