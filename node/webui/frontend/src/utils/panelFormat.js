/** 四档 mode 中文标签。 */
export function formatPolicyMode(mode) {
  switch (String(mode || "").trim()) {
    case "never":
      return "自动允许";
    case "always":
      return "需审批";
    case "rule":
      return "特殊规则";
    case "deny":
      return "禁止";
    default:
      return mode || "—";
  }
}

export function policyModeClass(mode) {
  switch (String(mode || "").trim()) {
    case "never":
      return "decision-pill--allow";
    case "always":
      return "decision-pill--approval";
    case "rule":
      return "decision-pill--rule";
    case "deny":
      return "decision-pill--deny";
    default:
      return "";
  }
}

/** @deprecated 使用 formatPolicyMode */
export function formatPolicyDecision(decision) {
  switch (String(decision || "").trim()) {
    case "allow_auto":
      return "自动允许";
    case "deny":
      return "禁止";
    case "require_approval":
      return "需审批";
    default:
      return decision || "—";
  }
}

/** @deprecated 使用 policyModeClass */
export function policyDecisionClass(decision) {
  switch (String(decision || "").trim()) {
    case "allow_auto":
      return "decision-pill--allow";
    case "require_approval":
      return "decision-pill--approval";
    case "deny":
      return "decision-pill--deny";
    default:
      return "";
  }
}

function intFromAny(v) {
  const n = Number(v);
  return Number.isFinite(n) ? Math.floor(n) : 0;
}

function floatFromAny(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/** 对齐 TUI formatTriggerCondition。 */
export function formatTriggerCondition(condition) {
  if (!condition || typeof condition !== "object") return "manual";
  const interval = intFromAny(condition.interval_seconds);
  if (interval > 0) return `interval ${interval}s`;
  const fireAt = floatFromAny(condition.fire_at);
  if (fireAt > 0) return `once @ ${formatUnixTime(fireAt)}`;
  const sched = condition.schedule;
  if (sched && typeof sched === "object" && Object.keys(sched).length > 0) {
    const kind = String(sched.kind || "calendar").trim() || "calendar";
    return `schedule:${kind}`;
  }
  const cmd = String(condition.cmd || "").trim();
  if (cmd) return `cmd gate: ${truncateText(cmd, 32)}`;
  return "manual";
}

export function formatUnixTime(ts) {
  const n = floatFromAny(ts);
  if (n <= 0) return "—";
  const ms = n * 1000;
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "—";
  const pad = (x) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function truncateText(text, max = 72) {
  const s = String(text || "");
  if (s.length <= max) return s;
  return `${s.slice(0, max - 1)}…`;
}

export function shortId(id, max = 24) {
  const s = String(id || "").trim();
  if (!s) return "—";
  return s.length <= max ? s : `${s.slice(0, max)}…`;
}
