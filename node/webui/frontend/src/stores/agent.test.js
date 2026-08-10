import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.stubGlobal("localStorage", {
  getItem: () => "",
  setItem: () => {},
  removeItem: () => {},
});

vi.mock("../api/node.js", () => ({
  postAgentAck: vi.fn(() => Promise.resolve({ ack_seq: 1 })),
  ensureAgentRuntime: vi.fn(() => Promise.resolve({ ok: true })),
}));

let agentStore;
let markEventApplied;
let resetEventTracking;
let ackAgentAfterHydrate;
let shouldAckSSEEvent;
let transcriptStore;
let api;

beforeAll(async () => {
  api = await import("../api/node.js");
  const mod = await import("./agent.js");
  agentStore = mod.agentStore;
  markEventApplied = mod.markEventApplied;
  resetEventTracking = mod.resetEventTracking;
  ackAgentAfterHydrate = mod.ackAgentAfterHydrate;
  shouldAckSSEEvent = mod.shouldAckSSEEvent;
  transcriptStore = (await import("./transcript.js")).transcriptStore;
});

describe("shouldAckSSEEvent", () => {
  it("acks HITL events", () => {
    expect(shouldAckSSEEvent("hitl_required", {})).toBe(true);
    expect(shouldAckSSEEvent("approval_required", {})).toBe(false);
  });

  it("acks done when turn completes", () => {
    expect(shouldAckSSEEvent("done", { finish_reason: "stop", turn_complete: true })).toBe(true);
  });

  it("skips streaming chunks and incomplete done", () => {
    expect(shouldAckSSEEvent("assistant", {})).toBe(false);
    expect(shouldAckSSEEvent("reasoning", {})).toBe(false);
    expect(shouldAckSSEEvent("tool_call", {})).toBe(false);
    expect(shouldAckSSEEvent("done", { finish_reason: "awaiting_hitl", awaiting: "hitl" })).toBe(false);
  });
});

describe("agent ack", () => {
  beforeEach(() => {
    vi.mocked(api.postAgentAck).mockClear();
    agentStore.agentId = "agt-test";
    transcriptStore.lastSeq = 0;
    resetEventTracking();
  });

  afterEach(async () => {
    await Promise.resolve();
  });

  it("updates lastSeq without POST for streaming chunks", async () => {
    markEventApplied(1);
    markEventApplied(2);
    markEventApplied(3);
    await Promise.resolve();
    expect(transcriptStore.lastSeq).toBe(3);
    expect(api.postAgentAck).not.toHaveBeenCalled();
  });

  it("POSTs ack only when ack flag set", async () => {
    markEventApplied(5, { ack: true });
    await Promise.resolve();
    expect(api.postAgentAck).toHaveBeenCalledTimes(1);
    expect(api.postAgentAck).toHaveBeenCalledWith("agt-test", 5);
  });

  it("ackAgentAfterHydrate flushes immediately", async () => {
    transcriptStore.lastSeq = 12;
    ackAgentAfterHydrate();
    await Promise.resolve();
    expect(api.postAgentAck).toHaveBeenCalledWith("agt-test", 12);
  });

  it("ackAgentAfterHydrate prefers notify_seq over lastSeq", async () => {
    transcriptStore.lastSeq = 99;
    ackAgentAfterHydrate(42);
    await Promise.resolve();
    expect(api.postAgentAck).toHaveBeenCalledWith("agt-test", 42);
  });
});
