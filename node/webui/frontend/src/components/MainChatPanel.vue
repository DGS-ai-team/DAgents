<script setup>
import { computed, ref, shallowRef, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import ComposerToolbar from "./ComposerToolbar.vue";
import ContextMeter from "./ContextMeter.vue";
import MessageBubble from "./MessageBubble.vue";
import StreamStatusBubble from "./StreamStatusBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import MemoryConflictBubble from "./MemoryConflictBubble.vue";
import ScrollToTailButton from "./ScrollToTailButton.vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import ToolGroupRow from "./ToolGroupRow.vue";
import { buildStream } from "../composables/useStream.js";
import { groupConsecutiveToolSteps } from "../utils/streamDisplay.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";
import { chromeStore, inputStripRight } from "../stores/chrome.js";
import { workerStripText } from "../stores/remoteWorkers.js";
import { toolJobsStore } from "../stores/toolJobs.js";
import { statusStore, statusPhaseOrder, hasStatus, formatStatusText } from "../stores/statusLines.js";
import { transcriptStore } from "../stores/transcript.js";
import { deriveActivityFromTranscript } from "../utils/workspaceActivity.js";
import {
  measureSync,
  updateRuntimeMetrics,
} from "../stores/performanceDiagnostics.js";
import { getDesktopClipboardFiles } from "../api/desktop.js";
import {
  formatPathsForComposer,
  mergePathInsertion,
  pathsFromFileList,
  pathsFromUriList,
  shouldResolvePathsViaShell,
} from "../utils/filePathPaste.js";
import { createFollowTailController, distanceFromTail } from "../utils/scrollTail.js";
const props = defineProps({
  entries: { type: Array, default: () => [] },
  hitlQueue: { type: Array, default: () => [] },
  toolVerbose: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
  hitlBusyIndex: { type: Number, default: -1 },
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
  agentTitle: { type: String, default: "" },
  error: { type: String, default: "" },
});

const emit = defineEmits([
  "send",
  "cancel",
  "toggle-thinking",
  "cycle-effort",
  "switch-profile",
  "approve-all",
  "reject-all",
  "approve-one",
  "reject-one",
  "user-info-submit",
  "user-info-selected",
  "memory-conflict-decide",
  "memory-conflict-cancel",
  "open-activity",
]);

const thinkingEnabled = computed(() => {
  const t = String(props.llmSettings?.thinking || "").toLowerCase();
  return !["disabled", "off"].includes(t);
});
const thinkingEffort = computed(() =>
  String(props.llmSettings?.reasoning_effort || "high").toLowerCase(),
);

const input = ref("");
const pendingImages = ref([]);
const imageInputRef = ref(null);
const attachInputRef = ref(null);
const textareaRef = ref(null);
const streamRef = ref(null);
const userInfoSelected = ref([]);

function onUserInfoSelected(next) {
  userInfoSelected.value = Array.isArray(next) ? [...next] : Number(next);
  emit("user-info-selected", userInfoSelected.value);
}

const scrollTail = createFollowTailController();
const showScrollToTail = ref(false);
let streamResizeObserver = null;
const MAX_RENDERED_STREAM_ITEMS = 180;
const streamWindowStart = ref(0);
let previousStreamItemCount = 0;
let streamBuildHandle = null;

const stream = shallowRef([]);

/**
 * 流式 token、工具轮询和 SSE 状态可能在同一帧内连续变化。
 * 将展示层重建合并到下一帧，避免每个 token 都重复构建消息列表和测量布局。
 */
const streamInputKey = computed(() => {
  const parts = [props.entries.length, props.hitlQueue.length];
  for (const entry of props.entries) {
    parts.push(
      entry.id,
      entry.kind,
      (entry.text || "").length,
      entry.streaming ? 1 : 0,
      entry.partial ? 1 : 0,
      String(entry.summary || "").length,
      String(entry.data?.content || "").length,
      String(entry.data?.arguments || "").length,
    );
  }
  for (const hitl of props.hitlQueue) {
    parts.push(hitl.kind, hitl.data?.request_id || hitl.data?.approval_id || "");
  }
  parts.push(
    toolJobsStore.runningCallIds.join(","),
    toolJobsStore.backgroundCallIds.join(","),
  );
  return parts.join("\0");
});

function rebuildStream() {
  streamBuildHandle = null;
  const items = measureSync(
    "stream.build",
    () => buildStream(props.entries, props.hitlQueue, toolJobsStore),
    { entries: props.entries.length },
  );
  updateRuntimeMetrics({ entries: props.entries.length, streamItems: items.length });
  stream.value = items;
}

function scheduleStreamBuild() {
  if (streamBuildHandle !== null) return;
  const run = () => rebuildStream();
  if (typeof requestAnimationFrame === "function") {
    streamBuildHandle = requestAnimationFrame(run);
  } else {
    streamBuildHandle = setTimeout(run, 0);
  }
}

const displayStream = computed(() => groupConsecutiveToolSteps(stream.value));

watch(streamInputKey, scheduleStreamBuild, { immediate: true });

const renderedStream = computed(() => {
  let visible = displayStream.value;
  if (displayStream.value.length > MAX_RENDERED_STREAM_ITEMS) {
    const start = Math.min(
      Math.max(0, streamWindowStart.value),
      Math.max(0, displayStream.value.length - MAX_RENDERED_STREAM_ITEMS),
    );
    visible = displayStream.value.slice(start, start + MAX_RENDERED_STREAM_ITEMS);
  }
  updateRuntimeMetrics({
    entries: props.entries.length,
    streamItems: displayStream.value.length,
    visibleItems: visible.length,
  });
  return visible;
});
const hasEarlierStreamItems = computed(
  () => displayStream.value.length > MAX_RENDERED_STREAM_ITEMS && streamWindowStart.value > 0,
);
const earlierStreamItemCount = computed(() => Math.max(0, streamWindowStart.value));

function streamItemMemo(item) {
  const call = item?.callEntry;
  const result = item?.resultEntry;
  const entry = item?.entry;
  const hitl = item?.hitl;
  return [
    item?.key,
    item?.kind,
    item?.executionHint,
    call?.partial ? 1 : 0,
    call?.text?.length || call?.data?.arguments?.length || 0,
    result?.text?.length || result?.data?.content?.length || 0,
    entry?.text?.length || 0,
    entry?.streaming ? 1 : 0,
    hitl?.data?.request_id || hitl?.data?.approval_id || "",
    props.toolVerbose ? 1 : 0,
    props.hitlBusy ? 1 : 0,
    props.hitlBusyIndex,
    JSON.stringify(userInfoSelected.value),
  ].join("|");
}

function streamGroupMemo(item) {
  return [
    item?.key,
    item?.steps?.length || 0,
    ...(item?.steps || []).map((step) => streamItemMemo(step)),
  ].join("|");
}

const hasActiveToolStep = computed(() =>
  stream.value.some((item) => item?.kind === "tool_step" && item.executionHint === "active"),
);

const activeStatusPhases = computed(() => {
  void statusStore.tick;
  return statusPhaseOrder.filter((phase) => {
    if (!hasStatus(phase)) return false;
    if (hasActiveToolStep.value) return false;
    if (phase === "thinking" && hasStreamingKind("reasoning")) return false;
    if (phase === "prefilling" && hasStreamingTextContent()) return false;
    return true;
  });
});

const compressionStatusText = computed(() => {
  void statusStore.tick;
  const state = statusStore.phases.compression;
  if (!state) return "";
  return formatStatusText("compression", state);
});

const pendingApprovals = computed(() =>
  props.hitlQueue
    .filter((h) => h.kind === "approval")
    .reduce((n, h) => n + extractToolApprovals(h.data).length, 0),
);

const workerStrip = computed(() => workerStripText());
const runningJobCount = computed(() => toolJobsStore.running);
const backgroundJobCount = computed(() => toolJobsStore.background);

const inputStripLeftText = computed(() => {
  if (props.cancelling) return "正在取消…";
  if (props.sending) return "本轮执行中，可先编辑下一条消息";
  if (props.hitlQueue.length > 1 && pendingApprovals.value === 0) {
    return `HITL 队列 ${props.hitlQueue.length}`;
  }
  return "";
});

const inputStripRightText = computed(() => inputStripRight());
const connectionState = computed(() => {
  const state = String(chromeStore.sseStatus || "idle").toLowerCase();
  if (state === "connected") return { tone: "connected", label: "已连接", title: "实时消息连接正常" };
  if (state === "connecting") return { tone: "connecting", label: "连接中", title: "正在建立实时消息连接" };
  if (state === "reconnecting") return { tone: "reconnecting", label: "重连中", title: "实时消息连接中断，正在重连" };
  if (state === "disconnected") return { tone: "disconnected", label: "已断开", title: "实时消息连接已断开" };
  return { tone: "idle", label: "未连接", title: "实时消息连接尚未建立" };
});
const composerPlaceholder = computed(() =>
  props.sending ? "本轮执行中，可先编辑下一条消息…" : "输入消息，或向助手提问…",
);

const activitySnap = computed(() => deriveActivityFromTranscript(transcriptStore.entries));
const activityFileCount = computed(() => activitySnap.value.file_count || 0);
const activityCmdCount = computed(() => activitySnap.value.command_count || 0);
const showActivityPill = computed(() => activityFileCount.value > 0 || activityCmdCount.value > 0);

function openActivityRail() {
  emit("open-activity");
}

const showCancel = computed(() => props.sending && !props.hitlBusy);
const multimodalEnabled = computed(() => {
  if (typeof chromeStore.llmSettings?.multimodal_enabled === "boolean") {
    return chromeStore.llmSettings.multimodal_enabled;
  }
  return Boolean(chromeStore.agentInfo?.multimodal_enabled);
});
const canSubmit = computed(
  () => !props.disabled && !props.sending && (!!input.value.trim() || pendingImages.value.length > 0),
);

function resizeTextarea() {
  const el = textareaRef.value;
  if (!el) return;
  el.style.height = "0px";
  el.style.height = `${Math.min(Math.max(el.scrollHeight, 28), 160)}px`;
}

watch(input, async () => {
  await nextTick();
  resizeTextarea();
});

const allowedImageTypes = new Set(["image/jpeg", "image/png", "image/gif", "image/webp"]);
const maxImageBytes = 10 * 1024 * 1024;

function readFileAsDataURL(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(reader.error || new Error("read failed"));
    reader.readAsDataURL(file);
  });
}

async function addImageFiles(fileList) {
  if (!multimodalEnabled.value) return;
  const files = Array.from(fileList || []);
  for (const file of files) {
    if (!allowedImageTypes.has(file.type)) continue;
    if (file.size > maxImageBytes) continue;
    if (pendingImages.value.length >= 8) break;
    const url = await readFileAsDataURL(file);
    pendingImages.value.push({ name: file.name, url });
  }
}

function removePendingImage(index) {
  pendingImages.value.splice(index, 1);
}

function openImagePicker() {
  imageInputRef.value?.click();
}

function openAttachmentPicker() {
  attachInputRef.value?.click();
}

async function onImageSelected(event) {
  await addImageFiles(event.target.files);
  event.target.value = "";
}

async function onAttachmentSelected(event) {
  const files = Array.from(event.target.files || []);
  event.target.value = "";
  if (!files.length) return;

  const paths = pathsFromFileList(files);
  if (paths.length) {
    applyPathInsertion(paths);
    return;
  }
  const names = files.map((f) => f.name).filter(Boolean);
  if (names.length) applyPathInsertion(names);
}

// 生成中允许继续编辑草稿；只有真正不能输入（HITL、无 Agent、取消中）时才锁定。
const attachDisabled = computed(() => props.disabled || props.cancelling);
const imageAttachDisabled = computed(
  () => attachDisabled.value || pendingImages.value.length >= 8,
);

function buildContentParts(text, images) {
  const parts = [];
  const trimmed = String(text || "").trim();
  if (trimmed) parts.push({ type: "text", text: trimmed });
  for (const img of images) {
    if (img?.url) parts.push({ type: "image_url", image_url: { url: img.url } });
  }
  return parts;
}

/** 消息/HITL/状态条等内容变化指纹；流式 assistant 改 text 长度也会触发。 */
const tailContentKey = computed(() => {
  return measureSync("tail.key", () => {
    const parts = [stream.value.length, props.hitlQueue.length, activeStatusPhases.value.length];
    for (const entry of props.entries) {
      parts.push(entry.id, entry.kind, (entry.text || "").length, entry.streaming ? 1 : 0);
      // 工具气泡内容在 data 上，text 常为空；纳入 summary / content 长度以免漏钉尾
      if (entry.kind === "tool_call" || entry.kind === "tool_result") {
        parts.push(
          String(entry.summary || "").length,
          String(entry.data?.content || "").length,
          entry.partial ? 1 : 0,
        );
      }
    }
    for (const hitl of props.hitlQueue) {
      parts.push(hitl.kind, hitl.data?.request_id || hitl.data?.approval_id || "");
    }
    return parts.join("\0");
  }, { entries: props.entries.length });
});

function maybeScrollToTail() {
  nextTick(() => {
    measureSync("scroll.update", () => {
      scrollTail.pinIfFollowing(streamRef.value);
      updateScrollToTailVisibility();
    });
  });
}

watch(tailContentKey, () => {
  maybeScrollToTail();
});

function onStreamScroll() {
  scrollTail.onScroll(streamRef.value);
  updateScrollToTailVisibility();
}

function updateScrollToTailVisibility() {
  const el = streamRef.value;
  showScrollToTail.value = Boolean(el && !scrollTail.follow && distanceFromTail(el) > 48);
}

function scrollToTail() {
  streamWindowStart.value = Math.max(0, displayStream.value.length - MAX_RENDERED_STREAM_ITEMS);
  nextTick(() => {
    scrollTail.forcePin(streamRef.value);
    updateScrollToTailVisibility();
  });
}

function loadEarlierStreamItems() {
  const el = streamRef.value;
  const beforeHeight = el?.scrollHeight || 0;
  const beforeTop = el?.scrollTop || 0;
  streamWindowStart.value = Math.max(0, streamWindowStart.value - MAX_RENDERED_STREAM_ITEMS);
  nextTick(() => {
    if (el) el.scrollTop = beforeTop + Math.max(0, el.scrollHeight - beforeHeight);
  });
}

function bindStreamResizeObserver() {
  const el = streamRef.value;
  if (!el || typeof ResizeObserver === "undefined") return;
  streamResizeObserver?.disconnect();
  streamResizeObserver = new ResizeObserver(() => {
    if (scrollTail.follow) scrollTail.pinIfFollowing(el);
    updateScrollToTailVisibility();
  });
  const inner = el.firstElementChild || el;
  streamResizeObserver.observe(inner);
  streamResizeObserver.observe(el);
}

onMounted(() => {
  bindStreamResizeObserver();
});

onBeforeUnmount(() => {
  streamResizeObserver?.disconnect();
  streamResizeObserver = null;
  if (streamBuildHandle !== null) {
    if (typeof cancelAnimationFrame === "function") cancelAnimationFrame(streamBuildHandle);
    else clearTimeout(streamBuildHandle);
    streamBuildHandle = null;
  }
});

watch(streamRef, (el) => {
  if (el) bindStreamResizeObserver();
});

watch(displayStream, (items) => {
  if (scrollTail.follow && items.length > previousStreamItemCount) {
    streamWindowStart.value = Math.max(0, items.length - MAX_RENDERED_STREAM_ITEMS);
  } else {
    streamWindowStart.value = Math.min(
      streamWindowStart.value,
      Math.max(0, items.length - MAX_RENDERED_STREAM_ITEMS),
    );
  }
  previousStreamItemCount = items.length;
}, { immediate: true });

async function submit() {
  const text = input.value.trim();
  const images = pendingImages.value.slice();
  if ((!text && !images.length) || props.disabled || props.sending) return;
  scrollToTail();
  const contentParts = buildContentParts(text, images);
  emit("send", { text, contentParts, images: images.map((img) => img.url) });
  input.value = "";
  pendingImages.value = [];
}

function onCancel() {
  if (!showCancel.value || props.cancelling) return;
  emit("cancel");
}

function onKeydown(e) {
  if (e.key === "Enter" && !e.shiftKey) {
    // 本轮执行中允许用户继续编辑下一条草稿，Enter 在此时插入换行，避免
    // 看起来像发送成功但实际上被 turn gate 静默拦截。
    if (props.sending) return;
    e.preventDefault();
    submit();
  }
}

function applyPathInsertion(paths) {
  const formatted = formatPathsForComposer(paths);
  if (!formatted) return false;
  const el = textareaRef.value;
  const start = el?.selectionStart ?? input.value.length;
  const end = el?.selectionEnd ?? start;
  const { value, cursor } = mergePathInsertion(input.value, formatted, { start, end });
  input.value = value;
  nextTick(() => {
    if (el) {
      el.selectionStart = cursor;
      el.selectionEnd = cursor;
      el.focus();
    }
  });
  return true;
}

async function resolveFilePaths({ text, files, uriList }) {
  let paths = pathsFromFileList(files);
  if (!paths.length && uriList) {
    paths = pathsFromUriList(uriList);
  }
  if (!paths.length && shouldResolvePathsViaShell({ text, files })) {
    try {
      const data = await getDesktopClipboardFiles();
      paths = data?.paths || [];
    } catch {
      /* Shell 不可用 */
    }
  }
  return paths;
}

function isImageOnlyFileList(fileList) {
  const files = Array.from(fileList || []);
  return files.length > 0 && files.every((f) => allowedImageTypes.has(f.type));
}

async function onComposerPaste(e) {
  const dt = e.clipboardData;
  if (!dt) return;
  const text = dt.getData("text/plain") || "";
  const files = dt.files;
  if (multimodalEnabled.value && isImageOnlyFileList(files)) {
    e.preventDefault();
    await addImageFiles(files);
    return;
  }
  const paths = await resolveFilePaths({
    text,
    files,
    uriList: dt.getData("text/uri-list") || dt.getData("text/plain"),
  });
  if (paths.length && applyPathInsertion(paths)) {
    e.preventDefault();
  }
}

async function onComposerDrop(e) {
  const dt = e.dataTransfer;
  if (!dt) return;
  e.preventDefault();
  const paths = await resolveFilePaths({
    text: "",
    files: dt.files,
    uriList: dt.getData("text/uri-list") || dt.getData("text/plain"),
  });
  if (paths.length) {
    applyPathInsertion(paths);
    return;
  }
  if (multimodalEnabled.value && isImageOnlyFileList(dt.files)) {
    await addImageFiles(dt.files);
  }
}

defineExpose({
  setDraft(text) {
    input.value = String(text || "");
  },
  scrollToTail,
});
</script>

<template>
  <section class="panel panel--flex chat">
    <header class="chat__header">
      <div class="chat__title">
        <span class="chat__title-main">{{ agentTitle || "助手" }}</span>
      </div>
      <div class="chat__header-meta">
        <span
          class="chat__connection"
          :class="`chat__connection--${connectionState.tone}`"
          :title="connectionState.title"
          role="status"
          aria-live="polite"
        >
          <span class="chat__connection-dot" aria-hidden="true" />
          <span>{{ connectionState.label }}</span>
        </span>
        <span v-if="pendingApprovals > 0" class="pill pill--warn">{{ pendingApprovals }} 待审批</span>
      </div>
    </header>

    <div class="chat__stream-wrap">
      <div
        ref="streamRef"
        class="chat__stream"
        role="log"
        aria-label="消息记录"
        :aria-busy="sending || cancelling"
        @scroll="onStreamScroll"
      >
      <div v-if="!stream.length" class="chat__empty">
        <div class="chat__empty-inner">
          <div class="chat__empty-title">开始对话</div>
          <div class="chat__empty-hint">输入消息与 Agent 协作，或在设置 › 帮助中查看命令</div>
        </div>
      </div>
      <button
        v-if="hasEarlierStreamItems"
        type="button"
        class="chat__load-earlier"
        @click="loadEarlierStreamItems"
      >
        加载更早的 {{ earlierStreamItemCount }} 条消息
      </button>
      <template v-for="item in renderedStream" :key="item.key">
        <MessageBubble
          v-memo="[streamItemMemo(item)]"
          v-if="['user', 'assistant', 'reasoning', 'system'].includes(item.kind)"
          :entry="item.entry"
        />
        <ToolSummaryRow
          v-memo="[streamItemMemo(item)]"
          v-else-if="item.kind === 'tool_step'"
          :call-entry="item.callEntry"
          :result-entry="item.resultEntry"
          :execution-hint="item.executionHint"
          :verbose="toolVerbose"
        />
        <ToolGroupRow
          v-memo="[streamGroupMemo(item), toolVerbose]"
          v-else-if="item.kind === 'tool_group'"
          :steps="item.steps"
          :verbose="toolVerbose"
        />
        <ApprovalBubble
          v-memo="[streamItemMemo(item)]"
          v-else-if="item.kind === 'approval'"
          :data="item.hitl.data"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @approve-all="emit('approve-all', item.hitlIndex)"
          @reject-all="emit('reject-all', item.hitlIndex)"
          @approve-one="(id) => emit('approve-one', { index: item.hitlIndex, callId: id })"
          @reject-one="(id) => emit('reject-one', { index: item.hitlIndex, callId: id })"
        />
        <UserInfoBubble
          v-memo="[streamItemMemo(item)]"
          v-else-if="item.kind === 'user_information'"
          :data="item.hitl.data"
          :selected="userInfoSelected"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @update:selected="onUserInfoSelected"
          @submit="emit('user-info-submit', item.hitlIndex)"
        />
        <MemoryConflictBubble
          v-memo="[streamItemMemo(item)]"
          v-else-if="item.kind === 'memory_conflict'"
          :data="item.hitl.data"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @decide="(decision) => emit('memory-conflict-decide', { index: item.hitlIndex, decision })"
          @cancel="emit('memory-conflict-cancel', item.hitlIndex)"
        />
      </template>
      <StreamStatusBubble
        v-for="phase in activeStatusPhases"
        :key="`status-${phase}`"
        :phase="phase"
      />
      </div>
      <ScrollToTailButton :visible="showScrollToTail" @click="scrollToTail" />
    </div>

    <footer class="chat__composer">
      <div v-if="error" class="chat__composer-alert" role="alert" aria-live="polite">
        <span class="chat__composer-alert-icon" aria-hidden="true">!</span>
        <span>{{ error }}</span>
      </div>
      <div class="chat__composer-pill">
        <input
          v-if="multimodalEnabled"
          ref="imageInputRef"
          type="file"
          accept="image/jpeg,image/png,image/gif,image/webp"
          multiple
          hidden
          @change="onImageSelected"
        />
        <input
          ref="attachInputRef"
          type="file"
          multiple
          hidden
          @change="onAttachmentSelected"
        />

        <div class="chat__composer-pill-left">
          <button
            type="button"
            class="chat__composer-plus"
            title="添加附件（插入文件路径）"
            aria-label="添加附件"
            :disabled="attachDisabled"
            @click="openAttachmentPicker"
          >
            <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M8 3.25v9.5M3.25 8h9.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
            </svg>
          </button>
          <button
            v-if="multimodalEnabled"
            type="button"
            class="chat__composer-plus chat__composer-plus--secondary"
            title="添加图片"
            aria-label="添加图片"
            :disabled="imageAttachDisabled"
            @click="openImagePicker"
          >
            <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <rect x="1.5" y="2.5" width="13" height="11" rx="1.5" stroke="currentColor" stroke-width="1.25" />
              <circle cx="5.25" cy="6" r="1.25" fill="currentColor" />
              <path d="M2 11.5l3.25-3 2.25 2.25L9 8l4.5 3.5" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>

        <div class="chat__composer-pill-center">
          <div v-if="multimodalEnabled && pendingImages.length" class="chat__pending-images">
            <div v-for="(img, idx) in pendingImages" :key="`${img.name}-${idx}`" class="chat__pending-image">
              <img class="chat__pending-image-thumb" :src="img.url" :alt="img.name" />
              <button type="button" class="chat__pending-image-remove" @click="removePendingImage(idx)">×</button>
            </div>
          </div>
          <textarea
            ref="textareaRef"
            v-model="input"
            class="chat__textarea"
            rows="1"
            :placeholder="composerPlaceholder"
            aria-label="输入消息"
            :disabled="disabled || cancelling"
            @keydown="onKeydown"
            @paste="onComposerPaste"
            @drop="onComposerDrop"
            @dragover.prevent
          />
        </div>

        <div class="chat__composer-pill-right">
          <ComposerToolbar
            class="chat__composer-toolbar"
            :llm-settings="llmSettings"
            :disabled="disabled || cancelling"
            @switch-profile="(id) => emit('switch-profile', id)"
          />
          <button
            v-if="showCancel"
            type="button"
            class="chat__composer-send chat__composer-send--cancel"
            :class="{ 'chat__composer-send--cancelling': cancelling }"
            :title="cancelling ? '正在停止本轮…' : '停止本轮'"
            :aria-label="cancelling ? '正在停止本轮' : '停止本轮'"
            :aria-busy="cancelling"
            :disabled="cancelling"
            @click="onCancel"
          >
            <span v-if="cancelling" class="chat__composer-stop-spinner" aria-hidden="true" />
            <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
            </svg>
          </button>
          <button
            v-else
            type="button"
            class="chat__composer-send"
            title="发送"
            aria-label="发送"
            :disabled="!canSubmit"
            @click="submit"
          >
            <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M8 12.25V3.75M8 3.75L4.5 7.25M8 3.75l3.5 3.5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>
      </div>

      <div class="chat__composer-statusline" aria-live="polite">
        <div class="chat__composer-statusline-left">
          <button
            v-if="showActivityPill"
            type="button"
            class="chat__activity-pill"
            title="打开变更与上下文"
            @click="openActivityRail"
          >
            <span class="chat__activity-pill-label">Changes</span>
            <span v-if="activityFileCount" class="chat__activity-pill-add">+{{ activityFileCount }}</span>
            <span v-if="activityCmdCount" class="chat__activity-pill-cmd">{{ activityCmdCount }} cmd</span>
          </button>
          <span
            v-if="runningJobCount > 0"
            class="chat__activity-pill chat__activity-pill--static"
            title="同步执行中的 bash"
          >
            <span class="chat__activity-pill-label">执行中</span>
            <span class="chat__activity-pill-add">{{ runningJobCount }}</span>
          </span>
          <span
            v-if="backgroundJobCount > 0"
            class="chat__activity-pill chat__activity-pill--static"
            title="后台执行中的 bash"
          >
            <span class="chat__activity-pill-label">后台</span>
            <span class="chat__activity-pill-cmd">{{ backgroundJobCount }}</span>
          </span>
          <span
            v-if="pendingApprovals > 0"
            class="chat__activity-pill chat__activity-pill--static"
            title="待审批工具"
          >
            <span class="chat__activity-pill-label">审批</span>
            <span class="chat__activity-pill-cmd">{{ pendingApprovals }}</span>
          </span>
          <span
            v-if="compressionStatusText"
            class="chat__activity-pill chat__activity-pill--static chat__activity-pill--compress"
            title="上下文压缩进行中"
          >
            <span class="chat__activity-pill-label">{{ compressionStatusText }}</span>
          </span>
          <button
            v-if="workerStrip"
            type="button"
            class="chat__worker-strip chat__worker-strip--btn"
            title="在侧栏活动中查看并取消子 Agent"
            @click="openActivityRail"
          >
            {{ workerStrip }}
          </button>
          <span v-if="inputStripLeftText" class="chat__input-strip-left">{{ inputStripLeftText }}</span>
        </div>
        <div class="chat__composer-statusline-right">
          <span
            v-if="inputStripRightText"
            class="chat__input-strip-right"
            :title="inputStripRightText"
          >{{ inputStripRightText }}</span>
          <div v-if="thinkingSupported" class="chat__statusline-thinking">
            <button
              type="button"
              class="composer-toolbar__btn"
              :class="{ 'composer-toolbar__btn--active': thinkingEnabled }"
              :title="thinkingEnabled ? '思考模式已开启，点击关闭' : '思考模式已关闭，点击开启'"
              :disabled="disabled || cancelling"
              @click="emit('toggle-thinking')"
            >
              <span class="composer-toolbar__label">{{ thinkingEnabled ? "思考" : "思考关" }}</span>
            </button>
            <button
              v-if="thinkingEnabled"
              type="button"
              class="composer-toolbar__btn composer-toolbar__btn--secondary"
              :title="`推理强度 ${thinkingEffort}，点击切换 high/max`"
              :disabled="disabled || cancelling"
              @click="emit('cycle-effort')"
            >
              <span class="composer-toolbar__label">{{ thinkingEffort }}</span>
            </button>
          </div>
          <ContextMeter />
        </div>
      </div>
    </footer>
  </section>
</template>
