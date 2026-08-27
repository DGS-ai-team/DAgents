const INACTIVE_PHASES = new Set(["idle", "completed", "failed", "cancelled", "interrupted", "budget_exhausted"]);

/** Classify a cancel response without relying on an optimistic UI update. */
export function classifyCancelOutcome(response, hydrate) {
  if (isHydrateIdle(hydrate)) return response?.cancelled === true ? "cancelled" : "already_idle";
  if (response?.cancelled === true) return "cancel_requested";
  return "not_cancelled";
}

export function isHydrateIdle(hydrate) {
  const state = hydrate?.turn_state;
  return Boolean(state && (state.terminal === true || INACTIVE_PHASES.has(String(state.phase || "").trim())));
}
