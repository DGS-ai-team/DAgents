import { reactive } from "vue";
import { applyToolExecutions } from "./toolJobs.js";

export const TERMINAL_TURN_PHASES = Object.freeze([
  "completed",
  "failed",
  "cancelled",
  "interrupted",
  "budget_exhausted",
  "finished",
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

export function normalizeTurnState(raw = {}) {
  const payload = raw?.turn_state && typeof raw.turn_state === "object" ? raw.turn_state : raw;
  const phase = trim(payload?.phase) || "idle";
  const terminal = Boolean(payload?.terminal) || TERMINAL_SET.has(phase);
  return {
    authority: trim(payload?.authority) || "turn_coordinator",
    phase,
    turnStatus: trim(payload?.turn_status),
    stepStatus: trim(payload?.step_status),
    turnId: trim(payload?.turn_id),
    stepId: trim(payload?.step_id),
    stepIndex: Number(payload?.step_index) || 0,
    generation: Number(payload?.generation) || 0,
    lifecycleSeq: Number(payload?.lifecycle_seq) || 0,
    historyRevision: Number(payload?.history_revision) || 0,
    terminal,
    endReason: trim(payload?.end_reason),
    interactionKind: trim(payload?.interaction_kind),
    recoveryRequired: Boolean(payload?.recovery_required),
    toolExecutions: Array.isArray(payload?.tool_executions)
      ? payload.tool_executions.map((item) => ({
          id: trim(item?.id),
          toolCallId: trim(item?.tool_call_id),
          toolName: trim(item?.tool_name),
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
  const next = normalizeTurnState(raw);
  if (!canAccept(next, turnStateStore, source)) return false;

  const submissionPending = ["posting", "accepted"].includes(turnStateStore.submitState);
  const cancellationPending = ["requesting", "confirmed"].includes(turnStateStore.cancelState);
  const identityChanged =
    next.turnId !== turnStateStore.turnId || next.generation !== turnStateStore.generation;
  const previousHistoryRevision = turnStateStore.historyRevision;
  Object.assign(turnStateStore, next);
  applyToolExecutions(next.toolExecutions);
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

/**
 * `turn_finished` is an explicit terminal event, but it can race with the
 * terminal turn_state projection (and some terminal paths have no projection
 * event). Correlate it to the active turn before using it to release the UI.
 */
export function applyTurnFinishedEvent(data = {}) {
  if (data.turn_complete !== true) return false;
  const turnId = trim(data.turn_id);
  const generation = Number(data.turn_generation) || 0;
  if (!turnId || generation <= 0) return false;
  if (!turnStateStore.turnId || turnStateStore.turnId !== turnId) return false;
  if (turnStateStore.generation !== generation) return false;
  // A terminal lifecycle projection carries richer information (recovery,
  // execution statuses, and the exact end reason); don't replace it with the
  // smaller SSE finish envelope.
  if (isTurnTerminal()) return true;

  const finishReason = trim(data.finish_reason).toLowerCase();
  const phase = finishReason === "budget_exhausted"
    ? "budget_exhausted"
    : finishReason === "cancelled"
      ? "cancelled"
      : ["error", "failed"].includes(finishReason)
        ? "failed"
        : ["stop", "completed", "success"].includes(finishReason)
          ? "completed"
          : "finished";
  return applyTurnState({
    authority: "turn_coordinator",
    phase,
    turn_id: turnId,
    generation,
    lifecycle_seq: turnStateStore.lifecycleSeq,
    terminal: true,
    end_reason: trim(data.budget_reason) || finishReason,
  });
}

const BUDGET_REASON_LABELS = Object.freeze({
  max_input_tokens: "输入令牌数上限",
  max_steps: "模型步骤数上限",
  max_summary_steps: "收尾摘要步骤预算",
  max_tool_calls: "工具调用数上限",
  max_tool_rounds: "工具轮次上限",
});

export function budgetExhaustionMessage(reason) {
  const key = trim(reason).toLowerCase();
  const label = BUDGET_REASON_LABELS[key];
  return label ? `本轮已达到${label}，请缩小任务后继续。` : "本轮已达到运行预算上限，请缩小任务后继续。";
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
    historyRevision: 0,
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
  return PROCESSING_SET.has(turnStateStore.phase) || INTERACTION_SET.has(turnStateStore.phase) || turnStateStore.submitState !== "idle";
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
