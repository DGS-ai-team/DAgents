<script setup>
import { computed, ref, watch, nextTick } from "vue";
import ComposerToolbar from "./ComposerToolbar.vue";
import ContextMeter from "./ContextMeter.vue";
import MessageBubble from "./MessageBubble.vue";
import StreamStatusBubble from "./StreamStatusBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import { buildStream } from "../composables/useStream.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";
import { chromeStore, inputStripRight } from "../stores/chrome.js";
import { workerStripText } from "../stores/remoteWorkers.js";
import { statusStore, statusPhaseOrder, hasStatus } from "../stores/statusLines.js";
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
  showReasoning: { type: Boolean, default: false },
  toolVerbose: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
  hitlBusyIndex: { type: Number, default: -1 },
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
});

const emit = defineEmits([
  "send",
  "cancel",
  "toggle-thinking",
  "cycle-effort",
  "approve-all",
  "reject-all",
  "approve-one",
  "reject-one",
  "user-info-submit",
  "user-info-selected",
]);

const input = ref("");
const pendingImages = ref([]);
const fileInputRef = ref(null);
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

const showCancel = computed(() => props.sending && !props.hitlBusy);
const multimodalEnabled = computed(() => Boolean(chromeStore.agentInfo?.multimodal_enabled));
const canSubmit = computed(
  () => !props.disabled && !props.sending && (!!input.value.trim() || pendingImages.value.length > 0),
);

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
  fileInputRef.value?.click();
}

async function onImageSelected(event) {
  await addImageFiles(event.target.files);
  event.target.value = "";
}

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
        <span class="chat__title-main">对话</span>
        <span class="chat__title-sub">与助手对话</span>
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
          :show-reasoning="showReasoning"
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
      </template>
      <StreamStatusBubble
        v-for="phase in activeStatusPhases"
        :key="`status-${phase}`"
        :phase="phase"
      />
    </div>

    <footer class="chat__composer">
      <div class="chat__composer-card">
        <div class="chat__composer-meta">
          <div class="chat__composer-meta-left">
            <ComposerToolbar
              class="chat__composer-toolbar"
              :thinking-supported="thinkingSupported"
              :llm-settings="llmSettings"
              :disabled="disabled || cancelling"
              @toggle-thinking="emit('toggle-thinking')"
              @cycle-effort="emit('cycle-effort')"
            />
            <div v-if="workerStrip || inputStripLeftText" class="chat__composer-meta-status">
              <span v-if="workerStrip" class="chat__worker-strip">{{ workerStrip }}</span>
              <span v-if="inputStripLeftText" class="chat__input-strip-left">{{ inputStripLeftText }}</span>
            </div>
          </div>
          <div class="chat__composer-meta-right">
            <span
              v-if="inputStripRightText"
              class="chat__input-strip-right"
              :title="inputStripRightText"
            >{{ inputStripRightText }}</span>
            <ContextMeter />
          </div>
        </div>
        <div v-if="multimodalEnabled && pendingImages.length" class="chat__pending-images">
          <div v-for="(img, idx) in pendingImages" :key="`${img.name}-${idx}`" class="chat__pending-image">
            <img class="chat__pending-image-thumb" :src="img.url" :alt="img.name" />
            <button type="button" class="chat__pending-image-remove" @click="removePendingImage(idx)">×</button>
          </div>
        </div>
        <div class="chat__composer-row">
          <input
            v-if="multimodalEnabled"
            ref="fileInputRef"
            type="file"
            accept="image/jpeg,image/png,image/gif,image/webp"
            multiple
            hidden
            @change="onImageSelected"
          />
          <button
            v-if="multimodalEnabled"
            type="button"
            class="btn btn--ghost chat__attach-btn"
            title="添加图片"
            :disabled="disabled || sending || cancelling || pendingImages.length >= 8"
            @click="openImagePicker"
          >
            🖼
          </button>
          <textarea
            ref="textareaRef"
            v-model="input"
            class="chat__textarea"
            rows="2"
            placeholder="输入消息，或向助手提问…（Enter 发送，Shift+Enter 换行；可粘贴/拖入文件路径）"
            :disabled="disabled || sending || cancelling"
            @keydown="onKeydown"
            @paste="onComposerPaste"
            @drop="onComposerDrop"
            @dragover.prevent
          />
          <button
            v-if="showCancel"
            type="button"
            class="btn btn--danger chat__composer-btn"
            :disabled="cancelling"
            @click="onCancel"
          >
            {{ cancelling ? "取消中…" : "取消" }}
          </button>
          <button
            v-else
            type="button"
            class="btn btn--primary chat__composer-btn"
            :disabled="!canSubmit"
            @click="submit"
          >
            发送
          </button>
        </div>
      </div>
    </footer>
  </section>
</template>
