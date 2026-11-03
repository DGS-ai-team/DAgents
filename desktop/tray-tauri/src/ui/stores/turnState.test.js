import { beforeEach, describe, expect, it } from "vitest";
import {
  applyTurnState,
  applyTurnFinishedEvent,
  applyHydrateTurnState,
  applyDesktopTurnEvent,
  budgetExhaustionMessage,
  beginTurnCancellation,
  isTurnInteractionWaiting,
  isTurnProcessing,
  isTurnTerminal,
  resetTurnState,
  setOutputChannel,
  toolExecutionForCall,
  turnStateStore,
} from "./turnState.js";
import { hasStatus, resetStatusLines, syncTurnStatus } from "./statusLines.js";

describe("turnState", () => {
  beforeEach(() => {
    resetTurnState();
    resetStatusLines();
  });

  it("accepts lifecycle events in sequence and ignores stale events", () => {
    expect(applyTurnState({
      authority: "turn_coordinator",
      phase: "model_generating",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 10,
    })).toBe(true);
    expect(applyTurnState({
      authority: "turn_coordinator",
      phase: "queued",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 9,
    })).toBe(false);
    expect(turnStateStore.phase).toBe("model_generating");
  });

  it("hydrates retry progress and keeps terminal error details after a late finish event", () => {
    applyTurnState({
      phase: "model_generating",
      turn_id: "turn-retry",
      generation: 12,
      lifecycle_seq: 40,
      model_attempt: { status: "retrying", attempt: 1, max_attempts: 2, retry_after_ms: 500 },
    }, { source: "hydrate" });
    expect(turnStateStore.modelAttempt).toEqual({ status: "retrying", attempt: 1, maxAttempts: 2, retryAfterMs: 500 });

    expect(applyTurnFinishedEvent({
      turn_complete: true,
      finish_reason: "error",
      turn_id: "turn-retry",
      turn_generation: 12,
    })).toBe(true);
    expect(isTurnTerminal()).toBe(true);
    expect(isTurnProcessing()).toBe(false);

    applyTurnState({
      phase: "failed",
      terminal: true,
      turn_id: "turn-retry",
      generation: 12,
      lifecycle_seq: 42,
      end_reason: "error",
      model_attempt: { status: "failed", attempt: 2, max_attempts: 2 },
      error: { code: "provider_unavailable", message: "模型服务暂时不可用", retryable: true },
    });
    expect(applyTurnFinishedEvent({
      turn_complete: true,
      finish_reason: "stop",
      turn_id: "turn-retry",
      turn_generation: 12,
    })).toBe(true);
    expect(turnStateStore.phase).toBe("failed");
    expect(turnStateStore.error).toEqual({ code: "provider_unavailable", message: "模型服务暂时不可用", retryable: true, incomplete: false });

    applyTurnState({ phase: "model_generating", turn_id: "turn-next", generation: 13, lifecycle_seq: 43 });
    expect(applyTurnState({ phase: "failed", terminal: true, turn_id: "turn-retry", generation: 12, lifecycle_seq: 42 })).toBe(false);
    expect(turnStateStore.phase).toBe("model_generating");
  });

  it("keeps Desktop state in sync across SSE, stale hydrate, and the terminal projection", () => {
    const retryState = {
      authority: "turn_coordinator",
      phase: "model_generating",
      turn_id: "turn-ui-1",
      generation: 21,
      lifecycle_seq: 80,
      model_attempt: { status: "retrying", attempt: 1, max_attempts: 3 },
    };
    expect(applyDesktopTurnEvent({ agent_id: "agent-a", event_type: "turn_state", data: retryState }, "agent-b")).toBe(false);
    expect(applyDesktopTurnEvent({ agent_id: "agent-a", event_type: "turn_state", data: retryState }, "agent-a")).toBe(true);

    expect(applyHydrateTurnState({ turn_state: { ...retryState, phase: "queued", lifecycle_seq: 79 } })).toBe(false);
    expect(turnStateStore.phase).toBe("model_generating");
    expect(turnStateStore.modelAttempt.status).toBe("retrying");

    expect(applyDesktopTurnEvent({
      agent_id: "agent-a",
      event_type: "turn_finished",
      data: { turn_complete: true, finish_reason: "error", turn_id: "turn-ui-1", turn_generation: 21 },
    }, "agent-a")).toBe(true);
    expect(isTurnProcessing()).toBe(false);

    expect(applyHydrateTurnState({ turn_state: {
      ...retryState,
      phase: "failed",
      terminal: true,
      lifecycle_seq: 82,
      end_reason: "模型服务暂时不可用",
      error: { code: "provider_unavailable", message: "模型服务暂时不可用", retryable: false, incomplete: true },
    } })).toBe(true);
    expect(turnStateStore.error.incomplete).toBe(true);
    expect(applyDesktopTurnEvent({
      agent_id: "agent-a",
      event_type: "turn_finished",
      data: { turn_complete: true, finish_reason: "stop", turn_id: "turn-ui-old", turn_generation: 20 },
    }, "agent-a")).toBe(false);
    expect(turnStateStore.phase).toBe("failed");
  });

  it("clears one Agent's error while hydrating the newly selected Agent", () => {
    applyTurnState({
      phase: "failed",
      terminal: true,
      turn_id: "agent-a-turn",
      generation: 1,
      lifecycle_seq: 9,
      error: { code: "invalid_api_key", message: "bad key" },
    });
    resetTurnState();
    expect(turnStateStore.error).toBeNull();
    applyHydrateTurnState({ turn_state: {
      phase: "completed",
      terminal: true,
      turn_id: "agent-b-turn",
      generation: 4,
      lifecycle_seq: 20,
    } });
    expect(turnStateStore.turnId).toBe("agent-b-turn");
    expect(turnStateStore.error).toBeNull();
  });

  it("accepts an idle post-clear snapshot while fencing late events from the cleared turn", () => {
    applyTurnState({ phase: "failed", terminal: true, turn_id: "cleared-turn", generation: 5, lifecycle_seq: 90 });
    resetTurnState({ preserveLifecycle: true });
    expect(applyHydrateTurnState({ turn_state: { phase: "idle", terminal: false } })).toBe(true);
    expect(turnStateStore.phase).toBe("idle");
    expect(applyDesktopTurnEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "model_generating", turn_id: "cleared-turn", generation: 5, lifecycle_seq: 99 },
    }, "agent-a")).toBe(false);
    expect(applyDesktopTurnEvent({
      agent_id: "agent-a",
      event_type: "turn_state",
      data: { phase: "queued", turn_id: "new-turn", generation: 6, lifecycle_seq: 91 },
    }, "agent-a")).toBe(true);
    expect(turnStateStore.turnId).toBe("new-turn");
  });

  it("shows thinking when reasoning output arrives without using it as turn authority", () => {
    applyTurnState({
      authority: "turn_coordinator",
      phase: "model_generating",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 10,
    });
    setOutputChannel("reasoning");
    syncTurnStatus(turnStateStore);
    expect(hasStatus("thinking")).toBe(true);
    expect(isTurnProcessing()).toBe(true);
  });

  it("distinguishes assistant output from reasoning output", () => {
    applyTurnState({ phase: "model_generating", turn_id: "turn-1", generation: 1 });
    setOutputChannel("assistant");
    syncTurnStatus(turnStateStore);
    expect(hasStatus("assistant_generating")).toBe(true);
    expect(hasStatus("thinking")).toBe(false);
  });

  it("keeps tool execution and interaction states explicit", () => {
    applyTurnState({
      phase: "tool_executing",
      tool_executions: [{ tool_call_id: "call-1", status: "running" }],
    });
    expect(isTurnProcessing()).toBe(true);
    expect(toolExecutionForCall("call-1")?.status).toBe("running");

    applyTurnState({
      phase: "tool_waiting",
      interaction_kind: "approval",
      tool_executions: [{ tool_call_id: "call-1", status: "pending" }],
    });
    expect(isTurnProcessing()).toBe(true);
    expect(isTurnInteractionWaiting()).toBe(true);
  });

  it("does not treat idle as a terminal completion", () => {
    applyTurnState({ phase: "idle", terminal: false });
    expect(isTurnTerminal()).toBe(false);

    applyTurnState({ phase: "cancelled", terminal: true, end_reason: "cancelled_by_user" });
    expect(isTurnTerminal()).toBe(true);
    expect(isTurnProcessing()).toBe(false);
  });

  it("shows a generic actionable message for legacy terminal errors without error details", () => {
    applyTurnState({ phase: "failed", terminal: true, turn_id: "legacy-turn", generation: 2, end_reason: "{\"secret\":\"raw\"}" });
    expect(turnStateStore.error).toEqual({
      code: "legacy_failure",
      message: "本轮未能完成，请稍后重试或切换模型。",
      retryable: false,
      incomplete: false,
    });
  });

  it("keeps an accepted submission busy until the authoritative turn starts", () => {
    turnStateStore.submitState = "accepted";
    applyTurnState({ phase: "idle", terminal: false }, { source: "hydrate" });
    expect(isTurnProcessing()).toBe(true);

    applyTurnState({ phase: "queued", turn_id: "turn-2", generation: 2 });
    expect(isTurnProcessing()).toBe(true);
    expect(turnStateStore.submitState).toBe("idle");
  });

  it("preserves cancellation confirmation when the terminal event arrives", () => {
    beginTurnCancellation();
    applyTurnState({ phase: "cancelled", terminal: true, turn_id: "turn-3" });
    expect(turnStateStore.cancelState).toBe("confirmed");
  });

  it("releases a matching turn from its correlated terminal event", () => {
    applyTurnState({
      phase: "tool_executing",
      turn_id: "turn-current",
      generation: 4,
      lifecycle_seq: 20,
      tool_executions: [{ tool_call_id: "call-last", status: "running" }],
    });

    expect(applyTurnFinishedEvent({
      turn_complete: true,
      finish_reason: "budget_exhausted",
      budget_reason: "max_steps",
      turn_id: "turn-current",
      turn_generation: 4,
    })).toBe(true);
    expect(isTurnTerminal()).toBe(true);
    expect(isTurnProcessing()).toBe(false);
    expect(turnStateStore.phase).toBe("budget_exhausted");
    expect(turnStateStore.endReason).toBe("max_steps");
  });

  it("does not let incomplete or stale turn_finished events end the active turn", () => {
    applyTurnState({ phase: "tool_executing", turn_id: "turn-new", generation: 5, lifecycle_seq: 30 });
    const stale = {
      turn_complete: true,
      finish_reason: "error",
      turn_id: "turn-old",
      turn_generation: 4,
    };
    expect(applyTurnFinishedEvent(stale)).toBe(false);
    expect(applyTurnFinishedEvent({ ...stale, turn_id: "turn-new", turn_generation: 5, turn_complete: false })).toBe(false);
    expect(isTurnProcessing()).toBe(true);
    expect(turnStateStore.turnId).toBe("turn-new");
  });

  it("shows the concrete budget limit when the backend provides one", () => {
    expect(budgetExhaustionMessage("max_steps")).toContain("模型步骤数上限");
    expect(budgetExhaustionMessage("max_tool_rounds")).toContain("工具轮次上限");
    expect(budgetExhaustionMessage("unexpected_reason")).toContain("运行预算上限");
  });

  it("preserves a richer terminal projection and does not infer success for an unknown reason", () => {
    applyTurnState({
      phase: "interrupted",
      terminal: true,
      recovery_required: true,
      end_reason: "worker_lost",
      turn_id: "turn-9",
      generation: 9,
      lifecycle_seq: 90,
      tool_executions: [{ tool_call_id: "call-x", status: "failed" }],
    });
    expect(applyTurnFinishedEvent({
      turn_complete: true,
      finish_reason: "stop",
      turn_id: "turn-9",
      turn_generation: 9,
    })).toBe(true);
    expect(turnStateStore.phase).toBe("interrupted");
    expect(turnStateStore.endReason).toBe("worker_lost");
    expect(turnStateStore.recoveryRequired).toBe(true);
    expect(toolExecutionForCall("call-x")?.status).toBe("failed");

    applyTurnState({ phase: "tool_executing", turn_id: "turn-10", generation: 10, lifecycle_seq: 100 });
    expect(applyTurnFinishedEvent({
      turn_complete: true,
      finish_reason: "new_terminal_reason",
      turn_id: "turn-10",
      turn_generation: 10,
    })).toBe(true);
    expect(turnStateStore.phase).toBe("finished");
    expect(isTurnTerminal()).toBe(true);
    expect(turnStateStore.phase).not.toBe("completed");
  });
});
