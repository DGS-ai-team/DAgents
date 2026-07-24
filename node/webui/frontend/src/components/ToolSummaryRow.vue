<script setup>
import { computed, onUnmounted, ref, watch } from "vue";
import { formatToolElapsed } from "../utils/format.js";
import {
  resolveToolStepPhase,
  toolStepIsInProgress,
  toolStepIsPending,
  toolStepStatusText,
  toolStepUserSummary,
} from "../utils/toolUserLabel.js";
import { entryMedia } from "../utils/showImage.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import {
  backgroundBashToolCall,
  bashControlMode,
  canBackgroundBashTool,
  cancelBashToolCall,
  isBashBackgroundActive,
  parseBashResultStatus,
  toolCallIdFromEntry,
  toolJobsStore,
} from "../stores/toolJobs.js";
import { agentStore } from "../stores/agent.js";
import ToolExecBubble from "./ToolExecBubble.vue";

const props = defineProps({
  callEntry: { type: Object, default: null },
  resultEntry: { type: Object, default: null },
  /** buildStream 标注：active=当前执行，pending=排队 */
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
const summary = computed(() => toolStepUserSummary(stepArgs.value));
const phase = computed(() => resolveToolStepPhase(stepArgs.value));
const stepInProgress = computed(() => toolStepIsInProgress(stepArgs.value));
const stepPending = computed(() => toolStepIsPending(stepArgs.value));
const backgroundActive = computed(() =>
  isBashBackgroundActive({ callEntry: props.callEntry, resultEntry: props.resultEntry }),
);
const controlMode = computed(() => bashControlMode({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
const inProgress = computed(() => stepInProgress.value || backgroundActive.value || controlMode.value === "background");
const showBashControls = computed(() => controlMode.value != null);
const showBackgroundAction = computed(() => canBackgroundBashTool({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
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

async function onBackground(ev) {
  ev?.stopPropagation?.();
  actionError.value = "";
  const agentId = agentStore.agentId;
  const callId = toolCallId.value;
  if (!agentId || !callId || busyAction.value) return;
  try {
    await backgroundBashToolCall(agentId, callId);
  } catch (err) {
    actionError.value = err?.message || "转后台失败";
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
      <button type="button" class="tool-summary-row__head" @click="toggle">
        <span class="tool-summary-row__glyph" aria-hidden="true">
          <span v-if="inProgress" class="tool-exec-spinner" />
          <span v-else-if="stepPending" class="tool-summary-row__pending">○</span>
          <span v-else class="tool-summary-row__check">✓</span>
        </span>
        <span class="tool-summary-row__kind">{{ visual.label }}</span>
        <span class="tool-summary-row__text">{{ summary }}</span>
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
        <button
          v-if="showBackgroundAction"
          type="button"
          class="tool-summary-row__action tool-summary-row__action--secondary"
          :disabled="!!busyAction"
          :title="actionError || undefined"
          @click="onBackground"
        >
          {{ busyAction === "background" ? "转后台中…" : "转后台" }}
        </button>
      </div>
      <span v-if="status" class="tool-summary-row__status">
        <span v-if="inProgress" class="msg__meta-dots tool-summary-row__dots" aria-hidden="true">
          <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
        </span>
        {{ status }}
      </span>
      <button
        type="button"
        class="tool-summary-row__chevron-btn"
        :aria-expanded="expanded"
        aria-label="展开工具详情"
        @click="toggle"
      >
        <span class="tool-summary-row__chevron" aria-hidden="true">{{ expanded ? "▾" : "▸" }}</span>
      </button>
    </div>
    <div v-if="expanded && detailEntry" class="tool-summary-row__detail">
      <ToolExecBubble :entry="detailEntry" :verbose="verbose" embedded />
    </div>
  </div>
</template>

<style scoped>
.tool-summary-row {
  margin: 2px 0;
  border-radius: 6px;
  background: transparent;
  border: 1px solid transparent;
  max-width: 100%;
}

.tool-summary-row:hover,
.tool-summary-row--expanded {
  background: var(--color-surface-muted);
  border-color: var(--color-border);
}

.tool-summary-row--progress {
  border-color: rgba(55, 148, 255, 0.35);
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

.tool-summary-row__kind {
  flex: 0 0 auto;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: uppercase;
  color: var(--color-text-subtle);
  padding: 1px 5px;
  border-radius: 3px;
  background: var(--color-surface-elevated);
  border: 1px solid var(--color-border);
}

.tool-summary-row--shell .tool-summary-row__kind {
  color: #e2a053;
  border-color: rgba(226, 160, 83, 0.25);
  background: rgba(226, 160, 83, 0.08);
}

.tool-summary-row--fs .tool-summary-row__kind {
  color: var(--color-success);
  border-color: rgba(137, 209, 133, 0.25);
  background: var(--color-success-soft);
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
  font-family: var(--font-mono);
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

.tool-summary-row__dots {
  --stream-meta-dot-size: 4px;
  --stream-meta-dot-gap: 2px;
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
  padding: 0 8px 8px 30px;
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
