import { truncateGraphemes } from "./textTruncate.js";
import { parseToolArguments, resolveToolArgumentsFromData, toolCallPurpose } from "./toolCalls.js";

/** 面向普通用户的工具类别名（非技术 tool id）。 */
const TOOL_USER_LABELS = {
  bash_run: "执行命令",
  read_file: "读取文件",
  write_file: "写入文件",
  search_replace: "编辑文件",
  glob_files: "查找文件",
  grep_files: "搜索文件",
  read_image: "读取图片",
  show_image: "展示图片",
  browser_snapshot: "网页截图",
  browser_screenshot: "网页截图",
  browser_navigate: "打开网页",
  wecom_send_markdown: "企微推送",
  wecom_send_file: "企微发文件",
  load_skills: "加载技能",
  unload_skills: "卸载技能",
  clear_skills: "清空技能",
  tool_callback: "处理回调",
  get_callback: "获取回调",
  ask_user_information: "向你提问",
  create_temporary_agent: "创建子任务",
  wait_temporary_agents: "等待子任务",
  temporary_agent_status: "查询子任务",
  cancel_temporary_agent: "取消子任务",
};

const GENERIC_TOOL_LABEL = "助手执行了一步操作";

function sanitizeInline(text) {
  return String(text || "").replace(/\s+/g, " ").trim();
}

function userLabelForTool(name) {
  const n = String(name || "").trim();
  if (!n || n === "tool" || n === "unknown" || n === "unknown_tool") {
    return GENERIC_TOOL_LABEL;
  }
  return TOOL_USER_LABELS[n] || "助手使用工具";
}

function argsFromStep(callEntry, resultEntry) {
  const data = resultEntry?.data || callEntry?.data || {};
  const parsed = resolveToolArgumentsFromData(data);
  if (Object.keys(parsed).length) return parsed;
  if (callEntry?.data) return resolveToolArgumentsFromData(callEntry.data);
  return {};
}

function toolNameFromStep(callEntry, resultEntry) {
  const data = resultEntry?.data || callEntry?.data || {};
  const name = String(data.tool_name || data.name || callEntry?.data?.tool_name || callEntry?.data?.name || "").trim();
  if (name && name !== "tool" && name !== "unknown") return name;
  return "";
}

/**
 * 用户向一行摘要（F-UI6）；禁止出现裸 tool()。
 */
export function toolStepUserSummary({ callEntry, resultEntry } = {}) {
  const name = toolNameFromStep(callEntry, resultEntry);
  const args = argsFromStep(callEntry, resultEntry);
  const label = userLabelForTool(name);
  const purpose = toolCallPurpose(args);

  if (purpose) return `${label}：${purpose}`;

  if (name === "bash_run") {
    const cmd = sanitizeInline(args.command);
    if (cmd) return `${label}：${cmd.length > 48 ? truncateGraphemes(cmd, 48) : cmd}`;
  }
  if (["read_file", "write_file", "search_replace", "show_image", "read_image"].includes(name)) {
    const path = sanitizeInline(args.path || args.file_path);
    if (path) return `${label}：${path.length > 56 ? truncateGraphemes(path, 56) : path}`;
  }
  if (name.startsWith("browser_")) {
    const url = sanitizeInline(args.url);
    if (url) return `${label}：${url.length > 48 ? truncateGraphemes(url, 48) : url}`;
  }

  const entrySummary = String(resultEntry?.summary || callEntry?.summary || "").trim();
  if (entrySummary && entrySummary !== "tool" && !entrySummary.endsWith("()")) {
    return entrySummary;
  }

  if (name) return label;
  return GENERIC_TOOL_LABEL;
}

export function toolStepStatusText({ callEntry, resultEntry } = {}) {
  const entry = resultEntry || callEntry || {};
  if (entry.sideEffectApplied) return "已入库";
  if (entry.sideEffectStale) return "已失效";
  if (resultEntry?.data?.rejected || callEntry?.data?.rejected) return "已拒绝";
  if (resultEntry?.data?.interrupted || callEntry?.data?.interrupted) return "已中断";
  if (callEntry?.partial) return "进行中";
  const content = String(resultEntry?.data?.content || "");
  if (/\[BASH_RESULT\]\s+status=CANCELLED\b/i.test(content)) return "已终止";
  if (/\[BASH_RESULT\]\s+status=RUNNING\b/i.test(content)) return "后台执行中";
  if (/\[BASH_RESULT\]\s+status=SUCCEEDED\b/i.test(content)) return "已完成";
  if (resultEntry) return "已完成";
  if (callEntry) return "进行中";
  return "";
}

export function toolStepIsInProgress({ callEntry, resultEntry } = {}) {
  return Boolean(callEntry?.partial || (callEntry && !resultEntry));
}
