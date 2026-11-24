import { reactive } from "vue";

export const TERMINAL_TURN_PHASES = Object.freeze([
  "completed",
  "failed",
  "cancelled",
  "interrupted",
  "budget_exhausted",
  "finished",
]);

const TERMINAL_SET = new Set(TERMINAL_TURN_PHASES);
const PROCESSING_SET = new Set(["queued", "model_generating", "tool_executing"]);
const INTERACTION_SET = new Set(["tool_waiting", "waiting_user"]);

function createInitialTurnState() {
  return reactive({
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
    modelAttempt: { status: "", attempt: 0, maxAttempts: 0, retryAfterMs: 0 },
    error: null,
    toolExecutions: [],
    outputChannel: "none",
  });
}

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
    modelAttempt: (() => {
      const attempt = payload?.model_attempt;
      return {
        status: trim(attempt?.status),
        attempt: Number(attempt?.attempt) || 0,
        maxAttempts: Number(attempt?.max_attempts) || 0,
        retryAfterMs: Number(attempt?.retry_after_ms) || 0,
      };
    })(),
    error: payload?.error && typeof payload.error === "object"
      ? {
          code: trim(payload.error.code),
          message: trim(payload.error.message),
          retryable: Boolean(payload.error.retryable),
          incomplete: Boolean(payload.error.incomplete),
        }
      : null,
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
  if (source === "hydrate" && !current.turnId && !next.turnId && current.phase === "idle" && next.phase === "idle") return true;
  if (source === "hydrate" && !current.turnId && current.lifecycleSeq === 0 && current.generation === 0) return true;
  if (source === "hydrate" && current.turnId && !next.turnId) return false;
  if (current.lifecycleSeq > 0 && !current.turnId && next.turnId && next.generation <= current.generation) return false;
  if (current.turnId && next.turnId === current.turnId && current.terminal && !next.terminal) return false;
  if (current.lifecycleSeq > 0 && next.lifecycleSeq === 0) return false;
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

function applyTurnStateTo(turnStateStore, raw, { source = "event" } = {}) {
  const next = normalizeTurnState(raw);
  if (!canAccept(next, turnStateStore, source)) return false;

  const identityChanged =
    next.turnId !== turnStateStore.turnId || next.generation !== turnStateStore.generation;
  const previousHistoryRevision = turnStateStore.historyRevision;
  const previousLifecycleSeq = turnStateStore.lifecycleSeq;
  const previousGeneration = turnStateStore.generation;
  Object.assign(turnStateStore, next);
  if (source === "hydrate" && !next.turnId && next.phase === "idle") {
    turnStateStore.lifecycleSeq = Math.max(turnStateStore.lifecycleSeq, previousLifecycleSeq);
    turnStateStore.generation = Math.max(turnStateStore.generation, previousGeneration);
  }
  if (next.phase === "failed" && !next.error) {
    turnStateStore.error = {
      code: "legacy_failure",
      message: "本轮未能完成，请稍后重试或切换模型。",
      retryable: false,
      incomplete: false,
    };
  } else if (next.phase === "interrupted" && !next.error) {
    turnStateStore.error = {
      code: "turn_interrupted",
      message: "本轮已中断。",
      retryable: false,
      incomplete: false,
    };
  }
  if (next.historyRevision === 0 && previousHistoryRevision > 0) {
    turnStateStore.historyRevision = previousHistoryRevision;
  }
  if (identityChanged || next.phase !== "model_generating") {
    turnStateStore.outputChannel = "none";
  }
  return true;
}

/**
 * `turn_finished` is an explicit terminal event, but it can race with the
 * terminal turn_state projection (and some terminal paths have no projection
 * event). Correlate it to the active turn before using it to release the UI.
 */
function applyTurnFinishedEventTo(turnStateStore, data = {}, applyTurnState, isTurnTerminal) {
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

/** Apply only turn lifecycle events belonging to the currently selected Agent. */
function applyDesktopTurnEventTo(event, activeAgentId, applyTurnState, applyTurnFinishedEvent) {
  if (!activeAgentId || event?.agent_id !== activeAgentId) return false;
  if (event.event_type === "turn_state") return applyTurnState(event.data);
  if (event.event_type === "turn_finished") return applyTurnFinishedEvent(event.data);
  return false;
}

const BUDGET_REASON_LABELS = Object.freeze({
  max_tool_calls: "工具调用数上限",
});

export function budgetExhaustionMessage(reason) {
  const key = trim(reason).toLowerCase();
  const label = BUDGET_REASON_LABELS[key];
  return label ? `本轮已达到${label}，请缩小任务后继续。` : "本轮已达到运行预算上限，请缩小任务后继续。";
}

function setOutputChannelFor(turnStateStore, channel) {
  const value = ["none", "reasoning", "assistant", "tool_call"].includes(channel)
    ? channel
    : "none";
  if (turnStateStore.outputChannel === value) return;
  turnStateStore.outputChannel = value;
}

function resetTurnStateFor(turnStateStore, { preserveLifecycle = false } = {}) {
  const lifecycleSeq = preserveLifecycle ? turnStateStore.lifecycleSeq : 0;
  const generation = preserveLifecycle ? turnStateStore.generation : 0;
  Object.assign(turnStateStore, {
    authority: "unknown",
    phase: "idle",
    turnStatus: "",
    stepStatus: "",
    turnId: "",
    stepId: "",
    stepIndex: 0,
    generation,
    lifecycleSeq,
    historyRevision: 0,
    terminal: false,
    endReason: "",
    interactionKind: "",
    recoveryRequired: false,
    modelAttempt: { status: "", attempt: 0, maxAttempts: 0, retryAfterMs: 0 },
    error: null,
    toolExecutions: [],
    outputChannel: "none",
  });
}

function isTurnProcessingFor(turnStateStore) {
  return PROCESSING_SET.has(turnStateStore.phase) || INTERACTION_SET.has(turnStateStore.phase);
}

function isTurnInteractionWaitingFor(turnStateStore) {
  return INTERACTION_SET.has(turnStateStore.phase);
}

function isTurnActiveFor(turnStateStore) {
  return PROCESSING_SET.has(turnStateStore.phase) || INTERACTION_SET.has(turnStateStore.phase);
}

function isTurnTerminalFor(turnStateStore) {
  return turnStateStore.terminal || TERMINAL_SET.has(turnStateStore.phase);
}

function toolExecutionForCallIn(turnStateStore, toolCallId) {
  const id = trim(toolCallId);
  if (!id) return null;
  return turnStateStore.toolExecutions.find((item) => item.toolCallId === id) || null;
}

/** Create the lifecycle reducer owned by one conversation store. */
export function createTurnStateStore() {
  const state = createInitialTurnState();
  const applyTurnState = (raw, options) => applyTurnStateTo(state, raw, options);
  const isTurnTerminal = () => isTurnTerminalFor(state);
  const applyTurnFinishedEvent = (data) => applyTurnFinishedEventTo(state, data, applyTurnState, isTurnTerminal);
  const advanceLifecycleSequence = (sequence) => {
    const next = Number(sequence);
    if (!Number.isSafeInteger(next) || next <= state.lifecycleSeq) return false;
    state.lifecycleSeq = next;
    return true;
  };
  return {
    state,
    applyTurnState,
    advanceLifecycleSequence,
    canAcceptHydrateTurnState: (data = {}) => canAccept(normalizeTurnState(data?.turn_state || {}), state, "hydrate"),
    applyHydrateTurnState: (data = {}) => applyTurnState(data?.turn_state || {}, { source: "hydrate" }),
    applyTurnFinishedEvent,
    applyDesktopTurnEvent: (event, activeAgentId) => applyDesktopTurnEventTo(event, activeAgentId, applyTurnState, applyTurnFinishedEvent),
    setOutputChannel: (channel) => setOutputChannelFor(state, channel),
    resetTurnState: (options) => resetTurnStateFor(state, options),
    isTurnProcessing: () => isTurnProcessingFor(state),
    isTurnInteractionWaiting: () => isTurnInteractionWaitingFor(state),
    isTurnActive: () => isTurnActiveFor(state),
    isTurnTerminal,
    toolExecutionForCall: (toolCallId) => toolExecutionForCallIn(state, toolCallId),
  };
}
