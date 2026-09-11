import { afterEach, describe, expect, it } from "vitest";
import { buildStreamURL, connectStream, shouldIgnoreSSEForAgent } from "./stream.js";
import { AGENT_STREAM_EVENT_TYPES } from "./agentEvents.js";
import { vi } from "vitest";

describe("shouldIgnoreSSEForAgent", () => {
  it("ignores events from a different agent after switch", () => {
    expect(shouldIgnoreSSEForAgent("agent-a", "agent-b")).toBe(true);
  });

  it("keeps events for the current agent", () => {
    expect(shouldIgnoreSSEForAgent("agent-a", "agent-a")).toBe(false);
  });

  it("rejects an event without an agent id on an Agent stream", () => {
    expect(shouldIgnoreSSEForAgent("", "agent-b")).toBe(true);
    expect(shouldIgnoreSSEForAgent("agent-a", "")).toBe(false);
  });
});

describe("buildStreamURL", () => {
  it("uses live=1 for first connect", () => {
    expect(buildStreamURL({ agentId: "agt-1", live: true })).toBe(
      "/v1/streams?agent_id=agt-1&live=1",
    );
  });

  it("uses the Agent cursor on filtered reconnect resume", () => {
    expect(buildStreamURL({ agentId: "agt-1", live: false, afterAgentSeq: 42 })).toBe(
      "/v1/streams?agent_id=agt-1&after_agent_seq=42",
    );
  });

  it("uses the global cursor on unfiltered reconnect resume", () => {
    expect(buildStreamURL({ live: false, afterSeq: 42 })).toBe(
      "/v1/streams?after_seq=42",
    );
  });

  it("keeps an explicit zero Agent cursor for the initial replay", () => {
    expect(buildStreamURL({ agentId: "agt-1", live: false, afterAgentSeq: 0 })).toBe(
      "/v1/streams?agent_id=agt-1&after_agent_seq=0",
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
    expect(instances[0].url).toBe("/v1/streams?agent_id=agt-1&live=1");
    expect([...instances[0].listeners.keys()]).toEqual(["stream_heartbeat", ...AGENT_STREAM_EVENT_TYPES]);
    handle.close();
  });

  it("reconnects once when the stream stops receiving heartbeats", () => {
    vi.useFakeTimers();
    try {
      const instances = [];
      class FakeEventSource {
        constructor(url) { this.url = url; this.listeners = new Map(); instances.push(this); }
        addEventListener(type, handler) { this.listeners.set(type, handler); }
        close() { this.closed = true; }
      }
      globalThis.EventSource = FakeEventSource;
      const statuses = [];
      const handle = connectStream({ getAgentId: () => "agt-1", onStatus: (status) => statuses.push(status) });
      instances[0].onopen();
      vi.advanceTimersByTime(45000);
      expect(instances).toHaveLength(1);
      expect(statuses).toContain("disconnected");
      vi.advanceTimersByTime(5000);
      expect(instances).toHaveLength(2);
      expect(instances[0].closed).toBe(true);
      vi.advanceTimersByTime(4000);
      expect(instances).toHaveLength(2);
      handle.close();
      vi.advanceTimersByTime(10000);
      expect(instances).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("ignores callbacks from an obsolete EventSource after reconnect", () => {
    vi.useFakeTimers();
    try {
      const instances = [];
      class FakeEventSource {
        constructor(url) { this.url = url; this.listeners = new Map(); instances.push(this); }
        addEventListener(type, handler) { this.listeners.set(type, handler); }
        close() { this.closed = true; }
      }
      globalThis.EventSource = FakeEventSource;
      const handle = connectStream({ getAgentId: () => "agt-1" });
      instances[0].onerror();
      vi.advanceTimersByTime(5000);
      expect(instances).toHaveLength(2);
      instances[0].onerror();
      expect(instances[1].closed).toBeFalsy();
      vi.advanceTimersByTime(5000);
      expect(instances).toHaveLength(2);
      handle.close();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps a healthy stream alive on named heartbeats and clears timers on close", () => {
    vi.useFakeTimers();
    try {
      const instances = [];
      class FakeEventSource {
        constructor(url) { this.url = url; this.listeners = new Map(); instances.push(this); }
        addEventListener(type, handler) { this.listeners.set(type, handler); }
        close() { this.closed = true; }
      }
      globalThis.EventSource = FakeEventSource;
      const handle = connectStream({ getAgentId: () => "agt-1" });
      instances[0].onopen();
      vi.advanceTimersByTime(30000);
      instances[0].listeners.get("stream_heartbeat")({ data: "{}" });
      vi.advanceTimersByTime(30000);
      expect(instances).toHaveLength(1);
      handle.close();
      vi.advanceTimersByTime(60000);
      expect(instances).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("starts a filtered stream from the hydrate cursor, including zero", () => {
    const instances = [];
    let reconnects = 0;
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

    const handle = connectStream({
      getAgentId: () => "agt-1",
      getAfterAgentSeq: () => 0,
      onReconnect: () => {
        reconnects += 1;
      },
    });
    expect(instances[0].url).toBe("/v1/streams?agent_id=agt-1&after_agent_seq=0");
    instances[0].onopen();
    expect(reconnects).toBe(0);
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
        "notification_changed",
      ]),
    );
  });

  it("does not contain duplicate named event registrations", () => {
    expect(new Set(AGENT_STREAM_EVENT_TYPES).size).toBe(AGENT_STREAM_EVENT_TYPES.length);
  });

  it("forwards the protocol cursors and stream epoch to the reducer", () => {
    let received;
    const instances = [];
    class FakeEventSource {
      constructor() {
        this.listeners = new Map();
        instances.push(this);
      }

      addEventListener(type, handler) {
        this.listeners.set(type, handler);
      }

      close() {}
    }
    globalThis.EventSource = FakeEventSource;

    const handle = connectStream({ onEvent: (event) => (received = event) });
    instances[0].listeners.get("assistant")({
      lastEventId: "99",
      data: JSON.stringify({
        seq: 99,
        agent_seq: 7,
        agent_id: "agt-1",
        stream_epoch: "epoch-1",
        event_version: 1,
        delivery: "replayable",
        data: { content: "hello" },
      }),
    });
    expect(received).toMatchObject({
      type: "assistant",
      seq: 99,
      agentSeq: 7,
      agentId: "agt-1",
      epoch: "epoch-1",
      delivery: "replayable",
      eventVersion: 1,
      data: { content: "hello" },
    });
    handle.close();
  });

  it("does not treat diagnostic cursors in a resync payload as consumed", () => {
    let received;
    const instances = [];
    class FakeEventSource {
      constructor() {
        this.listeners = new Map();
        instances.push(this);
      }

      addEventListener(type, handler) {
        this.listeners.set(type, handler);
      }

      close() {}
    }
    globalThis.EventSource = FakeEventSource;

    const handle = connectStream({ onEvent: (event) => (received = event) });
    instances[0].listeners.get("resync_required")({
      lastEventId: "77",
      data: JSON.stringify({
        agent_id: "agt-1",
        type: "resync_required",
        seq: 0,
        stream_epoch: "epoch-1",
        event_version: 1,
        delivery: "replayable",
        data: {
          seq: 77,
          agent_seq: 19,
          stream_epoch: "epoch-1",
          requires_hydrate: true,
        },
      }),
    });
    expect(received).toMatchObject({
      type: "resync_required",
      seq: 0,
      agentSeq: 0,
      epoch: "epoch-1",
      data: { requires_hydrate: true, agent_seq: 19 },
    });
    handle.close();
  });
});
