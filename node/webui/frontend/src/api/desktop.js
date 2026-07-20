/** Shell localhost desktop API（F-X8）；不可达时回退 Node。 */
import { getAgentUpdate } from "./node.js";

export const DESKTOP_API_BASE = "http://127.0.0.1:18767";

async function desktopFetch(path, { method = "GET", body } = {}) {
  const headers = { Accept: "application/json" };
  const init = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const resp = await fetch(`${DESKTOP_API_BASE}${path}`, init);
  let data = null;
  try {
    data = await resp.json();
  } catch {
    data = null;
  }
  if (!resp.ok) {
    const msg = data?.message || data?.error?.message || `HTTP ${resp.status}`;
    throw new Error(msg);
  }
  return data;
}

/** @returns {{ source: 'shell'|'node', data: object }} */
export async function getUpdateStatus() {
  try {
    const data = await desktopFetch("/v1/desktop/update");
    return { source: "shell", data };
  } catch {
    const data = await getAgentUpdate();
    return { source: "node", data };
  }
}

export async function applyDesktopUpdate({ force = false } = {}) {
  return desktopFetch("/v1/desktop/update/apply", {
    method: "POST",
    body: { force },
  });
}

/** @returns {Promise<{ paths: string[] }>} Shell CF_HDROP 路径（F-P2）；不可达时抛错。 */
export async function getDesktopClipboardFiles() {
  return desktopFetch("/v1/desktop/clipboard/files");
}

/** F-X5 / F-E9：上报 Web UI 当前聚焦 Agent；空串清除。Shell 线协议仍用 session_id 字段。 */
export async function reportDesktopUIFocus(agentId, { ttlSeconds = 90 } = {}) {
  try {
    await desktopFetch("/v1/desktop/ui/focus", {
      method: "POST",
      body: { session_id: agentId || "", ttl_seconds: ttlSeconds },
    });
  } catch {
    /* Shell unavailable */
  }
}
