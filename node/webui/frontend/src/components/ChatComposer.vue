<script setup>
import { computed, nextTick, ref, watch } from "vue";
import { useRouter } from "vue-router";
import ComposerToolbar from "./ComposerToolbar.vue";
import ContextMeter from "./ContextMeter.vue";
import McpStatusIndicator from "./McpStatusIndicator.vue";
import SkillsStatusIndicator from "./SkillsStatusIndicator.vue";
import TerminalSessionIndicator from "./TerminalSessionIndicator.vue";
import { chromeStore } from "../stores/chrome.js";
import { workerStripText } from "../stores/remoteWorkers.js";
import { toolJobsStore } from "../stores/toolJobs.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { statusStore, hasStatus, formatStatusText } from "../stores/statusLines.js";
import { turnStateStore } from "../stores/turnState.js";
import { getPlatformClipboardFiles } from "../api/platform.js";
import {
  fileReferenceKey,
  normalizeFileReferences,
  pathsFromFileList,
  pathsFromUriList,
  shouldResolvePathsViaShell,
} from "../utils/filePathPaste.js";
import { getThinkingControl, hasThinkingSecondaryControl } from "../utils/llmControls.js";
import {
  canSubmitComposer,
  hasPendingUserInformation,
  shouldShowCancel,
  shouldShowInteractionCancel,
} from "../utils/composerState.js";
import * as api from "../api/node.js";

const props = defineProps({
  hitlQueue: { type: Array, default: () => [] },
  disabled: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
  error: { type: String, default: "" },
  agentId: { type: String, default: "" },
  workspaceView: { type: String, default: "messages" },
  terminalRefreshKey: { type: Number, default: 0 },
});

const emit = defineEmits([
  "send",
  "cancel",
  "toggle-thinking",
  "cycle-effort",
  "switch-profile",
]);

const router = useRouter();
const thinkingEnabled = computed(() => {
  const thinking = String(props.llmSettings?.thinking || "").toLowerCase();
  return !["disabled", "off"].includes(thinking);
});
const thinkingEffort = computed(() =>
  String(props.llmSettings?.reasoning_effort || "high").toLowerCase(),
);
const thinkingControl = computed(() => getThinkingControl(props.llmSettings));
const thinkingFixed = computed(() => thinkingControl.value === "fixed");
const thinkingLabel = computed(() => {
  const label = String(props.llmSettings?.thinking_label || "").trim();
  return label || "思考";
});
const thinkingSecondaryLabel = computed(() => {
  const label = String(props.llmSettings?.thinking_secondary_label || "").trim();
  return label || (thinkingControl.value === "budget" ? "思考预算" : "推理强度");
});
const thinkingSecondarySupported = computed(
  () => hasThinkingSecondaryControl(props.llmSettings),
);

const input = ref("");
const pendingImages = ref([]);
const pendingFiles = ref([]);
const attachmentNotice = ref("");
const imageInputRef = ref(null);
const attachInputRef = ref(null);
const textareaRef = ref(null);
const userInfoPending = computed(() => hasPendingUserInformation(props.hitlQueue));
const terminals = ref([]);
const terminalLoading = ref(false);
let terminalLoadSeq = 0;

async function loadTerminals() {
  const agentId = String(props.agentId || "").trim();
  const seq = ++terminalLoadSeq;
  if (!agentId || props.workspaceView !== "messages") {
    if (seq === terminalLoadSeq) {
      terminals.value = [];
      terminalLoading.value = false;
    }
    return;
  }
  terminalLoading.value = true;
  try {
    const result = await api.listAgentTerminals(agentId);
    if (seq !== terminalLoadSeq || String(props.agentId || "").trim() !== agentId) return;
    terminals.value = Array.isArray(result?.terminals) ? result.terminals : [];
  } catch {
    if (seq === terminalLoadSeq) terminals.value = [];
  } finally {
    if (seq === terminalLoadSeq) terminalLoading.value = false;
  }
}

watch(
  () => [props.agentId, props.workspaceView, props.terminalRefreshKey],
  () => void loadTerminals(),
  { immediate: true },
);

function openTerminal(item) {
  const id = String(item?.terminal_id || "").trim();
  const agentId = String(props.agentId || "").trim();
  if (!id || !agentId) return;
  void router.replace({
    name: "agents",
    params: { agentId },
    query: {
      ...router.currentRoute.value.query,
      view: "terminal",
      terminal_id: id,
    },
  });
}

const runningJobCount = computed(() => toolJobsStore.running);
const pendingApprovals = computed(() =>
  props.hitlQueue
    .filter((item) => item.kind === "approval")
    .reduce((count, item) => count + extractToolApprovals(item.data).length, 0),
);
const runtimeStatusText = computed(() => {
  void statusStore.tick;
  const parts = [];
  if (props.cancelling) parts.push("正在取消");
  if (pendingApprovals.value > 0) parts.push(`待审批 ${pendingApprovals.value}`);
  if (runningJobCount.value > 0) parts.push(`工具执行中 ${runningJobCount.value}`);
  const phase = [
    "thinking",
    "assistant_generating",
    "model_generating",
    "tool_executing",
    "tool_waiting",
    "waiting_user",
    "queued",
  ].find((candidate) => hasStatus(candidate));
  if (phase) parts.push(formatStatusText(phase, statusStore.phases[phase]));
  if (hasStatus("compression")) {
    parts.push(formatStatusText("compression", statusStore.phases.compression));
  }
  const workers = workerStripText();
  if (workers) parts.push(workers);
  // Hydrate 后没有 SSE 草稿可恢复，但权威 Turn 终态仍应对用户可见。
  // 取消请求的即时响应已经由 ChatView 写入系统消息，避免这里重复展示。
  if (!parts.length && turnStateStore.phase === "cancelled" && turnStateStore.cancelState !== "confirmed") {
    parts.push("本轮已取消");
  }
  return parts.join(" · ");
});
const composerPlaceholder = computed(() =>
  props.sending ? "本轮执行中，可先编辑下一条消息…" : "输入消息，或向助手提问…",
);
const showCancel = computed(() =>
  shouldShowCancel({
    sending: props.sending,
    hitlBusy: props.hitlBusy,
    hasUserInformation: userInfoPending.value,
  }),
);
const showInteractionCancel = computed(() =>
  shouldShowInteractionCancel({
    sending: props.sending,
    hitlBusy: props.hitlBusy,
    hasUserInformation: userInfoPending.value,
  }),
);
const multimodalEnabled = computed(() => {
  if (typeof chromeStore.llmSettings?.multimodal_enabled === "boolean") {
    return chromeStore.llmSettings.multimodal_enabled;
  }
  return Boolean(chromeStore.agentInfo?.multimodal_enabled);
});
const canSubmit = computed(() =>
  canSubmitComposer({
    disabled: props.disabled,
    cancelling: props.cancelling,
    sending: props.sending,
    hitlBusy: props.hitlBusy,
    hasUserInformation: userInfoPending.value,
    hasContent:
      !!input.value.trim() || pendingImages.value.length > 0 || pendingFiles.value.length > 0,
  }),
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

const MAX_PENDING_FILES = 8;

function addPendingFiles(paths, source = "unknown") {
  let added = 0;
  for (const file of normalizeFileReferences((paths || []).map((path) => ({ path, source })))) {
    if (!file || pendingFiles.value.length >= MAX_PENDING_FILES) break;
    if (pendingFiles.value.some((item) => fileReferenceKey(item.path) === fileReferenceKey(file.path))) continue;
    pendingFiles.value.push(file);
    added += 1;
  }
  if (added) attachmentNotice.value = "";
  return added;
}

function removePendingFile(index) {
  pendingFiles.value.splice(index, 1);
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
    addPendingFiles(paths, "picker");
    return;
  }
  attachmentNotice.value = "无法获取文件真实路径，请通过拖拽文件或桌面端粘贴添加。";
}

const attachDisabled = computed(() => props.disabled || props.cancelling);
const imageAttachDisabled = computed(
  () => attachDisabled.value || pendingImages.value.length >= 8,
);

function buildContentParts(text, images) {
  const parts = [];
  const trimmed = String(text || "").trim();
  if (trimmed) parts.push({ type: "text", text: trimmed });
  for (const image of images) {
    if (image?.url) parts.push({ type: "image_url", image_url: { url: image.url } });
  }
  return parts;
}

function submit() {
  const text = input.value.trim();
  const images = pendingImages.value.slice();
  const files = pendingFiles.value.slice();
  if (!canSubmit.value) return;
  emit("send", {
    text,
    contentParts: buildContentParts(text, images),
    images: images.map((image) => image.url),
    fileRefs: files,
  });
  input.value = "";
  pendingImages.value = [];
  pendingFiles.value = [];
  attachmentNotice.value = "";
}

function onCancel() {
  if ((!showCancel.value && !showInteractionCancel.value) || props.cancelling) return;
  emit("cancel");
}

function onKeydown(event) {
  if (event.key === "Enter" && !event.shiftKey) {
    if (props.sending && !userInfoPending.value) return;
    event.preventDefault();
    submit();
  }
}

async function resolveFilePaths({ text, files, uriList }) {
  let paths = pathsFromFileList(files);
  if (!paths.length && uriList) paths = pathsFromUriList(uriList);
  if (!paths.length && shouldResolvePathsViaShell({ text, files })) {
    try {
      const data = await getPlatformClipboardFiles();
      paths = data?.paths || [];
    } catch {
      /* Shell 不可用 */
    }
  }
  return paths;
}

function isImageOnlyFileList(fileList) {
  const files = Array.from(fileList || []);
  return files.length > 0 && files.every((file) => allowedImageTypes.has(file.type));
}

async function onComposerPaste(event) {
  const dataTransfer = event.clipboardData;
  if (!dataTransfer) return;
  const text = dataTransfer.getData("text/plain") || "";
  const files = dataTransfer.files;
  if (multimodalEnabled.value && isImageOnlyFileList(files)) {
    event.preventDefault();
    await addImageFiles(files);
    return;
  }
  const paths = await resolveFilePaths({
    text,
    files,
    uriList: dataTransfer.getData("text/uri-list") || dataTransfer.getData("text/plain"),
  });
  if (paths.length) {
    addPendingFiles(paths, "paste");
    event.preventDefault();
  } else if (files.length || text.includes("file:")) {
    attachmentNotice.value = "无法解析文件路径，请确认文件来自本机并重新粘贴。";
  }
}

async function onComposerDrop(event) {
  const dataTransfer = event.dataTransfer;
  if (!dataTransfer) return;
  event.preventDefault();
  const paths = await resolveFilePaths({
    text: "",
    files: dataTransfer.files,
    uriList: dataTransfer.getData("text/uri-list") || dataTransfer.getData("text/plain"),
  });
  if (paths.length) {
    addPendingFiles(paths, "drop");
    return;
  }
  if (multimodalEnabled.value && isImageOnlyFileList(dataTransfer.files)) {
    await addImageFiles(dataTransfer.files);
  }
}

defineExpose({
  setDraft(text) {
    input.value = String(text || "");
  },
});
</script>

<template>
  <footer class="chat__composer">
    <div v-if="error" class="chat__composer-alert" role="alert" aria-live="polite">
      <span class="chat__composer-alert-icon" aria-hidden="true">!</span>
      <span>{{ error }}</span>
    </div>
    <div
      v-if="pendingFiles.length || (multimodalEnabled && pendingImages.length)"
      class="chat__pending-attachments"
      aria-label="待发送附件"
    >
      <div v-if="pendingFiles.length" class="chat__pending-files" aria-label="待引用文件">
        <div class="chat__pending-files-heading">
          <span>引用文件</span>
          <span class="chat__pending-files-count">{{ pendingFiles.length }}</span>
        </div>
        <div class="chat__pending-files-list">
          <div
            v-for="(file, idx) in pendingFiles"
            :key="file.path"
            class="chat__pending-file"
            :class="{ 'chat__pending-file--invalid': file.status !== 'ready' }"
          >
            <span class="chat__pending-file-icon" aria-hidden="true">
              <svg viewBox="0 0 20 20" fill="none">
                <path d="M5.25 2.75h6.1L15.5 6.9v10.35H5.25z" stroke="currentColor" stroke-width="1.25" stroke-linejoin="round" />
                <path d="M11.25 2.75V7h4.25M7.75 10h5.5M7.75 13h5.5" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" />
              </svg>
            </span>
            <span class="chat__pending-file-info" :title="file.path">
              <strong>{{ file.name }}</strong>
              <small>{{ file.displayPath || file.path }}</small>
            </span>
            <button
              type="button"
              class="chat__pending-file-remove"
              :aria-label="`移除 ${file.name}`"
              :title="`移除 ${file.name}`"
              @click="removePendingFile(idx)"
            >
              <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
      </div>
      <div
        v-if="multimodalEnabled && pendingImages.length"
        class="chat__pending-images"
        aria-label="待发送图片"
      >
        <div class="chat__pending-images-list">
          <div
            v-for="(image, idx) in pendingImages"
            :key="`${image.name}-${idx}`"
            class="chat__pending-image"
            tabindex="0"
            :aria-label="`第 ${idx + 1} 张待发送图片：${image.name}`"
          >
            <span class="chat__pending-image-thumb-wrap">
              <img class="chat__pending-image-thumb" :src="image.url" :alt="image.name" />
              <span class="chat__pending-image-index">{{ idx + 1 }}</span>
            </span>
            <span class="chat__pending-image-preview" aria-hidden="true">
              <img :src="image.url" :alt="image.name" />
              <span>{{ image.name }}</span>
            </span>
            <button
              type="button"
              class="chat__pending-image-remove"
              aria-label="移除待发送图片"
              title="移除图片"
              @click="removePendingImage(idx)"
            >
              <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
      </div>
    </div>
    <div v-if="attachmentNotice" class="chat__composer-attachment-notice" role="status" aria-live="polite">
      {{ attachmentNotice }}
    </div>
    <div
      class="chat__composer-runtime-rail"
      :class="{ 'chat__composer-runtime-rail--idle': !runtimeStatusText }"
      role="status"
      aria-live="polite"
      :title="runtimeStatusText"
      :aria-hidden="!runtimeStatusText"
    >
      {{ runtimeStatusText || "空闲" }}
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
          title="添加文件引用"
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
          v-if="showInteractionCancel"
          type="button"
          class="chat__composer-send chat__composer-send--cancel"
          title="取消本轮（不会提交回答）"
          aria-label="取消本轮（不会提交回答）"
          :disabled="cancelling"
          @click="onCancel"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
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

    <div class="chat__composer-statusline" aria-label="输入状态与工具栏">
      <div class="chat__composer-statusline-left">
        <McpStatusIndicator :agent-id="agentId" />
        <TerminalSessionIndicator
          :terminals="terminals"
          :loading="terminalLoading"
          @terminal-select="openTerminal"
        />
        <SkillsStatusIndicator :agent-id="agentId" />
      </div>
      <div class="chat__composer-statusline-right">
        <div v-if="thinkingSupported && thinkingControl" class="chat__statusline-thinking">
          <span
            v-if="thinkingFixed"
            class="composer-toolbar__btn composer-toolbar__btn--status"
            title="当前模型固定开启思考"
          >
            <span class="composer-toolbar__label">{{ thinkingLabel }}（固定）</span>
          </span>
          <button
            v-else
            type="button"
            class="composer-toolbar__btn"
            :class="{ 'composer-toolbar__btn--active': thinkingEnabled }"
            :title="thinkingEnabled ? `${thinkingLabel}已开启，点击关闭` : `${thinkingLabel}已关闭，点击开启`"
            :disabled="disabled || cancelling"
            @click="emit('toggle-thinking')"
          >
            <span class="composer-toolbar__label">{{ thinkingEnabled ? thinkingLabel : `${thinkingLabel}关` }}</span>
          </button>
          <button
            v-if="thinkingEnabled && thinkingSecondarySupported"
            type="button"
            class="composer-toolbar__btn composer-toolbar__btn--secondary"
            :title="`${thinkingSecondaryLabel} ${thinkingEffort}，点击切换 high/max`"
            :disabled="disabled || cancelling"
            @click="emit('cycle-effort')"
          >
            <span class="composer-toolbar__label">{{ thinkingSecondaryLabel }} {{ thinkingEffort }}</span>
          </button>
        </div>
        <ContextMeter />
      </div>
    </div>
  </footer>
</template>
