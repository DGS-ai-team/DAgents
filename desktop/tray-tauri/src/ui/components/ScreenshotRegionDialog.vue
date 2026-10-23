<script setup>
import { computed, onMounted, ref } from 'vue'
import UiIcon from '@desktop-ui/components/UiIcon.vue'

defineProps({ imageUrl: { type: String, required: true } })
const emit = defineEmits(['cancel', 'confirm', 'error'])
const stage = ref(null)
const image = ref(null)
const selection = ref(null)
const drawing = ref(false)
const origin = ref(null)
const hasSelection = computed(() => Boolean(selection.value && selection.value.width >= 8 && selection.value.height >= 8))
const selectionDimensions = computed(() => {
  const box = selection.value
  const source = image.value
  const bounds = stage.value?.getBoundingClientRect()
  if (!box || !source || !bounds) return ''
  return `${Math.round(box.width * source.naturalWidth / bounds.width)} × ${Math.round(box.height * source.naturalHeight / bounds.height)} px`
})
const selectionStyle = computed(() => {
  const box = selection.value
  if (!box) return {}
  return { left: `${box.left}px`, top: `${box.top}px`, width: `${box.width}px`, height: `${box.height}px` }
})

onMounted(() => stage.value?.focus())

function pointFromEvent(event) {
  const bounds = stage.value?.getBoundingClientRect()
  if (!bounds) return null
  return {
    x: Math.max(0, Math.min(bounds.width, event.clientX - bounds.left)),
    y: Math.max(0, Math.min(bounds.height, event.clientY - bounds.top)),
  }
}

function onPointerDown(event) {
  if (event.button !== 0) return
  const point = pointFromEvent(event)
  if (!point) return
  event.preventDefault()
  stage.value?.setPointerCapture?.(event.pointerId)
  drawing.value = true
  origin.value = point
  selection.value = { left: point.x, top: point.y, width: 0, height: 0 }
}

function onPointerMove(event) {
  if (!drawing.value || !origin.value) return
  const point = pointFromEvent(event)
  if (!point) return
  const left = Math.min(origin.value.x, point.x)
  const top = Math.min(origin.value.y, point.y)
  selection.value = {
    left,
    top,
    width: Math.abs(point.x - origin.value.x),
    height: Math.abs(point.y - origin.value.y),
  }
}

function onPointerUp(event) {
  if (drawing.value && Number.isFinite(event?.clientX) && Number.isFinite(event?.clientY)) onPointerMove(event)
  drawing.value = false
  origin.value = null
}

function onKeydown(event) {
  if (event.key !== 'Escape') return
  event.preventDefault()
  event.stopPropagation()
  emit('cancel')
}

function resetSelection() {
  selection.value = null
  stage.value?.focus()
}

async function confirmSelection() {
  const box = selection.value
  const source = image.value
  const bounds = stage.value?.getBoundingClientRect()
  if (!box || !source || !bounds || !hasSelection.value) return

  const scaleX = source.naturalWidth / bounds.width
  const scaleY = source.naturalHeight / bounds.height
  const x = Math.max(0, Math.floor(box.left * scaleX))
  const y = Math.max(0, Math.floor(box.top * scaleY))
  const width = Math.min(source.naturalWidth - x, Math.ceil(box.width * scaleX))
  const height = Math.min(source.naturalHeight - y, Math.ceil(box.height * scaleY))
  if (width < 1 || height < 1) return

  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const context = canvas.getContext('2d')
  if (!context) return
  context.drawImage(source, x, y, width, height, 0, 0, width, height)
  try {
    const blob = await new Promise((resolve, reject) => canvas.toBlob(value => value ? resolve(value) : reject(new Error('无法生成截图文件。')), 'image/png'))
    emit('confirm', new File([blob], `截图-${Date.now()}.png`, { type: 'image/png' }))
  } catch (error) {
    emit('error', String(error?.message || error || '无法生成截图文件。'))
  }
}
</script>

<template>
  <section class="screenshot-select-scrim" role="dialog" aria-modal="true" aria-labelledby="screenshot-select-title" @keydown="onKeydown">
    <div
      ref="stage"
      class="screenshot-stage"
      tabindex="0"
      aria-label="冻结的屏幕画面，拖动以选择截图区域"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerUp"
    >
      <img ref="image" :src="imageUrl" alt="冻结的屏幕画面" draggable="false">
      <div v-if="selection" class="screenshot-selection" :class="{ 'is-drawing': drawing }" :style="selectionStyle"></div>
    </div>
    <header class="screenshot-select-header">
      <div class="screenshot-select-instruction">
        <strong id="screenshot-select-title">选择截图区域</strong>
        <span>拖动框选 · Esc 取消</span>
      </div>
      <button class="screenshot-select-close" type="button" aria-label="取消截图" title="取消截图" @click="emit('cancel')"><UiIcon name="close" :size="16" /></button>
    </header>
    <footer class="screenshot-select-footer">
      <span class="screenshot-select-hint" aria-live="polite">{{ hasSelection ? selectionDimensions : '拖动鼠标框选要发送的区域' }}</span>
      <button type="button" class="screenshot-select-reset" :disabled="!selection" @click="resetSelection">重选</button>
      <button type="button" class="is-primary" :disabled="!hasSelection" @click="confirmSelection">添加截图</button>
    </footer>
  </section>
</template>

<style scoped>
.screenshot-select-scrim { position:fixed; z-index:9999; inset:0; width:100vw; height:100vh; overflow:hidden; background:#111; color:#30383c; }
.screenshot-stage { position:absolute; inset:0; width:100%; height:100%; overflow:hidden; cursor:crosshair; line-height:0; touch-action:none; user-select:none; outline:none; }
.screenshot-stage:focus-visible { outline:2px solid #fff; outline-offset:-3px; }
.screenshot-stage img { display:block; width:100%; height:100%; object-fit:fill; pointer-events:none; user-select:none; }
.screenshot-selection { position:absolute; z-index:1; box-sizing:border-box; border:1px solid #fff; outline:1px solid #30383c; box-shadow:0 0 0 9999px #111a21a8; pointer-events:none; }
.screenshot-selection.is-drawing { border-color:#fff; }
.screenshot-select-header { position:absolute; z-index:2; top:18px; left:20px; right:20px; display:flex; align-items:flex-start; justify-content:space-between; gap:16px; pointer-events:none; }
.screenshot-select-instruction { display:flex; align-items:center; gap:10px; min-height:38px; padding:0 13px; border:1px solid #d9dfe1; border-radius:10px; background:rgb(255 255 255 / 95%); box-shadow:0 5px 22px #1115; pointer-events:auto; }
.screenshot-select-instruction strong { font-size:12px; font-weight:600; }
.screenshot-select-instruction span { padding-left:10px; border-left:1px solid #e1e5e6; color:#687277; font-size:11px; }
.screenshot-select-close { display:grid; flex:none; width:36px; height:36px; place-items:center; padding:0; border:1px solid #d9dfe1; border-radius:10px; background:rgb(255 255 255 / 95%); color:#434b4f; box-shadow:0 5px 22px #1115; cursor:pointer; pointer-events:auto; }
.screenshot-select-close:hover { background:#f0f2f2; }
.screenshot-select-footer { position:absolute; z-index:2; right:50%; bottom:20px; display:flex; align-items:center; gap:8px; min-height:48px; width:max-content; max-width:calc(100vw - 24px); padding:6px 7px 6px 14px; transform:translateX(50%); border:1px solid #d9dfe1; border-radius:13px; background:rgb(255 255 255 / 96%); box-shadow:0 8px 26px #1115; backdrop-filter:blur(14px); }
.screenshot-select-hint { min-width:180px; color:#5e696e; font-size:11px; }
.screenshot-select-footer button { min-width:64px; height:32px; padding:0 11px; border:1px solid transparent; border-radius:8px; background:transparent; color:#343a3d; font:inherit; font-size:11px; cursor:pointer; }
.screenshot-select-footer button:hover:not(:disabled) { background:#f0f2f2; }
.screenshot-select-footer button.is-primary { border-color:#343a3d; background:#343a3d; color:#fff; }
.screenshot-select-footer button.is-primary:hover:not(:disabled) { background:#22282b; }
.screenshot-select-footer button:disabled { opacity:.42; cursor:default; }
@media (max-width:600px) { .screenshot-select-header { top:10px; left:10px; right:10px; } .screenshot-select-instruction { align-items:flex-start; flex-direction:column; justify-content:center; gap:3px; padding:5px 9px; } .screenshot-select-instruction span { padding:0; border:0; } .screenshot-select-footer { bottom:10px; gap:4px; padding-left:9px; } .screenshot-select-hint { min-width:0; max-width:40vw; } .screenshot-select-footer button { min-width:52px; padding:0 7px; } }
</style>
