import { reportDesktopUIFocus } from "../api/desktop.js";

/** Shell focus TTL 90s；心跳 30s 续期（F-X5 / F-E9）。 */
export const DESKTOP_FOCUS_HEARTBEAT_MS = 30_000;
const FOCUS_TTL_SECONDS = 90;

let heartbeatId = null;
let getSessionIdFn = () => "";
let lastSentSessionId = null;
let lastSentAt = 0;

/** 合并 mount/hydrate/switch 的瞬时重复上报；心跳 30s 间隔不受影响。 */
const MIN_REPORT_INTERVAL_MS = 1_000;

async function sendFocus(sessionId, { force = false } = {}) {
  const sid = String(sessionId || "").trim();
  const now = Date.now();
  if (!force && sid === lastSentSessionId && now - lastSentAt < MIN_REPORT_INTERVAL_MS) {
    return;
  }
  lastSentSessionId = sid;
  lastSentAt = now;
  await reportDesktopUIFocus(sid, { ttlSeconds: FOCUS_TTL_SECONDS });
}

/** 立即上报当前 session（切换 session 时调用）。 */
export function pulseDesktopFocus() {
  void sendFocus(getSessionIdFn(), { force: true });
}

/** 聊天页可见时启动 30s 心跳；首次立即上报。 */
export function startDesktopFocusHeartbeat(getSessionId) {
  stopDesktopFocusHeartbeat({ clearRemote: false });
  getSessionIdFn = getSessionId ?? (() => "");
  const tick = () => {
    void sendFocus(getSessionIdFn());
  };
  tick();
  heartbeatId = window.setInterval(tick, DESKTOP_FOCUS_HEARTBEAT_MS);
}

/** 离开聊天页时停止心跳并清除 Shell focus。 */
export function stopDesktopFocusHeartbeat({ clearRemote = true } = {}) {
  if (heartbeatId != null) {
    clearInterval(heartbeatId);
    heartbeatId = null;
  }
  getSessionIdFn = () => "";
  if (clearRemote) {
    lastSentSessionId = null;
    lastSentAt = 0;
    void sendFocus("", { force: true });
  }
}
