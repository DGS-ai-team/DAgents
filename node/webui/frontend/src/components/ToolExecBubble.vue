<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { formatToolCallLine, formatToolResultDisplay, formatToolElapsed } from "../utils/format.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { statusStore } from "../stores/statusLines.js";
import { isReadFileTool } from "../utils/readFilePreview.js";
import { toolDisplayName } from "../utils/toolCalls.js";
import { resolveToolStepPhase, toolStepUserSummary } from "../utils/toolUserLabel.js";
import ReadFileResultPreview from "./ReadFileResultPreview.vue";
import ImageResultPreview from "./ImageResultPreview.vue";
import { hasToolMedia, isShowImageTool } from "../utils/showImage.js";
import { copyText } from "../utils/clipboard.js";
import { buildToolCardModel } from "../utils/toolResultPresentation.js";

const FOLD_LINE_THRESHOLD = 8;
const FOLD_CHAR_THRESHOLD = 480;
const PREVIEW_LINES = 4;

const props = defineProps({
  entry: { type: Object, required: true },
  callEntry: { type: Object, default: null },
  resultEntry: { type: Object, default: null },
  verbose: { type: Boolean, default: false },
  /** 嵌在 ToolSummaryRow 内时隐藏重复标题/徽章 */
  embedded: { type: Boolean, default: false },
});

const outputExpanded = ref(false);
const copyState = ref("");
let copyTimer = null;

const isCall = computed(() => props.entry.kind === "tool_call");
const isResult = computed(() => props.entry.kind === "tool_result");
const callEntry = computed(() => props.callEntry || (isCall.value ? props.entry : null));
const resultEntry = computed(() => props.resultEntry || (isResult.value ? props.entry : null));
const card = computed(() =>
  buildToolCardModel({
    callEntry: callEntry.value,
    resultEntry: resultEntry.value,
    entry: props.entry,
  }),
);
const rejected = computed(() => !!props.entry.data?.rejected);
const interrupted = computed(() => !!props.entry.data?.interrupted);
const visual = computed(() => resolveToolVisual(props.entry));
const resultDisplay = computed(() =>
  isResult.value ? formatToolResultDisplay(props.entry, { verbose: props.verbose }) : null,
);
const toolName = computed(() => card.value.toolName);
const toolArgs = computed(() => card.value.args);
const toolTitle = computed(() => {
  const userSummary = toolStepUserSummary({
    callEntry: callEntry.value,
    resultEntry: resultEntry.value,
  });
  if (userSummary) return userSummary;
  return toolDisplayName(toolName.value || "tool", toolArgs.value);
});
const isBashDetail = computed(() => props.embedded && toolName.value === "bash_run");
const bashCommand = computed(() => {
  const commandField = card.value.inputFields.find((item) => item.label === "命令");
  return String(commandField?.value || toolArgs.value.command || "").trim();
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
  if (isBashDetail.value) {
    if (!resultEntry.value || !card.value.resultBlocks.length) return "";
    return card.value.resultBlocks
      .map((item) => `${item.label}\n${item.content}`)
      .join("\n\n");
  }
  if (card.value.resultBlocks.length) {
    return card.value.resultBlocks
      .map((item) => `${item.label}\n${item.content}`)
      .join("\n\n");
  }
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
const cardIsUnsuccessful = computed(() => ["danger", "warning"].includes(card.value.statusTone));
const statusText = computed(() => {
  if (props.entry.sideEffectApplied) return "已入库";
  if (props.entry.sideEffectStale) return "已失效";
  if (resultEntry.value && card.value.statusLabel) return `${card.value.statusLabel}${card.value.durationText}`;
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

async function copyValue(value, successLabel = "已复制") {
  if (!value) return;
  try {
    copyState.value = (await copyText(value)) ? successLabel : "复制失败";
  } catch {
    copyState.value = "复制失败";
  }
  clearCopyState();
  copyTimer = setTimeout(() => {
    copyState.value = "";
    copyTimer = null;
  }, 1600);
}

async function copyOutput() {
  await copyValue(outputText.value);
}

async function copyCommand() {
  const command = card.value.inputFields.find((item) => item.label === "命令")?.value || "";
  await copyValue(command, "命令已复制");
}

async function copyInputValue(value) {
  await copyValue(value);
}

function copyFieldLabel(label) {
  if (label === "命令") return "复制命令";
  if (label === "输入内容") return "复制输入内容";
  return `复制${label || "文本"}`;
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
                  rejected || isInterrupted || isUnsuccessfulResult || cardIsUnsuccessful ? "−" : "✓"
              }}</span>
              <span v-if="showStatusText">{{ statusText }}</span>
            </span>
          </div>
          <div v-if="isCall" class="tool-exec-bubble__summary">{{ formatToolCallLine(entry) }}</div>
        </template>

        <section v-if="isBashDetail" class="tool-card__section tool-card__section--input">
          <div class="tool-card__section-title">输入</div>
          <div class="tool-card__code-panel">
            <div class="tool-card__code-panel-heading">
              <span class="tool-card__code-panel-label">命令</span>
              <button
                v-if="bashCommand"
                type="button"
                class="tool-output__action"
                aria-label="复制执行命令"
                title="复制执行命令"
                @click.stop="copyCommand"
              >{{ copyState || "复制命令" }}</button>
            </div>
            <pre class="tool-exec-bubble__code tool-card__code-block">{{ bashCommand || "—" }}</pre>
          </div>
        </section>

        <section v-else-if="card.inputFields.length" class="tool-card__section tool-card__section--input">
          <div class="tool-card__section-title">输入</div>
          <dl class="tool-card__fields tool-card__fields--compact">
            <template v-for="item in card.inputFields" :key="`input-${item.label}`">
              <div v-if="item.kind === 'code'" class="tool-card__code-panel">
                <div class="tool-card__code-panel-heading">
                  <span class="tool-card__code-panel-label">{{ item.label }}</span>
                  <button
                    v-if="item.value"
                    type="button"
                    class="tool-output__action"
                    :aria-label="copyFieldLabel(item.label)"
                    :title="copyFieldLabel(item.label)"
                    @click.stop="copyInputValue(item.value)"
                  >{{ copyState || copyFieldLabel(item.label) }}</button>
                </div>
                <pre class="tool-exec-bubble__code tool-card__code-block">{{ item.value || "—" }}</pre>
              </div>
              <div v-else class="tool-card__field">
                <dt>{{ item.label }}</dt>
                <dd :class="`tool-card__value tool-card__value--${item.kind}`">
                  <pre v-if="item.kind === 'multiline'">{{ item.value }}</pre>
                  <span v-else>{{ item.value }}</span>
                </dd>
              </div>
            </template>
          </dl>
        </section>

        <section
          v-if="!isBashDetail && resultEntry && card.resultFields.length"
          class="tool-card__section tool-card__section--result"
        >
          <div class="tool-card__section-title">结果</div>
          <dl v-if="card.resultFields.length" class="tool-card__fields tool-card__fields--compact">
            <template v-for="item in card.resultFields" :key="`result-${item.label}`">
              <div v-if="item.kind === 'code'" class="tool-card__code-panel">
                <div class="tool-card__code-panel-heading">
                  <span class="tool-card__code-panel-label">{{ item.label }}</span>
                  <button
                    v-if="item.value"
                    type="button"
                    class="tool-output__action"
                    :aria-label="copyFieldLabel(item.label)"
                    :title="copyFieldLabel(item.label)"
                    @click.stop="copyInputValue(item.value)"
                  >{{ copyState || copyFieldLabel(item.label) }}</button>
                </div>
                <pre class="tool-exec-bubble__code tool-card__code-block">{{ item.value || "—" }}</pre>
              </div>
              <div v-else class="tool-card__field">
                <dt>{{ item.label }}</dt>
                <dd :class="`tool-card__value tool-card__value--${item.kind}`">{{ item.value }}</dd>
              </div>
            </template>
          </dl>
        </section>

        <ReadFileResultPreview
          v-if="showReadFilePreview"
          :path="readFilePath"
          :content="resultDetail"
        />
        <ImageResultPreview v-if="showImagePreview" :entry="entry" />

        <section
          v-if="outputText || (isBashDetail && resultEntry)"
          class="tool-card__section tool-card__section--output tool-output"
        >
          <div class="tool-card__section-title">输出</div>
          <div class="tool-card__code-panel">
            <div class="tool-card__code-panel-heading">
              <span class="tool-card__code-panel-label">文本</span>
              <button
                type="button"
                class="tool-output__action"
                aria-label="复制工具输出"
                title="复制工具输出"
                @click="copyOutput"
              >
                {{ copyState || "复制输出" }}
              </button>
            </div>
            <pre
              class="tool-exec-bubble__code tool-exec-bubble__result-detail"
              :class="{ 'tool-output__pre--shell': isShellOutput }"
            >{{ shouldFoldOutput && !outputExpanded ? previewText : outputText || "（无输出）" }}</pre>
          </div>
          <button
            v-if="shouldFoldOutput"
            type="button"
            class="tool-output__toggle"
            @click="toggleOutput"
          >
            {{ outputExpanded ? "收起输出" : `展开输出（${foldMeta}）` }}
          </button>
        </section>
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
  padding: 8px 0 0;
  gap: 0;
}
.tool-card__section {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--color-border);
}
.tool-card__section:first-of-type {
  margin-top: 0;
  padding-top: 0;
  border-top: 0;
}
.tool-card__section-title {
  color: var(--color-text-muted);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.04em;
  line-height: 1.3;
}
.tool-card__section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 20px;
}
.tool-card__code-block {
  width: 100%;
  box-sizing: border-box;
}
.tool-card__fields {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0;
  min-width: 0;
}
.tool-card__fields--compact {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
}
.tool-card__fields--compact .tool-card__code-panel {
  grid-column: 1 / -1;
}
.tool-card__field {
  display: grid;
  grid-template-columns: minmax(44px, 72px) minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  min-width: 0;
  padding: 7px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  font-size: 12px;
  line-height: 1.45;
}
.tool-card__field dt {
  color: var(--color-text-subtle);
  font-weight: 500;
  line-height: 1.45;
}
.tool-card__field dd {
  margin: 0;
  min-width: 0;
  color: var(--color-text);
  overflow-wrap: anywhere;
}
.tool-card__value--code,
.tool-card__value--mono {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Consolas, monospace);
}
.tool-card__value--code {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 5px;
}
.tool-card__value--code,
.tool-card__value--multiline {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.tool-card__value--error {
  color: var(--color-danger, #b42318) !important;
}
.tool-card__value pre {
  margin: 0;
  white-space: inherit;
  font: inherit;
}
.tool-card__code-panel {
  display: flex;
  flex-direction: column;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-code-bg);
}
.tool-card__code-panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 24px;
  padding: 3px 7px 3px 8px;
  border-bottom: 1px solid var(--color-border);
  background: var(--color-surface-muted);
}
.tool-card__code-panel-label {
  min-width: 0;
  color: var(--color-text-subtle);
  font-size: 10.5px;
  line-height: 1.35;
}
.tool-card__code-panel > .tool-exec-bubble__code {
  width: 100%;
  box-sizing: border-box;
  border: 0;
  border-radius: 0;
  background: transparent;
  padding: 8px 10px 9px;
}
.tool-output {
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
@media (max-width: 640px) {
  .tool-card__fields--compact {
    grid-template-columns: minmax(0, 1fr);
  }
  .tool-card__fields--compact .tool-card__code-panel {
    grid-column: auto;
  }
  .tool-card__field {
    grid-template-columns: minmax(42px, 64px) minmax(0, 1fr);
    gap: 8px;
  }
}
</style>
