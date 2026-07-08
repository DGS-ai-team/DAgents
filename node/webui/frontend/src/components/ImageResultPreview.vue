<script setup>
import { computed } from "vue";
import { entryMedia, showImageCaption } from "../utils/showImage.js";

const props = defineProps({
  entry: { type: Object, default: null },
});

const media = computed(() => entryMedia(props.entry));
const caption = computed(() => showImageCaption(props.entry));
</script>

<template>
  <div v-if="media.length" class="tool-media-preview">
    <p v-if="caption" class="tool-media-preview__caption">{{ caption }}</p>
    <div class="tool-media-preview__grid">
      <a
        v-for="item in media"
        :key="item.id || item.url"
        class="tool-media-preview__link"
        :href="item.url"
        target="_blank"
        rel="noopener noreferrer"
      >
        <img
          class="tool-exec-bubble__image tool-media-preview__img"
          :src="item.url"
          :alt="item.label || item.caption || '图片'"
          loading="lazy"
        />
      </a>
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

.tool-media-preview__link {
  display: block;
  line-height: 0;
}

.tool-media-preview__img {
  max-height: 220px;
}
</style>
