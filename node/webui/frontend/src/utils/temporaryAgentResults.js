const TEMPORARY_AGENT_TOOLS = new Set([
  "create_temporary_agent",
  "wait_temporary_agents",
  "temporary_agent_status",
  "cancel_temporary_agent",
]);

function shortChildId(id) {
  const s = String(id || "").trim();
  if (!s) return "";
  return s.length <= 16 ? s : `${s.slice(0, 16)}…`;
}

function intVal(v) {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

function stringList(v) {
  if (!Array.isArray(v)) return [];
  return v.map((x) => String(x || "").trim()).filter(Boolean);
}

function truncateText(text, max = 72) {
  const s = String(text || "").trim();
  if (!s) return "";
  return s.length <= max ? s : `${s.slice(0, max - 1)}…`;
}

function firstNonEmptyLine(text) {
  for (const line of String(text || "").split("\n")) {
    const t = line.trim();
    if (t) return t;
  }
  return String(text || "").trim();
}

function normalizeResult(item) {
  if (!item || typeof item !== "object") return null;
  const artifacts = Array.isArray(item.artifacts)
    ? item.artifacts.map((x) => String(x || "").trim()).filter(Boolean)
    : [];
  return {
    child_agent_id: String(item.child_agent_id || "").trim(),
    status: String(item.status || "").trim(),
    summary: String(item.summary || "").trim(),
    error: String(item.error || "").trim(),
    turn_count: intVal(item.turn_count),
    artifacts,
  };
}

function resultHint(item) {
  if (item.error) return truncateText(item.error);
  if (item.summary) return truncateText(firstNonEmptyLine(item.summary));
  return "";
}

function formatBatchResult(toolName, results, timedOut = false) {
  const parsed = results.map(normalizeResult).filter(Boolean);
  const total = parsed.length;
  if (!total) return { summary: `✓ ${toolName} · 无结果`, detail: "" };
  let completed = 0;
  let failed = 0;
  for (const item of parsed) {
    if (item.status === "completed") completed += 1;
    else if (["failed", "cancelled", "expired"].includes(item.status)) failed += 1;
  }
  let summary = `✓ ${toolName} · ${completed + failed}/${total} 已结束`;
  if (completed > 0) {
    summary += `（${completed} 成功`;
    if (failed > 0) summary += `，${failed} 异常`;
    summary += "）";
  } else if (failed > 0) {
    summary += `（${failed} 异常）`;
  }
  if (timedOut) summary += " · 超时";
  const lines = parsed.map((item, index) => {
    const short = shortChildId(item.child_agent_id);
    const status = item.status || "unknown";
    const prefix = `[${index + 1}] ${short} · ${status}`;
    const hint = resultHint(item);
    return hint ? `${prefix} · ${hint}` : prefix;
  });
  return { summary, detail: lines.join("\n") };
}

function formatCreateResult(payload) {
  const kind = String(payload.kind || "").trim();
  if (kind === "result") {
    const item = normalizeResult(payload);
    const short = shortChildId(item.child_agent_id);
    const status = item.status || "unknown";
    const summary = short ? `✓ 临时 Agent 完成 · ${short} · ${status}` : `✓ 临时 Agent 完成 · ${status}`;
    const detailParts = [];
    if (item.error) detailParts.push(`error: ${item.error}`);
    if (item.summary) detailParts.push(item.summary);
    if (item.artifacts.length) detailParts.push(`artifacts: ${item.artifacts.join(", ")}`);
    if (item.turn_count > 0) detailParts.push(`turn_count=${item.turn_count}`);
    return { summary, detail: detailParts.join("\n") };
  }
  const parts = ["✓ 已创建临时 Agent"];
  const short = shortChildId(payload.child_agent_id);
  const purpose = String(payload.purpose || "").trim();
  if (short) parts.push(short);
  if (purpose) parts.push(purpose);
  const maxTurns = intVal(payload.max_turns);
  if (maxTurns > 0) parts.push(`max_turns=${maxTurns}`);
  const skills = stringList(payload.loaded_skills);
  if (skills.length) parts.push(`skills=${skills.join(",")}`);
  return { summary: parts.join(" · "), detail: "" };
}

/** 对齐 Python parse_temporary_agent_tool_result。 */
export function parseTemporaryAgentToolResult(toolName, content) {
  const name = String(toolName || "").trim();
  if (!TEMPORARY_AGENT_TOOLS.has(name)) return null;
  const text = String(content || "").trim();
  if (!text) return null;
  if (text.startsWith("ERROR:")) return { summary: text, detail: text };
  let payload;
  try {
    payload = JSON.parse(text);
  } catch {
    return null;
  }
  if (name === "create_temporary_agent" && payload && typeof payload === "object") {
    return formatCreateResult(payload);
  }
  if (name === "wait_temporary_agents" && payload && typeof payload === "object") {
    const results = payload.results;
    if (!Array.isArray(results)) return { summary: "✓ wait_temporary_agents · 无结果", detail: "" };
    return formatBatchResult("wait_temporary_agents", results, !!payload.timed_out);
  }
  if (name === "temporary_agent_status") {
    if (Array.isArray(payload)) return formatBatchResult("temporary_agent_status", payload, false);
    if (payload && typeof payload === "object" && Array.isArray(payload.results)) {
      return formatBatchResult("temporary_agent_status", payload.results, false);
    }
  }
  if (name === "cancel_temporary_agent" && payload && typeof payload === "object") {
    const short = shortChildId(payload.child_agent_id);
    const status = String(payload.status || "cancelled").trim() || "cancelled";
    let summary = "✓ 已取消临时 Agent";
    if (short) summary += ` · ${short}`;
    summary += ` · ${status}`;
    return { summary, detail: "" };
  }
  return null;
}

export function isTemporaryAgentTool(name) {
  return TEMPORARY_AGENT_TOOLS.has(String(name || "").trim());
}
