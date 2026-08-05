import { reportDesktopUIFocus } from "../api/desktop.js";

/** Shell focus TTL 90s；心跳 30s 续期（F-X5 / F-E9）。 */
export const DESKTOP_FOCUS_HEARTBEAT_MS = 30_000;
const FOCUS_TTL_SECONDS = 90;

let heartbeatId = null;
let getAgentIdFn = () => "";
let lastSentAgentId = null;
let lastSentAt = 0;
let paused = false;
let visibilityListener = null;

/** 合并 mount/hydrate/switch 的瞬时重复上报；心跳 30s 间隔不受影响。 */
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
  await reportDesktopUIFocus(id, { ttlSeconds: FOCUS_TTL_SECONDS });
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

/** 立即上报当前 Agent（切换时调用）。 */
export function pulseDesktopFocus() {
  if (paused) return;
  void sendFocus(getAgentIdFn(), { force: true });
}

/** 聊天页可见时启动 30s 心跳；首次立即上报。 */
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

/** 离开聊天页时停止心跳并清除 Shell focus。 */
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
