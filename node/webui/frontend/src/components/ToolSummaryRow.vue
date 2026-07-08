<script setup>
import { computed, ref } from "vue";
import { toolStepIsInProgress, toolStepStatusText, toolStepUserSummary } from "../utils/toolUserLabel.js";
import ToolExecBubble from "./ToolExecBubble.vue";

const props = defineProps({
  callEntry: { type: Object, default: null },
  resultEntry: { type: Object, default: null },
  verbose: { type: Boolean, default: false },
});

const expanded = ref(false);

const summary = computed(() => toolStepUserSummary({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
const status = computed(() => toolStepStatusText({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
const inProgress = computed(() => toolStepIsInProgress({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
const detailEntry = computed(() => props.resultEntry || props.callEntry);

function toggle() {
  expanded.value = !expanded.value;
}
</script>

<template>
  <div class="tool-summary-row" :class="{ 'tool-summary-row--expanded': expanded, 'tool-summary-row--progress': inProgress }">
    <button type="button" class="tool-summary-row__head" @click="toggle">
      <span class="tool-summary-row__icon" aria-hidden="true">{{ inProgress ? "⏳" : "✓" }}</span>
      <span class="tool-summary-row__text">{{ summary }}</span>
      <span v-if="status" class="tool-summary-row__status">{{ status }}</span>
      <span class="tool-summary-row__chevron" aria-hidden="true">{{ expanded ? "▾" : "▸" }}</span>
    </button>
    <div v-if="expanded && detailEntry" class="tool-summary-row__detail">
      <ToolExecBubble :entry="detailEntry" :verbose="verbose" />
    </div>
  </div>
</template>

<style scoped>
.tool-summary-row {
  margin: 4px 0;
  border-radius: 10px;
  background: var(--color-surface-muted);
  border: 1px solid var(--color-border);
}

.tool-summary-row--progress {
  border-color: rgba(99, 102, 241, 0.25);
}

.tool-summary-row__head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
  font: inherit;
  color: var(--color-text-muted);
}

.tool-summary-row__head:hover {
  background: var(--color-surface-hover);
}

.tool-summary-row__icon {
  flex: 0 0 auto;
  font-size: 13px;
}

.tool-summary-row__text {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 13px;
  color: var(--color-text);
  line-height: 1.4;
}

.tool-summary-row__status {
  flex: 0 0 auto;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.tool-summary-row__chevron {
  flex: 0 0 auto;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.tool-summary-row__detail {
  padding: 0 8px 8px;
  border-top: 1px solid var(--color-border);
}

.tool-summary-row__detail :deep(.msg--tool-centered) {
  margin: 0;
}

.tool-summary-row__detail :deep(.tool-exec-bubble) {
  box-shadow: none;
  border: none;
  background: transparent;
}
</style>
