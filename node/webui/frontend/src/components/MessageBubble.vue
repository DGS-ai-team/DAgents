<script setup>
import { computed } from "vue";
import { renderMarkdown } from "../utils/markdown.js";
import { mediaFullUrl, mediaThumbnailUrl } from "../utils/media.js";
import { openLightbox } from "../stores/lightbox.js";
import ThinkingIndicator from "./ThinkingIndicator.vue";

const props = defineProps({
  entry: { type: Object, required: true },
});

const userImageSrcs = computed(() => {
  if (props.entry.kind !== "user") return [];
  const images = Array.isArray(props.entry.images) ? props.entry.images.filter(Boolean) : [];
  if (images.length) return images;
  const media = Array.isArray(props.entry.media) ? props.entry.media : [];
  return media.map((item) => String(item?.url || "").trim()).filter(Boolean);
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
      <div class="msg__bubble msg__bubble--user">
        <div v-if="userImageSrcs.length" class="msg__images">
          <button
            v-for="(src, idx) in userImageSrcs"
            :key="idx"
            type="button"
            class="msg__image-btn"
            @click="openUserImage(idx)"
          >
            <img
              class="msg__image"
              :src="userImageThumb(src)"
              alt="用户上传图片"
              loading="lazy"
            />
          </button>
        </div>
        <div v-if="entry.text">{{ entry.text }}</div>
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'user_deferred'" class="msg msg--user msg--user-deferred" :class="{ 'msg--user-applied': entry.sideEffectApplied, 'msg--user-stale': entry.sideEffectStale }">
    <div class="msg__body">
      <div class="msg__bubble msg__bubble--user msg__bubble--deferred">
        <span v-if="entry.userName" class="msg__deferred-tag">{{ entry.userName }}</span>
        <span v-if="entry.sideEffectApplied" class="msg__applied-tag">已入库</span>
        <span v-else-if="entry.sideEffectStale" class="msg__stale-tag">已失效</span>
        {{ entry.text }}
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'assistant'" class="msg msg--assistant" data-kind="assistant" :class="{ 'msg--generating': entry.streaming }">
    <div class="msg__body">
      <div v-if="entry.streaming && !entry.text" class="msg__body--hint-only">
        <div class="msg__hint msg__hint--stream-meta msg__hint--dots-only" aria-label="正在生成">
          <span class="msg__meta-dots" aria-hidden="true">
            <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
          </span>
        </div>
      </div>
      <div v-else class="msg__bubble msg__bubble--assistant-md">
        <pre v-if="entry.streaming" class="assistant-msg__stream-plain">{{ entry.text }}</pre>
        <div v-else class="tool-exec-bubble__markdown assistant-msg__md" v-html="renderMarkdown(entry.text)" />
        <div v-if="entry.usage" class="msg__usage">{{ entry.usage }}</div>
      </div>
    </div>
  </div>

  <div
    v-else-if="entry.kind === 'reasoning' && entry.streaming"
    class="msg msg--reasoning msg--reasoning-active"
  >
    <div class="msg__body msg__body--hint-only">
      <ThinkingIndicator />
    </div>
  </div>
</template>

<style scoped>
.msg__image-btn {
  padding: 0;
  border: 0;
  background: transparent;
  cursor: zoom-in;
  line-height: 0;
}
.msg__images {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}
.msg__image {
  max-width: min(240px, 100%);
  max-height: 180px;
  border-radius: 8px;
  object-fit: contain;
  background: rgba(0, 0, 0, 0.15);
}
.msg__usage {
  margin-top: 8px;
  text-align: right;
  font-size: 11px;
  color: var(--color-text-subtle);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
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
.msg__bubble--deferred {
  opacity: 0.85;
}
.msg--user-applied .msg__bubble--deferred {
  opacity: 1;
}
.msg--user-stale .msg__bubble--deferred {
  opacity: 0.55;
  text-decoration: line-through;
}
.msg__applied-tag,
.msg__stale-tag {
  display: inline-block;
  margin-right: 6px;
  font-size: 10px;
  text-transform: uppercase;
}
.msg__applied-tag {
  color: var(--color-success);
}
.msg__stale-tag {
  color: var(--color-text-subtle);
}
.msg__deferred-tag {
  display: inline-block;
  margin-right: 6px;
  font-size: 10px;
  text-transform: uppercase;
  color: var(--color-text-subtle);
}
</style>
