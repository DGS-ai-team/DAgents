<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { mediaFullUrl, mediaThumbnailUrl } from "../utils/media.js";
import { openLightbox } from "../stores/lightbox.js";
import BrowserCitationBlock from "./BrowserCitationBlock.vue";
import UiIcon from "./UiIcon.vue";
import ResourceMarkdown from "./ResourceMarkdown.vue";
import { getConversationItemContent } from "../api/node.js";
import {
  advanceTextReveal,
  commonTextPrefixLength,
  TEXT_REVEAL_FRAME_INTERVAL_MS,
  TEXT_REVEAL_INITIAL_BACKLOG_LIMIT,
} from "../utils/textReveal.js";

const props = defineProps({
  entry: { type: Object, required: true },
  conversationOrdinal: { type: Number, default: 0 },
  streamItemKey: { type: String, default: "" },
  agentId: { type: String, default: "" },
  source: { type: String, default: "main" },
});

const emit = defineEmits(["queue-context-menu"]);
const fullText = ref("");
const showFullText = ref(false);
const detailLoading = ref(false);
const detailError = ref("");
const revealedAssistantText = ref("");
const revealingAssistantText = ref(false);
let revealItemId = "";
let revealAttemptId = "";
let revealTarget = "";
let revealRemainder = 0;
let revealLastFrameAt = null;
let revealFrame = 0;
let revealFrameUsesTimeout = false;

function cancelRevealFrame() {
  if (!revealFrame) return;
  if (revealFrameUsesTimeout) clearTimeout(revealFrame);
  else if (typeof cancelAnimationFrame === "function") cancelAnimationFrame(revealFrame);
  revealFrame = 0;
}

function scheduleRevealFrame() {
  if (revealFrame || !revealingAssistantText.value || revealedAssistantText.value.length >= revealTarget.length) return;
  if (typeof requestAnimationFrame === "function") {
    revealFrameUsesTimeout = false;
    revealFrame = requestAnimationFrame(advanceRevealFrame);
  } else {
    revealFrameUsesTimeout = true;
    revealFrame = setTimeout(() => advanceRevealFrame(Date.now()), TEXT_REVEAL_FRAME_INTERVAL_MS);
  }
}

function advanceRevealFrame(timestamp) {
  revealFrame = 0;
  if (!revealingAssistantText.value) return;
  const now = Number.isFinite(Number(timestamp)) ? Number(timestamp) : Date.now();
  if (revealLastFrameAt === null) {
    revealLastFrameAt = now;
    scheduleRevealFrame();
    return;
  }
  const elapsedMs = Math.max(0, now - revealLastFrameAt);
  if (elapsedMs < TEXT_REVEAL_FRAME_INTERVAL_MS) {
    scheduleRevealFrame();
    return;
  }
  revealLastFrameAt = now;
  const next = advanceTextReveal({
    text: revealTarget,
    visibleLength: revealedAssistantText.value.length,
    elapsedMs,
    remainder: revealRemainder,
  });
  revealRemainder = next.remainder;
  if (next.visibleLength !== revealedAssistantText.value.length) {
    revealedAssistantText.value = revealTarget.slice(0, next.visibleLength);
  }
  if (next.done) {
    // Stop the animation loop while the provider is quiet. A later delta will
    // set revealingAssistantText again in syncAssistantReveal and wake it up.
    revealingAssistantText.value = false;
    revealLastFrameAt = null;
    return;
  }
  scheduleRevealFrame();
}

function syncAssistantReveal(entry) {
  if (entry?.kind !== "assistant") {
    cancelRevealFrame();
    revealItemId = "";
    revealAttemptId = "";
    revealTarget = "";
    revealRemainder = 0;
    revealLastFrameAt = null;
    revealingAssistantText.value = false;
    revealedAssistantText.value = "";
    return;
  }

  const itemId = String(entry.id || entry.item_id || "");
  const attemptId = String(entry.attempt_id || "");
  const text = String(entry.text || "");
  const isStreaming = Boolean(entry.streaming);
  const newItem = itemId !== revealItemId;
  const newAttempt = !newItem && isStreaming && attemptId && revealAttemptId && attemptId !== revealAttemptId;

  if (newItem || newAttempt) {
    cancelRevealFrame();
    revealItemId = itemId;
    revealAttemptId = attemptId;
    revealTarget = text;
    revealRemainder = 0;
    revealLastFrameAt = null;
    if (isStreaming && text.length <= TEXT_REVEAL_INITIAL_BACKLOG_LIMIT) {
      revealedAssistantText.value = "";
      revealingAssistantText.value = true;
    } else {
      // Switching back to an already-running long response should not replay
      // its entire prefix. Future deltas still enter the normal reveal buffer.
      revealedAssistantText.value = text;
      revealingAssistantText.value = false;
    }
  } else if (text.startsWith(revealTarget)) {
    const appended = text.length > revealTarget.length;
    revealTarget = text;
    if (attemptId) revealAttemptId = attemptId;
    if (isStreaming && appended) revealingAssistantText.value = true;
  } else if (text !== revealTarget) {
    const visiblePrefix = revealedAssistantText.value.slice(0, commonTextPrefixLength(revealedAssistantText.value, text));
    revealTarget = text;
    revealAttemptId = attemptId || revealAttemptId;
    revealedAssistantText.value = visiblePrefix;
    revealRemainder = 0;
    revealLastFrameAt = null;
    revealingAssistantText.value = visiblePrefix.length < text.length;
  }

  if (!isStreaming && revealedAssistantText.value.length >= revealTarget.length) {
    revealingAssistantText.value = false;
    cancelRevealFrame();
    revealLastFrameAt = null;
  } else if (revealingAssistantText.value) {
    scheduleRevealFrame();
  } else if (!isStreaming) {
    revealedAssistantText.value = revealTarget;
  }
}

const assistantRevealActive = computed(() => props.entry?.kind === "assistant" && (Boolean(props.entry?.streaming) || revealingAssistantText.value));

const displayEntry = computed(() => showFullText.value && fullText.value
  ? { ...props.entry, text: fullText.value }
  : assistantRevealActive.value
    ? { ...props.entry, text: revealedAssistantText.value }
    : props.entry);

watch(() => [props.entry?.id, props.entry?.item_version], () => {
  fullText.value = "";
  showFullText.value = false;
  detailError.value = "";
});

watch(() => [props.entry?.id, props.entry?.item_id, props.entry?.attempt_id, props.entry?.kind, props.entry?.streaming, props.entry?.text], () => {
  syncAssistantReveal(props.entry);
}, { immediate: true, flush: "sync" });

onBeforeUnmount(() => cancelRevealFrame());

async function toggleFullText() {
  if (fullText.value) { showFullText.value = !showFullText.value; return; }
  const roundId = String(props.entry?.round_id || "").trim();
  const revisionId = String(props.entry?.round_revision_id || "").trim();
  const itemId = String(props.entry?.detail_item_id || "").trim();
  if (!props.agentId || !roundId || !revisionId || !itemId || detailLoading.value) return;
  detailLoading.value = true;
  detailError.value = "";
  try {
    const response = await getConversationItemContent(props.agentId, props.agentId, roundId, itemId, { revisionId });
    fullText.value = String(response?.text || "");
    showFullText.value = true;
  } catch (cause) {
    detailError.value = String(cause?.message || "读取完整消息失败");
  } finally {
    detailLoading.value = false;
  }
}

function hasQueuedInputIdentity() {
  return props.entry?.kind === "user" && props.entry?.queue_status === "queued" &&
    Number.isSafeInteger(Number(props.entry?.input_seq)) && Number(props.entry?.input_seq) > 0 &&
    Number.isSafeInteger(Number(props.entry?.input_revision)) && Number(props.entry?.input_revision) > 0;
}

function openQueueMenu(event) {
  if (!hasQueuedInputIdentity()) return;
  event.preventDefault();
  event.stopPropagation();
  emit("queue-context-menu", { event, entry: props.entry, anchor: event.currentTarget });
}

function onUserKeydown(event) {
  if (event.key !== "ContextMenu" && !(event.shiftKey && event.key === "F10")) return;
  openQueueMenu(event);
}

const browserRefs = computed(() =>
  props.entry?.kind === "assistant" && Array.isArray(props.entry.browser_refs)
    ? props.entry.browser_refs
    : [],
);
const userImageSrcs = computed(() => {
  if (props.entry.kind !== "user") return [];
  const images = Array.isArray(props.entry.images) ? props.entry.images.filter(Boolean) : [];
  if (images.length) return images;
  const media = Array.isArray(props.entry.media) ? props.entry.media : [];
  return media.map((item) => String(item?.url || "").trim()).filter(Boolean);
});
const userFileRefs = computed(() => {
  if (props.entry.kind !== "user") return [];
  return Array.isArray(props.entry.file_refs) ? props.entry.file_refs : [];
});

function openUserImage(index) {
  openLightbox(
    userImageSrcs.value.map((src) => ({ src: mediaFullUrl(src), alt: "用户上传图片" })),
    index,
  );
}

function userImageThumb(src) {
  return mediaThumbnailUrl(src);
}
</script>

<template>
  <div v-if="entry.kind === 'context_boundary'" class="context-boundary" role="separator" aria-label="新对话，上下文已重置" :data-conversation-ordinal="conversationOrdinal || undefined" :data-stream-item-key="streamItemKey || undefined">
    <span>新对话 · 上下文已重置</span>
  </div>
  <div
    v-if="entry.kind === 'user'"
    class="msg msg--user"
    :data-conversation-ordinal="conversationOrdinal || undefined"
    :data-stream-item-key="streamItemKey || undefined"
    :class="{ 'msg--user-queued': entry.queue_status === 'queued' }"
    :tabindex="hasQueuedInputIdentity() ? 0 : undefined"
    :aria-label="hasQueuedInputIdentity() ? '待处理消息，右键查看操作' : undefined"
    @contextmenu="openQueueMenu"
    @keydown="onUserKeydown"
  >
    <div class="msg__body">
      <div v-if="displayEntry.text" class="msg__bubble msg__bubble--user">
        <div class="msg__text">{{ displayEntry.text }}</div>
      </div>
      <div v-if="entry.details_available" class="msg__content-detail">
        <button type="button" :disabled="detailLoading" @click="toggleFullText">{{ detailLoading ? '正在读取…' : showFullText ? '收起完整内容' : '查看完整内容' }}</button>
        <span v-if="detailError" role="alert">{{ detailError }}</span>
      </div>
      <div v-if="entry.queue_status" class="msg__queue-status" role="status">
        {{ entry.queue_status === 'claimed' ? '正在开始处理' : '等待当前任务完成' }}
      </div>
      <div v-if="userFileRefs.length" class="msg__file-refs" aria-label="引用文件">
        <div class="msg__file-refs-heading">
          <span>引用文件</span>
          <span class="msg__file-refs-count">{{ userFileRefs.length }}</span>
        </div>
        <div class="msg__file-refs-list">
          <div
            v-for="(file, index) in userFileRefs"
            :key="file.id || `${file.path}:${file.anchor?.label || index}:${index}`"
            class="msg__file-ref"
            :title="file.displayPath || file.path"
          >
            <span class="msg__file-ref-icon" aria-hidden="true">
              <UiIcon name="file" :size="16" />
            </span>
            <span class="msg__file-ref-info">
              <strong>{{ file.name || file.path }}</strong>
              <small>
                {{ file.displayPath || file.path }}
                <template v-if="file.anchor?.label && file.anchor.label !== '整个文件'"> · {{ file.anchor.label }}</template>
              </small>
            </span>
          </div>
        </div>
      </div>
      <div v-if="userImageSrcs.length" class="msg__images" aria-label="发送的图片">
        <button
          v-for="(src, idx) in userImageSrcs"
          :key="idx"
          type="button"
          class="msg__image-btn"
          @click="openUserImage(idx)"
        >
          <span class="msg__image-frame">
            <span class="msg__image-thumb-wrap">
              <img
                class="msg__image"
                :src="userImageThumb(src)"
                :alt="`用户上传图片 ${idx + 1}`"
                :aria-label="`查看第 ${idx + 1} 张用户图片`"
                :loading="idx === 0 ? 'eager' : 'lazy'"
              />
              <span class="msg__image-index">{{ idx + 1 }}</span>
            </span>
            <span class="msg__image-preview" aria-hidden="true">
              <img :src="mediaFullUrl(src)" :alt="`用户上传图片 ${idx + 1}`" />
            </span>
          </span>
        </button>
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'assistant' && displayEntry.text" class="msg msg--assistant" data-kind="assistant" :data-conversation-ordinal="conversationOrdinal || undefined" :data-stream-item-key="streamItemKey || undefined">
    <div class="msg__body">
      <div class="msg__bubble msg__bubble--assistant-md">
        <div
          class="tool-exec-bubble__markdown assistant-msg__md"
          :class="{ 'assistant-msg__md--streaming': assistantRevealActive }"
        ><ResourceMarkdown :entry="displayEntry" :agent-id="agentId" :source="source" :streaming="assistantRevealActive" /></div>
        <BrowserCitationBlock v-if="!entry.streaming && browserRefs.length" :refs="browserRefs" />
      </div>
      <div v-if="entry.details_available" class="msg__content-detail">
        <button type="button" :disabled="detailLoading" @click="toggleFullText">{{ detailLoading ? '正在读取…' : showFullText ? '收起完整内容' : '查看完整内容' }}</button>
        <span v-if="detailError" role="alert">{{ detailError }}</span>
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'system'" class="msg msg--system" role="status" :data-conversation-ordinal="conversationOrdinal || undefined" :data-stream-item-key="streamItemKey || undefined">
    <div class="msg__bubble msg__bubble--system">{{ entry.text }}</div>
  </div>
</template>

<style scoped>
.context-boundary {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 18px 0;
  color: var(--color-text-subtle);
  font-size: 11px;
  white-space: nowrap;
}

.msg__content-detail {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 4px 2px 0;
  color: var(--color-text-subtle);
  font-size: 12px;
}

.msg__content-detail button {
  border: 0;
  padding: 2px 0;
  background: transparent;
  color: var(--color-primary);
  font: inherit;
  cursor: pointer;
}

.msg__content-detail button:disabled { opacity: 0.6; cursor: wait; }
.msg__content-detail [role="alert"] { color: var(--color-danger); }
.context-boundary::before,
.context-boundary::after {
  content: "";
  flex: 1;
  height: 1px;
  background: var(--color-border);
}
.msg__image-btn {
  padding: 0;
  border: 0;
  background: transparent;
  cursor: zoom-in;
  line-height: 0;
  min-width: 0;
  text-align: left;
}
.msg__images {
  display: flex;
  align-items: flex-end;
  flex-wrap: wrap;
  width: min(420px, 100%);
  gap: 6px;
  margin-top: 6px;
}
.msg__queue-status {
  margin-top: 4px;
  color: var(--color-text-subtle);
  font-size: 11px;
  line-height: 1.4;
  text-align: right;
}
.msg__file-refs {
  width: min(440px, 100%);
  margin-top: 7px;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--color-primary) 22%, var(--color-border));
  border-radius: 9px;
  background: color-mix(in srgb, var(--color-primary-soft) 42%, var(--color-surface));
}
.msg__file-refs-heading {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
  color: var(--color-text-muted);
  font-size: 11px;
}
.msg__file-refs-count {
  display: inline-grid;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  place-items: center;
  border-radius: 999px;
  background: var(--color-primary-soft);
  color: var(--color-primary-strong);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}
.msg__file-refs-list {
  display: grid;
  gap: 4px;
}
.msg__file-ref {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  padding: 4px 5px;
  border-radius: 5px;
  background: color-mix(in srgb, var(--color-surface) 70%, transparent);
}
.msg__file-ref-icon {
  flex: 0 0 auto;
  color: var(--color-primary-strong);
  line-height: 0;
}
.msg__file-ref-icon svg {
  width: 15px;
  height: 15px;
}
.msg__file-ref-info {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1px;
}
.msg__file-ref-info strong,
.msg__file-ref-info small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.msg__file-ref-info strong {
  color: var(--color-text);
  font-size: 11.5px;
}
.msg__file-ref-info small {
  color: var(--color-text-subtle);
  font-family: var(--font-mono);
  font-size: 10px;
}
.msg__image-frame {
  position: relative;
  display: block;
  width: 56px;
  height: 56px;
  overflow: visible;
  border: 0;
  border-radius: 10px;
  background: transparent;
  transition: border-color 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease;
}
.msg__image-thumb-wrap {
  position: relative;
  display: block;
  width: 100%;
  height: 100%;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: var(--color-surface-hover);
}
.msg__image {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: color-mix(in srgb, var(--color-text) 4%, transparent);
}
.msg__image-preview {
  position: absolute;
  left: 0;
  bottom: calc(100% + 10px);
  width: min(240px, 64vw);
  aspect-ratio: 4 / 3;
  overflow: hidden;
  border: 1px solid var(--color-border-strong);
  border-radius: 12px;
  background: var(--color-editor);
  box-shadow: var(--shadow-lg);
  opacity: 0;
  pointer-events: none;
  transform: translateY(8px) scale(0.96);
  transition: opacity 0.16s ease, transform 0.16s ease;
}
.msg__image-preview img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: color-mix(in srgb, var(--color-text) 4%, transparent);
}
.msg__image-btn:hover .msg__image-preview,
.msg__image-btn:focus-visible .msg__image-preview {
  opacity: 1;
  transform: translateY(0) scale(1);
}
.msg__image-btn:hover .msg__image-frame,
.msg__image-btn:focus-visible .msg__image-frame {
  border-color: var(--color-primary);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary) 16%, transparent);
  transform: translateY(-1px);
}
.msg__image-btn:focus-visible {
  outline: none;
}
.msg__image-index {
  position: absolute;
  right: 6px;
  bottom: 6px;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  display: inline-grid;
  place-items: center;
  border-radius: 999px;
  background: color-mix(in srgb, #000 64%, transparent);
  color: #fff;
  font-size: 10px;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}
.msg__text {
  white-space: pre-wrap;
}
.assistant-msg__stream-plain {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.55;
  color: var(--color-text);
}
.msg__deferred-tag {
  display: inline-block;
  margin-right: 6px;
  font-size: 10px;
  text-transform: uppercase;
  color: var(--color-text-subtle);
}
</style>
