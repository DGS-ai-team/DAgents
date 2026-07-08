<script setup>
import { computed } from "vue";
import { entryMedia, showImageCaption } from "../utils/showImage.js";
import { openLightbox } from "../stores/lightbox.js";

const props = defineProps({
  entry: { type: Object, default: null },
});

const media = computed(() => entryMedia(props.entry));
const caption = computed(() => showImageCaption(props.entry));

function openImage(index) {
  openLightbox(
    media.value.map((item) => ({
      src: item.url,
      alt: item.label || item.caption || caption.value || "图片",
    })),
    index,
  );
}
</script>

<template>
  <div v-if="media.length" class="tool-media-preview">
    <p v-if="caption" class="tool-media-preview__caption">{{ caption }}</p>
    <div class="tool-media-preview__grid">
      <button
        v-for="(item, index) in media"
        :key="item.id || item.url"
        type="button"
        class="tool-media-preview__btn"
        @click="openImage(index)"
      >
        <img
          class="tool-exec-bubble__image tool-media-preview__img"
          :src="item.url"
          :alt="item.label || item.caption || '图片'"
          loading="lazy"
        />
      </button>
    </div>
  </div>
</template>

<style scoped>
.tool-media-preview {
  margin-top: 6px;
}

.tool-media-preview__caption {
  margin: 0 0 6px;
  font-size: 12px;
  color: var(--color-text-muted);
  line-height: 1.4;
}

.tool-media-preview__grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tool-media-preview__btn {
  display: block;
  padding: 0;
  border: 0;
  background: transparent;
  line-height: 0;
  cursor: zoom-in;
}

.tool-media-preview__img {
  max-height: 220px;
}
</style>
