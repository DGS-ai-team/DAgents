<script setup>
import { computed, ref } from "vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import BrandActivityIndicator from "./BrandActivityIndicator.vue";
import { resolveToolGroupVisual } from "../utils/toolSource.js";
import { resolveToolStepPhase } from "../utils/toolUserLabel.js";
import ToolGroupIcon from "./ToolGroupIcon.vue";

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
const visual = computed(() => {
  return resolveToolGroupVisual(props.steps);
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
        <span v-else>✓</span>
      </span>
      <span class="tool-group-row__visual" :title="visual.label">
        <ToolGroupIcon :name="visual.kind" />
      </span>
      <span class="tool-group-row__title">工具执行清单</span>
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
  color: #e2a053;
}

.tool-group-row--terminal .tool-group-row__visual,
.tool-group-row--browser .tool-group-row__visual,
.tool-group-row--linux .tool-group-row__visual {
  color: #569cd6;
}

.tool-group-row--fs .tool-group-row__visual,
.tool-group-row--skills .tool-group-row__visual {
  color: var(--color-success);
}

.tool-group-row--mcp .tool-group-row__visual {
  color: #9b8cff;
}

.tool-group-row--child .tool-group-row__visual {
  color: #c586c0;
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
