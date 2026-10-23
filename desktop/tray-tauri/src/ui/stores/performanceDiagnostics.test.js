import { beforeEach, describe, expect, it } from "vitest";
import {
  clearPerformanceDiagnostics,
  disablePerformanceDiagnostics,
  enablePerformanceDiagnostics,
  getPerformanceDiagnosticsSnapshot,
  measureSync,
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
