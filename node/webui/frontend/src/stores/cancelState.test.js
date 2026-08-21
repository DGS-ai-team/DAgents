import { describe, expect, it } from "vitest";
import { classifyCancelOutcome, isHydrateIdle } from "./cancelState.js";

describe("cancelState", () => {
  it("requires the API acknowledgement before reporting a cancellation", () => {
    expect(classifyCancelOutcome({ cancelled: false }, {
      has_active_turn: true,
      run_turn_phase: "model_streaming",
    })).toBe("not_cancelled");
  });

  it("keeps the cancellation pending until hydrate reports a terminal turn", () => {
    expect(classifyCancelOutcome({ cancelled: true }, {
      has_active_turn: true,
      run_turn_phase: "awaiting_tool_execution",
    })).toBe("cancel_requested");
  });

  it("distinguishes an already idle turn from a successful cancellation", () => {
    const idle = { has_active_turn: false, run_turn_phase: "complete", pending_hitl: null };
    expect(isHydrateIdle(idle)).toBe(true);
    expect(classifyCancelOutcome({ cancelled: false }, idle)).toBe("already_idle");
  });

  it("does not treat pending HITL as idle", () => {
    expect(isHydrateIdle({
      has_active_turn: false,
      run_turn_phase: "awaiting_hitl",
      pending_hitl: { items: [{ id: "call-1" }] },
    })).toBe(false);
  });
});
