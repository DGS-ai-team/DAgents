import { truncateGraphemes } from "./textTruncate.js";
import { parseTemporaryAgentToolResult } from "./temporaryAgentResults.js";

function tryFormatJsonToolResult(content) {
  const text = String(content || "").trim();
  if (!text.startsWith("{")) return null;
  let payload;
  try {
    payload = JSON.parse(text);
  } catch {
    return null;
  }
  if (!payload || typeof payload !== "object") return null;

  if (payload.kind === "result" && payload.child_session_id) {
    return parseTemporaryAgentToolResult("create_temporary_agent", text);
  }
  if (Array.isArray(payload.results)) {
    return parseTemporaryAgentToolResult("wait_temporary_agents", text);
  }
  if (payload.child_session_id && payload.purpose) {
    return parseTemporaryAgentToolResult("create_temporary_agent", text);
  }
  if (payload.child_session_id && payload.status === "cancelled") {
    return parseTemporaryAgentToolResult("cancel_temporary_agent", text);
  }
  const summary = String(payload.summary || "").trim();
  if (summary) return { summary, detail: "" };
  return null;
}

/** 解析并格式化 context 消息正文（不截断）。 */
export function buildContextMessageText(content) {
  const raw = String(content ?? "");
  const formatted = tryFormatJsonToolResult(raw);
  if (formatted) {
    return formatted.detail ? `${formatted.summary}\n${formatted.detail}` : formatted.summary;
  }
  return raw;
}

/** Context 面板单条消息：折叠预览 + 可展开的完整正文。 */
export function buildContextMessageView(content, previewLen = 160) {
  const full = buildContextMessageText(content);
  const preview = truncateGraphemes(full, previewLen);
  return {
    preview,
    full,
    expandable: preview !== full,
  };
}

/** @deprecated 使用 buildContextMessageView；保留供单测与旧调用。 */
export function formatContextMessageContent(content, maxLen = 200) {
  return truncateGraphemes(buildContextMessageText(content), maxLen);
}
