<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import {
  resolveToolStepPhase,
  toolStepStatusText,
  toolStepPurposeOrToolLabel,
} from "../utils/toolUserLabel.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { toolCallIdFromEntry, toolNameFromEntry } from "../utils/toolIdentity.js";
import { childProgressForTool } from "../stores/remoteWorkers.js";
import { childAgentIdsFromResult, isTemporaryAgentTool } from "../utils/temporaryAgentResults.js";
import ToolExecBubble from "./ToolExecBubble.vue";
import ToolGroupIcon from "./ToolGroupIcon.vue";
import UiIcon from "./UiIcon.vue";
import ChildAgentProgressPanel from "./ChildAgentProgressPanel.vue";
import { getConversationRoundToolStep } from "../api/node.js";

const props = defineProps({
  callEntry: { type: Object, default: null },
  resultEntry: { type: Object, default: null },
  conversationOrdinal: { type: Number, default: 0 },
  streamItemKey: { type: String, default: "" },
  /** null uses local state; a parent can retain expansion state while lazily unmounting rows. */
  expanded: { type: Boolean, default: null },
  /** buildStream 已收敛的工具状态；缺省时为独立工具行计算 */
  phase: { type: String, default: "" },
  verbose: { type: Boolean, default: false },
  embedded: { type: Boolean, default: false },
  agentId: { type: String, default: "" },
  restoreToolMedia: { type: Function, default: null },
  releaseToolMedia: { type: Function, default: null },
});
const emit = defineEmits(["expanded-change"]);

const localExpanded = ref(false);
const isExpanded = computed(() => props.expanded === null ? localExpanded.value : props.expanded);
const loadedCallEntry = ref(null);
const loadedResultEntry = ref(null);
const detailsLoading = ref(false);
const detailsError = ref("");
let detailLoadGeneration = 0;
let detailLoadKey = "";
let detailMediaOwnerKey = "";

const stepArgs = computed(() => ({
  callEntry: props.callEntry,
  resultEntry: props.resultEntry,
  phase: props.phase,
}));
const toolLabel = computed(() => toolStepPurposeOrToolLabel(stepArgs.value));
const resolvedPhase = computed(() => resolveToolStepPhase(stepArgs.value));
const detailCallEntry = computed(() => loadedCallEntry.value || props.callEntry);
const detailResultEntry = computed(() => loadedResultEntry.value || props.resultEntry);
const detailEntry = computed(() => detailResultEntry.value || detailCallEntry.value);
const visual = computed(() => resolveToolVisual(props.callEntry || props.resultEntry || {}));
const toolCallId = computed(
  () => toolCallIdFromEntry(props.callEntry) || toolCallIdFromEntry(props.resultEntry),
);
const detailRequestKey = computed(() => [
  props.agentId,
  props.callEntry?.round_id || props.resultEntry?.round_id || "",
  props.callEntry?.round_revision_id || props.resultEntry?.round_revision_id || "",
  props.callEntry?.detail_item_id || props.resultEntry?.detail_item_id || "",
  toolCallId.value,
  props.callEntry?.item_version ?? "",
  props.resultEntry?.item_version ?? "",
  props.callEntry?.id || "",
  props.resultEntry?.id || "",
].join("\0"));
const needsDetails = computed(() => Boolean(
  props.callEntry?.details_available || props.resultEntry?.details_available,
));
const toolName = computed(() => toolNameFromEntry(props.callEntry) || toolNameFromEntry(props.resultEntry));
const childAgentIds = computed(() =>
  childAgentIdsFromResult(toolName.value, props.resultEntry?.data?.content),
);
const childProgress = computed(() => {
  if (!isTemporaryAgentTool(toolName.value)) return [];
  return childProgressForTool(toolCallId.value, childAgentIds.value);
});
const status = computed(() => {
  if (resolvedPhase.value === "running") return "执行中";
  const base = toolStepStatusText(stepArgs.value);
  if (resolvedPhase.value === "idle" && props.callEntry?.details_available && !props.resultEntry) return "结果待读取";
  if (resolvedPhase.value === "idle") return "状态未知";
  if (resolvedPhase.value === "pending") return "待批准";
  return base;
});
const ariaLabel = computed(() => `${toolLabel.value}，${status.value || "工具操作"}，${isExpanded.value ? "收起详情" : "展开详情"}`);

async function loadDetails() {
  const sourceItemId = String(props.callEntry?.detail_item_id || props.resultEntry?.detail_item_id || "").trim();
  const roundId = String(props.callEntry?.round_id || props.resultEntry?.round_id || "").trim();
  const revisionId = String(props.callEntry?.round_revision_id || props.resultEntry?.round_revision_id || "").trim();
  const callId = toolCallId.value;
  const requestKey = detailRequestKey.value;
  if (!props.agentId || !sourceItemId || !roundId || !revisionId || !callId || !needsDetails.value || detailLoadKey === requestKey) return;
  const generation = ++detailLoadGeneration;
  detailLoadKey = requestKey;
  detailsLoading.value = true;
  detailsError.value = "";
  try {
    const response = await getConversationRoundToolStep(props.agentId, props.agentId, roundId, callId, { revisionId, sourceItemId });
    if (generation !== detailLoadGeneration || requestKey !== detailRequestKey.value) return;
    const step = response?.steps?.find(item => String(item?.tool_call_id || "") === callId);
    if (!step?.call_entry) throw new Error("工具详情暂不可用");
    let detailEntries = [step.call_entry, step.result_entry].filter(Boolean);
    if (typeof props.restoreToolMedia === "function") {
      detailMediaOwnerKey = `${sourceItemId}\0${callId}`;
      const restored = await props.restoreToolMedia(detailEntries, props.agentId, detailMediaOwnerKey, () => generation === detailLoadGeneration && requestKey === detailRequestKey.value);
      if (generation !== detailLoadGeneration || requestKey !== detailRequestKey.value || !Array.isArray(restored)) return;
      detailEntries = restored;
    }
    loadedCallEntry.value = detailEntries[0] || step.call_entry;
    loadedResultEntry.value = step.result_entry ? (detailEntries[1] || step.result_entry) : null;
  } catch (cause) {
    if (generation !== detailLoadGeneration || requestKey !== detailRequestKey.value) return;
    detailsError.value = String(cause?.message || "读取工具详情失败");
  } finally {
    if (generation === detailLoadGeneration) {
      detailsLoading.value = false;
      detailLoadKey = "";
    }
  }
}

function releaseDetailMedia() {
  if (!detailMediaOwnerKey) return;
  props.releaseToolMedia?.(props.agentId, detailMediaOwnerKey);
  detailMediaOwnerKey = "";
}

function toggle() {
  const next = !isExpanded.value;
  if (props.expanded === null) localExpanded.value = next;
  else emit("expanded-change", next);
}

watch(isExpanded, (value) => {
  if (value && !loadedCallEntry.value && needsDetails.value) void loadDetails();
}, { immediate: true });

watch(detailRequestKey, () => {
  releaseDetailMedia();
  detailLoadGeneration += 1;
  detailLoadKey = "";
  loadedCallEntry.value = null;
  loadedResultEntry.value = null;
  detailsLoading.value = false;
  detailsError.value = "";
  if (isExpanded.value && needsDetails.value) void loadDetails();
}, { flush: "sync" });

onBeforeUnmount(() => {
  detailLoadGeneration += 1;
  releaseDetailMedia();
});

</script>

<template>
  <div
    class="tool-summary-row"
    :data-conversation-ordinal="conversationOrdinal || undefined"
    :data-stream-item-key="streamItemKey || undefined"
    :class="{ 'tool-summary-row--expanded': isExpanded, 'tool-summary-row--embedded': embedded, [`tool-summary-row--${visual.kind}`]: true }"
  >
    <button
      type="button"
      class="tool-summary-row__head"
      :aria-expanded="isExpanded"
      :aria-label="ariaLabel"
      @click="toggle"
    >
      <span class="tool-summary-row__visual" :title="visual.label" aria-hidden="true"><ToolGroupIcon :name="visual.kind" /></span>
      <span class="tool-summary-row__text" :title="toolLabel">{{ toolLabel }}</span>
      <span v-if="status" class="tool-summary-row__status">{{ status }}</span>
      <UiIcon class="tool-summary-row__chevron" :name="isExpanded ? 'chevron-down' : 'chevron-right'" :size="14" aria-hidden="true" />
    </button>
    <div v-if="isExpanded && (detailEntry || detailsLoading || detailsError)" class="tool-summary-row__detail">
      <p v-if="detailsLoading" class="tool-summary-row__detail-state" role="status">正在读取完整工具参数与结果…</p>
      <p v-else-if="detailsError" class="tool-summary-row__detail-state is-error" role="alert">{{ detailsError }}</p>
      <ToolExecBubble
        v-if="!detailsLoading && detailEntry"
        :entry="detailEntry"
        :call-entry="detailCallEntry"
        :result-entry="detailResultEntry"
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

.tool-summary-row__detail-state {
  margin: 8px 0 0;
  color: var(--color-text-subtle);
  font-size: 12px;
}

.tool-summary-row__detail-state.is-error { color: var(--color-danger); }

.tool-summary-row__detail :deep(.msg--tool-centered) {
  margin: 0;
}

.tool-summary-row__detail :deep(.tool-exec-bubble) {
  box-shadow: none;
  border: none;
  background: transparent;
}
</style>
