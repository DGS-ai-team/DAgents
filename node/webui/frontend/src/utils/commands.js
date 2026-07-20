export async function runSlashCommand(cmd, ctx) {
  const parts = cmd.trim().split(/\s+/);
  const name = (parts[0] || "").toLowerCase();
  switch (name) {
    case "/help":
    case "/h":
    case "/?":
      return { panel: "help" };
    case "/status":
      return { panel: "status" };
    case "/version":
      return { panel: "update" };
    case "/update":
      return { panel: "update" };
    case "/agents":
    case "/sessions":
    case "/ls":
      return { panel: "agents" };
    case "/activity":
    case "/changes":
      return { panel: "activity" };
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
    case "/upload": {
      const parsed = parseUploadArgs(parts.slice(1));
      if (parsed.error) return { error: parsed.error };
      return { action: "upload", upload: parsed };
    }
    case "/tools":
      if (parts[1] === "verbose") return { action: "tools_verbose", on: true };
      if (parts[1] === "brief") return { action: "tools_verbose", on: false };
      return { system: `tool 输出: ${ctx.toolFoldVerbose ? "详细" : "折叠"}` };
    default:
      return { error: `未知命令: ${cmd}` };
  }
}

function parseUploadArgs(args) {
  if (args.length < 4) {
    return { error: "用法: /upload <skill|externaltool|plugin> PATH ID VERSION [NAME] [--platform X] [--publish]" };
  }
  const kind = (args[0] || "").toLowerCase();
  const path = args[1];
  const id = args[2];
  const version = args[3];
  let name = id;
  let platform = "";
  let publish = false;
  for (let i = 4; i < args.length; i += 1) {
    const a = args[i];
    if (a === "--publish") {
      publish = true;
    } else if (a === "--platform") {
      platform = args[i + 1] || "";
      i += 1;
    } else if (name === id) {
      name = a;
    } else {
      name += ` ${a}`;
    }
  }
  return { kind, path, id, version, name, platform, publish };
}
