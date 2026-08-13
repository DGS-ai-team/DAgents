const ACTIVE_PHASES = new Set(["model_streaming", "awaiting_tool_execution"]);

/** Classify a cancel response without relying on an optimistic UI update. */
export function classifyCancelOutcome(response, hydrate) {
  if (response?.cancelled === true) return "cancelled";
  if (isHydrateIdle(hydrate)) return "already_idle";
  return "not_cancelled";
}

export function isHydrateIdle(hydrate) {
  if (!hydrate || hydrate.has_active_turn) return false;
  if (ACTIVE_PHASES.has(String(hydrate.run_turn_phase || "").trim())) return false;
  const pending = hydrate.pending_hitl?.items;
  return !Array.isArray(pending) || pending.length === 0;
}
