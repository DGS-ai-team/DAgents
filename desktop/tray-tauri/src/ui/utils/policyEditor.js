/** Policy 四档 mode（对齐 txt：never / always / rule / deny）。 */
export const POLICY_MODES = [
  { value: "never", label: "自动允许" },
  { value: "always", label: "需审批" },
  { value: "rule", label: "特殊规则" },
  { value: "deny", label: "禁止" },
];

export const PROTECTED_POLICY_TOOL = "ask_user_information";

export function entryMode(row) {
  return String(row?.mode || "rule").trim() || "rule";
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

/** Build one atomic update list for the tools currently visible in the UI. */
export function buildBulkToolUpdates(tools, mode) {
  const rows = Array.isArray(tools) ? tools : [];
  return rows
    .map((row) => String(row?.name || "").trim())
    .filter((name) => name && canSetPolicyMode(name, mode))
    .map((name) => ({ name, mode }));
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
  const row = { name, mode, configured: true };
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
  const row = { command: cmd, mode, configured: true };
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
