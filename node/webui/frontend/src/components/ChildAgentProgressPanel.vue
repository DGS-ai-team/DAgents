<script setup>
import { computed } from "vue";

const props = defineProps({
  items: { type: Array, default: () => [] },
});

const phaseLabels = {
  creating: "创建中",
  queued: "排队中",
  model_generating: "生成中",
  tool_executing: "执行工具",
  tool_completed: "工具已完成",
  waiting_approval: "等待审批",
  completed: "已完成",
  failed: "执行失败",
  cancelled: "已取消",
  expired: "已过期",
  interrupted: "已中断",
};

const activityStatusLabels = {
  running: "执行中",
  executing: "执行中",
  pending: "等待执行",
  queued: "排队中",
  succeeded: "已完成",
  completed: "已完成",
  failed: "失败",
  error: "失败",
  denied: "已拒绝",
  rejected: "已拒绝",
  cancelled: "已取消",
  canceled: "已取消",
  timed_out: "已超时",
  interrupted: "已中断",
};

function normalizeStatus(value) {
  return String(value || "running").trim().toLowerCase() || "running";
}

function activityIsActive(status) {
  return ["running", "executing", "pending", "queued"].includes(normalizeStatus(status));
}

function activityStatusText(status) {
  const normalized = normalizeStatus(status);
  return activityStatusLabels[normalized] || "执行中";
}

const rows = computed(() =>
  (props.items || []).map((entry) => {
    const progress = entry?.progress || {};
    const phase = String(progress.phase || progress.status || "").trim();
    const status = String(progress.status || "active").trim();
    const activities = Array.isArray(progress.recentTools)
      ? progress.recentTools
          .map((activity) => ({
            ...activity,
            toolName: String(activity?.toolName || "").trim(),
            status: normalizeStatus(activity?.status),
            inputSummary: String(activity?.inputSummary || "").trim(),
            outputPreview: String(activity?.outputPreview || "").trim(),
          }))
          .filter((activity) => activity.toolName)
      : [];
    const currentTool = String(progress.currentTool || "").trim();
    const currentToolCallId = String(progress.currentToolCallId || "").trim();
    const activityRows = activities.map((activity, index) => ({
      ...activity,
      active: activityIsActive(activity.status),
      statusText: activityStatusText(activity.status),
    }));
    activityRows.forEach((activity, index) => {
      activity.current =
        activity.active &&
        ((currentToolCallId && activity.toolCallId === currentToolCallId) ||
          (!currentToolCallId && activity.toolName === currentTool && index === activities.length - 1));
    });
    if (!activityRows.some((activity) => activity.current) && (currentTool || activityRows.some((activity) => activity.active))) {
      const fallbackIndex = currentTool
        ? activityRows.findLastIndex((activity) => activity.toolName === currentTool && activity.active)
        : activityRows.findLastIndex((activity) => activity.active);
      if (fallbackIndex >= 0) activityRows[fallbackIndex].current = true;
    }
    if (!activityRows.length && currentTool && ["creating", "active"].includes(status)) {
      activityRows.push({
        toolName: currentTool,
        toolCallId: currentToolCallId,
        status: normalizeStatus(progress.currentToolStatus),
        inputSummary: "",
        outputPreview: "",
        current: true,
        active: true,
        statusText: activityStatusText(progress.currentToolStatus),
      });
    }
    return {
      ...entry,
      progress,
      status,
      phaseText: phaseLabels[phase] || phase || "执行中",
      active: ["creating", "active"].includes(status),
      activityRows,
    };
  }),
);
</script>

<template>
  <section class="child-progress" aria-label="子 Agent 执行过程">
    <div v-for="row in rows" :key="row.childAgentId" class="child-progress__row">
      <div class="child-progress__heading">
        <div class="child-progress__title">执行过程</div>
        <span class="child-progress__phase">{{ row.phaseText }}</span>
      </div>
      <div class="child-progress__purpose">
        <span class="child-progress__indicator" :class="{ 'child-progress__indicator--active': row.active }">
          <span v-if="row.active" class="child-progress__spinner" aria-hidden="true" />
          <span v-else aria-hidden="true">✓</span>
        </span>
        <span class="child-progress__purpose-text" :title="row.purpose || row.childAgentId">
          {{ row.purpose || row.childAgentId }}
        </span>
        <span class="child-progress__status">{{ row.status === "active" ? "运行中" : row.phaseText }}</span>
      </div>

      <ol v-if="row.activityRows.length" class="child-progress__activities">
        <li
          v-for="activity in row.activityRows"
          :key="activity.toolCallId || `${activity.toolName}-${activity.status}-${activity.inputSummary}`"
          class="child-progress__activity"
          :class="{ 'child-progress__activity--current': activity.current }"
        >
          <div class="child-progress__activity-head">
            <span
              class="child-progress__activity-indicator"
              :class="{ 'child-progress__activity-indicator--active': activity.active }"
            >
              <span v-if="activity.active" class="child-progress__spinner" aria-hidden="true" />
              <span v-else aria-hidden="true">✓</span>
            </span>
            <code class="child-progress__tool">{{ activity.toolName }}</code>
            <span v-if="activity.current" class="child-progress__current">当前</span>
            <span class="child-progress__activity-status">{{ activity.statusText }}</span>
          </div>
          <div v-if="activity.inputSummary" class="child-progress__detail">
            <span class="child-progress__detail-label">输入</span>
            <code :title="activity.inputSummary">{{ activity.inputSummary }}</code>
          </div>
          <div v-if="activity.outputPreview" class="child-progress__detail">
            <span class="child-progress__detail-label">输出</span>
            <code :title="activity.outputPreview">{{ activity.outputPreview }}</code>
          </div>
        </li>
      </ol>

      <div v-if="row.progress.summary" class="child-progress__summary">
        {{ row.progress.summary }}
      </div>
      <div v-if="row.progress.error" class="child-progress__error">
        {{ row.progress.error }}
      </div>
    </div>
  </section>
</template>

<style scoped>
.child-progress {
  margin-top: 10px;
  padding: 9px 10px;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  background: var(--color-surface-elevated);
}

.child-progress__row + .child-progress__row {
  margin-top: 9px;
  padding-top: 9px;
  border-top: 1px solid var(--color-border);
}

.child-progress__heading,
.child-progress__purpose,
.child-progress__activity-head,
.child-progress__detail {
  display: flex;
  align-items: center;
  min-width: 0;
}

.child-progress__heading {
  justify-content: space-between;
  gap: 8px;
}

.child-progress__title {
  color: var(--color-text-muted);
  font-size: 11px;
  font-weight: 600;
}

.child-progress__phase,
.child-progress__status,
.child-progress__activity-status {
  color: var(--color-text-subtle);
  font-size: 11px;
  white-space: nowrap;
}

.child-progress__purpose {
  gap: 7px;
  margin-top: 7px;
}

.child-progress__purpose-text {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.child-progress__status {
  margin-left: auto;
}

.child-progress__indicator,
.child-progress__activity-indicator {
  display: inline-flex;
  flex: 0 0 14px;
  width: 14px;
  justify-content: center;
  color: var(--color-success);
  font-size: 11px;
}

.child-progress__indicator--active,
.child-progress__activity-indicator--active {
  color: var(--color-text-muted);
}

.child-progress__spinner {
  width: 10px;
  height: 10px;
  border: 1.5px solid var(--color-border);
  border-top-color: var(--color-text-muted);
  border-radius: 50%;
  animation: child-progress-spin 0.8s linear infinite;
}

.child-progress__activities {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin: 8px 0 0 21px;
  padding: 0;
  list-style: none;
}

.child-progress__activity {
  min-width: 0;
  padding: 5px 7px;
  border: 1px solid transparent;
  border-radius: 5px;
}

.child-progress__activity--current {
  border-color: var(--color-border);
  background: var(--color-surface);
}

.child-progress__activity-head {
  gap: 6px;
  min-height: 17px;
}

.child-progress__tool {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.child-progress__current {
  flex: 0 0 auto;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--color-surface-muted);
  color: var(--color-text-muted);
  font-size: 10px;
}

.child-progress__activity-status {
  margin-left: auto;
}

.child-progress__detail {
  gap: 6px;
  margin: 3px 0 0 20px;
  min-width: 0;
  color: var(--color-text-muted);
  font-size: 11px;
}

.child-progress__detail-label {
  flex: 0 0 auto;
  color: var(--color-text-subtle);
}

.child-progress__detail code {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.child-progress__summary,
.child-progress__error {
  margin: 6px 0 0 21px;
  overflow: hidden;
  font-size: 11px;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.child-progress__summary {
  color: var(--color-text-muted);
}

.child-progress__error {
  color: var(--color-danger, #c45b5b);
}

@keyframes child-progress-spin {
  to { transform: rotate(360deg); }
}
</style>
