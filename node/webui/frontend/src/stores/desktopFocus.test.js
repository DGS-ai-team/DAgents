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
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(3);
  });

  it("debounces rapid duplicate reports within 1s", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledTimes(1);
  });

  it("pulse forces report on agent switch", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    vi.mocked(reportDesktopUIFocus).mockClear();

    startDesktopFocusHeartbeat(() => "agt-b");
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledWith(
      "agt-b",
      expect.objectContaining({ ttlSeconds: 90, sourceId: expect.any(String) }),
    );
  });

  it("clears focus on stop", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    vi.mocked(reportDesktopUIFocus).mockClear();

    stopDesktopFocusHeartbeat();
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledWith(
      "",
      expect.objectContaining({ ttlSeconds: 90, sourceId: expect.any(String) }),
    );
  });

  it("pauses heartbeat and clears focus when document hidden", async () => {
    startDesktopFocusHeartbeat(() => "agt-a");
    await Promise.resolve();
    vi.mocked(reportDesktopUIFocus).mockClear();

    document.visibilityState = "hidden";
    document.addEventListener.mock.calls
      .find(([event]) => event === "visibilitychange")?.[1]();
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledWith(
      "",
      expect.objectContaining({ ttlSeconds: 90, sourceId: expect.any(String) }),
    );

    vi.mocked(reportDesktopUIFocus).mockClear();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(reportDesktopUIFocus).not.toHaveBeenCalled();

    document.visibilityState = "visible";
    document.addEventListener.mock.calls
      .find(([event]) => event === "visibilitychange")?.[1]();
    await Promise.resolve();
    expect(reportDesktopUIFocus).toHaveBeenCalledWith(
      "agt-a",
      expect.objectContaining({ ttlSeconds: 90, sourceId: expect.any(String) }),
    );
    document.visibilityState = "visible";
  });
});
