const PREFIX = "dagents.node-preference";

export function nodePreferenceKey(nodeId, name) {
  const id = String(nodeId || "").trim();
  return id ? `${PREFIX}.${id}.${name}` : "";
}

export function readNodePreference(nodeId, name, fallback) {
  const key = nodePreferenceKey(nodeId, name);
  if (!key) return fallback;
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : fallback;
  } catch { return fallback; }
}

export function writeNodePreference(nodeId, name, value) {
  const key = nodePreferenceKey(nodeId, name);
  if (!key) return false;
  try { localStorage.setItem(key, JSON.stringify(value)); return true; } catch { return false; }
}
