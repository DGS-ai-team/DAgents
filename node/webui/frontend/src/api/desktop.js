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

export async function isShellDesktopAvailable() {
  try {
    await desktopFetch("/health");
    return true;
  } catch {
    return false;
  }
}
