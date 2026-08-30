import { formatToolElapsed } from "./format.js";
import { formatTriggerCondition, formatUnixTime, shortId, truncateText } from "./panelFormat.js";
import { resolveToolArgumentsFromData } from "./toolCalls.js";

const RESULT_STATUS_LABELS = {
  succeeded: ["已完成", "success"],
  completed: ["已完成", "success"],
  ok: ["已完成", "success"],
  failed: ["执行失败", "danger"],
  error: ["执行失败", "danger"],
  denied: ["已拒绝", "danger"],
  rejected: ["已拒绝", "danger"],
  cancelled: ["已终止", "warning"],
  canceled: ["已终止", "warning"],
  interrupted: ["已中断", "warning"],
  timed_out: ["已超时", "warning"],
  running: ["执行中", "info"],
  queued: ["排队中", "info"],
  awaiting_user: ["等待输入", "warning"],
  unknown: ["状态未知", "warning"],
};

const IMPORTANT_RESULT_LABELS = {
  ok: "调用结果",
  status: "状态",
  message: "消息",
  error: "错误",
  task_id: "任务 ID",
  trigger_id: "触发器 ID",
  terminal_id: "终端 ID",
  exit_code: "退出码",
  stdout_bytes: "stdout",
  stderr_bytes: "stderr",
  output_bytes: "输出",
  output_truncated: "输出完整性",
  next_seq: "下一序号",
  exited: "终端状态",
  steps: "执行步数",
  url: "最终地址",
  title: "页面标题",
  deleted: "删除结果",
  fire_count: "触发次数",
  next_fire_at: "下次执行",
};

function stringValue(value) {
  return String(value ?? "").trim();
}

function finiteNumber(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function compactText(value, max = 180) {
  const text = stringValue(value).replace(/\s+/g, " ");
  return text.length > max ? truncateText(text, max) : text;
}

function displayValue(value, { max = 180 } = {}) {
  if (value == null || value === "") return "";
  if (typeof value === "boolean") return value ? "是" : "否";
  if (typeof value === "number") return String(value);
  if (Array.isArray(value)) return `${value.length} 项`;
  if (typeof value === "object") return `${Object.keys(value).length} 个字段`;
  return compactText(value, max);
}

function field(label, value, kind = "text") {
  const text = stringValue(value);
  return text ? { label, value: text, kind } : null;
}

function block(label, content, kind = "text") {
  const text = stringValue(content);
  return text ? { label, content: text, kind } : null;
}

function addField(list, label, value, kind = "text") {
  const item = field(label, value, kind);
  if (item) list.push(item);
}

function addBlock(list, label, content, kind = "text") {
  const item = block(label, content, kind);
  if (item) list.push(item);
}

function parseJSONObject(raw) {
  if (raw && typeof raw === "object" && !Array.isArray(raw)) return raw;
  const text = stringValue(raw);
  if (!text.startsWith("{")) return null;
  try {
    const value = JSON.parse(text);
    return value && typeof value === "object" && !Array.isArray(value) ? value : null;
  } catch {
    return null;
  }
}

function dataFor(entry) {
  return entry?.data && typeof entry.data === "object" ? entry.data : {};
}

function resolveName(callEntry, resultEntry, fallbackEntry) {
  const sources = [dataFor(resultEntry), dataFor(callEntry), dataFor(fallbackEntry)];
  for (const data of sources) {
    const name = stringValue(data.tool_name || data.name);
    if (name) return name;
  }
  return "tool";
}

function resolveArgs(callEntry, resultEntry, fallbackEntry) {
  const args = {};
  for (const entry of [callEntry, fallbackEntry, resultEntry]) {
    const parsed = resolveToolArgumentsFromData(dataFor(entry));
    Object.assign(args, parsed);
  }
  return args;
}

function resolveContent(resultEntry, fallbackEntry) {
  const data = dataFor(resultEntry || fallbackEntry);
  return stringValue(data.content ?? data.output);
}

function statusFromContent(name, content, resultData) {
  if (resultData?.rejected) return "rejected";
  if (
    resultData?.interrupted ||
    resultData?.interrupted_by_stream_cancel ||
    resultData?.interrupted_by_user_message ||
    resultData?.interrupted_by_toolset_change
  ) {
    return "interrupted";
  }
  const explicit = stringValue(resultData?.status).toLowerCase();
  if (explicit) return explicit;

  const shellStatus = content.match(/\bstatus=([A-Za-z_]+)/i);
  if (shellStatus) return shellStatus[1].toLowerCase();

  const payload = parseJSONObject(content);
  if (payload) {
    const detail = payload.detail && typeof payload.detail === "object" ? payload.detail : payload;
    const nested = stringValue(detail.status).toLowerCase();
    if (nested) return nested;
    if (payload.ok === false) return "failed";
    if (payload.ok === true) return "succeeded";
  }
  if (name === "bash_run" && /\bCANCELLED\b/i.test(content)) return "cancelled";
  return resultData ? "succeeded" : "";
}

function statusPresentation(status) {
  const code = stringValue(status).toLowerCase();
  const [label, tone] = RESULT_STATUS_LABELS[code] || [code ? "已返回" : "", "neutral"];
  return { code, label, tone };
}

function baseModel(name, args, resultEntry, content) {
  const resultData = dataFor(resultEntry);
  const status = statusPresentation(statusFromContent(name, content, resultEntry ? resultData : null));
  return {
    toolName: name,
    args,
    statusCode: status.code,
    statusLabel: status.label,
    statusTone: status.tone,
    durationText: formatToolElapsed(resultData.duration_seconds),
    inputFields: [],
    resultFields: [],
    resultBlocks: [],
    hasResult: !!resultEntry,
  };
}

function addBashInput(model, args) {
  addField(model.inputFields, "命令", args.command, "code");
}

function parseDelimitedShellResult(content) {
  const lines = stringValue(content).split(/\r?\n/);
  const metadata = {};
  let section = "header";
  const stdout = [];
  const stderr = [];
  const other = [];
  for (const line of lines) {
    if (/^---\s+STDOUT\s+---$/i.test(line.trim())) {
      section = "stdout";
      continue;
    }
    if (/^---\s+STDERR\s+---$/i.test(line.trim())) {
      section = "stderr";
      continue;
    }
    if (section === "header") {
      const match = line.match(/^([A-Za-z_]+)\s*[:=]\s*(.*)$/);
      if (match) metadata[match[1].toLowerCase()] = match[2].trim();
      else if (!/^\[\w+_RESULT\]/i.test(line.trim()) && line.trim()) other.push(line);
    } else if (section === "stdout") {
      stdout.push(line);
    } else {
      stderr.push(line);
    }
  }
  return {
    metadata,
    stdout: stdout.join("\n").trim(),
    stderr: stderr.join("\n").trim(),
    other: other.join("\n").trim(),
  };
}

function addShellResult(model, content) {
  const parsed = parseDelimitedShellResult(content);
  const meta = parsed.metadata;
  const exitCode = meta.exit_code || meta.exit;
  if (exitCode != null && String(exitCode) !== "0") addField(model.resultFields, "退出码", exitCode);
  if (/true/i.test(meta.output_truncated || "")) {
    addField(model.resultFields, "输出完整性", "已截断");
  }
  addField(model.resultFields, "错误原因", meta.exit_error || meta.error);
  addBlock(model.resultBlocks, "stdout", parsed.stdout, "code");
  addBlock(model.resultBlocks, "stderr", parsed.stderr, "error");
  addBlock(model.resultBlocks, "详情", parsed.other, "text");
  if (!model.resultBlocks.length && content && !/^\s*\[[A-Z_]+_RESULT\]/i.test(content)) {
    addBlock(model.resultBlocks, "结果", content, "text");
  }
  if (meta.status) {
    const status = statusPresentation(meta.status);
    model.statusCode = status.code;
    model.statusLabel = status.label;
    model.statusTone = status.tone;
  }
}

function addTerminalInput(model, args) {
  addField(model.inputFields, "命令", args.command, "code");
  addField(model.inputFields, "终端", args.terminal_id);
  addField(model.inputFields, "工作目录", args.cwd);
}

function addStructuredTerminalResult(model, content, name) {
  const payload = parseJSONObject(content);
  if (!payload) {
    addShellResult(model, content);
    return;
  }
  const detail = payload.detail && typeof payload.detail === "object" ? payload.detail : payload;
  if (name === "terminal_open") {
    addField(model.resultFields, "终端 ID", detail.terminal_id || payload.terminal_id, "mono");
    if (detail.exited != null) addField(model.resultFields, "终端状态", detail.exited ? "已退出" : "运行中");
  } else {
    const exitCode = detail.exit_code;
    if (exitCode != null && String(exitCode) !== "0") addField(model.resultFields, "退出码", exitCode);
    if (detail.output_truncated) addField(model.resultFields, "输出完整性", "已截断");
  }
  addField(model.resultFields, "错误", payload.error || detail.error);
  addBlock(model.resultBlocks, "stdout", detail.stdout || detail.output, "code");
  addBlock(model.resultBlocks, "stderr", detail.stderr, "error");
  addBlock(model.resultBlocks, "详情", payload.message || detail.message, "text");
}

function humanInterval(seconds) {
  const value = Math.max(0, Math.floor(finiteNumber(seconds)));
  if (!value) return "";
  const units = [
    [86400, "天"],
    [3600, "小时"],
    [60, "分钟"],
  ];
  for (const [size, label] of units) {
    if (value >= size && value % size === 0) return `每 ${value / size} ${label}`;
  }
  return `每 ${value} 秒`;
}

function triggerConditionText(condition) {
  if (!condition || typeof condition !== "object") return "";
  if (finiteNumber(condition.interval_seconds) > 0) return humanInterval(condition.interval_seconds);
  return formatTriggerCondition(condition);
}

function addTriggerInput(model, args, name) {
  addField(model.inputFields, "任务名称", args.name);
  addField(model.inputFields, "触发器 ID", args.trigger_id, "mono");
  addField(model.inputFields, "触发频率", triggerConditionText(args.condition));
  const condition = args.condition && typeof args.condition === "object" ? args.condition : {};
  addField(model.inputFields, "触发前门控命令", condition.cmd, "code");
  addField(model.inputFields, "任务模板", args.task_template, "multiline");
  if (name === "trigger_list") addField(model.inputFields, "包含已禁用", args.include_disabled == null ? "是" : args.include_disabled ? "是" : "否");
}

function triggerTargetText(item) {
  const mode = stringValue(item.session_target_mode || item.agent_target_mode);
  const id = item.target_bound_agent_id || item.target_session_id;
  return id ? `${mode || "绑定目标"} · ${shortId(id, 18)}` : mode;
}

function addTriggerObjectResult(model, trigger, name) {
  if (!trigger || typeof trigger !== "object") return;
  if (name === "trigger_create") addField(model.resultFields, "触发器 ID", trigger.trigger_id, "mono");
  addField(model.resultFields, "状态", trigger.enabled == null ? "" : trigger.enabled ? "已启用" : "已禁用");
  addField(model.resultFields, "下次执行", formatUnixTime(trigger.next_fire_at));
  if (name === "trigger_get") {
    addField(model.resultFields, "任务名称", trigger.name);
    addField(model.resultFields, "触发频率", triggerConditionText(trigger.condition));
    addField(model.resultFields, "执行目标", triggerTargetText(trigger));
    if (trigger.fire_count != null) addField(model.resultFields, "触发次数", `${trigger.fire_count} 次`);
    addField(model.resultFields, "触发前门控命令", trigger.condition?.cmd, "code");
    addBlock(model.resultBlocks, "任务模板", trigger.task_template, "multiline");
  }
}

function addTriggerResult(model, name, content) {
  const payload = parseJSONObject(content);
  if (!payload) {
    addBlock(model.resultBlocks, "结果", content, "text");
    return;
  }
  if (payload.ok === false) {
    addField(model.resultFields, "错误", payload.error || payload.message, "error");
    return;
  }
  if (payload.trigger) {
    addTriggerObjectResult(model, payload.trigger, name);
    return;
  }
  if (Array.isArray(payload.triggers)) {
    addField(model.resultFields, "任务数量", `${payload.triggers.length} 个`);
    for (const item of payload.triggers) {
      if (!item || typeof item !== "object") continue;
      const schedule = triggerConditionText(item.condition) || "未设置频率";
      const enabled = item.enabled === false ? "已禁用" : "已启用";
      const next = formatUnixTime(item.next_fire_at);
      const task = compactText(item.task_template, 72);
      addBlock(
        model.resultBlocks,
        item.name || "定时任务",
        [schedule, enabled, next !== "—" ? `下次 ${next}` : "", task ? `任务：${task}` : ""].filter(Boolean).join(" · "),
        "text",
      );
    }
    return;
  }
  if (name === "trigger_delete") {
    addField(model.resultFields, "删除结果", payload.deleted ? "已删除" : "未找到");
    return;
  }
  addField(model.resultFields, "结果", "操作成功");
}

function addBrowserInput(model, args) {
  addField(model.inputFields, "任务", args.task, "multiline");
  addField(model.inputFields, "任务 ID", args.task_id, "mono");
  addField(model.inputFields, "最大步数", args.max_steps);
  if (args.wait != null) addField(model.inputFields, "等待完成", args.wait ? "是" : "否");
  if (finiteNumber(args.wait_timeout_seconds) > 0) addField(model.inputFields, "等待超时", `${finiteNumber(args.wait_timeout_seconds)} 秒`);
}

function addBrowserResult(model, content, name) {
  const payload = parseJSONObject(content);
  if (!payload) {
    addBlock(model.resultBlocks, "结果", content, "text");
    return;
  }
  const detail = payload.detail && typeof payload.detail === "object" ? payload.detail : payload;
  if (name === "browser_run_task") addField(model.resultFields, "任务 ID", detail.task_id || payload.task_id, "mono");
  addField(model.resultFields, "执行步数", detail.steps);
  addField(model.resultFields, "最终地址", payload.url || detail.url);
  addField(model.resultFields, "页面标题", payload.title || detail.title);
  addField(model.resultFields, "错误", payload.error || detail.error, "error");
  addBlock(model.resultBlocks, "摘要", detail.summary || payload.extracted_content || detail.extracted_content || payload.llm_representation, "text");
}

function addFileInput(model, args, name) {
  addField(model.inputFields, "路径", args.path || args.file_path || args.directory, "mono");
  if (name === "read_file") {
    addField(model.inputFields, "起始行", args.line_offset);
    addField(model.inputFields, "读取行数", args.line_limit);
    addField(model.inputFields, "编码", args.encoding);
  } else if (name === "write_file") {
    addField(model.inputFields, "编码", args.encoding);
    if (args.content != null) addField(model.inputFields, "写入内容", `${String(args.content).length} 字符`, "text");
  } else if (name === "search_replace") {
    addField(model.inputFields, "替换模式", args.replace_all ? "全部替换" : "单处替换");
    if (args.old_string != null) addField(model.inputFields, "原文本", `${String(args.old_string).length} 字符`);
    if (args.new_string != null) addField(model.inputFields, "新文本", `${String(args.new_string).length} 字符`);
  } else {
    addField(model.inputFields, "匹配模式", args.pattern || args.query || args.glob_pattern);
    if (name === "glob_files") addField(model.inputFields, "包含目录", args.include_dirs ? "是" : "否");
    addField(model.inputFields, "分页偏移", args.index_offset ?? args.offset ?? args.line_offset);
    addField(model.inputFields, "数量上限", args.count_limit ?? args.max_results ?? args.line_limit);
  }
}

function addReadFileResult(model, content) {
  const known = [
    ["文件编码", "文件编码"],
    ["文件总行数", "文件总行数"],
  ];
  for (const [source, label] of known) {
    const match = stringValue(content).match(new RegExp(`^${source}:\\s*(.+)$`, "m"));
    if (match) addField(model.resultFields, label, match[1]);
  }
}

function addTextKeyFields(model, content) {
  for (const line of stringValue(content).split(/\r?\n/)) {
    const match = line.match(/^\s*([^:：]{1,24})[:：]\s*(.*?)\s*$/);
    if (!match || !match[2]) continue;
    const label = match[1].trim();
    if (["成功", "替换次数", "路径", "错误", "写入字节数"].includes(label)) {
      addField(model.resultFields, label, match[2], label === "错误" ? "error" : "text");
    }
  }
  const wrote = stringValue(content).match(/^wrote\s+(\d+)\s+bytes\s+to\s+(.+?)\s+\(encoding=(.+)\)$/i);
  if (wrote) {
    addField(model.resultFields, "写入字节数", `${wrote[1]} B`);
    addField(model.resultFields, "路径", wrote[2], "mono");
    addField(model.resultFields, "编码", wrote[3]);
  }
}

function addSearchResult(model, content, name) {
  const labels = name === "glob_files"
    ? [
        ["total_matches", "匹配数量"],
        ["showing", "当前范围"],
        ["next_offset", "下一页偏移"],
        ["后方是否还有结果", "还有更多结果"],
      ]
    : [
        ["全文件命中数", "全文件命中数"],
        ["本页命中", "当前页命中"],
        ["next_index_offset", "下一页偏移"],
        ["前方是否还有命中", "前方还有命中"],
        ["后方是否还有命中", "后方还有命中"],
      ];
  const text = stringValue(content);
  for (const [source, label] of labels) {
    const match = text.match(new RegExp(`^${source}:\\s*(.+)$`, "m"));
    if (match) addField(model.resultFields, label, match[1]);
  }
  const separator = text.indexOf("\n---\n");
  const body = separator >= 0 ? text.slice(separator + 5).trim() : "";
  addBlock(model.resultBlocks, name === "glob_files" ? "匹配路径" : "匹配内容", body, "text");
}

function addGenericInput(model, args) {
  const keys = Object.keys(args).filter((key) => !["call_purpose", "purpose", "run_in_background"].includes(key)).sort();
  for (const key of keys.slice(0, 8)) {
    const value = displayValue(args[key], { max: 120 });
    if (value) addField(model.inputFields, key, value, typeof args[key] === "string" && String(args[key]).length > 80 ? "multiline" : "text");
  }
  if (keys.length > 8) addField(model.inputFields, "其他参数", `${keys.length - 8} 项`);
}

function addGenericResult(model, content) {
  const payload = parseJSONObject(content);
  if (!payload) {
    addBlock(model.resultBlocks, "结果", content, "text");
    return;
  }
  const detail = payload.detail && typeof payload.detail === "object" ? payload.detail : payload;
  let hasField = false;
  for (const [key, value] of Object.entries(detail)) {
    if ([
      "ok",
      "status",
      "call_purpose",
      "purpose",
      "terminal_id",
      "trigger_id",
      "task_id",
      "output",
      "stdout",
      "stderr",
      "summary",
      "extracted_content",
      "llm_representation",
      "items",
      "triggers",
    ].includes(key)) continue;
    if (!(key in IMPORTANT_RESULT_LABELS) && typeof value === "object") continue;
    const valueText = displayValue(value);
    if (!valueText) continue;
    addField(model.resultFields, IMPORTANT_RESULT_LABELS[key] || key, valueText, key === "error" ? "error" : "text");
    hasField = true;
  }
  addBlock(model.resultBlocks, "结果", detail.summary || detail.output || detail.stdout || payload.message, "text");
  if (!hasField && !model.resultBlocks.length) addField(model.resultFields, "结果", "已返回");
}

export function buildToolCardModel({ callEntry = null, resultEntry = null, entry = null } = {}) {
  const fallback = entry || resultEntry || callEntry || {};
  const name = resolveName(callEntry, resultEntry, fallback);
  const args = resolveArgs(callEntry, resultEntry, fallback);
  const content = resolveContent(resultEntry, fallback);
  const model = baseModel(name, args, resultEntry, content);

  switch (name) {
    case "bash_run":
    case "linux_exec":
      if (name === "bash_run") addBashInput(model, args);
      else {
        addTerminalInput(model, args);
        addField(model.inputFields, "远程配置", args.config_id);
      }
      if (resultEntry) addShellResult(model, content);
      break;
    case "terminal_command":
      addTerminalInput(model, args);
      if (resultEntry) addStructuredTerminalResult(model, content, name);
      break;
    case "terminal_open":
      addField(model.inputFields, "目标", args.config_id || args.target);
      addField(model.inputFields, "工作目录", args.cwd);
      if (resultEntry) addStructuredTerminalResult(model, content, name);
      break;
    case "terminal_input":
    case "terminal_read":
    case "terminal_terminate":
      addField(model.inputFields, "终端", args.terminal_id, "mono");
      addField(model.inputFields, "输入内容", args.data, "code");
      if (resultEntry) addStructuredTerminalResult(model, content, name);
      break;
    case "trigger_list":
    case "trigger_get":
    case "trigger_create":
    case "trigger_update":
    case "trigger_delete":
      addTriggerInput(model, args, name);
      if (resultEntry) addTriggerResult(model, name, content);
      break;
    case "browser_run_task":
    case "browser_task_status":
    case "browser_task_cancel":
      addBrowserInput(model, args);
      if (resultEntry) addBrowserResult(model, content, name);
      break;
    case "read_file":
    case "write_file":
    case "search_replace":
    case "glob_files":
    case "grep_file":
    case "grep_files":
      addFileInput(model, args, name);
      if (resultEntry) {
        if (name === "read_file") addReadFileResult(model, content);
        else if (name === "glob_files" || name === "grep_file" || name === "grep_files") addSearchResult(model, content, name);
        else {
          addTextKeyFields(model, content);
          const separator = content.indexOf("\n---\n");
          addBlock(model.resultBlocks, "结果", separator >= 0 ? content.slice(separator + 5) : content, "text");
        }
      }
      break;
    default:
      addGenericInput(model, args);
      if (resultEntry) addGenericResult(model, content);
      break;
  }

  return model;
}
