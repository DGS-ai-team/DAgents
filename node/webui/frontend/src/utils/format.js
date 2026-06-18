import { toolDisplayName, approvalItemDisplayName, parseToolArguments } from "./toolCalls.js";
import { parseTemporaryAgentToolResult } from "./temporaryAgentResults.js";

export { toolDisplayName, approvalItemDisplayName };

export function formatToolCallLine(entry, { a2a = false, peerSuffix = "" } = {}) {
  const data = entry?.data || entry || {};
  const name = entry?.summary || data.summary || toolDisplayName(data.tool_name || data.name, data.arguments || {});
  const partial = entry?.partial || data.partial ? " …" : "";
  const relay = a2a ? `${peerSuffix} · 待审批` : "";
  return `▶ ${name}${relay || partial}`;
}

export function formatToolResultSummary(entry) {
  return formatToolResultDisplay(entry).headline;
}

/** 对齐 Python format_tool_result / parse_temporary_agent_tool_result。 */
export function formatToolResultDisplay(entry, { verbose = false } = {}) {
  const data = entry?.data || entry || {};
  const name = String(data.tool_name || data.name || "tool").trim();
  const content = String(data.content || data.output || "").trim();
  const rejected = !!data.rejected;
  const elapsed = formatToolElapsed(data.duration_seconds);
  const args = parseToolArguments(data.arguments ?? data.raw_arguments);

  const parsed = parseTemporaryAgentToolResult(name, content);
  if (parsed) {
    let headline = parsed.summary;
    if (rejected) headline = `[已拒绝] ${headline}`;
    else if (elapsed) headline += elapsed;
    return {
      headline,
      detail: parsed.detail || "",
      raw: verbose ? content : "",
    };
  }

  const displayName = entry?.summary || data.summary || toolDisplayName(name, args);
  let headline = `${displayName}${rejected ? " · 已拒绝" : " · 已完成"}${elapsed}`;
  const detail = !rejected && content ? content : "";
  return { headline, detail, raw: verbose ? content : detail };
}

export function formatToolElapsed(seconds) {
  if (seconds == null || !Number.isFinite(Number(seconds))) return "";
  const s = Math.max(0, Number(seconds));
  if (s < 1) return ` · ${Math.round(s * 1000)}ms`;
  if (s < 60) return ` · ${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return ` · ${m}m${Math.round(s - m * 60)}s`;
}

export function formatRelativeTime(iso) {
  if (!iso) return "";
  const ts = Date.parse(iso);
  if (!Number.isFinite(ts)) return "";
  const diff = Date.now() - ts;
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return "刚刚";
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} 分钟前`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr} 小时前`;
  const day = Math.floor(hr / 24);
  if (day < 7) return `${day} 天前`;
  return new Date(ts).toLocaleDateString("zh-CN", { month: "short", day: "numeric" });
}

export function sessionDisplayTitle(session) {
  const first = String(session?.first_user_message || session?.FirstUserMessage || "").trim();
  if (first) return first.length > 48 ? `${first.slice(0, 48)}…` : first;
  const id = String(session?.session_id || "").trim();
  if (id) return `会话 ${id.slice(0, 8)}`;
  return "新对话";
}
