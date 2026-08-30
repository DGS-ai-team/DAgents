<script setup>
import { computed, onBeforeUnmount, ref } from "vue";
import { renderMarkdown } from "../utils/markdown.js";
import { copyText } from "../utils/clipboard.js";
import { mediaFullUrl, mediaThumbnailUrl } from "../utils/media.js";
import { openLightbox } from "../stores/lightbox.js";
import BrandActivityIndicator from "./BrandActivityIndicator.vue";
import BrowserCitationBlock from "./BrowserCitationBlock.vue";

const props = defineProps({
  entry: { type: Object, required: true },
});

const markdownActionTimer = ref(null);

function resetMarkdownAction(button) {
  if (button) button.textContent = button.dataset.defaultLabel || "复制代码";
  if (markdownActionTimer.value) {
    clearTimeout(markdownActionTimer.value);
    markdownActionTimer.value = null;
  }
}

async function onMarkdownAction(event) {
  const button = event.target?.closest?.("[data-markdown-action]");
  if (!button) return;
  const code =
    button.dataset.code ||
    button.closest(".markdown-code-block")?.querySelector("code")?.textContent ||
    "";
  if (!code) return;
  button.dataset.defaultLabel = button.textContent || "复制代码";
  try {
    button.textContent = (await copyText(code)) ? "已复制" : "复制失败";
  } catch {
    button.textContent = "操作失败";
  }
  if (markdownActionTimer.value) clearTimeout(markdownActionTimer.value);
  markdownActionTimer.value = setTimeout(() => resetMarkdownAction(button), 1600);
}

onBeforeUnmount(() => {
  if (markdownActionTimer.value) clearTimeout(markdownActionTimer.value);
});

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
  <div v-if="entry.kind === 'user'" class="msg msg--user">
    <div class="msg__body">
      <div v-if="entry.text" class="msg__bubble msg__bubble--user">
        <div class="msg__text">{{ entry.text }}</div>
      </div>
      <div v-if="userFileRefs.length" class="msg__file-refs" aria-label="引用文件">
        <div class="msg__file-refs-heading">
          <span>引用文件</span>
          <span class="msg__file-refs-count">{{ userFileRefs.length }}</span>
        </div>
        <div class="msg__file-refs-list">
          <div v-for="file in userFileRefs" :key="file.path" class="msg__file-ref" :title="file.path">
            <span class="msg__file-ref-icon" aria-hidden="true">
              <svg viewBox="0 0 20 20" fill="none">
                <path d="M5.25 2.75h6.1L15.5 6.9v10.35H5.25z" stroke="currentColor" stroke-width="1.25" stroke-linejoin="round" />
                <path d="M11.25 2.75V7h4.25" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            <span class="msg__file-ref-info">
              <strong>{{ file.name || file.path }}</strong>
              <small>{{ file.displayPath || file.path }}</small>
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

  <div v-else-if="entry.kind === 'assistant'" class="msg msg--assistant" data-kind="assistant" :class="{ 'msg--generating': entry.streaming }">
    <div class="msg__body">
      <div v-if="entry.streaming && !entry.text" class="msg__body--hint-only">
        <BrandActivityIndicator label="正在生成" mode="generating" :show-label="false" />
      </div>
      <div v-else class="msg__bubble msg__bubble--assistant-md">
        <div
          class="tool-exec-bubble__markdown assistant-msg__md"
          :class="{ 'assistant-msg__md--streaming': entry.streaming }"
          v-html="renderMarkdown(entry.text)"
          @click="onMarkdownAction"
        />
        <BrowserCitationBlock v-if="!entry.streaming && browserRefs.length" :refs="browserRefs" />
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'system'" class="msg msg--system" role="status">
    <div class="msg__bubble msg__bubble--system">{{ entry.text }}</div>
  </div>
</template>

<style scoped>
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
