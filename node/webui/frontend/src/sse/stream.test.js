import { describe, expect, it } from "vitest";
import { buildStreamURL, shouldIgnoreSSEForAgent } from "./stream.js";

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

describe("buildStreamURL", () => {
  it("uses live=1 for first connect", () => {
    expect(buildStreamURL({ agentId: "agt-1", live: true })).toBe(
      "/v1/streams?agent_id=agt-1&live=1",
    );
  });

  it("uses after_seq on reconnect resume", () => {
    expect(buildStreamURL({ agentId: "agt-1", live: false, afterSeq: 42 })).toBe(
      "/v1/streams?agent_id=agt-1&after_seq=42",
    );
  });

  it("falls back to live=1 when resume has no seq", () => {
    expect(buildStreamURL({ agentId: "agt-1", live: false, afterSeq: 0 })).toBe(
      "/v1/streams?agent_id=agt-1&live=1",
    );
  });
});
