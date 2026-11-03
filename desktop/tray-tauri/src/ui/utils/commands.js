export function runSlashCommand(cmd) {
  const name = String(cmd || "").trim().split(/\s+/)[0].toLowerCase();
  if (name === "/clear") return { action: "clear" };
  if (name === "/compress") return { action: "compress" };
  return { error: `未知命令: ${cmd}` };
}
