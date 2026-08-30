import { reactive } from "vue";

export const TERMINAL_TURN_PHASES = Object.freeze([
  "completed",
  "failed",
  "cancelled",
  "interrupted",
  "budget_exhausted",
]);

export const PROCESSING_TURN_PHASES = Object.freeze([
  "queued",
  "model_generating",
  "tool_executing",
]);

export const INTERACTION_TURN_PHASES = Object.freeze([
  "tool_waiting",
  "waiting_user",
]);

const TERMINAL_SET = new Set(TERMINAL_TURN_PHASES);
const PROCESSING_SET = new Set(PROCESSING_TURN_PHASES);
const INTERACTION_SET = new Set(INTERACTION_TURN_PHASES);

export const turnStateStore = reactive({
  authority: "unknown",
  phase: "idle",
  turnStatus: "",
  stepStatus: "",
  turnId: "",
  stepId: "",
  stepIndex: 0,
  generation: 0,
  lifecycleSeq: 0,
  historyRevision: 0,
  terminal: false,
  endReason: "",
  interactionKind: "",
  recoveryRequired: false,
  toolExecutions: [],
  outputChannel: "none",
  outputStartedAt: 0,
  submitState: "idle", // idle | posting | accepted | failed
  cancelState: "idle", // idle | requesting | confirmed | failed
});

function trim(value) {
  return String(value || "").trim();
}

export function normalizeTurnState(raw = {}, { source = "event" } = {}) {
  const payload = raw?.turn_state && typeof raw.turn_state === "object" ? raw.turn_state : raw;
  const phase = trim(payload?.phase) || "idle";
  const terminal = Boolean(payload?.terminal) || TERMINAL_SET.has(phase);
  return {
    authority: trim(payload?.authority) || "turn_coordinator",
    phase,
    turnStatus: trim(payload?.turn_status || payload?.turnStatus),
    stepStatus: trim(payload?.step_status || payload?.stepStatus),
    turnId: trim(payload?.turn_id || payload?.turnId),
    stepId: trim(payload?.step_id || payload?.stepId),
    stepIndex: Number(payload?.step_index || payload?.stepIndex) || 0,
    generation: Number(payload?.generation || payload?.turn_generation) || 0,
    lifecycleSeq: Number(payload?.lifecycle_seq || payload?.lifecycleSeq) || 0,
    historyRevision: Number(payload?.history_revision || payload?.historyRevision) || 0,
    terminal,
    endReason: trim(payload?.end_reason || payload?.reason || payload?.turn_end_reason),
    interactionKind: trim(payload?.interaction_kind || payload?.interactionKind),
    recoveryRequired: Boolean(payload?.recovery_required),
    toolExecutions: Array.isArray(payload?.tool_executions)
      ? payload.tool_executions.map((item) => ({
          id: trim(item?.id),
          toolCallId: trim(item?.tool_call_id || item?.toolCallId),
          toolName: trim(item?.tool_name || item?.toolName),
          status: trim(item?.status),
          attempt: Number(item?.attempt) || 0,
        }))
      : [],
  };
}

function canAccept(next, current, source) {
  if (source === "hydrate") return true;
  if (current.lifecycleSeq > 0 && next.lifecycleSeq > 0 && next.lifecycleSeq < current.lifecycleSeq) return false;
  if (current.generation > 0 && next.generation > 0 && next.generation < current.generation) return false;
  if (
    current.lifecycleSeq > 0 &&
    next.lifecycleSeq > 0 &&
    current.turnId &&
    next.turnId &&
    current.turnId !== next.turnId &&
    next.lifecycleSeq <= current.lifecycleSeq
  ) {
    return false;
  }
  return true;
}

export function applyTurnState(raw, { source = "event" } = {}) {
  const next = normalizeTurnState(raw, { source });
  if (!canAccept(next, turnStateStore, source)) return false;

  const submissionPending = ["posting", "accepted"].includes(turnStateStore.submitState);
  const cancellationPending = ["requesting", "confirmed"].includes(turnStateStore.cancelState);
  const identityChanged =
    next.turnId !== turnStateStore.turnId || next.generation !== turnStateStore.generation;
  const previousHistoryRevision = turnStateStore.historyRevision;
  Object.assign(turnStateStore, next);
  if (next.historyRevision === 0 && previousHistoryRevision > 0) {
    turnStateStore.historyRevision = previousHistoryRevision;
  }
  if (identityChanged || next.phase !== "model_generating") {
    turnStateStore.outputChannel = "none";
    turnStateStore.outputStartedAt = 0;
  }
  if (next.terminal || PROCESSING_SET.has(next.phase) || INTERACTION_SET.has(next.phase)) {
    turnStateStore.submitState = "idle";
  } else if (next.phase === "idle" && !submissionPending) {
    turnStateStore.submitState = "idle";
  }
  if (next.terminal || next.phase === "idle") {
    turnStateStore.cancelState = next.terminal && cancellationPending ? "confirmed" : "idle";
  }
  return true;
}

export function applyHydrateTurnState(data = {}) {
  return applyTurnState(data?.turn_state || {}, { source: "hydrate" });
}

export function setOutputChannel(channel) {
  const value = ["none", "reasoning", "assistant", "tool_call"].includes(channel)
    ? channel
    : "none";
  if (turnStateStore.outputChannel === value) return;
  turnStateStore.outputChannel = value;
  turnStateStore.outputStartedAt = value === "none" ? 0 : Date.now();
}

export function beginTurnSubmission() {
  turnStateStore.submitState = "posting";
  turnStateStore.cancelState = "idle";
  turnStateStore.outputChannel = "none";
  turnStateStore.outputStartedAt = 0;
}

export function markTurnAccepted() {
  turnStateStore.submitState = "accepted";
}

export function failTurnSubmission() {
  turnStateStore.submitState = "failed";
}

export function beginTurnCancellation() {
  turnStateStore.cancelState = "requesting";
}

export function markTurnCancellationConfirmed() {
  turnStateStore.cancelState = "confirmed";
}

export function markTurnCancellationFailed() {
  turnStateStore.cancelState = "failed";
}

export function resetTurnState() {
  Object.assign(turnStateStore, {
    authority: "unknown",
    phase: "idle",
    turnStatus: "",
    stepStatus: "",
    turnId: "",
    stepId: "",
    stepIndex: 0,
    generation: 0,
    lifecycleSeq: 0,
    terminal: false,
    endReason: "",
    interactionKind: "",
    recoveryRequired: false,
    toolExecutions: [],
    outputChannel: "none",
    outputStartedAt: 0,
    submitState: "idle",
    cancelState: "idle",
  });
}

export function isTurnProcessing() {
  return PROCESSING_SET.has(turnStateStore.phase) || turnStateStore.submitState !== "idle";
}

export function isTurnInteractionWaiting() {
  return INTERACTION_SET.has(turnStateStore.phase);
}

export function isTurnActive() {
  return PROCESSING_SET.has(turnStateStore.phase) || INTERACTION_SET.has(turnStateStore.phase);
}

export function isTurnTerminal() {
  return turnStateStore.terminal || TERMINAL_SET.has(turnStateStore.phase);
}

export function toolExecutionForCall(toolCallId) {
  const id = trim(toolCallId);
  if (!id) return null;
  return turnStateStore.toolExecutions.find((item) => item.toolCallId === id) || null;
}
