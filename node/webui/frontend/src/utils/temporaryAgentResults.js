const TEMPORARY_AGENT_TOOLS = new Set([
  "create_temporary_agent",
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

function formatCreateResult(payload) {
  if (String(payload.kind || "").trim() !== "result") return null;
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

/** 从工具结果中提取 child_agent_id，供进度卡片在结果到达后继续关联。 */
export function childAgentIdsFromResult(toolName, content) {
  if (!TEMPORARY_AGENT_TOOLS.has(String(toolName || "").trim())) return [];
  let payload;
  try {
    payload = JSON.parse(String(content || "").trim());
  } catch {
    return [];
  }
  const ids = [];
  const add = (value) => {
    const id = String(value || "").trim();
    if (id && !ids.includes(id)) ids.push(id);
  };
  const collect = (item) => {
    if (!item || typeof item !== "object") return;
    add(item.child_agent_id);
    if (Array.isArray(item.results)) item.results.forEach(collect);
  };
  if (Array.isArray(payload)) payload.forEach(collect);
  else collect(payload);
  return ids;
}

export function isTemporaryAgentTool(name) {
  return TEMPORARY_AGENT_TOOLS.has(String(name || "").trim());
}
