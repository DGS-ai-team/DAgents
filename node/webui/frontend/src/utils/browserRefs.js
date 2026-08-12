/**
 * 从 transcript 中的 browser_* tool_result 提取可折叠引用。
 */

const BROWSER_CITE_TOOLS = new Set([
  "browser_run_task",
  "browser_task_status",
]);

export function parseBrowserToolContent(raw) {
  if (raw == null) return null;
  let obj = raw;
  if (typeof raw === "string") {
    const s = raw.trim();
    if (!s.startsWith("{")) return null;
    try {
      obj = JSON.parse(s);
    } catch {
      return null;
    }
  }
  if (!obj || typeof obj !== "object") return null;
  const detail = obj.detail && typeof obj.detail === "object" ? obj.detail : {};
  return { ...obj, detail };
}

/**
 * 收集「当前助手回复之前、上一轮用户之后」的浏览器任务引用。
 * @param {Array} entries transcript entries
 * @param {number} beforeIndex 助手消息将插入/所在下标（不含该下标之后）
 */
export function collectBrowserRefsFromEntries(entries, beforeIndex = entries?.length ?? 0) {
  const list = Array.isArray(entries) ? entries : [];
  const end = Math.min(Math.max(0, beforeIndex), list.length);
  let start = 0;
  for (let i = end - 1; i >= 0; i--) {
    if (list[i]?.kind === "user") {
      start = i + 1;
      break;
    }
  }
  const refs = [];
  const seen = new Set();
  for (let i = start; i < end; i++) {
    const e = list[i];
    if (e?.kind !== "tool_result") continue;
    const name = String(e.data?.tool_name || "").trim();
    if (!BROWSER_CITE_TOOLS.has(name)) continue;
    const parsed = parseBrowserToolContent(e.data?.content);
    if (!parsed) continue;
    const d = parsed.detail || {};
    const status = String(d.status || "").toLowerCase();
    // 仅终态或带摘要的结果作为引用
    if (name === "browser_run_task" && status && !["completed", "failed", "cancelled"].includes(status)) {
      // wait=false 仅 queued：跳过，等 status 工具
      if (!d.summary && !parsed.extracted_content) continue;
    }
    const taskId = String(d.task_id || "").trim();
    const key = taskId || `${name}:${e.id}`;
    if (seen.has(key)) {
      // 同 task 以较新（靠后）覆盖
      const idx = refs.findIndex((r) => r.key === key);
      if (idx >= 0) refs.splice(idx, 1);
    }
    seen.add(key);
    const summary =
      String(d.summary || parsed.extracted_content || d.cite_label || "").trim() ||
      String(d.task || "").trim() ||
      "浏览器任务";
    const media = Array.isArray(e.data?.media)
      ? e.data.media
          .filter((m) => m && (m.url || m.id))
          .map((m) => ({
            id: m.id || null,
            url: String(m.url || "").trim(),
            label: m.label || null,
            caption: m.caption || null,
          }))
          .filter((m) => m.url)
      : [];
    refs.push({
      key,
      task_id: taskId || null,
      tool_name: name,
      tool_call_id: e.data?.tool_call_id || null,
      status: status || null,
      success: d.success,
      summary,
      task: d.task || null,
      steps: d.steps ?? null,
      urls: Array.isArray(d.urls) ? d.urls : [],
      action_names: Array.isArray(d.action_names) ? d.action_names : [],
      step_trace: Array.isArray(d.step_trace) ? d.step_trace : [],
      errors: Array.isArray(d.errors) ? d.errors : [],
      error: d.error || parsed.error || null,
      screenshot_paths: Array.isArray(d.screenshot_paths) ? d.screenshot_paths : [],
      screenshots: media,
      detail_md: d.detail_md || null,
      detail_json: d.detail_json || null,
      duration_seconds: d.duration_seconds ?? null,
    });
  }
  return refs;
}

/** hydrate 后：为每条 assistant 补 browser_refs（若缺失）。 */
export function attachBrowserRefsToAssistants(entries) {
  const list = Array.isArray(entries) ? entries : [];
  for (let i = 0; i < list.length; i++) {
    const e = list[i];
    if (e?.kind !== "assistant") continue;
    if (Array.isArray(e.browser_refs) && e.browser_refs.length) continue;
    const refs = collectBrowserRefsFromEntries(list, i);
    if (refs.length) e.browser_refs = refs;
  }
  return list;
}
