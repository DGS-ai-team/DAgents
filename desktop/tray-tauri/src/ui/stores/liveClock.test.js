import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { acquireLiveClock, liveClock } from "./liveClock.js";

describe("liveClock", () => {
  let releases;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000);
    releases = [];
  });

  afterEach(() => {
    for (const release of releases) release();
    vi.useRealTimers();
  });

  it("shares one timer and stops it after the last elapsed label is released", () => {
    const releaseFirst = acquireLiveClock();
    releases.push(releaseFirst);
    expect(vi.getTimerCount()).toBe(1);

    vi.advanceTimersByTime(500);
    expect(liveClock.tick).toBe(1_500);

    const releaseSecond = acquireLiveClock();
    releases.push(releaseSecond);
    expect(vi.getTimerCount()).toBe(1);

    releaseFirst();
    vi.advanceTimersByTime(500);
    expect(liveClock.tick).toBe(2_000);
    expect(vi.getTimerCount()).toBe(1);

    releaseSecond();
    expect(vi.getTimerCount()).toBe(0);
    vi.advanceTimersByTime(1_000);
    expect(liveClock.tick).toBe(2_000);
  });

  it("makes release idempotent", () => {
    const release = acquireLiveClock();
    releases.push(release);
    release();
    release();
    expect(vi.getTimerCount()).toBe(0);
  });
});
