/** Policy 四档 mode（对齐 txt：never / always / rule / deny）。 */
export const POLICY_MODES = [
  { value: "never", label: "自动允许" },
  { value: "always", label: "需审批" },
  { value: "rule", label: "特殊规则" },
  { value: "deny", label: "禁止" },
];

export const PROTECTED_POLICY_TOOL = "ask_user_information";

/** 兼容旧 decision 字段；require_approval 无法区分 always/rule，默认 always。 */
export function decisionToMode(decision) {
  switch (String(decision || "").trim()) {
    case "allow_auto":
      return "never";
    case "deny":
      return "deny";
    case "require_approval":
      return "always";
    default:
      return "rule";
  }
}

export function entryMode(row) {
  const mode = String(row?.mode || "").trim();
  if (mode) return mode;
  return decisionToMode(row?.decision);
}

export function canSetPolicyMode(toolName, mode) {
  if (String(toolName || "") === PROTECTED_POLICY_TOOL && mode === "deny") {
    return false;
  }
  return POLICY_MODES.some((d) => d.value === mode);
}

export function filterPolicyTools(tools, filterText) {
  const rows = Array.isArray(tools) ? tools : [];
  const filt = String(filterText || "").trim().toLowerCase();
  return rows.filter((row) => {
    const name = String(row?.name || "");
    return !filt || name.toLowerCase().includes(filt);
  });
}

export function filterPolicyShellEntries(entries, filterText) {
  const rows = Array.isArray(entries) ? entries : [];
  const filt = String(filterText || "").trim().toLowerCase();
  return rows.filter((row) => {
    const command = String(row?.command || "");
    return !filt || command.toLowerCase().includes(filt);
  });
}

export function normalizeShellCommand(raw) {
  const s = String(raw || "").trim();
  if (!s) return "";
  return s.split(/\s+/)[0].toLowerCase();
}

export function applyLocalToolUpdate(snapshot, name, mode) {
  if (!snapshot || typeof snapshot !== "object") return;
  const tools = Array.isArray(snapshot.tools) ? snapshot.tools : [];
  const idx = tools.findIndex((item) => item?.name === name);
  const row = { name, mode, decision: modeToLegacyDecision(mode), configured: true };
  if (idx >= 0) {
    tools[idx] = { ...tools[idx], ...row };
  } else {
    tools.push(row);
  }
  snapshot.tools = tools;
}

export function applyLocalShellUpdate(snapshot, shellType, command, mode) {
  if (!snapshot || typeof snapshot !== "object") return;
  const shell = snapshot.shell && typeof snapshot.shell === "object" ? snapshot.shell : {};
  const cmd = normalizeShellCommand(command);
  if (!cmd) return;
  const items = Array.isArray(shell[shellType]) ? shell[shellType] : [];
  const idx = items.findIndex((item) => normalizeShellCommand(item?.command) === cmd);
  const row = { command: cmd, mode, decision: modeToLegacyDecision(mode), configured: true };
  if (idx >= 0) {
    items[idx] = { ...items[idx], ...row };
  } else {
    items.push(row);
  }
  shell[shellType] = items;
  snapshot.shell = shell;
}

export function removeLocalShellEntry(snapshot, shellType, command) {
  if (!snapshot || typeof snapshot !== "object") return;
  const shell = snapshot.shell && typeof snapshot.shell === "object" ? snapshot.shell : {};
  const cmd = normalizeShellCommand(command);
  const items = Array.isArray(shell[shellType]) ? shell[shellType] : [];
  shell[shellType] = items.filter((item) => normalizeShellCommand(item?.command) !== cmd);
  snapshot.shell = shell;
}

function modeToLegacyDecision(mode) {
  switch (mode) {
    case "never":
      return "allow_auto";
    case "deny":
      return "deny";
    case "always":
      return "require_approval";
    default:
      return "require_approval";
  }
}
