export async function runSlashCommand(cmd) {
  const parts = cmd.trim().split(/\s+/);
  const name = (parts[0] || "").toLowerCase();
  switch (name) {
    case "/clear":
      return { action: "clear" };
    case "/compress":
      return { action: "compress" };
    case "/thinking":
      return { action: "thinking", arg: parts.slice(1).join(" ") };
    case "/upload": {
      const parsed = parseUploadArgs(parts.slice(1));
      if (parsed.error) return { error: parsed.error };
      return { action: "upload", upload: parsed };
    }
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
