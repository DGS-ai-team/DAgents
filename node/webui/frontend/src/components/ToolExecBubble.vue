<script setup>
import { computed } from "vue";
import { formatToolCallLine, formatToolResultDisplay, formatToolElapsed } from "../utils/format.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { statusStore } from "../stores/statusLines.js";
import { isReadFileTool } from "../utils/readFilePreview.js";
import { parseToolArguments, toolDisplayName, resolveToolArgumentsFromData } from "../utils/toolCalls.js";
import { toolStepUserSummary } from "../utils/toolUserLabel.js";
import ReadFileResultPreview from "./ReadFileResultPreview.vue";
import ImageResultPreview from "./ImageResultPreview.vue";
import { hasToolMedia, isShowImageTool } from "../utils/showImage.js";

const props = defineProps({
  entry: { type: Object, required: true },
  verbose: { type: Boolean, default: false },
});

const isCall = computed(() => props.entry.kind === "tool_call");
const isResult = computed(() => props.entry.kind === "tool_result");
const rejected = computed(() => !!props.entry.data?.rejected);
const interrupted = computed(() => !!props.entry.data?.interrupted);
const visual = computed(() => resolveToolVisual(props.entry));
const resultDisplay = computed(() =>
  isResult.value ? formatToolResultDisplay(props.entry, { verbose: props.verbose }) : null,
);
const toolName = computed(() => String(props.entry.data?.tool_name || props.entry.data?.name || "").trim());
const toolArgs = computed(() => resolveToolArgumentsFromData(props.entry.data));
const toolTitle = computed(() => {
  const userSummary = toolStepUserSummary({
    callEntry: isCall.value ? props.entry : null,
    resultEntry: isResult.value ? props.entry : null,
  });
  if (userSummary) return userSummary;
  return toolDisplayName(toolName.value || "tool", toolArgs.value);
});
const displayName = computed(() => toolTitle.value);
const resultDetail = computed(() => resultDisplay.value?.detail || "");
const codePreview = computed(() => props.entry.codePreview || "");
const readFilePath = computed(() => {
  const args = toolArgs.value;
  return String(args.path || args.file_path || "").trim();
});
const showReadFilePreview = computed(
  () => isResult.value && !rejected.value && isReadFileTool(toolName.value) && !!resultDetail.value && !props.verbose,
);
const showImagePreview = computed(
  () =>
    isResult.value &&
    !rejected.value &&
    !props.verbose &&
    (hasToolMedia(props.entry) || isShowImageTool(toolName.value)),
);
const elapsedLive = computed(() => {
  void statusStore.tick;
  if (!isCall.value || !props.entry.partial || !props.entry.startedAt) return "";
  return formatToolElapsed((Date.now() - props.entry.startedAt) / 1000);
});
const statusText = computed(() => {
  if (props.entry.sideEffectApplied) return "已入库";
  if (props.entry.sideEffectStale) return "已失效";
  if (isCall.value) {
    if (interrupted.value) return "已中断";
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
      <div class="tool-exec-bubble" :class="[`tool-exec-bubble--${visual.kind}`, { 'tool-exec-bubble--applied': entry.sideEffectApplied, 'tool-exec-bubble--stale': entry.sideEffectStale && !entry.sideEffectApplied }]">
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
            <span v-else class="tool-exec-status-icon tool-exec-status-icon--success" aria-hidden="true">{{ rejected || interrupted ? "−" : "✓" }}</span>
            <span>{{ statusText }}</span>
          </span>
        </div>
        <div v-if="isCall" class="tool-exec-bubble__summary">{{ formatToolCallLine(entry) }}</div>
        <ReadFileResultPreview
          v-if="showReadFilePreview"
          :path="readFilePath"
          :content="resultDetail"
        />
        <ImageResultPreview v-else-if="showImagePreview" :entry="entry" />
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
.tool-exec-bubble--applied {
  opacity: 1;
}
.tool-exec-bubble--stale {
  opacity: 0.55;
}
.tool-exec-bubble__result-detail {
  margin-top: 4px;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
