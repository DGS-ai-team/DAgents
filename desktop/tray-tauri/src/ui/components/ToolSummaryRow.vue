<script setup>
import { computed, ref } from "vue";
import {
  resolveToolStepPhase,
  toolStepStatusText,
  toolStepPurposeOrToolLabel,
} from "../utils/toolUserLabel.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { toolCallIdFromEntry, toolNameFromEntry } from "../stores/toolJobs.js";
import { childProgressForTool } from "../stores/remoteWorkers.js";
import { childAgentIdsFromResult, isTemporaryAgentTool } from "../utils/temporaryAgentResults.js";
import ToolExecBubble from "./ToolExecBubble.vue";
import ToolGroupIcon from "./ToolGroupIcon.vue";
import UiIcon from "./UiIcon.vue";
import ChildAgentProgressPanel from "./ChildAgentProgressPanel.vue";

const props = defineProps({
  callEntry: { type: Object, default: null },
  resultEntry: { type: Object, default: null },
  /** buildStream 标注：active=并行执行中，pending=HITL 待批尚未开跑 */
  executionHint: { type: String, default: null },
  /** turn_state.tool_executions 中的权威终态，优先于冲突的 transcript result status */
  lifecycleStatus: { type: String, default: "" },
  verbose: { type: Boolean, default: false },
  embedded: { type: Boolean, default: false },
});

const expanded = ref(false);
const everExpanded = ref(false);

const stepArgs = computed(() => ({
  callEntry: props.callEntry,
  resultEntry: props.resultEntry,
  executionHint: props.executionHint,
  lifecycleStatus: props.lifecycleStatus,
}));
const toolLabel = computed(() => toolStepPurposeOrToolLabel(stepArgs.value));
const phase = computed(() => resolveToolStepPhase(stepArgs.value));
const detailEntry = computed(() => props.resultEntry || props.callEntry);
const visual = computed(() => resolveToolVisual(props.callEntry || props.resultEntry || {}));
const toolCallId = computed(
  () => toolCallIdFromEntry(props.callEntry) || toolCallIdFromEntry(props.resultEntry),
);
const toolName = computed(() => toolNameFromEntry(props.callEntry) || toolNameFromEntry(props.resultEntry));
const childAgentIds = computed(() =>
  childAgentIdsFromResult(toolName.value, props.resultEntry?.data?.content),
);
const childProgress = computed(() => {
  if (!isTemporaryAgentTool(toolName.value)) return [];
  return childProgressForTool(toolCallId.value, childAgentIds.value);
});
const status = computed(() => {
  if (phase.value === "running") return "执行中";
  const base = toolStepStatusText(stepArgs.value);
  if (phase.value === "idle") return "状态未知";
  if (phase.value === "pending") return "待批准";
  return base;
});
const ariaLabel = computed(() => `${toolLabel.value}，${status.value || "工具操作"}，${expanded.value ? "收起详情" : "展开详情"}`);

function toggle() {
  expanded.value = !expanded.value;
  if (expanded.value) everExpanded.value = true;
}

</script>

<template>
  <div
    class="tool-summary-row"
    :class="{ 'tool-summary-row--expanded': expanded, 'tool-summary-row--embedded': embedded, [`tool-summary-row--${visual.kind}`]: true }"
  >
    <button
      type="button"
      class="tool-summary-row__head"
      :aria-expanded="expanded"
      :aria-label="ariaLabel"
      @click="toggle"
    >
      <span class="tool-summary-row__visual" :title="visual.label" aria-hidden="true"><ToolGroupIcon :name="visual.kind" /></span>
      <span class="tool-summary-row__text" :title="toolLabel">{{ toolLabel }}</span>
      <span v-if="status" class="tool-summary-row__status">{{ status }}</span>
      <UiIcon class="tool-summary-row__chevron" :name="expanded ? 'chevron-down' : 'chevron-right'" :size="14" aria-hidden="true" />
    </button>
    <div v-if="everExpanded && detailEntry" v-show="expanded" class="tool-summary-row__detail">
      <ToolExecBubble
        :entry="detailEntry"
        :call-entry="callEntry"
        :result-entry="resultEntry"
        :verbose="verbose"
        embedded
      />
      <ChildAgentProgressPanel v-if="childProgress.length" :items="childProgress" />
    </div>
  </div>
</template>

<style scoped>
.tool-summary-row {
  flex: 0 0 auto;
  margin: 2px 0;
  border-radius: 6px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  max-width: 100%;
}

.tool-summary-row--embedded {
  margin: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
}

.tool-summary-row--embedded + .tool-summary-row--embedded {
  border-top: 1px solid var(--color-border);
}

.tool-summary-row__head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1 1 auto;
  width: 100%;
  min-width: 0;
  padding: 6px 8px;
  border: 0;
  border-radius: inherit;
  background: transparent;
  text-align: left;
  cursor: pointer;
  font: inherit;
  color: var(--color-text-muted);
}

.tool-summary-row__head:hover {
  background: var(--color-surface-hover);
}

.tool-summary-row__head:focus-visible {
  outline: 2px solid var(--color-focus, var(--color-primary));
  outline-offset: 2px;
}

.tool-summary-row__visual {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 20px;
  color: var(--color-text-muted);
}

.tool-summary-row--shell .tool-summary-row__visual {
  color: var(--color-warning);
}

.tool-summary-row--terminal .tool-summary-row__visual,
.tool-summary-row--browser .tool-summary-row__visual,
.tool-summary-row--linux .tool-summary-row__visual {
  color: var(--color-info);
}

.tool-summary-row--fs .tool-summary-row__visual,
.tool-summary-row--skills .tool-summary-row__visual {
  color: var(--color-success);
}

.tool-summary-row--mcp .tool-summary-row__visual {
  color: var(--color-auto);
}

.tool-summary-row--child .tool-summary-row__visual {
  color: var(--color-auto);
}

.tool-summary-row__text {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  color: var(--color-text);
  line-height: 1.35;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tool-summary-row__status {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-width: 4.5em;
  justify-content: flex-end;
  font-size: 11px;
  color: var(--color-text-subtle);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.tool-summary-row__chevron {
  flex: 0 0 auto;
  font-size: 10px;
  color: var(--color-text-subtle);
}

.tool-summary-row__detail {
  min-width: 0;
  overflow: hidden;
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
