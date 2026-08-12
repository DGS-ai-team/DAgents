import { truncateGraphemes } from "./textTruncate.js";

export const USER_INFORMATION_TOOL = "ask_user_information";

const TEMPORARY_AGENT_TOOLS = new Set([
  "create_temporary_agent",
  "wait_temporary_agents",
  "temporary_agent_status",
  "cancel_temporary_agent",
]);

function shortChildId(id) {
  const s = String(id || "").trim();
  if (!s) return "";
  return s.length <= 16 ? s : truncateGraphemes(s, 16);
}

function stringList(value) {
  if (!Array.isArray(value)) return [];
  return value.map((v) => String(v || "").trim()).filter(Boolean);
}

function intVal(value) {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

/** 对齐 Python format_temporary_agent_tool_title / Go FormatTemporaryAgentToolTitle。 */
export function formatTemporaryAgentToolTitle(name, args = {}) {
  const n = String(name || "").trim();
  if (!TEMPORARY_AGENT_TOOLS.has(n)) return null;
  if (n === "create_temporary_agent") {
    const purpose = String(args.purpose || "—").trim() || "—";
    return args.wait ? `创建临时 Agent · ${purpose} (wait)` : `创建临时 Agent · ${purpose}`;
  }
  if (n === "wait_temporary_agents") {
    const ids = stringList(args.child_agent_ids);
    let title = ids.length ? `等待 ${ids.length} 个临时 Agent` : "等待临时 Agent";
    const timeout = intVal(args.timeout_seconds);
    if (timeout > 0) title += ` · ${timeout}s`;
    return title;
  }
  if (n === "temporary_agent_status") {
    const ids = stringList(args.child_agent_ids);
    return ids.length ? `查询 ${ids.length} 个临时 Agent 状态` : "查询临时 Agent 状态";
  }
  if (n === "cancel_temporary_agent") {
    const short = shortChildId(args.child_agent_id);
    return short ? `取消临时 Agent · ${short}` : "取消临时 Agent";
  }
  return `${n}()`;
}

function formatGenericToolTitle(name, args = {}) {
  const keys = Object.keys(args || {})
    .filter((key) => key !== "call_purpose" && key !== "run_in_background")
    .sort();
  if (!keys.length) return `${name}()`;
  const parts = keys.map((key) => `${key}=${formatToolArgValue(args[key])}`);
  return `${name}(${parts.join(", ")})`;
}

function formatToolArgValue(value) {
  if (value == null) return "null";
  if (typeof value === "string") return JSON.stringify(value);
  if (typeof value === "boolean") return value ? "True" : "False";
  if (typeof value === "number") {
    return Number.isInteger(value) ? String(value) : String(value);
  }
  return JSON.stringify(value);
}

export function resolveToolArgumentsFromData(data) {
  if (!data || typeof data !== "object") return {};
  if (data.arguments && typeof data.arguments === "object" && !Array.isArray(data.arguments)) {
    if (Object.keys(data.arguments).length > 0) return data.arguments;
  }
  const fn = data.function;
  if (fn && typeof fn === "object") {
    const fromFn = parseToolArguments(fn.arguments);
    if (Object.keys(fromFn).length) return fromFn;
  }
  return parseToolArguments(data.raw_arguments ?? data.arguments);
}

export function parseToolArguments(raw) {
  if (raw && typeof raw === "object" && !Array.isArray(raw)) return raw;
  if (typeof raw === "string" && raw.trim()) {
    try {
      const parsed = JSON.parse(raw);
      return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : {};
    } catch {
      return {};
    }
  }
  return {};
}

export function normalizeToolCallItem(item) {
  if (!item || typeof item !== "object") {
    return { id: "", name: "unknown", arguments: {}, rawArguments: "" };
  }
  let callId = String(item.id || "").trim();
  let name = String(item.name || item.tool_name || "").trim();
  let argsRaw = item.arguments ?? item.raw_arguments;
  const fn = item.function;
  if (fn && typeof fn === "object") {
    if (!name) name = String(fn.name || "").trim();
    if (argsRaw == null || (typeof argsRaw === "string" && !argsRaw.trim())) {
      argsRaw = fn.arguments;
    }
  }
  const rawArguments =
    typeof argsRaw === "string" && argsRaw.trim()
      ? argsRaw
      : item.raw_arguments && typeof item.raw_arguments === "string" && item.raw_arguments.trim()
        ? item.raw_arguments
        : argsRaw
          ? JSON.stringify(argsRaw)
          : "";
  let parsedArgs = parseToolArguments(argsRaw);
  if (!Object.keys(parsedArgs).length && rawArguments.trim()) {
    parsedArgs = parseToolArguments(rawArguments);
  }
  return {
    id: callId,
    name: name || "unknown",
    arguments: parsedArgs,
    rawArguments,
  };
}

export function extractToolCallsFromEvent(data) {
  const calls = data?.tool_calls;
  if (Array.isArray(calls) && calls.length) {
    return calls.filter((c) => c && typeof c === "object").map((c) => normalizeToolCallItem(c));
  }
  const name = String(data?.name || data?.tool_name || "").trim();
  if (name || data?.id || data?.tool_call_id) {
    return [normalizeToolCallItem(data)];
  }
  return [];
}

export function toolIndexFromEvent(data) {
  const n = Number(data?.tool_index);
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : -1;
}

export function toolCallPurpose(args) {
  const value = String(args?.call_purpose || "").trim();
  if (!value) return "";
  return value.length > 48 ? truncateGraphemes(value, 48) : value;
}

function sanitizeInline(text) {
  return String(text || "").replace(/\s+/g, " ").trim();
}

export function toolDisplayName(name, args = {}) {
  const n = String(name || "unknown").trim() || "unknown";
  if (n === USER_INFORMATION_TOOL) return "Agent 询问";
  const purpose = toolCallPurpose(args);
  const base = n === "bash_run" ? "bash" : n;
  if (purpose) return `${base}(${purpose})`;
  if (n === "bash_run") {
    const cmd = sanitizeInline(args.command);
    if (!cmd) return "bash(—)";
    return `bash(${cmd.length > 48 ? truncateGraphemes(cmd, 48) : cmd})`;
  }
  if (n === "browser_run_task") {
    const task = sanitizeInline(args.task);
    if (!task) return "浏览器任务";
    return `浏览器任务：${task.length > 56 ? truncateGraphemes(task, 56) : task}`;
  }
  if (n === "trigger_create") {
    return `trigger_create(${sanitizeInline(args.name) || "—"})`;
  }
  if (["write_file", "read_file", "search_replace"].includes(n)) {
    const path = sanitizeInline(args.path || args.file_path);
    return `${n}(${path || "—"})`;
  }
  const tempTitle = formatTemporaryAgentToolTitle(n, args);
  if (tempTitle) return tempTitle;
  if (args && Object.keys(args).length) return formatGenericToolTitle(n, args);
  return `${n}()`;
}

export function approvalItemDisplayName(item) {
  const name = String(item?.name || "unknown").trim() || "unknown";
  const args =
    item?.arguments && typeof item.arguments === "object"
      ? item.arguments
      : parseToolArguments(item?.rawArgs || item?.raw_arguments || item?.arguments);
  return toolDisplayName(name, args);
}

/** HITL 卡片副文案：突出自然语言目标，避免只看 raw JSON。 */
export function approvalItemHint(item) {
  const name = String(item?.name || "").trim();
  const args =
    item?.arguments && typeof item.arguments === "object"
      ? item.arguments
      : parseToolArguments(item?.rawArgs || item?.raw_arguments || item?.arguments);
  if (name === "browser_run_task") {
    const task = sanitizeInline(args.task);
    return task ? `目标：${task}` : "";
  }
  if (name === "bash_run") {
    const cmd = sanitizeInline(args.command);
    return cmd ? `命令：${cmd}` : "";
  }
  if (["write_file", "read_file", "search_replace"].includes(name)) {
    const path = sanitizeInline(args.path || args.file_path);
    return path ? `路径：${path}` : "";
  }
  return "";
}

/** reason 已含命令/路径等细节时不再重复渲染 hint。 */
export function approvalItemHintVisible(item) {
  const hint = approvalItemHint(item);
  if (!hint) return false;
  const reason = String(item?.reason || "").trim();
  if (!reason) return true;
  const core = hint.replace(/^(命令|路径|目标)：/, "").trim();
  if (core && reason.includes(core)) return false;
  return true;
}

function extractPartialJsonString(raw, key) {
  const marker = `"${key}"`;
  const start = raw.indexOf(marker);
  if (start < 0) return "";
  const colon = raw.indexOf(":", start + marker.length);
  if (colon < 0) return "";
  let i = colon + 1;
  while (i < raw.length && " \t\r\n".includes(raw[i])) i += 1;
  if (i >= raw.length || raw[i] !== '"') return "";
  i += 1;
  let out = "";
  while (i < raw.length) {
    const ch = raw[i];
    if (ch === "\\" && i + 1 < raw.length) {
      out += raw[i + 1];
      i += 2;
      continue;
    }
    if (ch === '"') break;
    out += ch;
    i += 1;
  }
  return out;
}

export function streamingToolCallPreview(name, rawArguments) {
  const n = String(name || "").trim() || "unknown";
  const raw = typeof rawArguments === "string" ? rawArguments : "";
  const parsed = parseToolArguments(raw);
  if (Object.keys(parsed).length) {
    return toolCallParts(n, parsed, { streaming: false });
  }
  const trimmed = raw.trim();
  if (!trimmed) return { arguments: {}, summary: toolDisplayName(n, {}), codePreview: "" };
  if (n === "bash_run") {
    const command = extractPartialJsonString(trimmed, "command");
    if (command) return { arguments: { command }, summary: toolDisplayName(n, { command }), codePreview: command };
  }
  if (n === "write_file") {
    const content = extractPartialJsonString(trimmed, "content");
    if (content) return { arguments: { content }, summary: toolDisplayName(n, { content }), codePreview: content };
  }
  return { arguments: {}, summary: toolDisplayName(n, {}), codePreview: trimmed };
}

export function toolCallParts(name, args, { streaming = false, rawArguments = "" } = {}) {
  const n = String(name || "unknown").trim() || "unknown";
  if (streaming && n !== "unknown" && String(rawArguments).trim()) {
    const preview = streamingToolCallPreview(n, rawArguments);
    return {
      summary: preview.summary,
      codePreview: preview.codePreview || "",
      arguments: preview.arguments,
    };
  }
  const summary = toolDisplayName(n, args);
  if (n === "bash_run") {
    const command = String(args.command || "");
    if (toolCallPurpose(args)) return { summary, codePreview: command, arguments: args };
    return { summary, codePreview: command, arguments: args };
  }
  if (n === "write_file") {
    const content = String(args.content || "");
    return { summary, codePreview: content, arguments: args };
  }
  return { summary, codePreview: "", arguments: args };
}
