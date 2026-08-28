import { describe, expect, it } from "vitest";
import { classifyCancelOutcome, isHydrateIdle } from "./cancelState.js";

describe("cancelState", () => {
  it("requires the API acknowledgement before reporting a cancellation", () => {
    expect(classifyCancelOutcome({ cancelled: false }, {
      turn_state: { phase: "model_generating", terminal: false },
    })).toBe("not_cancelled");
  });

  it("keeps the cancellation pending until hydrate reports a terminal turn", () => {
    expect(classifyCancelOutcome({ cancelled: true }, {
      turn_state: { phase: "tool_executing", terminal: false },
    })).toBe("cancel_requested");
  });

  it("distinguishes an already idle turn from a successful cancellation", () => {
    const idle = { turn_state: { phase: "idle", terminal: false } };
    expect(isHydrateIdle(idle)).toBe(true);
    expect(classifyCancelOutcome({ cancelled: false }, idle)).toBe("already_idle");
  });

  it("does not treat pending HITL as idle", () => {
    expect(isHydrateIdle({
      turn_state: { phase: "tool_waiting", terminal: false },
    })).toBe(false);
  });
});
