<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { formatToolCallLine, formatToolResultDisplay, formatToolElapsed } from "../utils/format.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { statusStore } from "../stores/statusLines.js";
import { isReadFileTool } from "../utils/readFilePreview.js";
import { toolDisplayName, resolveToolArgumentsFromData } from "../utils/toolCalls.js";
import { resolveToolStepPhase, toolStepUserSummary } from "../utils/toolUserLabel.js";
import ReadFileResultPreview from "./ReadFileResultPreview.vue";
import ImageResultPreview from "./ImageResultPreview.vue";
import { hasToolMedia, isShowImageTool } from "../utils/showImage.js";
import { copyText } from "../utils/clipboard.js";

const FOLD_LINE_THRESHOLD = 8;
const FOLD_CHAR_THRESHOLD = 480;
const PREVIEW_LINES = 4;

const props = defineProps({
  entry: { type: Object, required: true },
  verbose: { type: Boolean, default: false },
  /** 嵌在 ToolSummaryRow 内时隐藏重复标题/徽章 */
  embedded: { type: Boolean, default: false },
});

const outputExpanded = ref(false);
const copyState = ref("");
let copyTimer = null;

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

const isShellOutput = computed(() => toolName.value === "bash_run" || visual.value.kind === "shell");
const outputText = computed(() => {
  if (isResult.value && resultDetail.value) return resultDetail.value;
  if (isCall.value && codePreview.value) return codePreview.value;
  return "";
});
const outputLines = computed(() => {
  const t = outputText.value;
  if (!t) return [];
  return t.split(/\r?\n/);
});
const shouldFoldOutput = computed(() => {
  if (props.verbose) return false;
  if (!outputText.value) return false;
  if (showReadFilePreview.value || showImagePreview.value) return false;
  const lines = outputLines.value.length;
  return lines > FOLD_LINE_THRESHOLD || outputText.value.length > FOLD_CHAR_THRESHOLD;
});
const previewText = computed(() => {
  const lines = outputLines.value;
  if (lines.length <= PREVIEW_LINES) return outputText.value;
  return `${lines.slice(0, PREVIEW_LINES).join("\n")}\n…`;
});
const foldMeta = computed(() => {
  const n = outputLines.value.length;
  return n > 1 ? `${n} 行` : `${outputText.value.length} 字符`;
});

watch(
  () => props.entry?.id ?? props.entry?.blockId ?? outputText.value.slice(0, 40),
  () => {
    outputExpanded.value = false;
  },
);

const elapsedLive = computed(() => {
  void statusStore.tick;
  if (!isCall.value || !props.entry.partial || !props.entry.startedAt) return "";
  return formatToolElapsed((Date.now() - props.entry.startedAt) / 1000);
});
const toolPhase = computed(() =>
  resolveToolStepPhase({
    callEntry: isCall.value ? props.entry : null,
    resultEntry: isResult.value ? props.entry : null,
  }),
);
const isGenerating = computed(() => toolPhase.value === "generating");
const isInterrupted = computed(() => interrupted.value || toolPhase.value === "interrupted");
const isUnsuccessfulResult = computed(() =>
  ["denied", "rejected", "failed", "error", "cancelled", "canceled", "timed_out", "unknown"].includes(
    String(props.entry.data?.status || "").trim().toLowerCase(),
  ),
);
const statusText = computed(() => {
  if (props.entry.sideEffectApplied) return "已入库";
  if (props.entry.sideEffectStale) return "已失效";
  if (isCall.value) {
    if (isInterrupted.value) return "已中断";
    if (toolPhase.value === "generating") return elapsedLive.value ? `生成中${elapsedLive.value}` : "生成中";
    if (toolPhase.value === "background") return "后台执行中";
    if (toolPhase.value === "running") return "执行中";
    return "执行中";
  }
  const resultStatus = String(props.entry.data?.status || "").trim().toLowerCase();
  if (resultStatus === "denied" || resultStatus === "rejected") return "已拒绝";
  if (resultStatus === "failed" || resultStatus === "error") return "执行失败";
  if (resultStatus === "unknown") return "状态未知";
  if (resultStatus === "timed_out") return "已超时";
  if (resultStatus === "cancelled" || resultStatus === "canceled") return "已终止";
  if (resultStatus === "queued") return "后台执行中";
  if (resultStatus === "running") return "执行中";
  if (resultStatus === "awaiting_user") return "等待输入";
  if (rejected.value) return "已拒绝";
  if (isInterrupted.value) return "已中断";
  const bashStatus = String(props.entry.data?.content || "").match(/\[BASH_RESULT\]\s+status=([A-Za-z_]+)/i);
  if (bashStatus && bashStatus[1].toUpperCase() === "CANCELLED") return "已终止";
  if (bashStatus && bashStatus[1].toUpperCase() === "RUNNING") return "后台执行中";
  return "已完成";
});
const showStatusText = computed(() => statusText.value !== "已完成");

function toggleOutput() {
  outputExpanded.value = !outputExpanded.value;
}

function clearCopyState() {
  if (copyTimer) {
    clearTimeout(copyTimer);
    copyTimer = null;
  }
}

async function copyOutput() {
  const value = outputText.value;
  if (!value) return;
  try {
    copyState.value = (await copyText(value)) ? "已复制" : "复制失败";
  } catch {
    copyState.value = "复制失败";
  }
  clearCopyState();
  copyTimer = setTimeout(() => {
    copyState.value = "";
    copyTimer = null;
  }, 1600);
}

function downloadOutput() {
  const value = outputText.value;
  if (!value) return;
  const blob = new Blob([value], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${isShellOutput.value ? "terminal-output" : "tool-output"}-${Date.now()}.txt`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

onBeforeUnmount(clearCopyState);
</script>

<template>
  <div class="msg msg--tool-centered" :class="{ 'msg--tool-embedded': embedded }">
    <div class="msg__body msg__body--wide">
      <div
        class="tool-exec-bubble"
        :class="[
          `tool-exec-bubble--${visual.kind}`,
          {
            'tool-exec-bubble--applied': entry.sideEffectApplied,
            'tool-exec-bubble--stale': entry.sideEffectStale && !entry.sideEffectApplied,
            'tool-exec-bubble--embedded': embedded,
            'tool-exec-bubble--shell-out': isShellOutput,
          },
        ]"
      >
        <template v-if="!embedded">
          <div class="tool-exec-bubble__source">
            <span class="tool-source-badge" :class="`tool-source-badge--${visual.kind}`" :title="visual.label">
              <span class="tool-source-badge__icon" aria-hidden="true">{{ visual.icon }}</span>
              <span class="tool-source-badge__text">{{ visual.label }}</span>
            </span>
          </div>
          <div class="tool-exec-bubble__head">
            <span class="tool-exec-bubble__name">{{ toolTitle }}</span>
            <span class="tool-exec-bubble__status" role="status" :aria-label="statusText">
              <span v-if="isGenerating" class="tool-exec-spinner" aria-hidden="true" />
              <span v-else class="tool-exec-status-icon tool-exec-status-icon--success" aria-hidden="true">{{
                rejected || isInterrupted || isUnsuccessfulResult ? "−" : "✓"
              }}</span>
              <span v-if="showStatusText">{{ statusText }}</span>
            </span>
          </div>
          <div v-if="isCall" class="tool-exec-bubble__summary">{{ formatToolCallLine(entry) }}</div>
        </template>

        <ReadFileResultPreview
          v-if="showReadFilePreview"
          :path="readFilePath"
          :content="resultDetail"
        />
        <ImageResultPreview v-else-if="showImagePreview" :entry="entry" />

        <div v-else-if="outputText && !verbose" class="tool-output">
          <div class="tool-output__head">
            <span class="tool-output__label">{{ isShellOutput ? "shell" : "输出" }}</span>
            <div class="tool-output__actions">
              <button
                type="button"
                class="tool-output__action"
                aria-label="复制工具输出"
                title="复制工具输出"
                @click="copyOutput"
              >
                {{ copyState || "复制" }}
              </button>
              <button
                type="button"
                class="tool-output__action"
                aria-label="下载工具输出"
                title="下载工具输出"
                @click="downloadOutput"
              >下载</button>
            </div>
          </div>
          <pre
            class="tool-exec-bubble__code tool-exec-bubble__result-detail"
            :class="{ 'tool-output__pre--shell': isShellOutput }"
          >{{ shouldFoldOutput && !outputExpanded ? previewText : outputText }}</pre>
          <button
            v-if="shouldFoldOutput"
            type="button"
            class="tool-output__toggle"
            @click="toggleOutput"
          >
            {{ outputExpanded ? "收起输出" : `展开输出（${foldMeta}）` }}
          </button>
        </div>

        <details v-if="verbose" class="tool-exec-bubble__details" open>
          <summary>原始输出</summary>
          <div class="tool-exec-bubble__details-body">
            <div v-if="outputText" class="tool-output__head">
              <span class="tool-output__label">原始输出</span>
              <div class="tool-output__actions">
            <button
              type="button"
              class="tool-output__action"
              aria-label="复制原始工具输出"
              title="复制原始工具输出"
              @click="copyOutput"
            >
              {{ copyState || "复制" }}
            </button>
            <button
              type="button"
              class="tool-output__action"
              aria-label="下载原始工具输出"
              title="下载原始工具输出"
              @click="downloadOutput"
            >下载</button>
              </div>
            </div>
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
.tool-exec-bubble--embedded {
  padding: 6px 0 0;
  gap: 6px;
}
.tool-output {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.tool-output__toggle {
  align-self: flex-start;
  border: 0;
  background: transparent;
  color: var(--color-primary);
  font-size: 11.5px;
  padding: 0;
  cursor: pointer;
}
.tool-output__toggle:hover {
  text-decoration: underline;
}
.tool-output__pre--shell {
  max-height: none;
}
</style>
