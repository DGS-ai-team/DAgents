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
};

const rows = computed(() =>
  (props.items || []).map((entry) => {
    const progress = entry?.progress || {};
    const phase = String(progress.phase || progress.status || "").trim();
    const status = String(progress.status || "active").trim();
    const turnCount = Number(progress.turnCount) || 0;
    const maxTurns = Number(progress.maxTurns) || 0;
    return {
      ...entry,
      progress,
      status,
      phaseText: phaseLabels[phase] || phase || "执行中",
      turnText: maxTurns > 0 ? `第 ${turnCount}/${maxTurns} 轮` : turnCount > 0 ? `第 ${turnCount} 轮` : "",
      active: ["creating", "active"].includes(status),
    };
  }),
);
</script>

<template>
  <section class="child-progress" aria-label="子 Agent 进度">
    <div class="child-progress__title">子 Agent 进度</div>
    <div v-for="row in rows" :key="row.childAgentId" class="child-progress__row">
      <div class="child-progress__row-head">
        <span class="child-progress__indicator" :class="{ 'child-progress__indicator--active': row.active }">
          <span v-if="row.active" class="child-progress__spinner" aria-hidden="true" />
          <span v-else aria-hidden="true">✓</span>
        </span>
        <span class="child-progress__purpose">{{ row.purpose || row.childAgentId }}</span>
        <span class="child-progress__phase">{{ row.phaseText }}</span>
      </div>
      <div class="child-progress__meta">
        <span v-if="row.turnText">{{ row.turnText }}</span>
        <code v-if="row.progress.currentTool">{{ row.progress.currentTool }}</code>
        <span v-if="row.progress.currentToolStatus">{{ row.progress.currentToolStatus }}</span>
      </div>
      <div v-if="row.progress.lastOutputPreview" class="child-progress__preview">
        {{ row.progress.lastOutputPreview }}
      </div>
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

.child-progress__title {
  margin-bottom: 7px;
  color: var(--color-text-muted);
  font-size: 11px;
  font-weight: 600;
}

.child-progress__row + .child-progress__row {
  margin-top: 9px;
  padding-top: 9px;
  border-top: 1px solid var(--color-border);
}

.child-progress__row-head,
.child-progress__meta {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 7px;
}

.child-progress__indicator {
  display: inline-flex;
  width: 14px;
  flex: 0 0 14px;
  justify-content: center;
  color: var(--color-success);
  font-size: 11px;
}

.child-progress__indicator--active {
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

.child-progress__purpose {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.child-progress__phase,
.child-progress__meta {
  color: var(--color-text-subtle);
  font-size: 11px;
}

.child-progress__phase {
  margin-left: auto;
  flex: 0 0 auto;
}

.child-progress__meta {
  margin: 4px 0 0 21px;
}

.child-progress__meta code {
  max-width: 55%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.child-progress__preview,
.child-progress__summary,
.child-progress__error {
  margin: 5px 0 0 21px;
  overflow: hidden;
  color: var(--color-text-muted);
  font-size: 11px;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.child-progress__error {
  color: var(--color-danger, #c45b5b);
}

@keyframes child-progress-spin {
  to { transform: rotate(360deg); }
}
</style>
