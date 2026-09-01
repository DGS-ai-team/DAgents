const USER_INFORMATION_TOOL = "ask_user_information";
const MCP_TOOL_PREFIX = "mcp__";

const CHILD_AGENT_TOOLS = new Set([
  "create_temporary_agent",
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
]);

const SHELL_TOOLS = new Set(["bash_run", "bash"]);
const COMPUTER_TOOLS = new Set(["screen_capture", "computer_use"]);

const LINUX_TOOLS = new Set(["linux_exec", "linux_file_upload", "linux_file_download"]);

const SKILLS_TOOLS = new Set(["load_skills", "unload_skills", "clear_skills"]);

const KIND_META = {
  mcp: { label: "mcp", short: "mcp", icon: "M" },
  terminal: { label: "terminal", short: "terminal", icon: ">_" },
  shell: { label: "shell", short: "shell", icon: "$" },
  fs: { label: "fs", short: "fs", icon: "F" },
  browser: { label: "browser", short: "browser", icon: "◉" },
  computer: { label: "computer", short: "computer", icon: "▣" },
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
  if (n.startsWith(MCP_TOOL_PREFIX)) return "mcp";
  if (data?.child_agent_id || CHILD_AGENT_TOOLS.has(n)) return "child";
  if (n.startsWith("browser_")) return "browser";
  if (COMPUTER_TOOLS.has(n)) return "computer";
  if (n.startsWith("wecom_")) return "wecom";
  if (n.startsWith("trigger_")) return "triggers";
  if (n.startsWith("terminal_")) return "terminal";
  if (SKILLS_TOOLS.has(n)) return "skills";
  if (LINUX_TOOLS.has(n) || n.startsWith("linux_")) return "terminal";
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
  const kind = inferToolKind(name, data);
  if (kind === "mcp") {
    const qualified = name.slice(MCP_TOOL_PREFIX.length);
    const separator = qualified.indexOf("__");
    const serverName = separator > 0 ? qualified.slice(0, separator) : "mcp";
    return { kind, label: serverName, short: serverName, icon: KIND_META.mcp.icon };
  }
  return visualForKind(kind);
}

/**
 * 合并工具气泡的总图标。
 * 同一气泡里出现多个工具组时，不使用首个工具的图标，避免让用户误以为
 * 所有步骤都属于同一类工具；展开后仍由每个 ToolSummaryRow 展示准确图标。
 */
export function resolveToolGroupVisual(steps = []) {
  const visuals = (Array.isArray(steps) ? steps : [])
    .map((step) => resolveToolVisual(step?.resultEntry || step?.callEntry || step || {}))
    .filter(Boolean);
  const first = visuals[0] || visualForKind("tool");
  const kinds = new Set(visuals.map((item) => item.kind));
  if (kinds.size > 1) {
    return { kind: "wrench", label: "多种工具组", short: "多工具", icon: "⚒", mixed: true };
  }
  return first;
}
