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

const APPROVAL_TOOL_LABELS = {
  bash_run: "bash",
  linux_exec: "Linux 命令",
  linux_file_upload: "上传 Linux 文件",
  linux_file_download: "下载 Linux 文件",
  terminal_command: "终端命令",
  terminal_input: "终端输入",
  terminal_open: "打开终端",
  terminal_read: "读取终端",
  terminal_terminate: "关闭终端",
  browser_run_task: "浏览器任务",
  browser_task_status: "查询浏览器任务",
  browser_task_cancel: "取消浏览器任务",
  read_file: "读取文件",
  write_file: "写入文件",
  search_replace: "替换文件内容",
  glob_files: "查找文件",
  grep_file: "搜索文件",
  grep_files: "搜索文件",
  trigger_create: "创建定时任务",
  trigger_update: "更新定时任务",
  trigger_delete: "删除定时任务",
  background_job_cancel: "取消后台任务",
};

const APPROVAL_ACTION_LABELS = {
  bash_run: "将执行 Shell 命令",
  linux_exec: "执行 Linux SSH 命令",
  linux_file_upload: "上传 Linux 文件",
  linux_file_download: "下载 Linux 文件",
  terminal_command: "将在终端执行命令",
  terminal_input: "向终端发送输入",
  terminal_open: "将打开终端",
  terminal_terminate: "将关闭终端",
  write_file: "将修改本地文件",
  search_replace: "将修改本地文件",
  trigger_create: "将创建定时触发器",
  trigger_update: "将更新定时触发器",
  trigger_delete: "将删除定时触发器",
  background_job_cancel: "将取消后台任务",
};

const APPROVAL_REASON_PREFIXES = {
  bash_run: ["将执行 Shell 命令"],
  linux_exec: ["执行 Linux SSH 命令", "在 Linux SSH channel"],
  linux_file_upload: ["上传 Linux 文件"],
  linux_file_download: ["下载 Linux 文件"],
  terminal_command: ["在终端"],
  terminal_input: ["向终端"],
  terminal_open: ["将打开终端"],
  terminal_terminate: ["将关闭终端"],
  write_file: ["将修改本地文件"],
  search_replace: ["将修改本地文件"],
  trigger_create: ["将创建定时触发器"],
  trigger_update: ["将更新定时触发器"],
  trigger_delete: ["将删除定时触发器"],
  background_job_cancel: ["将取消后台任务"],
};

function approvalItemArguments(item) {
  if (
    item?.arguments &&
    typeof item.arguments === "object" &&
    !Array.isArray(item.arguments) &&
    Object.keys(item.arguments).length
  ) {
    return item.arguments;
  }
  return parseToolArguments(item?.rawArgs || item?.raw_arguments || item?.arguments);
}

/** 审批卡片标题只标识工具，不把参数再次拼到标题中。 */
export function approvalItemToolLabel(item) {
  const name = String(item?.name || "unknown").trim() || "unknown";
  return APPROVAL_TOOL_LABELS[name] || name;
}

/**
 * 审批原因与关键参数分开呈现。
 * 内置策略原因中的动态参数已经会在表单中展示，因此只保留动作描述；
 * 自定义策略原因则保留冒号前的说明，避免丢失“为什么需要审批”。
 */
export function approvalItemReason(item) {
  const reason = String(item?.reason || "").trim();
  if (!reason) return "";
  const name = String(item?.name || "").trim();
  const args = approvalItemArguments(item);
  const overlaps = Object.values(args).some((value) => {
    if (value == null || typeof value === "object") return false;
    const text = sanitizeInline(value);
    return text.length >= 2 && reason.includes(text);
  });
  if (!overlaps) return reason;

  const prefix = reason.split(/[:：]/, 1)[0].trim();
  const knownPrefixes = APPROVAL_REASON_PREFIXES[name] || [];
  if (knownPrefixes.some((candidate) => prefix.startsWith(candidate))) {
    return APPROVAL_ACTION_LABELS[name] || prefix;
  }
  return prefix || reason;
}

/** 将审批参数格式化为默认折叠的原始 JSON，解析失败时保留原文。 */
export function formatApprovalRawArguments(raw, args = {}) {
  const text = typeof raw === "string" ? raw.trim() : "";
  if (text) {
    try {
      return JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      return text;
    }
  }
  if (args && typeof args === "object" && !Array.isArray(args) && Object.keys(args).length) {
    return JSON.stringify(args, null, 2);
  }
  return "";
}

/** HITL 卡片副文案：突出自然语言目标，避免只看 raw JSON。 */
export function approvalItemHint(item) {
  const name = String(item?.name || "").trim();
  const args = approvalItemArguments(item);
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
