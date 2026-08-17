import { toolDisplayName, approvalItemDisplayName, approvalItemHint, approvalItemHintVisible, parseToolArguments } from "./toolCalls.js";
import { truncateGraphemes } from "./textTruncate.js";
import { parseTemporaryAgentToolResult } from "./temporaryAgentResults.js";
import { isTerminalResultTool, normalizeTerminalResultContent } from "./terminalOutput.js";

export { toolDisplayName, approvalItemDisplayName, approvalItemHint, approvalItemHintVisible };

export function formatToolCallLine(entry) {
  const data = entry?.data || entry || {};
  const name = entry?.summary || data.summary || toolDisplayName(data.tool_name || data.name, data.arguments || {});
  const partial = entry?.partial || data.partial ? " …" : "";
  return `▶ ${name}${partial}`;
}

/** 对齐 Python format_tool_result / parse_temporary_agent_tool_result。 */
export function formatToolResultDisplay(entry, { verbose = false } = {}) {
  const data = entry?.data || entry || {};
  const name = String(data.tool_name || data.name || "tool").trim();
  const content = String(data.content || data.output || "").trim();
  const terminalResult = isTerminalResultTool(name);
  const displayContent = terminalResult ? normalizeTerminalResultContent(content) : content;
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
  const detail = !rejected && displayContent ? displayContent : "";
  return { headline, detail, raw: verbose ? displayContent : detail };
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

/** 侧栏紧凑相对时间：1m / 9h / 2d */
export function formatCompactRelativeTime(iso) {
  if (!iso) return "";
  const ts = Date.parse(iso);
  if (!Number.isFinite(ts)) return "";
  const diff = Math.max(0, Date.now() - ts);
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return "now";
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 48) return `${hr}h`;
  const day = Math.floor(hr / 24);
  if (day < 14) return `${day}d`;
  return new Date(ts).toLocaleDateString("zh-CN", { month: "numeric", day: "numeric" });
}

export function agentRecordId(agent) {
  return String(agent?.agent_id || agent?.AgentID || "").trim();
}

export function agentDisplayTitle(agent) {
  const name = String(agent?.display_name || agent?.DisplayName || "").trim();
  if (name) return name.length > 48 ? truncateGraphemes(name, 48) : name;
  return "未命名 Agent";
}
