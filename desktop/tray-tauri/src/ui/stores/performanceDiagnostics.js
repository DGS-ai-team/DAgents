const STORAGE_KEY = "dagents_webui_perf_debug";
const MAX_RECENT_EVENTS = 120;
const MAX_RECENT_SPANS = 160;
const LONG_TASK_THRESHOLD_MS = 50;

let enabled = readEnabled();
let observer = null;

function readEnabled() {
  try {
    return globalThis.localStorage?.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

function persistEnabled(value) {
  try {
    if (value) globalThis.localStorage?.setItem(STORAGE_KEY, "1");
    else globalThis.localStorage?.removeItem(STORAGE_KEY);
  } catch {
    /* ignore storage failures */
  }
}

function now() {
  return typeof globalThis.performance?.now === "function"
    ? globalThis.performance.now()
    : Date.now();
}

function emptyStats() {
  return {
    startedAt: Date.now(),
    eventCount: 0,
    eventTypes: {},
    firstEventAt: null,
    lastEventAt: null,
    lastSeq: 0,
    seqGaps: 0,
    durations: {},
    longTasks: { count: 0, totalMs: 0, maxMs: 0 },
    runtime: {
      entries: 0,
      streamItems: 0,
      visibleItems: 0,
      assistantBufferLength: 0,
      unrevealedChars: 0,
    },
    recentEvents: [],
    recentSpans: [],
  };
}

let stats = emptyStats();

function pushBounded(list, value, limit) {
  list.push(value);
  if (list.length > limit) list.splice(0, list.length - limit);
}

function durationMetric(name) {
  if (!stats.durations[name]) {
    stats.durations[name] = { count: 0, totalMs: 0, maxMs: 0, samples: [] };
  }
  return stats.durations[name];
}

function recordDuration(name, durationMs, details = {}) {
  if (!enabled) return;
  const duration = Math.max(0, Number(durationMs) || 0);
  const metric = durationMetric(name);
  metric.count += 1;
  metric.totalMs += duration;
  metric.maxMs = Math.max(metric.maxMs, duration);
  metric.samples.push(duration);
  if (metric.samples.length > MAX_RECENT_SPANS) {
    metric.samples.splice(0, metric.samples.length - MAX_RECENT_SPANS);
  }
  pushBounded(
    stats.recentSpans,
    { at: Date.now(), name, durationMs: round(duration), ...details },
    MAX_RECENT_SPANS,
  );
}

function round(value) {
  return Math.round(Number(value || 0) * 100) / 100;
}

function percentile(samples, fraction) {
  if (!samples.length) return 0;
  const sorted = [...samples].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.floor(sorted.length * fraction));
  return round(sorted[index]);
}

export function isPerformanceDiagnosticsEnabled() {
  return enabled;
}

export function recordSSEEvent(type, seq = 0) {
  if (!enabled) return;
  const eventType = String(type || "unknown");
  const eventSeq = Number(seq) || 0;
  const at = now();
  const previousAt = stats.lastEventAt;
  const previousSeq = stats.lastSeq;
  const gap = eventSeq > 0 && previousSeq > 0 && eventSeq > previousSeq + 1
    ? eventSeq - previousSeq - 1
    : 0;

  stats.eventCount += 1;
  stats.eventTypes[eventType] = (stats.eventTypes[eventType] || 0) + 1;
  stats.firstEventAt ??= at;
  stats.lastEventAt = at;
  if (gap > 0) stats.seqGaps += gap;
  if (eventSeq > stats.lastSeq) stats.lastSeq = eventSeq;

  pushBounded(
    stats.recentEvents,
    {
      at: Date.now(),
      type: eventType,
      seq: eventSeq,
      gap,
      interArrivalMs: previousAt == null ? null : round(at - previousAt),
    },
    MAX_RECENT_EVENTS,
  );
}

export function measureSync(name, fn, details = {}) {
  if (!enabled) return fn();
  const started = now();
  try {
    return fn();
  } finally {
    recordDuration(name, now() - started, details);
  }
}

export function startPerformanceSpan(name, details = {}) {
  if (!enabled) return { end() {} };
  const started = now();
  return {
    end(extra = {}) {
      recordDuration(name, now() - started, { ...details, ...extra });
    },
  };
}

export function updateRuntimeMetrics(next = {}) {
  if (!enabled || !next || typeof next !== "object") return;
  for (const key of Object.keys(stats.runtime)) {
    if (next[key] == null) continue;
    stats.runtime[key] = Math.max(0, Number(next[key]) || 0);
  }
}

function observeLongTasks() {
  if (observer || typeof globalThis.PerformanceObserver !== "function") return;
  try {
    observer = new globalThis.PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        const duration = Math.max(0, Number(entry.duration) || 0);
        stats.longTasks.count += 1;
        stats.longTasks.totalMs += duration;
        stats.longTasks.maxMs = Math.max(stats.longTasks.maxMs, duration);
        pushBounded(
          stats.recentSpans,
          {
            at: Date.now(),
            name: "longtask",
            durationMs: round(duration),
            source: entry.name || "unknown",
          },
          MAX_RECENT_SPANS,
        );
        if (duration >= LONG_TASK_THRESHOLD_MS && globalThis.console?.warn) {
          console.warn("[DAgents perf] long task", {
            durationMs: round(duration),
            entries: stats.runtime.entries,
            streamItems: stats.runtime.streamItems,
            visibleItems: stats.runtime.visibleItems,
          });
        }
      }
    });
    observer.observe({ entryTypes: ["longtask"] });
  } catch {
    observer = null;
  }
}

function disconnectLongTasks() {
  observer?.disconnect?.();
  observer = null;
}

export function enablePerformanceDiagnostics() {
  enabled = true;
  persistEnabled(true);
  observeLongTasks();
  return getPerformanceDiagnosticsSnapshot();
}

export function disablePerformanceDiagnostics() {
  enabled = false;
  persistEnabled(false);
  disconnectLongTasks();
}

export function clearPerformanceDiagnostics() {
  stats = emptyStats();
}

export function getPerformanceDiagnosticsSnapshot() {
  const durations = {};
  for (const [name, metric] of Object.entries(stats.durations)) {
    durations[name] = {
      count: metric.count,
      totalMs: round(metric.totalMs),
      maxMs: round(metric.maxMs),
      avgMs: round(metric.count ? metric.totalMs / metric.count : 0),
      p95Ms: percentile(metric.samples, 0.95),
    };
  }
  return {
    enabled,
    startedAt: stats.startedAt,
    durationMs: Math.max(0, Date.now() - stats.startedAt),
    eventCount: stats.eventCount,
    eventTypes: { ...stats.eventTypes },
    firstEventAt: stats.firstEventAt,
    lastEventAt: stats.lastEventAt,
    lastSeq: stats.lastSeq,
    seqGaps: stats.seqGaps,
    durations,
    longTasks: {
      count: stats.longTasks.count,
      totalMs: round(stats.longTasks.totalMs),
      maxMs: round(stats.longTasks.maxMs),
    },
    runtime: { ...stats.runtime },
    recentEvents: stats.recentEvents.map((event) => ({ ...event })),
    recentSpans: stats.recentSpans.map((span) => ({ ...span })),
  };
}

export function printPerformanceDiagnostics() {
  const snapshot = getPerformanceDiagnosticsSnapshot();
  if (globalThis.console?.groupCollapsed) {
    console.groupCollapsed("[DAgents perf] snapshot");
    console.table({
      enabled: snapshot.enabled,
      eventCount: snapshot.eventCount,
      seqGaps: snapshot.seqGaps,
      longTasks: snapshot.longTasks.count,
      maxLongTaskMs: snapshot.longTasks.maxMs,
      entries: snapshot.runtime.entries,
      streamItems: snapshot.runtime.streamItems,
      visibleItems: snapshot.runtime.visibleItems,
      unrevealedChars: snapshot.runtime.unrevealedChars,
    });
    console.table(snapshot.durations);
    console.log({ recentEvents: snapshot.recentEvents, recentSpans: snapshot.recentSpans });
    console.groupEnd();
  }
  return snapshot;
}

export function installPerformanceDiagnosticsGlobal() {
  if (!globalThis.window) return;
  globalThis.window.dagentsPerf = {
    enable: enablePerformanceDiagnostics,
    disable: disablePerformanceDiagnostics,
    clear: clearPerformanceDiagnostics,
    snapshot: getPerformanceDiagnosticsSnapshot,
    print: printPerformanceDiagnostics,
  };
  if (enabled) observeLongTasks();
}
