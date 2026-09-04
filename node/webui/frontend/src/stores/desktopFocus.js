import { reportPlatformUIFocus } from "../api/platform.js";

/** Shell focus TTL 90s; heartbeat 30s. */
export const DESKTOP_FOCUS_HEARTBEAT_MS = 30_000;
const FOCUS_TTL_SECONDS = 90;
const FOCUS_SOURCE_STORAGE_KEY = "dagents_desktop_focus_source";

function createFocusSourceId() {
  try {
    const existing = window.sessionStorage?.getItem(FOCUS_SOURCE_STORAGE_KEY);
    if (existing) return existing;
    const generated =
      globalThis.crypto?.randomUUID?.() ||
      `webui-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    window.sessionStorage?.setItem(FOCUS_SOURCE_STORAGE_KEY, generated);
    return generated;
  } catch {
    return `webui-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  }
}

// Each tab owns an independent claim, so a hidden tab cannot clear a visible tab.
const FOCUS_SOURCE_ID = createFocusSourceId();

let heartbeatId = null;
let getAgentIdFn = () => "";
let lastSentAgentId = null;
let lastSentAt = 0;
let paused = false;
let visibilityListener = null;

/** Merge mount/hydrate/switch reports while keeping the heartbeat interval. */
const MIN_REPORT_INTERVAL_MS = 1_000;

function isDocumentVisible() {
  return typeof document === "undefined" || document.visibilityState === "visible";
}

async function sendFocus(agentId, { force = false } = {}) {
  const id = String(agentId || "").trim();
  const now = Date.now();
  if (!force && id === lastSentAgentId && now - lastSentAt < MIN_REPORT_INTERVAL_MS) {
    return;
  }
  lastSentAgentId = id;
  lastSentAt = now;
  try {
    await reportPlatformUIFocus({
      agent_id: id,
      ttl_seconds: FOCUS_TTL_SECONDS,
      source_id: FOCUS_SOURCE_ID,
    });
  } catch (error) {
    // The same Web UI also runs in a browser-only Node process. In that
    // mode the desktop Shell endpoint intentionally returns this capability
    // error; it must not become an unhandled rejection in the page.
    const message = String(error?.message || error || "");
    if (message.includes("desktop Shell is unavailable")) return;
    throw error;
  }
}

function onVisibilityChange() {
  if (isDocumentVisible()) {
    paused = false;
    void sendFocus(getAgentIdFn(), { force: true });
  } else {
    paused = true;
    void sendFocus("", { force: true });
  }
}

/** Immediately report the current Agent, for route switches. */
export function pulseDesktopFocus() {
  if (paused) return;
  void sendFocus(getAgentIdFn(), { force: true });
}

/** Start the heartbeat while a chat page is visible. */
export function startDesktopFocusHeartbeat(getAgentId) {
  stopDesktopFocusHeartbeat({ clearRemote: false });
  getAgentIdFn = getAgentId ?? (() => "");
  paused = !isDocumentVisible();
  const tick = () => {
    if (paused) return;
    void sendFocus(getAgentIdFn());
  };
  if (!paused) {
    tick();
  } else {
    void sendFocus("", { force: true });
  }
  heartbeatId = window.setInterval(tick, DESKTOP_FOCUS_HEARTBEAT_MS);
  if (!visibilityListener) {
    visibilityListener = onVisibilityChange;
    document.addEventListener("visibilitychange", visibilityListener);
  }
}

/** Stop the heartbeat and clear only this tab's Shell focus claim. */
export function stopDesktopFocusHeartbeat({ clearRemote = true } = {}) {
  if (heartbeatId != null) {
    clearInterval(heartbeatId);
    heartbeatId = null;
  }
  if (visibilityListener) {
    document.removeEventListener("visibilitychange", visibilityListener);
    visibilityListener = null;
  }
  paused = false;
  getAgentIdFn = () => "";
  if (clearRemote) {
    lastSentAgentId = null;
    lastSentAt = 0;
    void sendFocus("", { force: true });
  }
}
