<script setup>
import { computed, ref } from "vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import { resolveToolVisual } from "../utils/toolSource.js";
import { resolveToolStepPhase, toolStepToolLabelParts } from "../utils/toolUserLabel.js";
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
const foregroundSteps = computed(() =>
  props.steps.filter((step) => ["generating", "running"].includes(resolveToolStepPhase(step))),
);
const hasActive = computed(() => activeSteps.value.length > 0);
const hasForeground = computed(() => foregroundSteps.value.length > 0);
const hasPending = computed(() => phaseCounts.value.pending > 0);
const pendingSteps = computed(() => props.steps.filter((step) => resolveToolStepPhase(step) === "pending"));
const selectedStep = computed(() => {
  const steps = foregroundSteps.value.length
    ? foregroundSteps.value
    : pendingSteps.value.length
      ? pendingSteps.value
      : activeSteps.value;
  return steps.length ? steps[steps.length - 1] : props.steps[props.steps.length - 1] || null;
});
const selectedVisual = computed(() => {
  const step = selectedStep.value;
  return resolveToolVisual(step?.resultEntry || step?.callEntry || step || {});
});
const selectedLabel = computed(() => toolStepToolLabelParts(selectedStep.value || {}));
const selectedIdentity = computed(() => selectedLabel.value.name);
const purposeIdentity = computed(() => `${selectedIdentity.value}\u0000${selectedLabel.value.purpose}`);
const titleLabel = computed(() => selectedLabel.value.purpose
  ? `${selectedLabel.value.name}(${selectedLabel.value.purpose})`
  : selectedLabel.value.name);
const headLabel = computed(() => `${titleLabel.value}，${statusText.value || "工具执行清单"}，${expanded.value ? "收起" : "展开"}`);
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
      `tool-group-row--${selectedVisual.kind}`,
      { 'tool-group-row--expanded': expanded, 'tool-group-row--progress': hasActive },
    ]"
  >
    <button
      type="button"
      class="tool-group-row__head"
      :aria-expanded="expanded"
      :aria-label="headLabel"
      @click="toggle"
    >
      <span class="tool-group-row__glyph" aria-hidden="true">
        <span v-if="hasActive" class="tool-group-row__activity-dot" />
        <UiIcon v-else :class="`tool-group-row__status-icon--${statusTone}`" :name="statusIcon" :size="14" />
      </span>
      <span class="tool-group-row__visual" :title="selectedVisual.label">
        <ToolGroupIcon :name="selectedVisual.kind" />
      </span>
      <span class="tool-group-row__title" :class="{ 'tool-group-row__title--active': hasForeground, 'tool-group-row__title--purpose': !!selectedLabel.purpose }" :title="titleLabel">
        <Transition name="tool-title">
          <span :key="selectedIdentity" class="tool-group-row__title-name"><span class="tool-group-row__title-tool-name">{{ selectedLabel.name }}</span><span class="tool-group-row__title-purpose-viewport"><Transition name="tool-purpose"><span v-if="selectedLabel.purpose" :key="purposeIdentity" class="tool-group-row__title-purpose">({{ selectedLabel.purpose }})</span></Transition></span></span>
        </Transition>
      </span>
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

.tool-group-row__activity-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-text-muted);
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

.tool-group-row__title {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  position: relative;
  height: 1.35em;
  color: var(--color-text);
  font-family: inherit;
  font-size: 12.5px;
  font-weight: 400;
  line-height: 1.35;
  white-space: nowrap;
}

.tool-group-row__title-name {
  position: absolute;
  inset: 0;
  display: flex;
  min-width: 0;
  align-items: center;
  white-space: nowrap;
}

.tool-group-row__title-tool-name {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tool-group-row__title--purpose .tool-group-row__title-tool-name {
  max-width: 60%;
}

.tool-group-row__title-purpose-viewport {
  position: relative;
  flex: 1 1 0;
  min-width: 0;
  height: 1.35em;
  overflow: hidden;
  white-space: nowrap;
}

.tool-group-row__title-purpose {
  display: inline-block;
  width: 100%;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tool-group-row__title--active {
  color: var(--color-text);
}

.tool-group-row__title--active .tool-group-row__title-tool-name,
.tool-group-row__title--active .tool-group-row__title-purpose {
  background-image: linear-gradient(100deg, var(--color-text-muted) 0%, var(--color-text) 35%, var(--color-text-muted) 70%);
  background-size: 220% 100%;
  background-position: 100% 0;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  animation: tool-title-shimmer 3.8s ease-in-out infinite;
}

.tool-title-enter-active,
.tool-title-leave-active,
.tool-purpose-enter-active,
.tool-purpose-leave-active {
  transition: opacity 260ms ease, transform 260ms ease;
}

.tool-title-enter-active,
.tool-title-leave-active,
.tool-purpose-enter-active,
.tool-purpose-leave-active {
  position: absolute;
  inset-inline: 0;
  overflow: hidden;
}

.tool-title-enter-from,
.tool-purpose-enter-from {
  opacity: 0;
  transform: translateY(100%);
}

.tool-title-leave-to,
.tool-purpose-leave-to {
  opacity: 0;
  transform: translateY(-100%);
}

@keyframes tool-title-shimmer {
  0%, 35% { background-position: 100% 0; }
  70%, 100% { background-position: -120% 0; }
}

@media (prefers-reduced-motion: reduce) {
  .tool-group-row__title--active .tool-group-row__title-tool-name,
  .tool-group-row__title--active .tool-group-row__title-purpose {
    background: none;
    -webkit-text-fill-color: var(--color-text);
    animation: none;
  }
  .tool-title-enter-active,
  .tool-title-leave-active,
  .tool-purpose-enter-active,
  .tool-purpose-leave-active { transition: none; }
}

@media (forced-colors: active) {
  .tool-group-row__title,
  .tool-group-row__title--active {
    color: CanvasText;
    background: none;
    -webkit-text-fill-color: CanvasText !important;
  }
  .tool-group-row__title--active .tool-group-row__title-tool-name,
  .tool-group-row__title--active .tool-group-row__title-purpose,
  .tool-group-row__title-tool-name,
  .tool-group-row__title-purpose { background: none !important; animation: none; -webkit-text-fill-color: CanvasText !important; }
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
