import { beforeEach, describe, expect, it } from "vitest";
import {
  applyTurnState,
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
    expect(isTurnProcessing()).toBe(false);
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
});
