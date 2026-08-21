import { afterEach, describe, expect, it } from "vitest";
import { buildStreamURL, connectStream, shouldIgnoreSSEForAgent } from "./stream.js";
import { AGENT_STREAM_EVENT_TYPES } from "./agentEvents.js";

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

describe("agent stream event registration", () => {
  afterEach(() => {
    delete globalThis.EventSource;
  });

  it("registers every event from the shared registry on EventSource", () => {
    const instances = [];
    class FakeEventSource {
      constructor(url) {
        this.url = url;
        this.listeners = new Map();
        instances.push(this);
      }

      addEventListener(type, handler) {
        this.listeners.set(type, handler);
      }

      close() {}
    }
    globalThis.EventSource = FakeEventSource;

    const handle = connectStream({ getAgentId: () => "agt-1" });
    expect([...instances[0].listeners.keys()]).toEqual(AGENT_STREAM_EVENT_TYPES);
    handle.close();
  });

  it("includes runtime and notice events that are published by Node", () => {
    expect(AGENT_STREAM_EVENT_TYPES).toEqual(
      expect.arrayContaining([
        "system_notice",
        "runtime/config-changed",
        "memory/changed",
        "skills/changed",
        "mcp/catalog-changed",
      ]),
    );
  });

  it("does not contain duplicate named event registrations", () => {
    expect(new Set(AGENT_STREAM_EVENT_TYPES).size).toBe(AGENT_STREAM_EVENT_TYPES.length);
  });
});
