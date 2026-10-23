/**
 * 从 transcript 中的同步 browser_call/browser_evaluate 结果提取可折叠引用。
 * 这只是展示层聚合，不参与浏览器执行或状态管理。
 */
const BROWSER_CITE_TOOLS = new Set(["browser_call", "browser_evaluate"]);

export function parseBrowserToolContent(raw) {
  if (raw == null) return null;
  let obj = raw;
  if (typeof raw === "string") {
    const text = raw.trim();
    if (!text.startsWith("{")) return null;
    try {
      obj = JSON.parse(text);
    } catch {
      return null;
    }
  }
  if (!obj || typeof obj !== "object") return null;
  const detail = obj.detail && typeof obj.detail === "object" ? obj.detail : {};
  return { ...obj, detail };
}

export function collectBrowserRefsFromEntries(entries, beforeIndex = entries?.length ?? 0) {
  const list = Array.isArray(entries) ? entries : [];
  const end = Math.min(Math.max(0, beforeIndex), list.length);
  let start = 0;
  for (let i = end - 1; i >= 0; i -= 1) {
    if (list[i]?.kind === "user") {
      start = i + 1;
      break;
    }
  }
  const refs = [];
  for (let i = start; i < end; i += 1) {
    const entry = list[i];
    if (entry?.kind !== "tool_result") continue;
    const name = String(entry.data?.tool_name || "").trim();
    if (!BROWSER_CITE_TOOLS.has(name)) continue;
    const parsed = parseBrowserToolContent(entry.data?.content);
    if (!parsed) continue;
    const detail = parsed.detail || {};
    const observation = detail.observation && typeof detail.observation === "object" ? detail.observation : {};
    const actionResults = Array.isArray(detail.action_results) ? detail.action_results : [];
    const key = String(entry.data?.tool_call_id || entry.id || `${name}:${i}`);
    const media = Array.isArray(entry.data?.media)
      ? entry.data.media
          .filter((item) => item && (item.url || item.id))
          .map((item) => ({
            id: item.id || null,
            url: String(item.url || "").trim(),
            label: item.label || null,
            caption: item.caption || null,
          }))
          .filter((item) => item.url)
      : [];
    refs.push({
      key,
      tool_name: name,
      tool_call_id: entry.data?.tool_call_id || null,
      status: detail.status || (parsed.ok === false ? "failed" : "succeeded"),
      summary: observation.content || detail.value || parsed.error || "浏览器操作结果",
      url: parsed.url || null,
      title: parsed.title || null,
      action_results: actionResults,
      observation,
      error: parsed.error || null,
      screenshots: media,
    });
  }
  return refs;
}

export function attachBrowserRefsToAssistants(entries) {
  const list = Array.isArray(entries) ? entries : [];
  for (let i = 0; i < list.length; i += 1) {
    const entry = list[i];
    if (entry?.kind !== "assistant") continue;
    if (Array.isArray(entry.browser_refs) && entry.browser_refs.length) continue;
    const refs = collectBrowserRefsFromEntries(list, i);
    if (refs.length) entry.browser_refs = refs;
  }
  return list;
}
