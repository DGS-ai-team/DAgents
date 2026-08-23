export function normalizeWorkbenchMode(value) {
  return String(value || "").trim().toLowerCase() === "agent" ? "agent" : "terminal";
}

export function terminalTargetLabel(item) {
  if (!item) return "未选择终端";
  const kind = String(item.target_kind || "").trim().toLowerCase();
  const shell = String(item.shell || "").trim();
  const user = String(item.username || item.user || "").trim();
  const host = String(item.host || "").trim();
  if (kind === "linux_channel" || kind === "linux") {
    const remote = user && host ? ` · ${user}@${host}` : "";
    return `远程 Linux${remote}${shell ? ` · ${shell}` : ""}`;
  }
  if (shell.toLowerCase() === "wsl" || String(item.target_id || "").toLowerCase() === "wsl") {
    return "本机 · WSL";
  }
  if (shell) return `本机 · ${shell}`;
  return kind === "local" ? "本机终端" : kind || "终端";
}

export function terminalStatusLabel(status) {
  return (
    {
      running: "运行中",
      exited: "已退出",
      closed: "已关闭",
      error: "错误",
    }[String(status || "")] || String(status || "未知")
  );
}

// A session returned by terminal_list is already usable while its authoritative
// lifecycle is running; the WebSocket's connected label is a transport detail.
export function terminalStatusReady(status) {
  return ["running", "connected", "已连接", "运行中"].includes(String(status || "").trim());
}

export function terminalInputText(value, { appendNewline = false } = {}) {
  const text = String(value ?? "");
  if (!text) return "";
  return text + (appendNewline ? "\n" : "");
}
