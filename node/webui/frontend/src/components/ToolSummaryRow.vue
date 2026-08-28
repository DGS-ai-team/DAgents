<script setup>
import { computed, onUnmounted, ref, watch } from "vue";
import { formatToolElapsed } from "../utils/format.js";
import {
  resolveToolStepPhase,
  toolStepPurpose,
  toolStepIsInProgress,
  toolStepIsPending,
  toolStepStatusText,
} from "../utils/toolUserLabel.js";
import { entryMedia } from "../utils/showImage.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import {
  bashControlMode,
  cancelBashToolCall,
  isBashBackgroundActive,
  parseBashResultStatus,
  toolCallIdFromEntry,
  toolJobsStore,
} from "../stores/toolJobs.js";
import { agentStore } from "../stores/agent.js";
import ToolExecBubble from "./ToolExecBubble.vue";
import ToolGroupIcon from "./ToolGroupIcon.vue";

const props = defineProps({
  callEntry: { type: Object, default: null },
  resultEntry: { type: Object, default: null },
  /** buildStream 标注：active=并行执行中，pending=HITL 待批尚未开跑 */
  executionHint: { type: String, default: null },
  verbose: { type: Boolean, default: false },
});

const expanded = ref(false);
const nowTick = ref(Date.now());
const actionError = ref("");
let tickTimer = null;

const stepArgs = computed(() => ({
  callEntry: props.callEntry,
  resultEntry: props.resultEntry,
  executionHint: props.executionHint,
}));
const purpose = computed(() => toolStepPurpose(stepArgs.value));
const phase = computed(() => resolveToolStepPhase(stepArgs.value));
const stepInProgress = computed(() => toolStepIsInProgress(stepArgs.value));
const stepPending = computed(() => toolStepIsPending(stepArgs.value));
const backgroundActive = computed(() =>
  isBashBackgroundActive({ callEntry: props.callEntry, resultEntry: props.resultEntry }),
);
const controlMode = computed(() => bashControlMode({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
const inProgress = computed(() => stepInProgress.value || backgroundActive.value || controlMode.value === "background");
const showBashControls = computed(() => controlMode.value != null);
const detailEntry = computed(() => props.resultEntry || props.callEntry);
const inlineMedia = computed(() => entryMedia(props.resultEntry));
const visual = computed(() => resolveToolVisual(props.resultEntry || props.callEntry || {}));
const toolCallId = computed(
  () => toolCallIdFromEntry(props.callEntry) || toolCallIdFromEntry(props.resultEntry),
);
const busyAction = computed(() => toolJobsStore.busyCallIds[toolCallId.value] || "");

const status = computed(() => {
  void nowTick.value;
  if (backgroundActive.value || controlMode.value === "background") return "后台执行中";
  if (controlMode.value === "running" || phase.value === "running") return "执行中";
  const bashStatus = parseBashResultStatus(props.resultEntry?.data?.content);
  if (bashStatus === "CANCELLED") return "已终止";
  if (bashStatus === "SUCCEEDED") return "已完成";
  const base = toolStepStatusText(stepArgs.value);
  if (phase.value !== "generating" || !props.callEntry?.startedAt) return base;
  const elapsed = formatToolElapsed((Date.now() - props.callEntry.startedAt) / 1000);
  if (!elapsed) return base || "生成中";
  return `生成中${elapsed}`;
});

function stopTick() {
  if (tickTimer) {
    clearInterval(tickTimer);
    tickTimer = null;
  }
}

function ensureTick() {
  if (tickTimer) return;
  nowTick.value = Date.now();
  tickTimer = setInterval(() => {
    nowTick.value = Date.now();
  }, 400);
}

watch(
  inProgress,
  (active) => {
    if (active) ensureTick();
    else stopTick();
  },
  { immediate: true },
);

onUnmounted(stopTick);

function toggle() {
  expanded.value = !expanded.value;
}

async function onCancel(ev) {
  ev?.stopPropagation?.();
  actionError.value = "";
  const agentId = agentStore.agentId;
  const callId = toolCallId.value;
  if (!agentId || !callId || busyAction.value) return;
  try {
    await cancelBashToolCall(agentId, callId);
  } catch (err) {
    actionError.value = err?.message || "终止失败";
  }
}

</script>

<template>
  <div
    class="tool-summary-row"
    :class="{
      'tool-summary-row--expanded': expanded,
      'tool-summary-row--progress': inProgress,
      [`tool-summary-row--${visual.kind}`]: true,
    }"
  >
    <div class="tool-summary-row__bar">
      <button
        type="button"
        class="tool-summary-row__head"
        :aria-expanded="expanded"
        :aria-label="expanded ? '收起工具详情' : '展开工具详情'"
        @click="toggle"
      >
        <span class="tool-summary-row__glyph" aria-hidden="true">
          <span v-if="inProgress" class="tool-exec-spinner" />
          <span v-else-if="stepPending" class="tool-summary-row__pending">○</span>
          <span v-else class="tool-summary-row__check">✓</span>
        </span>
        <span class="tool-summary-row__visual" :title="visual.label">
          <ToolGroupIcon :name="visual.kind" />
        </span>
        <span v-if="purpose" class="tool-summary-row__text">{{ purpose }}</span>
        <span v-if="inlineMedia.length && !expanded" class="tool-summary-row__thumb-wrap">
          <img
            class="tool-summary-row__thumb"
            :src="inlineMedia[0].url"
            :alt="inlineMedia[0].label || '图片'"
            loading="lazy"
          />
        </span>
      </button>
      <div v-if="showBashControls" class="tool-summary-row__actions">
        <button
          type="button"
          class="tool-summary-row__action"
          :disabled="!!busyAction"
          :title="actionError || undefined"
          @click="onCancel"
        >
          {{ busyAction === "cancel" ? "终止中…" : "终止" }}
        </button>
      </div>
      <span v-if="status" class="tool-summary-row__status">
        {{ status }}
      </span>
      <button
        type="button"
        class="tool-summary-row__chevron-btn"
        :aria-expanded="expanded"
        :aria-label="expanded ? '收起工具详情' : '展开工具详情'"
        @click="toggle"
      >
        <span class="tool-summary-row__chevron" aria-hidden="true">{{ expanded ? "▾" : "▸" }}</span>
      </button>
    </div>
    <div v-if="expanded && detailEntry" class="tool-summary-row__detail">
      <ToolExecBubble
        :entry="detailEntry"
        :call-entry="callEntry"
        :result-entry="resultEntry"
        :verbose="verbose"
        embedded
      />
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

.tool-summary-row:hover,
.tool-summary-row--expanded {
  background: var(--color-surface);
  border-color: var(--color-border);
}

.tool-summary-row--progress {
  /* 白底 + 浅灰边框，不用主色深描边 */
  background: var(--color-surface);
  border-color: var(--color-border);
}

.tool-summary-row__bar {
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  gap: 6px;
  width: 100%;
  min-width: 0;
  padding: 5px 8px;
}

.tool-summary-row__head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1 1 auto;
  min-width: 0;
  padding: 0;
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
  font: inherit;
  color: var(--color-text-muted);
}

.tool-summary-row__glyph {
  flex: 0 0 auto;
  display: inline-flex;
  width: 14px;
  justify-content: center;
  color: var(--color-success);
  font-size: 12px;
}

.tool-summary-row__check {
  opacity: 0.9;
}

.tool-summary-row__pending {
  opacity: 0.55;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.tool-summary-row__visual {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 20px;
  color: var(--color-text-muted);
}

.tool-summary-row--shell .tool-summary-row__visual {
  color: #e2a053;
}

.tool-summary-row--terminal .tool-summary-row__visual,
.tool-summary-row--browser .tool-summary-row__visual,
.tool-summary-row--linux .tool-summary-row__visual {
  color: #569cd6;
}

.tool-summary-row--fs .tool-summary-row__visual,
.tool-summary-row--skills .tool-summary-row__visual {
  color: var(--color-success);
}

.tool-summary-row--mcp .tool-summary-row__visual {
  color: #9b8cff;
}

.tool-summary-row--child .tool-summary-row__visual {
  color: #c586c0;
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

.tool-summary-row__thumb-wrap {
  flex: 0 0 auto;
  line-height: 0;
}

.tool-summary-row__thumb {
  width: 28px;
  height: 28px;
  object-fit: cover;
  border-radius: 4px;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
}

.tool-summary-row__actions {
  display: inline-flex;
  align-items: center;
  flex: 0 0 auto;
  flex-wrap: nowrap;
  gap: 4px;
}

.tool-summary-row__action {
  appearance: none;
  border: 1px solid var(--color-border);
  background: var(--color-surface-elevated);
  color: var(--color-text);
  font: inherit;
  font-size: 11px;
  line-height: 1.2;
  padding: 2px 9px;
  border-radius: 8px;
  cursor: pointer;
  white-space: nowrap;
}

.tool-summary-row__action:hover:not(:disabled) {
  border-color: var(--color-text-muted);
}

.tool-summary-row__action:disabled {
  opacity: 0.55;
  cursor: default;
}

.tool-summary-row__action--secondary {
  color: var(--color-text-muted);
}

.tool-summary-row__status {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--color-text-subtle);
  white-space: nowrap;
}

.tool-summary-row__chevron-btn {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: none;
  background: transparent;
  cursor: pointer;
  color: inherit;
}

.tool-summary-row__chevron {
  font-size: 10px;
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
