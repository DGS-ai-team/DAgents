import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.stubGlobal("window", {
  setInterval: (...args) => setInterval(...args),
  clearInterval: (...args) => clearInterval(...args),
});

vi.mock("../api/desktop.js", () => ({
  reportDesktopUIFocus: vi.fn(() => Promise.resolve()),
}));

let reportDesktopUIFocus;
let startDesktopFocusHeartbeat;
let stopDesktopFocusHeartbeat;
let pulseDesktopFocus;

beforeEach(async () => {
  vi.useFakeTimers();
  reportDesktopUIFocus = (await import("../api/desktop.js")).reportDesktopUIFocus;
  vi.mocked(reportDesktopUIFocus).mockClear();
  const mod = await import("./desktopFocus.js");
  startDesktopFocusHeartbeat = mod.startDesktopFocusHeartbeat;
  stopDesktopFocusHeartbeat = mod.stopDesktopFocusHeartbeat;
  pulseDesktopFocus = mod.pulseDesktopFocus;
});

afterEach(() => {
  stopDesktopFocusHeartbeat();
  vi.useRealTimers();
});

describe("desktopFocus", () => {
  it("reports immediately on start and renews on heartbeat", async () => {
    startDesktopFocusHeartbeat(() => "sess-a");
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(3);
  });

  it("debounces rapid duplicate reports within 1s", async () => {
    startDesktopFocusHeartbeat(() => "sess-a");
    await Promise.resolve();
    startDesktopFocusHeartbeat(() => "sess-a");
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(1);
  });

  it("pulse forces report on session switch", async () => {
    startDesktopFocusHeartbeat(() => "sess-a");
    await Promise.resolve();
    vi.mocked(reportDesktopUIFocus).mockClear();

    startDesktopFocusHeartbeat(() => "sess-b");
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledWith("sess-b", { ttlSeconds: 90 });
  });

  it("clears focus on stop", async () => {
    startDesktopFocusHeartbeat(() => "sess-a");
    await Promise.resolve();
    vi.mocked(reportDesktopUIFocus).mockClear();

    stopDesktopFocusHeartbeat();
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledWith("", { ttlSeconds: 90 });
  });
});
