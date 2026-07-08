import { reactive } from "vue";

export const lightboxStore = reactive({
  open: false,
  items: [],
  index: 0,
});

export function openLightbox(items, index = 0) {
  const list = (Array.isArray(items) ? items : [items])
    .map((item) => {
      if (typeof item === "string") return { src: item, alt: "" };
      const src = String(item?.src || item?.url || "").trim();
      if (!src) return null;
      return { src, alt: String(item?.alt || item?.label || item?.caption || "图片").trim() };
    })
    .filter(Boolean);
  if (!list.length) return;
  lightboxStore.items = list;
  lightboxStore.index = Math.min(Math.max(0, index), list.length - 1);
  lightboxStore.open = true;
}

export function closeLightbox() {
  lightboxStore.open = false;
}

export function stepLightbox(delta) {
  if (!lightboxStore.open || lightboxStore.items.length <= 1) return;
  const len = lightboxStore.items.length;
  lightboxStore.index = (lightboxStore.index + delta + len) % len;
}

export function currentLightboxItem() {
  return lightboxStore.items[lightboxStore.index] || null;
}
