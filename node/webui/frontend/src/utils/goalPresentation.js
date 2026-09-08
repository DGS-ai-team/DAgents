export const GOAL_TEMPLATES = Object.freeze({
  observe: { label: "持续观察", title: "持续观察并汇报", objective: "持续观察指定对象的变化，按批次记录重要进展。", acceptance: "每次变化都有时间、证据和下一步。" },
  batch: { label: "分批完成", title: "分批完成一组工作", objective: "把工作拆成可验证的小批次，逐批推进并保存产物。", acceptance: "所有批次都有可检查产物，未完成批次列出原因。" },
  followup: { label: "条件跟进", title: "满足条件后跟进", objective: "等待外部条件满足后继续推进目标。", acceptance: "条件满足后完成后续动作并记录证据。" },
});

export function goalStatusReason(goal) {
  return String(goal?.status_reason || goal?.reason || goal?.last_checkpoint?.external_condition || "").trim();
}

export function goalBudgetRemaining(goal) {
  return Math.max(0, Number(goal?.token_budget || 0) - Number(goal?.tokens_used || 0));
}
export const GOAL_REASON_LABELS = Object.freeze({ no_progress: "连续两轮没有新进展", usage_unknown: "用量未知，已暂停保护预算", budget_exhausted: "预算已用尽", run_limit_exhausted: "运行次数已用尽", approval_required: "等待审批", assistant_completed: "上一轮已完成", manual: "手动运行", schedule: "定时唤醒", expired: "已超过截止时间", restart: "节点重启后需要核对" });
export function goalReasonLabel(goal) {
  const status = String(goal?.status || "").trim();
  if (status === "completed" || status === "stopped") return "";
  const reason = goalStatusReason(goal);
  return GOAL_REASON_LABELS[reason] || reason;
}
export function goalStatusLabel(goal) {
  const status = String(goal?.status || "").trim();
  if (status === "waiting" && goalStatusReason(goal) === "approval_required") return "等待审批";
  return ({ active: "已启用", paused: "已暂停", waiting: "等待下次唤醒", stopped: "已停止", completed: "已完成", failed: "失败" }[status] || status);
}
export function goalNextWakeLabel(goal) {
  if (["completed", "stopped"].includes(String(goal?.status || "").trim())) return "不会再运行";
  return formatGoalDate(goal?.next_wake_at);
}
export function goalHasActiveRun(goal) { return Boolean(goal?.active_run || goal?.current_run || goal?.run_status === "running"); }
export function formatGoalDate(value) { if (!value) return "未安排"; const d = new Date(value); return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString(); }
