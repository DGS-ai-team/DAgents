/**
 * Resolve the meaning of a normal composer submission.
 * Pending HITL interactions are answered only by their own card; while the
 * owning turn is active, ordinary composer text is a new queued input.
 */
export function normalComposerIntent({ turnRunning = false, interactionPending = false } = {}) {
  if (interactionPending && !turnRunning) return "blocked";
  return turnRunning ? "queue" : "send";
}
