<script setup>
import { computed, onUnmounted, ref, watch } from "vue";
import { formatToolElapsed } from "../utils/format.js";
import { toolStepIsInProgress, toolStepStatusText, toolStepUserSummary } from "../utils/toolUserLabel.js";
import { entryMedia } from "../utils/showImage.js";
import ToolExecBubble from "./ToolExecBubble.vue";

const props = defineProps({
  callEntry: { type: Object, default: null },
  resultEntry: { type: Object, default: null },
  verbose: { type: Boolean, default: false },
});

const expanded = ref(false);
/** 本地时钟：tool_call 长参数期间 statusStore 可能已停表，不能依赖它刷新耗时。 */
const nowTick = ref(Date.now());
let tickTimer = null;

const summary = computed(() => toolStepUserSummary({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
const inProgress = computed(() => toolStepIsInProgress({ callEntry: props.callEntry, resultEntry: props.resultEntry }));
const detailEntry = computed(() => props.resultEntry || props.callEntry);
const inlineMedia = computed(() => entryMedia(props.resultEntry));
/** 长参数 tool_call 期间正文未必可见增长；用耗时 + 动画表达「仍在生成」。 */
const status = computed(() => {
  void nowTick.value;
  const base = toolStepStatusText({ callEntry: props.callEntry, resultEntry: props.resultEntry });
  if (!props.callEntry?.partial || !props.callEntry?.startedAt) return base;
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
</script>

<template>
  <div class="tool-summary-row" :class="{ 'tool-summary-row--expanded': expanded, 'tool-summary-row--progress': inProgress }">
    <button type="button" class="tool-summary-row__head" @click="toggle">
      <span class="tool-summary-row__icon" aria-hidden="true">
        <span v-if="inProgress" class="tool-exec-spinner" />
        <span v-else>✓</span>
      </span>
      <span class="tool-summary-row__text">{{ summary }}</span>
      <span v-if="inlineMedia.length && !expanded" class="tool-summary-row__thumb-wrap">
        <img
          class="tool-summary-row__thumb"
          :src="inlineMedia[0].url"
          :alt="inlineMedia[0].label || '图片'"
          loading="lazy"
        />
      </span>
      <span v-if="status" class="tool-summary-row__status">
        <span v-if="inProgress" class="msg__meta-dots tool-summary-row__dots" aria-hidden="true">
          <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
        </span>
        {{ status }}
      </span>
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
  border-color: rgba(99, 102, 241, 0.4);
  animation: tool-summary-progress-pulse 1.6s ease-in-out infinite;
}

@keyframes tool-summary-progress-pulse {
  0%,
  100% {
    border-color: rgba(99, 102, 241, 0.28);
  }
  50% {
    border-color: rgba(99, 102, 241, 0.55);
  }
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
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  font-size: 13px;
}

.tool-summary-row__text {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 13px;
  color: var(--color-text);
  line-height: 1.4;
}

.tool-summary-row__thumb-wrap {
  flex: 0 0 auto;
  line-height: 0;
}

.tool-summary-row__thumb {
  width: 40px;
  height: 40px;
  object-fit: cover;
  border-radius: 6px;
  border: 1px solid var(--color-border);
  background: #fff;
}

.tool-summary-row__status {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.tool-summary-row__dots {
  --stream-meta-dot-size: 4px;
  --stream-meta-dot-gap: 2px;
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
