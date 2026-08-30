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
let applyHydrateSeqHint;
let ackAgentAfterHydrate;
let shouldAckSSEEvent;
let observeEventContinuity;
let transcriptStore;
let api;

beforeAll(async () => {
  api = await import("../api/node.js");
  const mod = await import("./agent.js");
  agentStore = mod.agentStore;
  markEventApplied = mod.markEventApplied;
  resetEventTracking = mod.resetEventTracking;
  applyHydrateSeqHint = mod.applyHydrateSeqHint;
  ackAgentAfterHydrate = mod.ackAgentAfterHydrate;
  shouldAckSSEEvent = mod.shouldAckSSEEvent;
  observeEventContinuity = mod.observeEventContinuity;
  transcriptStore = (await import("./transcript.js")).transcriptStore;
});

describe("shouldAckSSEEvent", () => {
  it("acks HITL events", () => {
    expect(shouldAckSSEEvent("hitl_required", {})).toBe(true);
    expect(shouldAckSSEEvent("approval_required", {})).toBe(false);
  });

  it("acks the unambiguous terminal event", () => {
    expect(shouldAckSSEEvent("turn_finished", { finish_reason: "stop" })).toBe(true);
    expect(shouldAckSSEEvent("turn_finished", { finish_reason: "error" })).toBe(false);
    expect(shouldAckSSEEvent("turn_finished", { finish_reason: "cancelled" })).toBe(false);
  });

  it("skips streaming chunks and non-terminal events", () => {
    expect(shouldAckSSEEvent("assistant", {})).toBe(false);
    expect(shouldAckSSEEvent("reasoning", {})).toBe(false);
    expect(shouldAckSSEEvent("tool_call", {})).toBe(false);
  });
});

describe("agent ack", () => {
  beforeEach(() => {
    vi.mocked(api.postAgentAck).mockClear();
    agentStore.agentId = "agt-test";
    transcriptStore.lastSeq = 0;
    transcriptStore.lastAgentSeq = 0;
    transcriptStore.streamEpoch = "";
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

  it("resets the SSE sequence watermark after a Node restart", () => {
    transcriptStore.lastSeq = 45;
    transcriptStore.lastAgentSeq = 45;
    applyHydrateSeqHint({ stream_epoch: "new", stream_seq_hint: 0, agent_seq_hint: 0 });
    expect(transcriptStore.lastSeq).toBe(0);
    expect(transcriptStore.lastAgentSeq).toBe(0);
    expect(agentStore.seqFence).toBe(0);
  });

  it("switches to a lower sequence watermark when the stream epoch changes", () => {
    transcriptStore.lastSeq = 45;
    transcriptStore.streamEpoch = "old";
    applyHydrateSeqHint({ stream_epoch: "new", stream_seq_hint: 12, agent_seq_hint: 4 });
    expect(transcriptStore.lastSeq).toBe(12);
    expect(transcriptStore.lastAgentSeq).toBe(4);
    expect(agentStore.seqFence).toBe(12);
  });

  it("detects a gap only in replayable Agent events", () => {
    transcriptStore.streamEpoch = "epoch-1";
    transcriptStore.lastAgentSeq = 4;
    expect(observeEventContinuity(5, "epoch-1")).toEqual({ epochChanged: false, gap: false });
    expect(observeEventContinuity(7, "epoch-1")).toEqual({ epochChanged: false, gap: true });
    expect(observeEventContinuity(0, "epoch-1")).toEqual({ epochChanged: false, gap: false });
  });

  it("forces reconciliation when the Node stream epoch changes", () => {
    transcriptStore.streamEpoch = "epoch-old";
    transcriptStore.lastAgentSeq = 99;
    expect(observeEventContinuity(1, "epoch-new")).toEqual({ epochChanged: true, gap: false });
  });
});
