export const HELP_TEXT = `命令:
/help /status /sessions /switch <id> /new /clear /context /skill /children
/policy /triggers /compress /reasoning on|off /thinking on|off|effort high|max
/cancel 或点「取消」中断在途 turn`;

export async function runSlashCommand(cmd, ctx) {
  const parts = cmd.trim().split(/\s+/);
  const name = (parts[0] || "").toLowerCase();
  switch (name) {
    case "/help":
    case "/h":
    case "/?":
      return { system: HELP_TEXT };
    case "/status":
      return { panel: "status" };
    case "/sessions":
    case "/ls":
      return { panel: "sessions" };
    case "/context":
      return { panel: "context" };
    case "/skill":
      return { panel: "skills", arg: parts.slice(1).join(" ") };
    case "/children":
    case "/child":
      return { panel: "children" };
    case "/policy":
      return { panel: "policy" };
    case "/triggers":
    case "/trigger":
      return { panel: "triggers" };
    case "/compress":
      return { action: "compress" };
    case "/clear":
      return { action: "clear" };
    case "/new":
      return { action: "new" };
    case "/switch":
      return { action: "switch", arg: parts[1] || "" };
    case "/reasoning":
      return { action: "reasoning", arg: parts[1] || "" };
    case "/thinking":
      return { action: "thinking", arg: parts.slice(1).join(" ") };
    case "/cancel":
      return { action: "cancel" };
    case "/tools":
      if (parts[1] === "verbose") return { action: "tools_verbose", on: true };
      if (parts[1] === "brief") return { action: "tools_verbose", on: false };
      return { system: `tool 输出: ${ctx.toolFoldVerbose ? "详细" : "折叠"}` };
    default:
      return { error: `未知命令: ${cmd}` };
  }
}
