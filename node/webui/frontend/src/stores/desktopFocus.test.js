import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.stubGlobal("document", {
  visibilityState: "visible",
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
});

vi.stubGlobal("window", {
  setInterval: (...args) => setInterval(...args),
  clearInterval: (...args) => clearInterval(...args),
});

vi.mock("../api/platform.js", () => ({
  reportPlatformUIFocus: vi.fn(() => Promise.resolve()),
}));

let reportPlatformUIFocus;
let startDesktopFocusHeartbeat;
let stopDesktopFocusHeartbeat;

beforeEach(async () => {
  vi.useFakeTimers();
  reportPlatformUIFocus = (await import("../api/platform.js")).reportPlatformUIFocus;
  vi.mocked(reportPlatformUIFocus).mockClear();
  const mod = await import("./desktopFocus.js");
  startDesktopFocusHeartbeat = mod.startDesktopFocusHeartbeat;
  stopDesktopFocusHeartbeat = mod.stopDesktopFocusHeartbeat;
});

afterEach(() => {
  stopDesktopFocusHeartbeat();
  vi.useRealTimers();
});

describe("desktopFocus", () => {
  it("reports immediately on start and renews on heartbeat", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    expect(reportPlatformUIFocus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportPlatformUIFocus).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportPlatformUIFocus).toHaveBeenCalledTimes(3);
  });

  it("debounces rapid duplicate reports within 1s", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    expect(reportPlatformUIFocus).toHaveBeenCalledTimes(1);
  });

  it("pulse forces report on agent switch", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    vi.mocked(reportPlatformUIFocus).mockClear();

    startDesktopFocusHeartbeat(() => "agt-b");
    await Promise.resolve();
    expect(reportPlatformUIFocus).toHaveBeenCalledWith(
      expect.objectContaining({ ttl_seconds: 90, source_id: expect.any(String), agent_id: "agt-b" }),
    );
  });

  it("clears focus on stop", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    vi.mocked(reportPlatformUIFocus).mockClear();

    stopDesktopFocusHeartbeat();
    await Promise.resolve();
    expect(reportPlatformUIFocus).toHaveBeenCalledWith(
      expect.objectContaining({ ttl_seconds: 90, source_id: expect.any(String), agent_id: "" }),
    );
  });

  it("ignores the expected browser-only Shell capability error", async () => {
    vi.mocked(reportPlatformUIFocus).mockRejectedValueOnce(new Error("desktop Shell is unavailable"));

    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();

    expect(reportPlatformUIFocus).toHaveBeenCalledTimes(1);
  });

  it("pauses heartbeat and clears focus when document hidden", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    vi.mocked(reportPlatformUIFocus).mockClear();

    document.visibilityState = "hidden";
    document.addEventListener.mock.calls
      .find(([event]) => event === "visibilitychange")?.[1]();
    await Promise.resolve();
    expect(reportPlatformUIFocus).toHaveBeenCalledWith(
      expect.objectContaining({ ttl_seconds: 90, source_id: expect.any(String), agent_id: "" }),
    );

    vi.mocked(reportPlatformUIFocus).mockClear();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportPlatformUIFocus).not.toHaveBeenCalled();

    document.visibilityState = "visible";
    document.addEventListener.mock.calls
      .find(([event]) => event === "visibilitychange")?.[1]();
    await Promise.resolve();
    expect(reportPlatformUIFocus).toHaveBeenCalledWith(
      expect.objectContaining({ ttl_seconds: 90, source_id: expect.any(String), agent_id: "agt-a" }),
    );
    document.visibilityState = "visible";
  });
});
