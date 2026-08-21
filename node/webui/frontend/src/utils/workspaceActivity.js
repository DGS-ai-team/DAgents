/**
 * 从 transcript 条目推导 workspace activity（与后端 activity.DeriveFromMessages 对齐）。
 */
import { resolveToolArgumentsFromData } from "./toolCalls.js";

function sanitize(s) {
  return String(s || "").replace(/\s+/g, " ").trim();
}

function truncate(s, max) {
  const t = sanitize(s);
  if (t.length <= max) return t;
  return `${t.slice(0, max)}…`;
}

function looksRejected(content) {
  const c = String(content || "").toLowerCase();
  return c.includes("rejected") || c.includes("approval denied") || c.startsWith("error: tool rejected");
}

function looksError(content) {
  const c = String(content || "").trim();
  if (c.startsWith("ERROR:") || c.startsWith("error:")) return true;
  const lower = c.toLowerCase();
  return lower.includes("exit code") && (lower.includes("exit code 1") || lower.includes("non-zero"));
}

/**
 * @param {Array} entries transcriptStore.entries
 */
export function deriveActivityFromTranscript(entries) {
  const files = new Map();
  const fileOrder = [];
  const commands = [];

  const list = Array.isArray(entries) ? entries : [];
  const callById = new Map();

  for (const e of list) {
    if (e?.kind === "tool_call") {
      const id = String(e.blockId || e.data?.tool_call_id || e.data?.id || "").trim();
      if (id) callById.set(id, e);
    }
  }

  for (const e of list) {
    if (e?.kind !== "tool_result") continue;
    const data = e.data || {};
    const id = String(data.tool_call_id || e.blockId || "").trim();
    const call = id ? callById.get(id) : null;
    const name = String(data.tool_name || data.name || call?.data?.tool_name || "").trim();
    const args = resolveToolArgumentsFromData(data) || resolveToolArgumentsFromData(call?.data || {}) || {};
    const content = String(data.content || e.text || "");
    const resultStatus = String(data.status || "").trim().toLowerCase();
    const rejected = ["denied", "rejected"].includes(resultStatus) || (!resultStatus && (!!data.rejected || looksRejected(content)));
    const failed = ["failed", "error", "timed_out", "unknown"].includes(resultStatus) || (!resultStatus && looksError(content));

    if (name === "write_file" || name === "search_replace") {
      const path = sanitize(args.path || args.file_path);
      if (!path) continue;
      const op = name === "search_replace" ? "replace" : "write";
      let rec = files.get(path);
      if (!rec) {
        rec = { path, ops: [], last_tool_call_id: id, last_tool_name: name, rejected, failed, preview: truncate(content, 120) };
        files.set(path, rec);
        fileOrder.push(path);
      }
      if (!rec.ops.includes(op)) rec.ops.push(op);
      rec.last_tool_call_id = id;
      rec.last_tool_name = name;
      rec.rejected = rejected;
      rec.failed = failed;
      rec.preview = truncate(content, 120);
    }

    if (name === "bash_run") {
      const command = sanitize(args.command);
      if (!command) continue;
      let status = "ok";
      if (rejected) status = "rejected";
      else if (failed) status = "error";
      else if (["cancelled", "canceled"].includes(resultStatus)) status = "cancelled";
      else if (["running", "queued"].includes(resultStatus)) status = "running";
      else if (!resultStatus && looksError(content)) status = "error";
      commands.push({
        command,
        tool_call_id: id,
        rejected,
        status,
        content_preview: truncate(content, 160),
      });
    }
  }

  commands.reverse();
  return {
    files: fileOrder.map((p) => files.get(p)),
    commands,
    file_count: fileOrder.length,
    command_count: commands.length,
  };
}
