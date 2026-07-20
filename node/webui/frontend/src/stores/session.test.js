import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.stubGlobal("localStorage", {
  getItem: () => "",
  setItem: () => {},
  removeItem: () => {},
});

vi.mock("../api/node.js", () => ({
  postSessionAck: vi.fn(() => Promise.resolve({ ack_seq: 1 })),
  createSession: vi.fn(),
  ensureAgentRuntime: vi.fn(() => Promise.resolve({ ok: true })),
}));

let sessionStore;
let markEventApplied;
let resetEventTracking;
let ackSessionAfterHydrate;
let shouldAckSSEEvent;
let transcriptStore;
let api;

beforeAll(async () => {
  api = await import("../api/node.js");
  const mod = await import("./session.js");
  sessionStore = mod.sessionStore;
  markEventApplied = mod.markEventApplied;
  resetEventTracking = mod.resetEventTracking;
  ackSessionAfterHydrate = mod.ackSessionAfterHydrate;
  shouldAckSSEEvent = mod.shouldAckSSEEvent;
  transcriptStore = (await import("./transcript.js")).transcriptStore;
});

describe("shouldAckSSEEvent", () => {
  it("acks HITL events", () => {
    expect(shouldAckSSEEvent("hitl_required", {})).toBe(true);
    expect(shouldAckSSEEvent("approval_required", {})).toBe(true);
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

describe("session ack", () => {
  beforeEach(() => {
    vi.mocked(api.postSessionAck).mockClear();
    sessionStore.sessionId = "sess-test";
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
    expect(api.postSessionAck).not.toHaveBeenCalled();
  });

  it("POSTs ack only when ack flag set", async () => {
    markEventApplied(5, { ack: true });
    await Promise.resolve();
    expect(api.postSessionAck).toHaveBeenCalledTimes(1);
    expect(api.postSessionAck).toHaveBeenCalledWith("sess-test", 5);
  });

  it("ackSessionAfterHydrate flushes immediately", async () => {
    transcriptStore.lastSeq = 12;
    ackSessionAfterHydrate();
    await Promise.resolve();
    expect(api.postSessionAck).toHaveBeenCalledWith("sess-test", 12);
  });
});
