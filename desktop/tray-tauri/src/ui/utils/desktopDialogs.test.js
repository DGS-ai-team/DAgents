// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { confirmDesktopAction, showDesktopMessage } from "./desktopDialogs.js";

const originalBridge = globalThis.__DAGENTS_NATIVE_DIALOGS__;

afterEach(() => {
  if (originalBridge === undefined) delete globalThis.__DAGENTS_NATIVE_DIALOGS__;
  else globalThis.__DAGENTS_NATIVE_DIALOGS__ = originalBridge;
  vi.restoreAllMocks();
});

describe("Desktop native dialogs", () => {
  it("delegates confirmations to the native dialog bridge", async () => {
    const confirm = vi.fn().mockResolvedValue(true);
    globalThis.__DAGENTS_NATIVE_DIALOGS__ = { confirm };

    await expect(confirmDesktopAction("删除工作组？")).resolves.toBe(true);
    expect(confirm).toHaveBeenCalledWith("删除工作组？", {});
  });

  it("prevents a second confirmation from opening while a native prompt is active", async () => {
    let resolveConfirm;
    const confirm = vi.fn(() => new Promise((resolve) => { resolveConfirm = resolve; }));
    globalThis.__DAGENTS_NATIVE_DIALOGS__ = { confirm };

    const first = confirmDesktopAction("删除工作组？");
    await Promise.resolve();
    await expect(confirmDesktopAction("删除工作组？")).resolves.toBe(false);
    expect(confirm).toHaveBeenCalledTimes(1);
    resolveConfirm(true);
    await expect(first).resolves.toBe(true);
  });

  it("uses the browser fallback outside the Desktop shell", async () => {
    delete globalThis.__DAGENTS_NATIVE_DIALOGS__;
    vi.spyOn(window, "confirm").mockReturnValue(false);

    await expect(confirmDesktopAction("继续？")).resolves.toBe(false);
    expect(window.confirm).toHaveBeenCalledWith("继续？");
  });

  it("shows status messages through the native dialog bridge", async () => {
    const message = vi.fn().mockResolvedValue("Ok");
    globalThis.__DAGENTS_NATIVE_DIALOGS__ = { message };

    await showDesktopMessage("更新已完成");
    expect(message).toHaveBeenCalledWith("更新已完成", {});
  });
});
