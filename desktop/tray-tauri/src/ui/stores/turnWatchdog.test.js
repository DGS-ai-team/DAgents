import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createTurnWatchdog } from "./turnWatchdog.js";

describe("createTurnWatchdog", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("fires onStuck when awaiting + stuck status and idle past threshold", async () => {
    const onStuck = vi.fn(() => Promise.resolve());
    const wd = createTurnWatchdog({
      isAwaiting: () => true,
      hasStuckStatus: () => true,
      onStuck,
      stuckMs: 30_000,
      intervalMs: 5_000,
    });
    wd.start();
    wd.noteActivity();
    await vi.advanceTimersByTimeAsync(29_000);
    expect(onStuck).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(6_000);
    expect(onStuck).toHaveBeenCalledTimes(1);
    wd.stop();
  });

  it("does not fire when status phases already cleared", async () => {
    const onStuck = vi.fn();
    const wd = createTurnWatchdog({
      isAwaiting: () => true,
      hasStuckStatus: () => false,
      onStuck,
      stuckMs: 1_000,
      intervalMs: 500,
    });
    wd.start();
    await vi.advanceTimersByTimeAsync(5_000);
    expect(onStuck).not.toHaveBeenCalled();
    wd.stop();
  });

  it("noteActivity resets the idle clock", async () => {
    const onStuck = vi.fn();
    const wd = createTurnWatchdog({
      isAwaiting: () => true,
      hasStuckStatus: () => true,
      onStuck,
      stuckMs: 10_000,
      intervalMs: 2_000,
    });
    wd.start();
    await vi.advanceTimersByTimeAsync(9_000);
    wd.noteActivity();
    await vi.advanceTimersByTimeAsync(9_000);
    expect(onStuck).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(3_000);
    expect(onStuck).toHaveBeenCalledTimes(1);
    wd.stop();
  });
});
