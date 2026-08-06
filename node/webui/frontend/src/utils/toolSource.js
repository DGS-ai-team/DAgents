const USER_INFORMATION_TOOL = "ask_user_information";

const CHILD_AGENT_TOOLS = new Set([
  "create_temporary_agent",
  "wait_temporary_agents",
  "temporary_agent_status",
  "cancel_temporary_agent",
]);

const FS_TOOLS = new Set([
  "read_file",
  "write_file",
  "glob_files",
  "grep_files",
  "grep_file",
  "search_replace",
  "show_image",
  "read_image",
  "list_dir",
  "delete_file",
]);

const SHELL_TOOLS = new Set(["bash_run", "bash", "background_job_status", "background_job_cancel"]);

const SKILLS_TOOLS = new Set(["load_skills", "unload_skills", "clear_skills"]);

const KIND_META = {
  shell: { label: "shell", short: "shell", icon: "$" },
  fs: { label: "fs", short: "fs", icon: "F" },
  browser: { label: "browser", short: "browser", icon: "◉" },
  wecom: { label: "wecom", short: "wecom", icon: "✉" },
  triggers: { label: "triggers", short: "triggers", icon: "⏱" },
  skills: { label: "skills", short: "skills", icon: "S" },
  child: { label: "child", short: "child", icon: "⎇" },
  hitl: { label: "hitl", short: "hitl", icon: "?" },
  memory: { label: "memory", short: "memory", icon: "✎" },
  tool: { label: "tool", short: "tool", icon: "◎" },
};

/** 根据 tool 名推断工具组（对齐 Agent defaults.tools.enabled_groups）。 */
export function inferToolKind(name, data = {}) {
  const n = String(name || "").trim();
  if (data?.child_agent_id || CHILD_AGENT_TOOLS.has(n)) return "child";
  if (n.startsWith("browser_")) return "browser";
  if (n.startsWith("wecom_")) return "wecom";
  if (n.startsWith("trigger_")) return "triggers";
  if (SKILLS_TOOLS.has(n)) return "skills";
  if (SHELL_TOOLS.has(n) || n.startsWith("background_job_")) return "shell";
  if (FS_TOOLS.has(n)) return "fs";
  if (n === USER_INFORMATION_TOOL) return "hitl";
  if (n === "remember") return "memory";
  if (/^(glob|grep|read|write|show)_/.test(n) || n.includes("file") || n.includes("image")) return "fs";
  return "tool";
}

function visualForKind(kind) {
  const meta = KIND_META[kind] || KIND_META.tool;
  return { kind, label: meta.label, short: meta.short, icon: meta.icon };
}

/** 工具来源/类别，用于 Web UI 视觉区分（终端无法做到的丰富样式）。 */
export function resolveToolVisual(entry) {
  const data = entry?.data || entry || {};
  const name = String(data.tool_name || data.name || entry?.tool_name || "").trim();
  return visualForKind(inferToolKind(name, data));
}
