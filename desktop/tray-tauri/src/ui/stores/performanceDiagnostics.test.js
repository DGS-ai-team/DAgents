import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  clearPerformanceDiagnostics,
  disablePerformanceDiagnostics,
  enablePerformanceDiagnostics,
  getPerformanceDiagnosticsSnapshot,
  measureSync,
  measureElementLayout,
  recordSSEEvent,
  startPerformanceSpan,
  updateRuntimeMetrics,
} from "./performanceDiagnostics.js";

describe("performance diagnostics", () => {
  beforeEach(() => {
    disablePerformanceDiagnostics();
    clearPerformanceDiagnostics();
    enablePerformanceDiagnostics();
  });

  it("records SSE counts, event types, and sequence gaps without payloads", () => {
    recordSSEEvent("assistant", 10);
    recordSSEEvent("tool_result", 12);

    const snapshot = getPerformanceDiagnosticsSnapshot();
    expect(snapshot.eventCount).toBe(2);
    expect(snapshot.eventTypes).toEqual({ assistant: 1, tool_result: 1 });
    expect(snapshot.lastSeq).toBe(12);
    expect(snapshot.seqGaps).toBe(1);
    expect(snapshot.recentEvents[0]).toMatchObject({ type: "assistant", seq: 10 });
    expect(snapshot.recentEvents[0]).not.toHaveProperty("content");
  });

  it("keeps independent sequence cursors when switching between Agents", () => {
    recordSSEEvent("assistant", 18, "agent-a");
    recordSSEEvent("assistant", 2, "agent-b");
    recordSSEEvent("tool_result", 4, "agent-b");
    recordSSEEvent("assistant", 21, "agent-a");

    const snapshot = getPerformanceDiagnosticsSnapshot();
    expect(snapshot.seqGaps).toBe(3);
    expect(snapshot.lastSeq).toBe(21);
    expect(snapshot.recentEvents.at(-1)).toMatchObject({ streamKey: "agent-a", seq: 21, gap: 2 });
  });

  it("records synchronous spans and runtime scale metrics", () => {
    const value = measureSync("stream.build", () => 42, { entries: 240 });
    const span = startPerformanceSpan("scroll.update");
    span.end({ visibleItems: 180 });
    updateRuntimeMetrics({ entries: 240, streamItems: 200, visibleItems: 180, unrevealedChars: 32 });

    const snapshot = getPerformanceDiagnosticsSnapshot();
    expect(value).toBe(42);
    expect(snapshot.durations["stream.build"].count).toBe(1);
    expect(snapshot.durations["stream.build"].avgMs).toBeGreaterThanOrEqual(0);
    expect(snapshot.durations["scroll.update"].count).toBe(1);
    expect(snapshot.runtime).toMatchObject({
      entries: 240,
      streamItems: 200,
      visibleItems: 180,
      unrevealedChars: 32,
    });
  });

  it("measures an actual DOM layout read and records its node count", () => {
    const element = {
      getBoundingClientRect: vi.fn(() => ({ width: 320, height: 80 })),
      querySelectorAll: vi.fn(() => [{}, {}]),
    };
    const result = measureElementLayout("stream.layout", element, { visibleItems: 1 });

    expect(element.getBoundingClientRect).toHaveBeenCalledTimes(1);
    expect(result).toEqual({ width: 320, height: 80 });
    expect(getPerformanceDiagnosticsSnapshot().durations["stream.layout"]).toMatchObject({ count: 1 });
    expect(getPerformanceDiagnosticsSnapshot().recentSpans.at(-1)).toMatchObject({
      name: "stream.layout",
      domNodes: 2,
      visibleItems: 1,
    });
  });

  it("does not collect metrics while disabled", () => {
    disablePerformanceDiagnostics();
    recordSSEEvent("assistant", 1);
    measureSync("sse.handle", () => "ok");
    updateRuntimeMetrics({ entries: 99 });

    const snapshot = getPerformanceDiagnosticsSnapshot();
    expect(snapshot.enabled).toBe(false);
    expect(snapshot.eventCount).toBe(0);
    expect(snapshot.durations).toEqual({});
    expect(snapshot.runtime.entries).toBe(0);
  });
});
