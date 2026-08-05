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
    caller_notified: "pill-task-caller-notified",
    caller_responded: "pill-task-caller-responded",
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

export function touchLastRefreshedLabel() {
  const now = new Date().toLocaleTimeString("zh-CN", { hour12: false });
  return `更新于 ${now}`;
}

export const VIEW_META = {
  home: {
    title: "首页",
    subtitle: "运行概览与模块入口",
  },
  workgroup: {
    title: "工作组",
    subtitle: "创建与管理协作组，修改设置或与 Supervisor 对话",
  },
  templates: {
    title: "Agent 模板",
    subtitle: "可复用的 Agent 蓝图；工作组新增成员时可快速选用",
  },
  marketplace: {
    title: "能力市场",
    subtitle: "上传与分发 Skills、Hooks、External Tools 等 Node 扩展能力",
  },
  nodes: {
    title: "Node 列表",
    subtitle: "已注册 Agent Node、在线状态与 discovery 分组",
  },
  permissions: {
    title: "权限管理",
    subtitle: "管理员会话、工作组 ACL 与发现组可见性（建设中）",
  },
  settings: {
    title: "配置",
    subtitle: "LLM 配置、版本发布与系统偏好",
  },
  cases: {
    title: "案例库",
    subtitle: "演示会话 JSONL、关联 Skills / Plugins / External Tools",
  },
};
