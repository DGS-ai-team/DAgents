export function truncate(text, max = 120) {
  const s = String(text || "");
  if (s.length <= max) return s;
  return s.slice(0, max) + "…";
}

export function formatUnix(ts) {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleString("zh-CN", { hour12: false });
}

export function agentInitials(agent) {
  const name = String(agent?.name || agent?.agent_id || "?").trim();
  if (!name) return "?";
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  if (name.length >= 2 && /[\u4e00-\u9fff]/.test(name)) {
    return name.slice(0, 2);
  }
  return name.slice(0, 2).toUpperCase();
}

export function computeStats(agents) {
  const list = agents || [];
  return {
    online: list.filter((a) => a.status === "online").length,
    offline: list.filter((a) => a.status === "offline").length,
    total: list.length,
    peers: list.filter((a) => a.expose_to_peers && a.status === "online").length,
  };
}

export function statusPillClass(status) {
  if (status === "online") return "pill-online";
  if (status === "offline") return "pill-offline";
  return "pill-expired";
}

export function taskStatusPillClass(status) {
  const map = {
    queued: "pill-task-queued",
    delivered: "pill-task-delivered",
    processing: "pill-task-processing",
    awaiting_caller: "pill-task-awaiting",
    completed: "pill-task-done",
    failed: "pill-task-failed",
    expired: "pill-expired",
  };
  return map[status] || "pill-muted";
}

export function riskPillClass(level) {
  if (level === "high") return "pill-risk-high";
  if (level === "low") return "pill-risk-low";
  return "pill-risk-medium";
}

export function sortSessions(sessions) {
  return [...(sessions || [])].sort((a, b) => {
    if (Boolean(b.active) !== Boolean(a.active)) return Number(b.active) - Number(a.active);
    if (Boolean(b.has_active_turn) !== Boolean(a.has_active_turn)) {
      return Number(b.has_active_turn) - Number(a.has_active_turn);
    }
    return String(b.updated_at || "").localeCompare(String(a.updated_at || ""));
  });
}

export function touchLastRefreshedLabel() {
  const now = new Date().toLocaleTimeString("zh-CN", { hour12: false });
  return `更新于 ${now}`;
}

export const VIEW_META = {
  registry: {
    title: "Agent 目录",
    subtitle: "已注册 Agent Node 目录与 discovery 分组",
  },
  inbox: {
    title: "A2A Inbox",
    subtitle: "A2A Task 只读观测（不会 deliver）",
  },
};
