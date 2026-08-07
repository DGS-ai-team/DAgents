import { truncateGraphemes } from "./textTruncate.js";
import { resolveToolArgumentsFromData, toolCallPurpose } from "./toolCalls.js";
import {
  isBashBackgroundActive,
  parseBashResultStatus,
  toolCallIdFromEntry,
  toolJobsStore,
} from "../stores/toolJobs.js";

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
  browser_run_task: "浏览器任务",
  browser_task_status: "查询浏览器任务",
  browser_task_cancel: "取消浏览器任务",
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

/** 从 browser_task_* 工具结果 JSON 提取短状态（不含长摘要，留给引用卡片）。 */
function browserTaskShortStatus(resultEntry) {
  const raw = resultEntry?.data?.content;
  if (raw == null) return "";
  let obj = raw;
  if (typeof raw === "string") {
    const s = raw.trim();
    if (!s.startsWith("{")) return "";
    try {
      obj = JSON.parse(s);
    } catch {
      return "";
    }
  }
  if (!obj || typeof obj !== "object") return "";
  const detail = obj.detail && typeof obj.detail === "object" ? obj.detail : obj;
  const status = sanitizeInline(detail.status);
  if (status === "completed" && detail.success === false) return "未完全完成";
  if (status === "completed") {
    const steps = detail.steps;
    return steps != null ? `已完成 · ${steps} 步` : "已完成";
  }
  if (status === "failed") return "失败";
  if (status === "cancelled") return "已取消";
  if (status === "running") return "执行中";
  if (status === "queued") return "排队中";
  return status;
}

/** 从 browser_task_* 工具结果 JSON 提取一行用户可读提示。 */
function browserTaskResultHint(resultEntry) {
  const raw = resultEntry?.data?.content;
  if (raw == null) return "";
  let obj = raw;
  if (typeof raw === "string") {
    const s = raw.trim();
    if (!s.startsWith("{")) return "";
    try {
      obj = JSON.parse(s);
    } catch {
      return "";
    }
  }
  if (!obj || typeof obj !== "object") return "";
  const detail = obj.detail && typeof obj.detail === "object" ? obj.detail : obj;
  const summary = sanitizeInline(detail.summary || obj.extracted_content || detail.extracted_content);
  if (summary) return summary.length > 48 ? truncateGraphemes(summary, 48) : summary;
  return browserTaskShortStatus(resultEntry);
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
  if (name === "browser_run_task") {
    const task = sanitizeInline(args.task);
    const short = resultEntry ? browserTaskShortStatus(resultEntry) : "";
    if (task && short) {
      const goal = task.length > 40 ? truncateGraphemes(task, 40) : task;
      return `${label}：${goal} · ${short}`;
    }
    if (task) return `${label}：${task.length > 48 ? truncateGraphemes(task, 48) : task}`;
    if (short) return `${label}：${short}`;
  }
  if (name === "browser_task_status" || name === "browser_task_cancel") {
    const fromResult = browserTaskResultHint(resultEntry);
    if (fromResult) return `${label}：${fromResult}`;
    const tid = sanitizeInline(args.task_id);
    if (tid) return `${label}：${tid}`;
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

/**
 * 工具步骤相位（同批免审批 tool_call 后端并行执行）：
 * generating | running | background | pending | completed | cancelled | rejected | interrupted | idle
 *
 * @param {{ callEntry?: object, resultEntry?: object, executionHint?: 'active' | 'pending' | null }} args
 */
export function resolveToolStepPhase({ callEntry, resultEntry, executionHint = null } = {}) {
  const entry = resultEntry || callEntry || {};
  if (entry.sideEffectApplied) return "completed";
  if (resultEntry?.data?.rejected || callEntry?.data?.rejected) return "rejected";
  if (resultEntry?.data?.interrupted || callEntry?.data?.interrupted) return "interrupted";

  const bashStatus = parseBashResultStatus(resultEntry?.data?.content);
  if (bashStatus === "CANCELLED") return "cancelled";

  if (callEntry?.partial) return "generating";

  const id = toolCallIdFromEntry(callEntry) || toolCallIdFromEntry(resultEntry);
  if (id && toolJobsStore.runningCallIds.includes(id)) return "running";
  if (id && toolJobsStore.backgroundCallIds.includes(id)) return "background";
  if (isBashBackgroundActive({ callEntry, resultEntry })) return "background";

  // 残留 RUNNING 但已不在后台队列 → 视为结束
  if (bashStatus === "RUNNING" || bashStatus === "SUCCEEDED") return "completed";
  if (resultEntry) return "completed";

  if (callEntry && !resultEntry) {
    // HITL 待批：尚未开跑
    if (executionHint === "pending") return "pending";
    // 并行模型：已 final 的 tool_call 默认即在执行（含 /tool-jobs 未登记的 FS 等）
    return "running";
  }
  return "idle";
}

export function toolStepStatusText({ callEntry, resultEntry, executionHint = null } = {}) {
  const entry = resultEntry || callEntry || {};
  if (entry.sideEffectStale) return "已失效";
  if (entry.sideEffectApplied) return "已入库";

  switch (resolveToolStepPhase({ callEntry, resultEntry, executionHint })) {
    case "generating":
      return "生成中";
    case "running":
      return "执行中";
    case "background":
      return "后台执行中";
    case "pending":
      return "待执行";
    case "cancelled":
      return "已终止";
    case "rejected":
      return "已拒绝";
    case "interrupted":
      return "已中断";
    case "completed":
      return "已完成";
    default:
      return "";
  }
}

/** 真正在跑（生成参数 / 执行 / 后台）— 用于转圈与高亮；待审批不算。 */
export function toolStepIsInProgress({ callEntry, resultEntry, executionHint = null } = {}) {
  const phase = resolveToolStepPhase({ callEntry, resultEntry, executionHint });
  return phase === "generating" || phase === "running" || phase === "background";
}

/** 已收到 tool_call、尚未开跑（如 HITL 待审批）。 */
export function toolStepIsPending({ callEntry, resultEntry, executionHint = null } = {}) {
  return resolveToolStepPhase({ callEntry, resultEntry, executionHint }) === "pending";
}
