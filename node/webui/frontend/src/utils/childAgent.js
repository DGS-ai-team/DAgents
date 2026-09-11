const TERMINAL_STATUSES = new Set(["completed", "failed", "cancelled", "expired", "interrupted"]);

export function isChildAgentActive(status) {
  const s = String(status || "").trim().toLowerCase();
  return s && !TERMINAL_STATUSES.has(s);
}

export function formatChildAgentStatus(status) {
  switch (String(status || "").trim().toLowerCase()) {
    case "creating":
      return "启动中";
    case "active":
      return "运行中";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "cancelled":
      return "已取消";
    case "expired":
      return "已过期";
    case "interrupted":
      return "已中断";
    default:
      return status || "未知";
  }
}

export function childAgentItems(data) {
  const rows = data?.items;
  return Array.isArray(rows) ? rows : [];
}

export function sortChildAgentItems(items) {
  return [...items].sort((a, b) => {
    const aActive = isChildAgentActive(a.status);
    const bActive = isChildAgentActive(b.status);
    if (aActive && !bActive) return -1;
    if (!aActive && bActive) return 1;
    return String(b.created_at || "").localeCompare(String(a.created_at || ""));
  });
}
