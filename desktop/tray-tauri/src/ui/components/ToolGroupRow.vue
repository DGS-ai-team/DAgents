<script setup>
import { computed, ref } from "vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import { resolveToolVisual } from "../utils/toolSource.js";
import { resolveToolStepPhase, toolStepPurposeOrToolLabel } from "../utils/toolUserLabel.js";
import ToolGroupIcon from "./ToolGroupIcon.vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  steps: { type: Array, default: () => [] },
  verbose: { type: Boolean, default: false },
});

const expanded = ref(false);
const everExpanded = ref(false);

const phaseCounts = computed(() => {
  const counts = {
    generating: 0,
    running: 0,
    background: 0,
    pending: 0,
    completed: 0,
    failed: 0,
    cancelled: 0,
    rejected: 0,
    interrupted: 0,
    idle: 0,
  };
  for (const step of props.steps) {
    const phase = resolveToolStepPhase(step);
    counts[phase] = (counts[phase] || 0) + 1;
  }
  return counts;
});
const selectedStep = computed(() => {
  const priority = props.steps.filter((step) => ["generating", "running"].includes(resolveToolStepPhase(step)));
  if (priority.length) return priority[priority.length - 1];
  const pending = props.steps.filter((step) => resolveToolStepPhase(step) === "pending");
  if (pending.length) return pending[pending.length - 1];
  const active = props.steps.filter((step) => resolveToolStepPhase(step) === "background");
  return active[active.length - 1] || props.steps[props.steps.length - 1] || null;
});
const selectedVisual = computed(() => {
  const step = selectedStep.value;
  return resolveToolVisual(step?.callEntry || step?.resultEntry || step || {});
});
const selectedLabel = computed(() => toolStepPurposeOrToolLabel(selectedStep.value || {}));
const allCompleted = computed(() => props.steps.length > 0 && phaseCounts.value.completed === props.steps.length);
const titleLabel = computed(() => allCompleted.value ? `已完成 ${props.steps.length} 项操作` : selectedLabel.value);
const statusParts = computed(() => {
  const counts = phaseCounts.value;
  const parts = [];
  const activeCount = counts.generating + counts.running;
  if (activeCount) parts.push(`${activeCount} 项执行中`);
  if (counts.background) parts.push(`${counts.background} 项后台执行中`);
  if (counts.pending) parts.push(`${counts.pending} 项待批准`);
  if (counts.completed && !allCompleted.value) parts.push(`${counts.completed} 项完成`);
  if (counts.failed) parts.push(`${counts.failed} 项失败`);
  if (counts.cancelled) parts.push(`${counts.cancelled} 项取消`);
  if (counts.rejected) parts.push(`${counts.rejected} 项未批准`);
  if (counts.interrupted) parts.push(`${counts.interrupted} 项中断`);
  if (counts.idle) parts.push(`${counts.idle} 项状态未知`);
  return parts;
});
const statusText = computed(() => {
  const counts = phaseCounts.value;
  const foreground = counts.generating + counts.running;
  if (foreground) return foreground > 1 ? `${foreground}项执行中` : "执行中";
  if (counts.pending) return counts.pending > 1 ? `${counts.pending}项待确认` : "待确认";
  if (counts.background) return counts.background > 1 ? `${counts.background}项后台执行中` : "后台执行中";
  if (counts.failed) return `${counts.failed}项失败`;
  if (counts.completed && counts.rejected) return `${counts.completed}完成·${counts.rejected}未批准`;
  if (counts.rejected) return `${counts.rejected}项拒绝`;
  if (counts.cancelled) return `${counts.cancelled}项取消`;
  if (counts.interrupted) return `${counts.interrupted}项中断`;
  if (counts.idle) return `${counts.idle}项状态未知`;
  return "";
});
const statusDetailText = computed(() => statusParts.value.join(" · ") || (allCompleted.value ? "全部完成" : ""));
const headLabel = computed(() => `${titleLabel.value}，${statusDetailText.value ? `状态明细：${statusDetailText.value}，` : ""}${expanded.value ? "收起" : "展开"}详情`);

function toggle() {
  expanded.value = !expanded.value;
  if (expanded.value) everExpanded.value = true;
}
</script>

<template>
  <div
    class="tool-group-row"
    :class="[`tool-group-row--${selectedVisual.kind}`, { 'tool-group-row--expanded': expanded }]"
  >
    <button
      type="button"
      class="tool-group-row__head"
      :aria-expanded="expanded"
      :aria-label="headLabel"
      @click="toggle"
    >
      <span class="tool-group-row__visual" :title="selectedVisual.label">
        <ToolGroupIcon :name="selectedVisual.kind" />
      </span>
      <span class="tool-group-row__title" :title="titleLabel">{{ titleLabel }}</span>
      <span v-if="statusText" class="tool-group-row__status" :title="statusDetailText">{{ statusText }}</span>
      <UiIcon class="tool-group-row__chevron" :name="expanded ? 'chevron-down' : 'chevron-right'" :size="14" />
    </button>

    <div v-if="everExpanded" v-show="expanded" class="tool-group-row__steps">
      <ToolSummaryRow
        v-for="step in steps"
        :key="step.key"
        :call-entry="step.callEntry"
        :result-entry="step.resultEntry"
        :execution-hint="step.executionHint"
        :lifecycle-status="step.lifecycleStatus"
        :verbose="verbose"
        embedded
      />
    </div>
  </div>
</template>

<style scoped>
.tool-group-row {
  width: 100%;
  min-width: 0;
  flex: 0 0 auto;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface);
}

.tool-group-row__head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-width: 0;
  min-height: 36px;
  padding: 6px 10px;
  border: 0;
  background: transparent;
  color: var(--color-text);
  font: inherit;
  text-align: left;
}

.tool-group-row__head:hover { background: var(--color-surface-hover); }
.tool-group-row__head:focus-visible { outline: 2px solid var(--color-focus, var(--color-primary)); outline-offset: -2px; }

.tool-group-row__visual {
  display: inline-flex;
  flex: 0 0 20px;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
}

.tool-group-row__visual :deep(.tool-group-icon) { width: 20px; height: 20px; }
.tool-group-row__title { flex: 1 1 auto; min-width: 0; overflow: hidden; color: var(--color-text); font-size: 12.5px; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.tool-group-row__status { flex: 0 0 110px; width: 110px; overflow: hidden; color: var(--color-text-subtle); font-size: 11px; line-height: 1.3; text-align: right; text-overflow: ellipsis; white-space: nowrap; }
.tool-group-row__chevron { flex: 0 0 auto; color: var(--color-text-subtle); }
.tool-group-row__steps { min-width: 0; padding: 0 10px 6px; border-top: 1px solid var(--color-border); }
.tool-group-row__steps :deep(.tool-summary-row__head) { min-height: 34px; }
</style>
