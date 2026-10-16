<script setup>
import { computed, ref } from "vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import BrandActivityIndicator from "./BrandActivityIndicator.vue";
import { resolveToolGroupVisual } from "../utils/toolSource.js";
import { resolveToolStepPhase } from "../utils/toolUserLabel.js";
import ToolGroupIcon from "./ToolGroupIcon.vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  steps: { type: Array, default: () => [] },
  verbose: { type: Boolean, default: false },
});

const expanded = ref(false);

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
const activeSteps = computed(() =>
  props.steps.filter((step) => ["generating", "running", "background"].includes(resolveToolStepPhase(step))),
);
const hasActive = computed(() => activeSteps.value.length > 0);
const hasPending = computed(() => phaseCounts.value.pending > 0);
const visual = computed(() => {
  return resolveToolGroupVisual(props.steps);
});
const statusParts = computed(() => {
  const counts = phaseCounts.value;
  const parts = [];
  const activeCount = counts.generating + counts.running;
  if (activeCount) parts.push(`${activeCount} 项执行中`);
  if (counts.background) parts.push(`${counts.background} 项后台执行中`);
  if (counts.pending) parts.push(`${counts.pending} 项待批准`);
  if (counts.completed) {
    if (props.steps.length === counts.completed) return [`${props.steps.length} 项已完成`];
    parts.push(`${counts.completed} 项成功`);
  }
  if (counts.failed) parts.push(`${counts.failed} 项失败`);
  if (counts.cancelled) parts.push(`${counts.cancelled} 项取消`);
  if (counts.rejected) parts.push(`${counts.rejected} 项拒绝`);
  if (counts.interrupted) parts.push(`${counts.interrupted} 项中断`);
  if (counts.idle) parts.push(`${counts.idle} 项状态未知`);
  return parts;
});
const statusText = computed(() => {
  return statusParts.value.join(" · ");
});
const statusIcon = computed(() => {
  if (hasActive.value) return "";
  if (hasPending.value) return "pending";
  if (phaseCounts.value.failed || phaseCounts.value.rejected) return "close";
  if (phaseCounts.value.cancelled || phaseCounts.value.interrupted) return "close";
  if (props.steps.length > 0 && phaseCounts.value.completed === props.steps.length) return "check";
  return "alert";
});
const statusTone = computed(() => {
  if (hasActive.value) return "active";
  if (phaseCounts.value.failed || phaseCounts.value.rejected) return "danger";
  if (phaseCounts.value.cancelled || phaseCounts.value.interrupted) return "warning";
  if (props.steps.length > 0 && phaseCounts.value.completed === props.steps.length) return "success";
  return "muted";
});

function toggle() {
  expanded.value = !expanded.value;
}
</script>

<template>
  <div
    class="tool-group-row"
    :class="[
      `tool-group-row--${visual.kind}`,
      { 'tool-group-row--expanded': expanded, 'tool-group-row--progress': hasActive },
    ]"
  >
    <button
      type="button"
      class="tool-group-row__head"
      :aria-expanded="expanded"
      :aria-label="expanded ? '收起工具执行清单' : '展开工具执行清单'"
      @click="toggle"
    >
      <span class="tool-group-row__glyph" aria-hidden="true">
        <BrandActivityIndicator v-if="hasActive" mode="tool" :show-label="false" compact />
        <UiIcon v-else :class="`tool-group-row__status-icon--${statusTone}`" :name="statusIcon" :size="14" />
      </span>
      <span class="tool-group-row__visual" :title="visual.label">
        <ToolGroupIcon :name="visual.kind" />
      </span>
      <span class="tool-group-row__title">工具执行清单</span>
      <span class="tool-group-row__status">{{ statusText }}</span>
      <UiIcon class="tool-group-row__chevron" :name="expanded ? 'chevron-down' : 'chevron-right'" :size="14" />
    </button>

    <div v-if="!expanded && activeSteps.length" class="tool-group-row__active">
      <ToolSummaryRow
        v-for="step in activeSteps"
        :key="step.key"
        :call-entry="step.callEntry"
        :result-entry="step.resultEntry"
        :execution-hint="step.executionHint"
        :verbose="verbose"
      />
    </div>

    <div v-if="expanded" class="tool-group-row__steps">
      <ToolSummaryRow
        v-for="step in steps"
        :key="step.key"
        :call-entry="step.callEntry"
        :result-entry="step.resultEntry"
        :execution-hint="step.executionHint"
        :verbose="verbose"
      />
    </div>
  </div>
</template>

<style scoped>
.tool-group-row {
  width: 100%;
  min-width: 0;
  flex: 0 0 auto;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface);
  overflow: hidden;
}

.tool-group-row--progress {
  border-color: color-mix(in srgb, var(--color-primary) 38%, var(--color-border));
}

.tool-group-row__head {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  min-width: 0;
  min-height: 31px;
  padding: 5px 9px;
  border: 0;
  background: transparent;
  color: var(--color-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.tool-group-row__head:hover,
.tool-group-row--expanded .tool-group-row__head {
  background: var(--color-surface-hover);
}

.tool-group-row__glyph {
  flex: 0 0 14px;
  display: inline-grid;
  place-items: center;
  color: var(--color-success);
  font-size: 12px;
}

.tool-group-row__status-icon--danger { color: var(--color-danger); }
.tool-group-row__status-icon--warning { color: var(--color-warning); }
.tool-group-row__status-icon--muted { color: var(--color-text-subtle); }

.tool-group-row__visual {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 20px;
  color: var(--color-text-muted);
}

.tool-group-row__visual :deep(.tool-group-icon) {
  width: 20px;
  height: 20px;
}

/* 合并气泡沿用独立工具气泡的工具组色彩，避免同一命令行工具出现两种颜色。 */
.tool-group-row--shell .tool-group-row__visual {
  color: var(--color-warning);
}

.tool-group-row--terminal .tool-group-row__visual,
.tool-group-row--browser .tool-group-row__visual,
.tool-group-row--linux .tool-group-row__visual {
  color: var(--color-info);
}

.tool-group-row--fs .tool-group-row__visual,
.tool-group-row--skills .tool-group-row__visual {
  color: var(--color-success);
}

.tool-group-row--mcp .tool-group-row__visual {
  color: var(--color-auto);
}

.tool-group-row--child .tool-group-row__visual {
  color: var(--color-auto);
}

.tool-group-row--wrench .tool-group-row__visual,
.tool-group-row--tool .tool-group-row__visual {
  color: var(--color-text-muted);
}

.tool-group-row__title {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--color-text);
  font-family: inherit;
  font-size: 12.5px;
  font-weight: 400;
  line-height: 1.35;
  white-space: nowrap;
}

.tool-group-row__status {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 52%;
  color: var(--color-text-subtle);
  font-size: 11px;
  line-height: 1.3;
  text-align: right;
  white-space: normal;
}

.tool-group-row__chevron {
  flex: 0 0 auto;
  color: var(--color-text-subtle);
  font-size: 10px;
}

.tool-group-row__active,
.tool-group-row__steps {
  padding: 0 7px 7px;
  border-top: 1px solid var(--color-border);
}

.tool-group-row__active :deep(.tool-summary-row),
.tool-group-row__steps :deep(.tool-summary-row) {
  margin: 6px 0 0;
}
</style>
