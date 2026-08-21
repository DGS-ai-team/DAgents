import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  finishStatus,
  hasStatus,
  resetStatusLines,
  startStatus,
  statusStore,
  syncTurnStatus,
} from "./statusLines.js";

describe("statusLines compression watchdog", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStatusLines();
  });

  afterEach(() => {
    resetStatusLines();
    vi.useRealTimers();
  });

  it("clears compression if end never arrives", () => {
    startStatus("compression");
    expect(hasStatus("compression")).toBe(true);
    vi.advanceTimersByTime(119_000);
    expect(hasStatus("compression")).toBe(true);
    vi.advanceTimersByTime(2_000);
    expect(hasStatus("compression")).toBe(false);
  });

  it("cancels watchdog when compression ends normally", () => {
    startStatus("compression");
    finishStatus("compression");
    vi.advanceTimersByTime(130_000);
    expect(hasStatus("compression")).toBe(false);
  });

  it("keeps the same visible phase stable across streaming deltas", () => {
    syncTurnStatus({ phase: "model_generating", outputChannel: "reasoning" });
    const startedAt = statusStore.phases.thinking.startedAt;

    vi.advanceTimersByTime(1_000);
    syncTurnStatus({ phase: "model_generating", outputChannel: "reasoning" });

    expect(statusStore.phases.thinking.startedAt).toBe(startedAt);
    expect(Object.keys(statusStore.phases)).toEqual(["thinking"]);
  });

  it("replaces the status only when the authoritative output phase changes", () => {
    syncTurnStatus({ phase: "model_generating", outputChannel: "reasoning" });
    syncTurnStatus({ phase: "model_generating", outputChannel: "assistant" });

    expect(hasStatus("thinking")).toBe(false);
    expect(hasStatus("assistant_generating")).toBe(true);
  });
});
