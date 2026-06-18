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
  "list_dir",
  "grep",
  "glob",
  "search_replace",
  "delete_file",
]);

const SHELL_TOOLS = new Set(["bash_run", "bash"]);

/** 工具来源/类别，用于 Web UI 视觉区分（终端无法做到的丰富样式）。 */
export function resolveToolVisual(entry) {
  const data = entry?.data || entry || {};
  const name = String(data.tool_name || data.name || "").trim();

  if (data.a2a_relay) {
    return {
      kind: "a2a",
      label: "A2A 中继",
      short: "A2A",
      icon: "⇄",
    };
  }

  if (data.child_session_id || CHILD_AGENT_TOOLS.has(name)) {
    return {
      kind: "child",
      label: "子 Agent",
      short: "子",
      icon: "⎇",
    };
  }

  if (SHELL_TOOLS.has(name)) {
    return { kind: "shell", label: "Shell", short: "$", icon: "$" };
  }

  if (FS_TOOLS.has(name)) {
    return { kind: "fs", label: "文件", short: "fs", icon: "F" };
  }

  if (name === USER_INFORMATION_TOOL) {
    return { kind: "user", label: "询问用户", short: "?", icon: "?" };
  }

  return { kind: "agent", label: "主 Agent", short: "主", icon: "◎" };
}
