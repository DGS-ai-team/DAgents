<script setup>
import { computed } from "vue";
import { formatToolCallLine, formatToolResultDisplay, formatToolElapsed } from "../utils/format.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { statusStore } from "../stores/statusLines.js";
import { isReadFileTool } from "../utils/readFilePreview.js";
import { parseToolArguments } from "../utils/toolCalls.js";
import ReadFileResultPreview from "./ReadFileResultPreview.vue";

const props = defineProps({
  entry: { type: Object, required: true },
  verbose: { type: Boolean, default: false },
});

const isCall = computed(() => props.entry.kind === "tool_call");
const isResult = computed(() => props.entry.kind === "tool_result");
const rejected = computed(() => !!props.entry.data?.rejected);
const visual = computed(() => resolveToolVisual(props.entry));
const resultDisplay = computed(() =>
  isResult.value ? formatToolResultDisplay(props.entry, { verbose: props.verbose }) : null,
);
const displayName = computed(() => {
  if (isResult.value && resultDisplay.value) return resultDisplay.value.headline;
  return props.entry.summary || props.entry.data?.summary || props.entry.data?.tool_name || props.entry.data?.name || "tool";
});
const resultDetail = computed(() => resultDisplay.value?.detail || "");
const codePreview = computed(() => props.entry.codePreview || "");
const toolName = computed(() => String(props.entry.data?.tool_name || props.entry.data?.name || "").trim());
const readFilePath = computed(() => {
  const args = parseToolArguments(props.entry.data?.arguments ?? props.entry.data?.raw_arguments);
  return String(args.path || args.file_path || "").trim();
});
const showReadFilePreview = computed(
  () => isResult.value && !rejected.value && isReadFileTool(toolName.value) && !!resultDetail.value && !props.verbose,
);
const elapsedLive = computed(() => {
  void statusStore.tick;
  if (!isCall.value || !props.entry.partial || !props.entry.startedAt) return "";
  return formatToolElapsed((Date.now() - props.entry.startedAt) / 1000);
});
const statusText = computed(() => {
  if (isCall.value) {
    if (props.entry.partial) return elapsedLive.value ? `生成中${elapsedLive.value}` : "生成中";
    return "待执行";
  }
  if (rejected.value) return "已拒绝";
  return "已完成";
});
</script>

<template>
  <div class="msg msg--tool-centered">
    <div class="msg__body msg__body--wide">
      <div class="tool-exec-bubble" :class="`tool-exec-bubble--${visual.kind}`">
        <div class="tool-exec-bubble__source">
          <span class="tool-source-badge" :class="`tool-source-badge--${visual.kind}`" :title="visual.label">
            <span class="tool-source-badge__icon" aria-hidden="true">{{ visual.icon }}</span>
            <span class="tool-source-badge__text">{{ visual.label }}</span>
          </span>
        </div>
        <div class="tool-exec-bubble__head">
          <span class="tool-exec-bubble__name">{{ displayName }}</span>
          <span class="tool-exec-bubble__status">
            <span v-if="isCall && entry.partial" class="tool-exec-spinner" aria-hidden="true" />
            <span v-else class="tool-exec-status-icon tool-exec-status-icon--success" aria-hidden="true">{{ rejected ? "−" : "✓" }}</span>
            <span>{{ statusText }}</span>
          </span>
        </div>
        <div v-if="isCall" class="tool-exec-bubble__summary">{{ formatToolCallLine(entry) }}</div>
        <ReadFileResultPreview
          v-if="showReadFilePreview"
          :path="readFilePath"
          :content="resultDetail"
        />
        <pre
          v-else-if="isResult && resultDetail && !verbose"
          class="tool-exec-bubble__code tool-exec-bubble__result-detail"
        >{{ resultDetail }}</pre>
        <pre v-if="isCall && codePreview && !verbose" class="tool-exec-bubble__code">{{ codePreview }}</pre>
        <details v-if="verbose" class="tool-exec-bubble__details">
          <summary>查看详情</summary>
          <div class="tool-exec-bubble__details-body">
            <pre class="tool-card__args tool-card__args--compact">{{
              entry.data?.content || entry.data?.raw_arguments || entry.data?.output || JSON.stringify(entry.data, null, 2)
            }}</pre>
          </div>
        </details>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tool-exec-bubble__result-detail {
  margin-top: 4px;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
