<script setup>
import { computed, onMounted, ref } from 'vue'
import UiIcon from '../../../node/webui/frontend/src/components/UiIcon.vue'

const props = defineProps({ imageUrl: { type: String, required: true } })
const emit = defineEmits(['cancel', 'confirm', 'error'])
const stage = ref(null)
const image = ref(null)
const selection = ref(null)
const drawing = ref(false)
const origin = ref(null)
const hasSelection = computed(() => Boolean(selection.value && selection.value.width >= 8 && selection.value.height >= 8))
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
  <div class="screenshot-select-scrim" @pointerdown.self="emit('cancel')">
    <section class="screenshot-select-dialog" role="dialog" aria-modal="true" aria-labelledby="screenshot-select-title" @keydown="onKeydown">
      <header class="screenshot-select-header">
        <div>
          <h2 id="screenshot-select-title">选择截图区域</h2>
          <p>拖动鼠标框选区域，确认后添加到消息</p>
        </div>
        <button class="screenshot-select-close" type="button" aria-label="取消截图" title="取消截图" @click="emit('cancel')"><UiIcon name="close" :size="16" /></button>
      </header>
      <div class="screenshot-stage-frame">
        <div
          ref="stage"
          class="screenshot-stage"
          tabindex="0"
          aria-label="截图预览，拖动以选择截图区域"
          @pointerdown="onPointerDown"
          @pointermove="onPointerMove"
          @pointerup="onPointerUp"
          @pointercancel="onPointerUp"
        >
          <img ref="image" :src="imageUrl" alt="屏幕截图预览" draggable="false">
          <div v-if="selection" class="screenshot-selection" :class="{ 'is-drawing': drawing }" :style="selectionStyle"></div>
        </div>
      </div>
      <footer class="screenshot-select-footer">
        <span class="screenshot-select-hint">{{ hasSelection ? '已选中区域' : '请拖动鼠标选择区域' }}</span>
        <button type="button" @click="resetSelection">重新选择</button>
        <button type="button" class="is-primary" :disabled="!hasSelection" @click="confirmSelection">确认截图</button>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.screenshot-select-scrim { position:fixed; z-index:90; inset:0; display:grid; place-items:center; padding:24px; background:#111a1ed9; backdrop-filter:blur(2px); }
.screenshot-select-dialog { display:flex; flex-direction:column; width:min(1120px,calc(100vw - 48px)); max-height:calc(100vh - 48px); overflow:hidden; border:1px solid #d7dcde; border-radius:12px; background:#f8f9f9; color:#30383c; box-shadow:0 18px 60px #0005; }
.screenshot-select-header { display:flex; align-items:center; justify-content:space-between; gap:18px; padding:14px 18px; border-bottom:1px solid #e2e5e6; }
.screenshot-select-header h2 { margin:0; font-size:15px; font-weight:600; }
.screenshot-select-header p { margin:4px 0 0; color:#697378; font-size:12px; }
.screenshot-select-close { display:grid; flex:none; width:30px; height:30px; place-items:center; padding:0; border:0; border-radius:6px; background:transparent; color:#50595e; cursor:pointer; }
.screenshot-select-close:hover { background:#eceeef; }
.screenshot-stage-frame { display:grid; min-height:0; flex:1; place-items:center; overflow:auto; padding:12px; background:#e6e9ea; }
.screenshot-stage { position:relative; display:inline-block; justify-self:center; align-self:center; width:fit-content; max-width:100%; max-height:100%; flex:none; cursor:crosshair; line-height:0; touch-action:none; user-select:none; outline:none; }
.screenshot-stage:focus-visible { box-shadow:0 0 0 2px #454d51; }
.screenshot-stage img { display:block; max-width:min(100%,calc(100vw - 80px)); max-height:calc(100vh - 190px); object-fit:contain; pointer-events:none; user-select:none; }
.screenshot-selection { position:absolute; z-index:1; box-sizing:border-box; border:1px solid #fff; outline:1px solid #313a3e; box-shadow:0 0 0 9999px #10161970; pointer-events:none; }
.screenshot-selection.is-drawing { border-color:#fff; }
.screenshot-select-footer { display:flex; align-items:center; gap:8px; min-height:54px; padding:8px 14px; border-top:1px solid #e2e5e6; }
.screenshot-select-hint { flex:1; color:#6a7479; font-size:12px; }
.screenshot-select-footer button { min-width:76px; height:32px; padding:0 12px; border:1px solid #cbd2d5; border-radius:6px; background:#fff; color:#353c40; font:inherit; font-size:12px; cursor:pointer; }
.screenshot-select-footer button:hover:not(:disabled) { background:#f0f2f2; }
.screenshot-select-footer button.is-primary { border-color:#353c40; background:#353c40; color:#fff; }
.screenshot-select-footer button.is-primary:hover:not(:disabled) { background:#242a2d; }
.screenshot-select-footer button:disabled { opacity:.48; cursor:default; }
@media (max-width:600px) { .screenshot-select-scrim { padding:8px; } .screenshot-select-dialog { width:100%; max-height:calc(100vh - 16px); } .screenshot-stage-frame { padding:6px; } .screenshot-stage img { max-width:calc(100vw - 30px); max-height:calc(100vh - 145px); } .screenshot-select-footer { gap:6px; padding-right:8px; padding-left:8px; } .screenshot-select-footer button { min-width:64px; padding:0 8px; } }
</style>
