<script setup>
import { computed, ref } from "vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import BrandActivityIndicator from "./BrandActivityIndicator.vue";
import { resolveToolVisual } from "../utils/toolSource.js";
import { resolveToolStepPhase, toolStepUserSummary } from "../utils/toolUserLabel.js";

const props = defineProps({
  steps: { type: Array, default: () => [] },
  verbose: { type: Boolean, default: false },
});

const expanded = ref(false);

const activeSteps = computed(() =>
  props.steps.filter((step) => {
    if (!step?.callEntry) return false;
    if (step.executionHint === "active") return true;
    return !step.resultEntry && resolveToolStepPhase(step) !== "interrupted";
  }),
);

const completedCount = computed(() => props.steps.filter((step) => !!step?.resultEntry).length);
const hasActive = computed(() => activeSteps.value.length > 0);
const firstStep = computed(() => props.steps[0] || null);
const summary = computed(() => {
  const step = activeSteps.value[0] || firstStep.value;
  return step ? toolStepUserSummary(step) : "";
});
const visual = computed(() => {
  const step = activeSteps.value[0] || firstStep.value;
  return resolveToolVisual(step?.resultEntry || step?.callEntry || {});
});
const statusText = computed(() => {
  if (hasActive.value) return `${activeSteps.value.length} 项执行中`;
  if (completedCount.value === props.steps.length) return `${props.steps.length} 项已完成`;
  return `${completedCount.value}/${props.steps.length} 项完成`;
});

function toggle() {
  expanded.value = !expanded.value;
}
</script>

<template>
  <div class="tool-group-row" :class="{ 'tool-group-row--expanded': expanded, 'tool-group-row--progress': hasActive }">
    <button
      type="button"
      class="tool-group-row__head"
      :aria-expanded="expanded"
      aria-label="展开工具执行清单"
      @click="toggle"
    >
      <span class="tool-group-row__glyph" aria-hidden="true">
        <BrandActivityIndicator v-if="hasActive" mode="tool" :show-label="false" compact />
        <span v-else>✓</span>
      </span>
      <span class="tool-group-row__kind">工具执行</span>
      <span class="tool-group-row__count">{{ steps.length }} 项</span>
      <span class="tool-group-row__summary">{{ summary || visual.label }}</span>
      <span class="tool-group-row__status">{{ statusText }}</span>
      <span class="tool-group-row__chevron" aria-hidden="true">{{ expanded ? "▾" : "▸" }}</span>
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

.tool-group-row__kind,
.tool-group-row__count {
  flex: 0 0 auto;
  font-size: 10.5px;
  font-weight: 600;
  color: var(--color-text-subtle);
}

.tool-group-row__kind {
  padding: 1px 5px;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  background: var(--color-surface-elevated);
}

.tool-group-row__count {
  color: var(--color-primary-strong);
  font-variant-numeric: tabular-nums;
}

.tool-group-row__summary {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-muted);
  font-family: var(--font-mono);
  font-size: 11.5px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tool-group-row__status {
  flex: 0 0 auto;
  color: var(--color-text-subtle);
  font-size: 11px;
  white-space: nowrap;
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
