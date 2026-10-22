<script setup>
import { computed, createVNode, nextTick, onBeforeUnmount, onMounted, ref, render, unref, watch } from "vue";
import { copyText } from "../utils/clipboard.js";
import { createHtmlPreviewDocument, HTML_PREVIEW_LAYOUT_LIMIT, HTML_PREVIEW_MIN_HEIGHT, sanitizeSvg } from "../utils/markdownPreview.js";
import { renderMarkdown, renderResourceMarkdown } from "../utils/markdown.js";
import { useResourceRenderContext } from "../utils/resourceContext.js";
import { openLightbox, closeLightbox, lightboxStore } from "../stores/lightbox.js";
import UiIcon from "./UiIcon.vue";

const props = defineProps({ entry: { type: Object, required: true }, agentId: { type: String, default: "" }, source: { type: String, default: "main" } });
const context = useResourceRenderContext();
const root = ref(null);
const resourceDomKey = ref(0);
const rendered = computed(() => props.entry?.kind === "assistant"
  ? renderResourceMarkdown(props.entry?.text || "")
  : { html: renderMarkdown(props.entry?.text || ""), resources: [] });
const html = computed(() => rendered.value.html);
const epoch = ref(0);
const objectUrls = new Set();
const loaded = new Map();
const previewUrls = new Map();
const renderModes = new Map();
const previewHeights = new Map();
const iconHosts = new Set();
const htmlPreviewFrames = new Map();
let hydrationTicket = 0;
let markdownActionTimer = null;
const MAX_IMAGES = 8;

function createPreviewChannel() {
  if (!globalThis.crypto?.getRandomValues) return "";
  const bytes = new Uint8Array(16);
  try { globalThis.crypto.getRandomValues(bytes); } catch { return ""; }
  return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

function clearHtmlPreviewFrames() {
  for (const state of htmlPreviewFrames.values()) {
    if (state.raf) cancelAnimationFrame(state.raf);
  }
  htmlPreviewFrames.clear();
}

function onHtmlPreviewMessage(event) {
  const data = event?.data;
  if (!data || typeof data !== "object" || Array.isArray(data)) return;
  const keys = Object.keys(data).sort();
  if (keys.length !== 3 || keys[0] !== "channel" || keys[1] !== "height" || keys[2] !== "kind") return;
  if (data.kind !== "dagents-preview-height" || typeof data.channel !== "string" || typeof data.height !== "number" || !Number.isFinite(data.height) || data.height <= 0) return;
  for (const state of htmlPreviewFrames.values()) {
    if (event.source !== state.frame.contentWindow || data.channel !== state.channel) continue;
    if (state.ticket !== hydrationTicket || state.epoch !== epoch.value || !state.frame.isConnected || !root.value?.contains(state.frame)) return;
    if (data.height > HTML_PREVIEW_LAYOUT_LIMIT) {
      if (state.raf) cancelAnimationFrame(state.raf);
      const status = document.createElement("div");
      status.className = "markdown-preview-status";
      status.setAttribute("role", "status");
      status.textContent = "HTML 预览尺寸超出显示范围";
      state.frame.replaceWith(status);
      previewHeights.delete(state.renderKey);
      htmlPreviewFrames.delete(state.channel);
      return;
    }
    state.pendingHeight = Math.ceil(Math.max(HTML_PREVIEW_MIN_HEIGHT, data.height));
    if (!state.raf) {
      state.raf = requestAnimationFrame(() => {
        state.raf = 0;
        if (state.ticket !== hydrationTicket || state.epoch !== epoch.value || !state.frame.isConnected || !root.value?.contains(state.frame)) return;
        const measuredHeight = `${state.pendingHeight}px`;
        if (state.frame.style.height !== measuredHeight) state.frame.style.height = measuredHeight;
        previewHeights.set(state.renderKey, { source: state.source, height: state.pendingHeight, compact: state.compact });
      });
    }
    return;
  }
}

function requestPreviewMeasure(frame) {
  const state = [...htmlPreviewFrames.values()].find((item) => item.frame === frame);
  if (state && frame.isConnected && frame.contentWindow) frame.contentWindow.postMessage({ kind: "dagents-preview-measure", channel: state.channel }, "*");
}

function releaseUrls() {
  const urls = [...objectUrls];
  if (urls.some((url) => lightboxStore.items.some((item) => item.src === url))) closeLightbox();
  urls.forEach((url) => URL.revokeObjectURL(url));
  objectUrls.clear();
  loaded.clear();
  previewUrls.clear();
}

function prunePreviewUrls(previews) {
  const active = new Set((previews || []).filter((preview) => preview.type === "svg").map((preview) => `svg-preview:${preview.source}`));
  for (const [key, url] of previewUrls) {
    if (active.has(key)) continue;
    if (lightboxStore.items.some((item) => item.src === url)) closeLightbox();
    URL.revokeObjectURL(url);
    objectUrls.delete(url);
    previewUrls.delete(key);
  }
}

function removeIcon(holder) {
  const host = holder?.querySelector?.(".resource-markdown__icon");
  if (host) { render(null, host); iconHosts.delete(host); }
}

function readBlobText(blob) {
  if (typeof blob?.text === "function") return blob.text();
  return new Promise((resolve, reject) => {
    if (typeof FileReader === "undefined") return reject(new Error("Blob text is unavailable"));
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(reader.error || new Error("Blob read failed"));
    reader.readAsText(blob);
  });
}

function syncRenderBlock(block, previewNode) {
  const key = block?.dataset.renderKey;
  if (!key || !previewNode) return;
  const mode = renderModes.get(key) || "preview";
  const source = block.querySelector(".markdown-render-block__source");
  const toggle = block.querySelector('[data-markdown-action="render-mode"]');
  previewNode.hidden = mode !== "preview";
  if (source) source.hidden = mode !== "source";
  if (toggle) {
    toggle.dataset.renderMode = mode === "preview" ? "source" : "preview";
    toggle.textContent = mode === "preview" ? "源码" : "预览";
    toggle.setAttribute("aria-label", mode === "preview" ? "显示源码" : "显示预览");
  }
}

function button(label, icon, disabled = false) {
  const el = document.createElement("button");
  el.type = "button";
  el.className = "resource-markdown__button";
  el.disabled = disabled;
  const iconHost = document.createElement("span");
  iconHost.className = "resource-markdown__icon";
  render(createVNode(UiIcon, { name: icon, size: 14 }), iconHost);
  iconHosts.add(iconHost);
  const text = document.createElement("span");
  text.textContent = label;
  el.append(iconHost, text);
  return el;
}

function showImageError(holder, label) {
  removeIcon(holder);
  holder.replaceChildren(document.createTextNode(`${label}（加载失败）`));
  holder.disabled = true;
  holder.setAttribute("aria-label", `图片加载失败：${label}`);
}

async function loadImage(holder, target, label, owner, myEpoch, ticket) {
  const key = `${owner}\0${target.path}`;
  holder.disabled = true;
  holder.setAttribute("aria-label", `正在加载图片：${label}`);
  holder.querySelector("span:last-child").textContent = `${label}（加载中）`;
  let promise = loaded.get(key);
  if (!promise) {
    promise = Promise.resolve().then(() => context.readImage(owner, target.path));
    loaded.set(key, promise);
  }
  try {
    let blob = await promise;
    if (target.path.toLowerCase().endsWith(".svg") || blob?.type === "image/svg+xml") {
      const safeSvg = sanitizeSvg(await readBlobText(blob));
      if (!safeSvg) throw new Error("Invalid SVG image");
      blob = new Blob([safeSvg], { type: "image/svg+xml" });
    }
    if (ticket !== hydrationTicket || epoch.value !== myEpoch || owner !== String(unref(context?.agentId) || "") || !holder.isConnected || !blob) return;
    let url = loaded.get(`${key}\0url`);
    if (!url) {
      url = URL.createObjectURL(blob);
      objectUrls.add(url);
      loaded.set(`${key}\0url`, url);
    }
    const img = document.createElement("img");
    img.src = url;
    img.alt = label;
    img.className = "resource-markdown__image";
    img.setAttribute("aria-label", `查看图片：${label}`);
    img.onerror = () => {
      if (holder.isConnected) showImageError(holder, label);
    };
    removeIcon(holder);
    holder.replaceChildren(img);
    holder.disabled = false;
    holder.setAttribute("aria-label", `查看图片：${label}`);
    holder.onclick = (event) => {
      event.preventDefault();
      event.stopPropagation();
      openLightbox([{ src: url, alt: label }]);
    };
  } catch {
    if (ticket === hydrationTicket && epoch.value === myEpoch && holder.isConnected) showImageError(holder, label);
  }
}

async function hydrate() {
  const ticket = ++hydrationTicket;
  const myEpoch = epoch.value;
  const owner = String(props.entry?.agent_id || props.agentId || "");
  clearHtmlPreviewFrames();
  iconHosts.forEach((el) => render(null, el));
  iconHosts.clear();
  await nextTick();
  if (!root.value || ticket !== hydrationTicket || epoch.value !== myEpoch || owner !== String(props.entry?.agent_id || props.agentId || "")) return;
  const activeOwner = String(unref(context?.agentId) || "");
  const ownerIsActive = !!owner && owner === activeOwner;
  const resources = rendered.value.resources;
  const previews = rendered.value.previews || [];
  prunePreviewUrls(previews);
  const activeHtmlRenderKeys = new Set();
  let imageCount = 0;
  for (const node of root.value.querySelectorAll("[data-resource-index]")) {
    if (ticket !== hydrationTicket || epoch.value !== myEpoch) return;
    if (!node.isConnected) continue;
    const target = resources[Number(node.dataset.resourceIndex)];
    if (!target) continue;
    const canOpen = ownerIsActive && !!context?.openResource;
    if (target.type === "image") {
      imageCount++;
      const label = node.textContent || "图片";
      const holder = button(label, "image", !ownerIsActive || !context?.readImage || imageCount > MAX_IMAGES);
      holder.setAttribute("aria-label", `查看图片：${label}`);
      holder.dataset.resourceImage = "true";
      node.replaceChildren(holder);
      if (!holder.disabled) {
        void loadImage(holder, target, label, owner, myEpoch, ticket);
      } else {
        removeIcon(holder);
        holder.textContent = imageCount > MAX_IMAGES ? `${label}（图片数量已达上限）` : `${label}（不可用）`;
      }
    } else {
      const label = node.textContent || (target.type === "file" ? target.path : target.id);
      const holder = button(label, target.type === "file" ? "file" : "terminal", !canOpen);
      holder.setAttribute("aria-label", `${target.type === "file" ? "打开文件" : "打开终端"}：${label}`);
      holder.onclick = (event) => {
        event.preventDefault();
        event.stopPropagation();
        if (canOpen && epoch.value === myEpoch) context.openResource(target, { agentId: owner, source: props.source });
      };
      node.replaceChildren(holder);
    }
  }
  for (const node of root.value.querySelectorAll("[data-markdown-preview-index]")) {
    if (ticket !== hydrationTicket || epoch.value !== myEpoch) return;
    if (!node.isConnected) continue;
    const preview = previews[Number(node.dataset.markdownPreviewIndex)];
    if (!preview) continue;
    if (preview.type === "html") {
      const block = node.closest(".markdown-render-block");
      const channel = createPreviewChannel();
      const documentSource = createHtmlPreviewDocument(preview.source, channel);
      if (!documentSource) {
        node.textContent = "HTML 预览不可用";
        node.setAttribute("role", "status");
        syncRenderBlock(block, node);
        continue;
      }
      const frame = document.createElement("iframe");
      frame.setAttribute("sandbox", "allow-scripts");
      frame.setAttribute("referrerpolicy", "no-referrer");
      frame.setAttribute("title", "隔离的 HTML 预览");
      frame.className = "markdown-live-preview__frame";
      frame.srcdoc = documentSource;
      const renderKey = block?.dataset.renderKey || `render-${Number(node.dataset.markdownPreviewIndex)}`;
      activeHtmlRenderKeys.add(renderKey);
      const compact = props.source === "dock";
      const previousHeight = previewHeights.get(renderKey);
      const canReuseHeight = previousHeight?.source === preview.source && previousHeight.compact === compact;
      const initialHeight = canReuseHeight ? previousHeight.height : HTML_PREVIEW_MIN_HEIGHT;
      if (!canReuseHeight) previewHeights.delete(renderKey);
      frame.style.height = `${initialHeight}px`;
      frame.addEventListener("load", () => requestPreviewMeasure(frame), { once: true });
      node.replaceChildren(frame);
      htmlPreviewFrames.set(channel, { frame, channel, ticket, epoch: myEpoch, renderKey, source: preview.source, compact, raf: 0, pendingHeight: initialHeight });
      syncRenderBlock(block, node);
    } else if (preview.type === "svg") {
      const block = node.closest(".markdown-render-block");
      const safeSvg = sanitizeSvg(preview.source);
      if (!safeSvg) {
        node.textContent = "SVG 预览不可用";
        node.setAttribute("role", "status");
        syncRenderBlock(block, node);
        continue;
      }
      const key = `svg-preview:${preview.source}`;
      let url = previewUrls.get(key);
      if (!url) {
        url = URL.createObjectURL(new Blob([safeSvg], { type: "image/svg+xml" }));
        previewUrls.set(key, url);
        objectUrls.add(url);
      }
      const button = document.createElement("button");
      button.type = "button";
      button.className = "markdown-live-preview__image-button";
      button.setAttribute("aria-label", "放大 SVG 预览");
      const img = document.createElement("img");
      img.src = url;
      img.alt = "SVG 预览";
      img.className = "markdown-live-preview__svg";
      button.append(img);
      button.onclick = (event) => {
        event.preventDefault();
        event.stopPropagation();
        openLightbox([{ src: url, alt: "SVG 预览" }]);
      };
      node.replaceChildren(button);
      syncRenderBlock(block, node);
    }
  }
  for (const key of previewHeights.keys()) {
    if (!activeHtmlRenderKeys.has(key)) previewHeights.delete(key);
  }
}

function resetMarkdownAction(button) {
  if (button) button.textContent = button.dataset.defaultLabel || "复制代码";
  if (markdownActionTimer) {
    clearTimeout(markdownActionTimer);
    markdownActionTimer = null;
  }
}

async function onMarkdownAction(event) {
  const modeButton = event.target?.closest?.('[data-markdown-action="render-mode"]');
  if (modeButton && root.value?.contains(modeButton)) {
    event.preventDefault();
    const block = modeButton.closest(".markdown-render-block");
    const key = block?.dataset.renderKey;
    if (!key) return;
    const mode = modeButton.dataset.renderMode;
    renderModes.set(key, mode);
    syncRenderBlock(block, block.querySelector(".markdown-render-block__preview"));
    if (mode === "preview") requestPreviewMeasure(block.querySelector("iframe"));
    return;
  }
  const button = event.target?.closest?.('[data-markdown-action="copy"]');
  if (!button || !root.value?.contains(button)) return;
  const block = button.closest(".markdown-render-block");
  const code = button.dataset.code || block?.querySelector(".markdown-render-block__source code")?.textContent || button.closest(".markdown-code-block")?.querySelector("code")?.textContent || "";
  if (!code) return;
  button.dataset.defaultLabel ||= button.textContent || "复制代码";
  try {
    button.textContent = (await copyText(code)) ? "已复制" : "复制失败";
  } catch {
    button.textContent = "操作失败";
  }
  if (markdownActionTimer) clearTimeout(markdownActionTimer);
  markdownActionTimer = setTimeout(() => resetMarkdownAction(button), 1600);
}

watch([() => props.agentId, () => props.entry?.agent_id, () => unref(context?.agentId)], () => {
  epoch.value++;
  resourceDomKey.value++;
  renderModes.clear();
  previewHeights.clear();
  releaseUrls();
  hydrate();
});
watch(() => props.entry?.text, () => { epoch.value++; hydrate(); });
watch(() => props.entry?.kind, () => {
  epoch.value++;
  resourceDomKey.value++;
  renderModes.clear();
  previewHeights.clear();
  releaseUrls();
  hydrate();
});
watch(() => props.entry?.id, () => {
  renderModes.clear();
  previewHeights.clear();
  epoch.value++;
  hydrate();
});
onMounted(() => { window.addEventListener("message", onHtmlPreviewMessage); hydrate(); });
onBeforeUnmount(() => { window.removeEventListener("message", onHtmlPreviewMessage); clearHtmlPreviewFrames(); epoch.value++; hydrationTicket++; releaseUrls(); renderModes.clear(); previewHeights.clear(); iconHosts.forEach((el) => render(null, el)); iconHosts.clear(); resetMarkdownAction(); });
</script>

<template>
  <div :key="resourceDomKey" ref="root" class="resource-markdown" :class="{ 'resource-markdown--compact': source === 'dock' }" v-html="html" @click="onMarkdownAction" />
</template>

<style>
.resource-markdown [hidden] { display: none !important; }
.resource-markdown__button { display: inline-flex; align-items: center; gap: 4px; min-width: 0; max-width: 100%; padding: 2px 7px; border: 1px solid var(--color-border); border-radius: 6px; background: var(--color-surface); color: var(--color-primary-strong); font: inherit; cursor: pointer; vertical-align: baseline; }
.resource-markdown__button > span:last-child { min-width: 0; overflow-wrap: anywhere; }
.resource-markdown__button:disabled { color: var(--color-text-subtle); cursor: default; }
.resource-markdown__icon { flex: none; font-size: .9em; }
.resource-markdown__image { display: block; width: min(420px, 70vw); max-width: 100%; max-height: 300px; object-fit: contain; cursor: zoom-in; }
.resource-markdown--compact { width: 100%; min-width: 0; max-width: 100%; box-sizing: border-box; overflow-x: hidden; font-size: 12.5px; line-height: 1.4; overflow-wrap: anywhere; word-break: break-word; }
.resource-markdown.resource-markdown--compact :is(h1, h2, h3, h4, h5, h6) { max-width: 100%; margin: .3em 0 .25em; font-size: 1em; line-height: 1.3; overflow-wrap: anywhere; }
.resource-markdown.resource-markdown--compact h1 { font-size: 1.04em; }
.resource-markdown.resource-markdown--compact p,
.resource-markdown.resource-markdown--compact ul,
.resource-markdown.resource-markdown--compact ol,
.resource-markdown.resource-markdown--compact blockquote { max-width: 100%; margin: .3em 0; }
.resource-markdown--compact ul,
.resource-markdown--compact ol { padding-left: 1.2em; }
.resource-markdown--compact li { margin: .12em 0; }
.resource-markdown--compact img { display: block; width: auto; max-width: 100%; max-height: 220px; height: auto; object-fit: contain; }
.resource-markdown--compact .resource-markdown__image { width: 100%; max-width: 100%; max-height: 220px; }
.resource-markdown--compact .markdown-code-block { width: 100%; min-width: 0; max-width: 100%; box-sizing: border-box; margin: .35em 0; }
.resource-markdown--compact .markdown-code-block > pre { width: 100%; min-width: 0; max-width: 100%; box-sizing: border-box; overflow-x: auto; white-space: pre; }
.resource-markdown--compact .markdown-code-block > pre code { white-space: inherit; overflow-wrap: normal; word-break: normal; }
.resource-markdown--compact :not(pre) > code { white-space: pre-wrap; overflow-wrap: anywhere; word-break: break-word; }
.resource-markdown--compact table { display: block; width: 100%; max-width: 100%; overflow-x: auto; table-layout: fixed; }
.resource-markdown--compact th,
.resource-markdown--compact td { overflow-wrap: anywhere; word-break: break-word; }
.markdown-render-block { max-width: 100%; min-width: 0; }
.markdown-render-block__toolbar { display: flex; justify-content: flex-end; min-height: 24px; }
.markdown-render-block__toolbar .markdown-code-block__action { border-color: transparent; background: transparent; }
.markdown-render-block__toolbar .markdown-code-block__action:hover,
.markdown-render-block__toolbar .markdown-code-block__action:focus-visible { border-color: var(--color-primary); background: var(--color-primary-soft); }
.markdown-render-block__preview { max-width: 100%; min-width: 0; }
.markdown-render-block__source { max-width: 100%; min-width: 0; }
.markdown-render-block__source .markdown-code-block { margin: 0; }
.markdown-render-block__source .markdown-code-block__toolbar { border-radius: var(--radius-sm) var(--radius-sm) 0 0; }
.markdown-render-block__source pre { margin-top: 0; border-radius: 0 0 var(--radius-sm) var(--radius-sm); }
.markdown-live-preview__frame { display: block; width: 100%; border: 0; background: transparent; }
.markdown-live-preview__image-button { display: block; max-width: 100%; margin: 0; padding: 0; border: 0; background: transparent; cursor: zoom-in; }
.markdown-live-preview__svg { display: block; width: auto; max-width: 100%; max-height: 420px; object-fit: contain; }
.resource-markdown--compact .markdown-live-preview__svg { max-height: 200px; }
</style>
