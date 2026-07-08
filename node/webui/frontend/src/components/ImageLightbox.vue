<script setup>
import { computed, onMounted, onUnmounted } from "vue";
import {
  lightboxStore,
  closeLightbox,
  stepLightbox,
  currentLightboxItem,
} from "../stores/lightbox.js";

const current = computed(() => currentLightboxItem());
const hasMany = computed(() => lightboxStore.items.length > 1);
const counter = computed(() =>
  hasMany.value ? `${lightboxStore.index + 1} / ${lightboxStore.items.length}` : "",
);

function onKeydown(event) {
  if (!lightboxStore.open) return;
  if (event.key === "Escape") {
    event.preventDefault();
    closeLightbox();
  } else if (event.key === "ArrowLeft") {
    event.preventDefault();
    stepLightbox(-1);
  } else if (event.key === "ArrowRight") {
    event.preventDefault();
    stepLightbox(1);
  }
}

onMounted(() => window.addEventListener("keydown", onKeydown));
onUnmounted(() => window.removeEventListener("keydown", onKeydown));
</script>

<template>
  <Teleport to="body">
    <div
      v-if="lightboxStore.open && current"
      class="image-lightbox"
      role="dialog"
      aria-modal="true"
      aria-label="图片预览"
      @click.self="closeLightbox"
    >
      <button type="button" class="image-lightbox__close" aria-label="关闭" @click="closeLightbox">×</button>
      <button
        v-if="hasMany"
        type="button"
        class="image-lightbox__nav image-lightbox__nav--prev"
        aria-label="上一张"
        @click="stepLightbox(-1)"
      >
        ‹
      </button>
      <figure class="image-lightbox__figure">
        <img class="image-lightbox__img" :src="current.src" :alt="current.alt || '图片'" />
        <figcaption v-if="current.alt || counter" class="image-lightbox__caption">
          <span v-if="current.alt">{{ current.alt }}</span>
          <span v-if="counter" class="image-lightbox__counter">{{ counter }}</span>
        </figcaption>
      </figure>
      <button
        v-if="hasMany"
        type="button"
        class="image-lightbox__nav image-lightbox__nav--next"
        aria-label="下一张"
        @click="stepLightbox(1)"
      >
        ›
      </button>
    </div>
  </Teleport>
</template>

<style scoped>
.image-lightbox {
  position: fixed;
  inset: 0;
  z-index: 10000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(15, 23, 42, 0.82);
  backdrop-filter: blur(4px);
}

.image-lightbox__figure {
  margin: 0;
  max-width: min(96vw, 1200px);
  max-height: 92vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}

.image-lightbox__img {
  max-width: 100%;
  max-height: calc(92vh - 40px);
  object-fit: contain;
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-lg);
  background: #0f172a;
}

.image-lightbox__caption {
  display: flex;
  align-items: center;
  gap: 12px;
  color: #f8fafc;
  font-size: 13px;
}

.image-lightbox__counter {
  opacity: 0.75;
}

.image-lightbox__close {
  position: absolute;
  top: 16px;
  right: 16px;
  width: 40px;
  height: 40px;
  border: 0;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
  font-size: 24px;
  line-height: 1;
  cursor: pointer;
}

.image-lightbox__nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  width: 44px;
  height: 44px;
  border: 0;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
  font-size: 28px;
  line-height: 1;
  cursor: pointer;
}

.image-lightbox__nav--prev {
  left: 16px;
}

.image-lightbox__nav--next {
  right: 16px;
}
</style>
