<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import * as pdfjs from 'pdfjs-dist'
import pdfWorkerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import UiIcon from '@desktop-ui/components/UiIcon.vue'

pdfjs.GlobalWorkerOptions.workerSrc = pdfWorkerUrl

const props = defineProps({ bytes: { type: Uint8Array, default: () => new Uint8Array() } })
const emit = defineEmits(['selection', 'reference-contextmenu'])
const canvas = ref(null)
const loading = ref(false)
const error = ref('')
const pageNumber = ref(1)
const pageCount = ref(0)
const zoom = ref(1)
const pageWidth = ref(0)
const pageHeight = ref(0)
const selection = ref(null)
let loadingTask = null
let renderTask = null
let documentProxy = null
let pageViewport = null
let pageTextItems = []
let loadSequence = 0
let renderSequence = 0
let pointerStart = null

const canPrevious = computed(() => pageNumber.value > 1 && !loading.value)
const canNext = computed(() => pageNumber.value < pageCount.value && !loading.value)
const selectionStyle = computed(() => selection.value ? {
  left: `${selection.value.x * 100}%`,
  top: `${selection.value.y * 100}%`,
  width: `${selection.value.width * 100}%`,
  height: `${selection.value.height * 100}%`,
} : {})

async function destroyDocument() {
  if (renderTask) { try { renderTask.cancel() } catch { /* A completed render cannot be cancelled. */ } }
  renderTask = null
  const task = loadingTask
  loadingTask = null
  documentProxy = null
  if (task) { try { await task.destroy() } catch { /* Ignore teardown races. */ } }
}

async function loadDocument() {
  const sequence = ++loadSequence
  await destroyDocument()
  if (!props.bytes?.byteLength) { pageCount.value = 0; return }
  loading.value = true
  error.value = ''
  selection.value = null
  emit('selection', null)
  try {
    const bytes = new Uint8Array(props.bytes)
    const task = pdfjs.getDocument({ data: bytes, isEvalSupported: false })
    loadingTask = task
    const document = await task.promise
    if (sequence !== loadSequence) { await task.destroy(); return }
    documentProxy = document
    pageCount.value = document.numPages
    pageNumber.value = 1
    await renderPage(sequence)
  } catch (cause) {
    if (sequence === loadSequence) error.value = String(cause?.message || cause)
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

async function renderPage(sequence = loadSequence) {
  if (!documentProxy || !canvas.value) return
  const renderId = ++renderSequence
  const previousRender = renderTask
  renderTask = null
  if (previousRender) {
    try { previousRender.cancel() } catch { /* Ignore finished render tasks. */ }
    try { await previousRender.promise } catch { /* A cancelled page render is expected. */ }
  }
  try {
    const page = await documentProxy.getPage(pageNumber.value)
    if (sequence !== loadSequence || renderId !== renderSequence || !canvas.value) return
    const viewport = page.getViewport({ scale: zoom.value })
    const pixelRatio = Math.min(window.devicePixelRatio || 1, 1.5, Math.sqrt(16_000_000 / Math.max(1, viewport.width * viewport.height)))
    const target = canvas.value
    target.width = Math.ceil(viewport.width * pixelRatio)
    target.height = Math.ceil(viewport.height * pixelRatio)
    target.style.width = `${viewport.width}px`
    target.style.height = `${viewport.height}px`
    pageWidth.value = viewport.width
    pageHeight.value = viewport.height
    pageViewport = viewport
    selection.value = null
    emit('selection', null)
    const context = target.getContext('2d', { alpha: false })
    const task = page.render({ canvas: target, canvasContext: context, viewport, transform: pixelRatio === 1 ? null : [pixelRatio, 0, 0, pixelRatio, 0, 0], background: '#ffffff' })
    renderTask = task
    await task.promise
    if (sequence !== loadSequence || renderId !== renderSequence) return
    const text = await page.getTextContent()
    if (sequence === loadSequence && renderId === renderSequence) pageTextItems = text.items.filter(item => item.str?.trim())
  } catch (cause) {
    if (cause?.name !== 'RenderingCancelledException' && sequence === loadSequence && renderId === renderSequence) error.value = String(cause?.message || cause)
  } finally {
    if (renderTask && renderTask === previousRender) renderTask = null
    else if (renderId === renderSequence) renderTask = null
  }
}

function setPage(delta) {
  const next = pageNumber.value + delta
  if (!documentProxy || next < 1 || next > pageCount.value) return
  pageNumber.value = next
  void renderPage()
}
function setZoom(delta) {
  zoom.value = Math.max(0.6, Math.min(2.2, Math.round((zoom.value + delta) * 10) / 10))
  void renderPage()
}
function pointerPoint(event) {
  const bounds = event.currentTarget.getBoundingClientRect()
  return {
    x: Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width)),
    y: Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height)),
  }
}
function onPointerDown(event) {
  if (event.button !== 0 || loading.value) return
  event.preventDefault()
  event.currentTarget.setPointerCapture?.(event.pointerId)
  pointerStart = pointerPoint(event)
  selection.value = { x: pointerStart.x, y: pointerStart.y, width: 0, height: 0 }
}
function onPointerMove(event) {
  if (!pointerStart) return
  const point = pointerPoint(event)
  selection.value = {
    x: Math.min(pointerStart.x, point.x),
    y: Math.min(pointerStart.y, point.y),
    width: Math.abs(point.x - pointerStart.x),
    height: Math.abs(point.y - pointerStart.y),
  }
}
function selectedText(rect) {
  if (!pageViewport || !pageWidth.value || !pageHeight.value) return ''
  const bounds = { left: rect.x * pageWidth.value, top: rect.y * pageHeight.value, right: (rect.x + rect.width) * pageWidth.value, bottom: (rect.y + rect.height) * pageHeight.value }
  return pageTextItems.filter(item => {
    const transform = item.transform || []
    if (transform.length < 6) return false
    const point = pageViewport.convertToViewportPoint(transform[4], transform[5])
    const fontHeight = Math.max(4, Math.hypot(transform[2] || 0, transform[3] || 0) * pageViewport.scale)
    const itemWidth = Math.max(2, (item.width || 0) * pageViewport.scale)
    const left = Math.min(point[0], point[0] + itemWidth)
    const right = Math.max(point[0], point[0] + itemWidth)
    const top = point[1] - fontHeight
    const bottom = point[1] + fontHeight * 0.25
    return right >= bounds.left && left <= bounds.right && bottom >= bounds.top && top <= bounds.bottom
  }).map(item => item.str.trim()).filter(Boolean).join(' ').slice(0, 2000)
}
function onPointerUp(event) {
  if (!pointerStart || !selection.value) return
  onPointerMove(event)
  pointerStart = null
  const rect = selection.value
  if (rect.width < 0.012 || rect.height < 0.008) { selection.value = null; emit('selection', null); return }
  emit('selection', {
    type: 'pdf-region',
    label: `第 ${pageNumber.value} 页区域`,
    quote: selectedText(rect),
    location: { page: pageNumber.value, x: round(rect.x), y: round(rect.y), width: round(rect.width), height: round(rect.height) },
  })
}
function round(value) { return Math.round(value * 10000) / 10000 }

watch(() => props.bytes, () => { void loadDocument() }, { immediate: true })
watch(canvas, () => { if (documentProxy) void renderPage() })
onUnmounted(() => { loadSequence++; void destroyDocument() })
</script>

<template>
  <section class="workspace-pdf-preview" aria-label="PDF 文档预览">
    <div class="workspace-pdf-preview__toolbar">
      <button type="button" class="desktop-icon-button" title="上一页" aria-label="上一页" :disabled="!canPrevious" @click="setPage(-1)"><UiIcon name="chevron-left" :size="15" /></button>
      <span>{{ pageCount ? `${pageNumber} / ${pageCount}` : 'PDF' }}</span>
      <button type="button" class="desktop-icon-button" title="下一页" aria-label="下一页" :disabled="!canNext" @click="setPage(1)"><UiIcon name="chevron-right" :size="15" /></button>
      <span class="workspace-pdf-preview__spacer"></span>
      <button type="button" class="desktop-icon-button" title="缩小" aria-label="缩小" :disabled="zoom <= 0.6" @click="setZoom(-0.1)"><span class="workspace-pdf-preview__minus" aria-hidden="true"></span></button>
      <span>{{ Math.round(zoom * 100) }}%</span>
      <button type="button" class="desktop-icon-button" title="放大" aria-label="放大" :disabled="zoom >= 2.2" @click="setZoom(0.1)"><UiIcon name="plus" :size="15" /></button>
    </div>
    <div class="workspace-pdf-preview__viewport">
      <div v-if="loading" class="workspace-pdf-preview__empty">正在载入 PDF…</div>
      <div v-else-if="error" class="workspace-pdf-preview__empty">无法预览此 PDF：{{ error }}</div>
      <div v-else-if="pageCount" class="workspace-pdf-preview__page" :style="{ width: `${pageWidth}px`, height: `${pageHeight}px` }" @pointerdown="onPointerDown" @pointermove="onPointerMove" @pointerup="onPointerUp" @pointercancel="onPointerUp" @contextmenu.prevent="emit('reference-contextmenu', $event)">
        <canvas ref="canvas" aria-label="PDF 页面"></canvas>
        <div v-if="selection" class="workspace-pdf-preview__selection" :style="selectionStyle"></div>
      </div>
      <div v-else class="workspace-pdf-preview__empty">此 PDF 没有可显示的页面。</div>
    </div>
  </section>
</template>

<style scoped>
.workspace-pdf-preview{display:flex;flex:1;min-width:0;min-height:0;flex-direction:column;overflow:hidden;background:#f4f6f6}
.workspace-pdf-preview__toolbar{display:flex;flex:none;align-items:center;gap:6px;min-height:38px;padding:0 10px;border-bottom:1px solid #e1e5e6;background:#fff;color:#697479;font-size:11px}
.workspace-pdf-preview__toolbar .desktop-icon-button{display:grid;flex:none;width:28px;height:28px;place-items:center;border:0;border-radius:5px;background:transparent;color:#4c5559;cursor:pointer}
.workspace-pdf-preview__toolbar .desktop-icon-button:hover:not(:disabled){background:#eceeef}
.workspace-pdf-preview__toolbar .desktop-icon-button:disabled{opacity:.4;cursor:default}
.workspace-pdf-preview__minus{width:10px;height:1.5px;background:currentColor}
.workspace-pdf-preview__spacer{flex:1}
.workspace-pdf-preview__viewport{display:flex;flex:1;min-width:0;min-height:0;justify-content:center;overflow:auto;padding:18px}
.workspace-pdf-preview__page{position:relative;flex:none;box-shadow:0 3px 16px #1721262b;touch-action:none;user-select:none}
.workspace-pdf-preview__page canvas{display:block;background:#fff}
.workspace-pdf-preview__selection{position:absolute;border:2px solid #506873;background:#78909c38;pointer-events:none}
.workspace-pdf-preview__empty{display:grid;flex:1;place-items:center;padding:24px;color:#778186;font-size:12px;text-align:center}
</style>
