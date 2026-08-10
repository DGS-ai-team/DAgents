import { describe, expect, it } from "vitest";
import { shouldIgnoreSSEForAgent } from "./stream.js";

describe("shouldIgnoreSSEForAgent", () => {
  it("ignores events from a different agent after switch", () => {
    expect(shouldIgnoreSSEForAgent("agent-a", "agent-b")).toBe(true);
  });

  it("keeps events for the current agent", () => {
    expect(shouldIgnoreSSEForAgent("agent-a", "agent-a")).toBe(false);
  });

  it("keeps events when agent id missing (compat)", () => {
    expect(shouldIgnoreSSEForAgent("", "agent-b")).toBe(false);
    expect(shouldIgnoreSSEForAgent("agent-a", "")).toBe(false);
  });
});
