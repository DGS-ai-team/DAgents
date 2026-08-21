const ACTIVE_PHASES = new Set(["queued", "model_generating", "tool_executing", "model_streaming", "awaiting_tool_execution"]);
const INACTIVE_PHASES = new Set(["idle", "completed", "failed", "cancelled", "interrupted", "budget_exhausted"]);

/** Classify a cancel response without relying on an optimistic UI update. */
export function classifyCancelOutcome(response, hydrate) {
  if (isHydrateIdle(hydrate)) return response?.cancelled === true ? "cancelled" : "already_idle";
  if (response?.cancelled === true) return "cancel_requested";
  return "not_cancelled";
}

export function isHydrateIdle(hydrate) {
  const state = hydrate?.turn_state;
  if (state && (state.terminal === true || INACTIVE_PHASES.has(String(state.phase || "").trim()))) return true;
  if (!hydrate || hydrate.has_active_turn) return false;
  if (ACTIVE_PHASES.has(String(hydrate.run_turn_phase || "").trim())) return false;
  const pending = hydrate.pending_hitl?.items;
  return !Array.isArray(pending) || pending.length === 0;
}
