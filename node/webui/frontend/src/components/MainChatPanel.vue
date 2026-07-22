<script setup>
import { computed, ref, watch, nextTick } from "vue";
import ComposerToolbar from "./ComposerToolbar.vue";
import ContextMeter from "./ContextMeter.vue";
import MessageBubble from "./MessageBubble.vue";
import StreamStatusBubble from "./StreamStatusBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import MemoryConflictBubble from "./MemoryConflictBubble.vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import { buildStream } from "../composables/useStream.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";
import { chromeStore, inputStripRight } from "../stores/chrome.js";
import { workerStripText } from "../stores/remoteWorkers.js";
import { toolJobsStripText } from "../stores/toolJobs.js";
import { statusStore, statusPhaseOrder, hasStatus } from "../stores/statusLines.js";
import { transcriptStore } from "../stores/transcript.js";
import { deriveActivityFromTranscript } from "../utils/workspaceActivity.js";
import { getDesktopClipboardFiles } from "../api/desktop.js";
import {
  formatPathsForComposer,
  mergePathInsertion,
  pathsFromFileList,
  pathsFromUriList,
  shouldResolvePathsViaShell,
} from "../utils/filePathPaste.js";

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
const userInfoSelected = ref(0);
const followTail = ref(true);
const SCROLL_TAIL_THRESHOLD = 48;

const stream = computed(() => buildStream(props.entries, props.hitlQueue));

const activeStatusPhases = computed(() => {
  void statusStore.tick;
  return statusPhaseOrder.filter((phase) => {
    if (!hasStatus(phase)) return false;
    if (phase === "thinking" && hasStreamingKind("reasoning")) return false;
    if (phase === "prefilling" && hasStreamingTextContent()) return false;
    return true;
  });
});

const pendingApprovals = computed(() =>
  props.hitlQueue
    .filter((h) => h.kind === "approval")
    .reduce((n, h) => n + extractToolApprovals(h.data).length, 0),
);

const workerStrip = computed(() => workerStripText());
const toolJobsStrip = computed(() => toolJobsStripText());

const inputStripLeftText = computed(() => {
  if (props.cancelling) return "正在取消…";
  if (pendingApprovals.value > 0) {
    return `${pendingApprovals.value} 个工具待审批`;
  }
  if (props.hitlQueue.length > 1) {
    return `HITL 队列 ${props.hitlQueue.length}`;
  }
  return "";
});

const inputStripRightText = computed(() => inputStripRight());

const activitySnap = computed(() => deriveActivityFromTranscript(transcriptStore.entries));
const activityFileCount = computed(() => activitySnap.value.file_count || 0);
const activityCmdCount = computed(() => activitySnap.value.command_count || 0);
const showActivityPill = computed(() => activityFileCount.value > 0 || activityCmdCount.value > 0);

function openActivityRail() {
  chromeStore.panel = "activity";
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

const attachDisabled = computed(() => props.disabled || props.sending || props.cancelling);
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
  const parts = [stream.value.length, props.hitlQueue.length, activeStatusPhases.value.length];
  for (const entry of props.entries) {
    parts.push(entry.id, entry.kind, (entry.text || "").length, entry.streaming ? 1 : 0);
  }
  for (const hitl of props.hitlQueue) {
    parts.push(hitl.kind, hitl.data?.request_id || hitl.data?.approval_id || "");
  }
  return parts.join("\0");
});

function maybeScrollToTail() {
  if (!followTail.value) return;
  nextTick(() => {
    const el = streamRef.value;
    if (el) el.scrollTop = el.scrollHeight;
  });
}

watch(tailContentKey, () => {
  maybeScrollToTail();
});

function onStreamScroll() {
  const el = streamRef.value;
  if (!el) return;
  followTail.value = el.scrollHeight - el.scrollTop - el.clientHeight <= SCROLL_TAIL_THRESHOLD;
}

function scrollToTail() {
  followTail.value = true;
  maybeScrollToTail();
}

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
        <span v-if="pendingApprovals > 0" class="pill pill--warn">{{ pendingApprovals }} 待审批</span>
      </div>
    </header>

    <div ref="streamRef" class="chat__stream" @scroll="onStreamScroll">
      <div v-if="!stream.length" class="chat__empty">
        <div class="chat__empty-inner">
          <div class="chat__empty-title">开始对话</div>
          <div class="chat__empty-hint">输入消息与 Agent 协作，或在设置 › 帮助中查看命令</div>
        </div>
      </div>
      <template v-for="item in stream" :key="item.key">
        <MessageBubble
          v-if="['user', 'assistant', 'reasoning'].includes(item.kind)"
          :entry="item.entry"
        />
        <ToolSummaryRow
          v-else-if="item.kind === 'tool_step'"
          :call-entry="item.callEntry"
          :result-entry="item.resultEntry"
          :verbose="toolVerbose"
        />
        <ApprovalBubble
          v-else-if="item.kind === 'approval'"
          :data="item.hitl.data"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @approve-all="emit('approve-all', item.hitlIndex)"
          @reject-all="emit('reject-all', item.hitlIndex)"
          @approve-one="(id) => emit('approve-one', { index: item.hitlIndex, callId: id })"
          @reject-one="(id) => emit('reject-one', { index: item.hitlIndex, callId: id })"
        />
        <UserInfoBubble
          v-else-if="item.kind === 'user_information'"
          :data="item.hitl.data"
          :selected="userInfoSelected"
          @update:selected="(v) => { userInfoSelected = v; emit('user-info-selected', v); }"
          @submit="emit('user-info-submit', item.hitlIndex)"
        />
        <MemoryConflictBubble
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

    <footer class="chat__composer">
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
            placeholder="输入消息，或向助手提问…"
            :disabled="disabled || sending || cancelling"
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
            title="取消"
            aria-label="取消"
            :disabled="cancelling"
            @click="onCancel"
          >
            <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
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

      <div class="chat__composer-statusline">
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
          <span v-if="workerStrip" class="chat__worker-strip">{{ workerStrip }}</span>
          <span v-if="toolJobsStrip" class="chat__worker-strip">{{ toolJobsStrip }}</span>
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
