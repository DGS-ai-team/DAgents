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

function intFromAny(v) {
  const n = Number(v);
  return Number.isFinite(n) ? Math.floor(n) : 0;
}

function floatFromAny(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

const WEEKDAY_LABELS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

function formatClock(hour, minute) {
  const pad = (x) => String(x).padStart(2, "0");
  return `${pad(hour)}:${pad(minute)}`;
}

/** 对齐 TUI formatTriggerCondition（产品 UI 中文摘要）。 */
export function formatTriggerCondition(condition) {
  if (!condition || typeof condition !== "object") return "手动";
  const interval = intFromAny(condition.interval_seconds);
  if (interval > 0) return `每 ${interval} 秒`;
  const fireAt = floatFromAny(condition.fire_at);
  if (fireAt > 0) return `单次 · ${formatUnixTime(fireAt)}`;
  const sched = condition.schedule;
  if (sched && typeof sched === "object" && Object.keys(sched).length > 0) {
    const kind = String(sched.kind || "daily").trim().toLowerCase();
    const hour = intFromAny(sched.hour);
    const minute = intFromAny(sched.minute);
    const clock = formatClock(hour, minute);
    if (kind === "daily") return `每天 ${clock}`;
    if (kind === "weekly") {
      const wd = intFromAny(sched.weekday);
      const label = WEEKDAY_LABELS[wd] || `周${wd}`;
      return `每${label} ${clock}`;
    }
    if (kind === "monthly") {
      const day = intFromAny(sched.day);
      const dayLabel = day < 0 ? `倒数第 ${-day} 天` : `${day} 日`;
      return `每月 ${dayLabel} ${clock}`;
    }
    return `日历 ${kind} ${clock}`;
  }
  const cmd = String(condition.cmd || "").trim();
  if (cmd) return `门控脚本 · ${truncateText(cmd, 32)}`;
  return "手动";
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
