import { beforeEach, describe, expect, it } from "vitest";
import {
  applyTurnState,
  applyTurnFinishedEvent,
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
