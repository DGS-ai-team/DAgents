import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { finishStatus, hasStatus, resetStatusLines, startStatus } from "./statusLines.js";

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
});
