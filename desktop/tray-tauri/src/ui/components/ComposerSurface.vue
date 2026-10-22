<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { COMPOSER_MORPH_DURATION_MS, createComposerMotionController } from '../composer-surface-motion.js'

const props = defineProps({
  mode: { type: String, default: 'chat', validator: value => ['chat', 'dock', 'hidden'].includes(value) },
})
const emit = defineEmits(['transition-start', 'transition-settled'])

const root = ref(null)
const overlay = ref(null)
const activeMode = ref(props.mode)
const transitioning = ref(false)
const overlayStyle = ref({})
const motionController = createComposerMotionController()
let transitionId = 0
let resizeObserver
let resizeFrame = 0
let lastVisibleRect

function surfaceFor(mode) {
  if (mode === 'chat') return root.value?.querySelector('.composer')
  if (mode === 'dock') return root.value?.querySelector('.dock-input-surface')
  return null
}

function readRect(element) {
  if (!element) return null
  const rect = element.getBoundingClientRect()
  if (!rect.width || !rect.height) return null
  const style = getComputedStyle(element)
  return { left: rect.left, top: rect.top, width: rect.width, height: rect.height, radius: Number.parseFloat(style.borderTopLeftRadius) || 0 }
}

function currentRect() {
  if (overlay.value && transitioning.value) {
    const rect = readRect(overlay.value)
    if (rect) return rect
  }
  return readRect(surfaceFor(activeMode.value)) || lastVisibleRect
}

function baseStyle(rect) {
  return {
    left: `${rect.left}px`, top: `${rect.top}px`,
    width: `${rect.width}px`, height: `${rect.height}px`,
    borderRadius: `${rect.radius || 18}px`,
  }
}

function cancelMotion() {
  motionController.cancel()
  overlay.value?.getAnimations?.().forEach(animation => animation.cancel())
}

async function moveTo(nextMode, fromOverride) {
  const id = ++transitionId
  cancelMotion()
  emit('transition-start', { id, mode: nextMode })
  const from = fromOverride || currentRect()
  const target = surfaceFor(nextMode)
  await nextTick()
  const to = nextMode === 'hidden'
    ? (from ? { left: from.left + from.width / 2, top: from.top + from.height / 2, width: 1, height: 1, radius: 0 } : null)
    : readRect(target)

  if (id !== transitionId) return
  if (!from || !to || activeMode.value === 'hidden') {
    activeMode.value = nextMode
    transitioning.value = false
    overlayStyle.value = {}
    if (to) lastVisibleRect = to
    emit('transition-settled', { id, mode: nextMode, animated: false })
    return
  }

  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
    activeMode.value = nextMode
    transitioning.value = false
    overlayStyle.value = {}
    lastVisibleRect = to
    emit('transition-settled', { id, mode: nextMode, animated: false })
    return
  }

  transitioning.value = true
  overlayStyle.value = baseStyle(from)
  await nextTick()
  if (id !== transitionId || !overlay.value) return

  const motion = motionController.start(overlay.value, from, to, {
    duration: COMPOSER_MORPH_DURATION_MS,
    fromRadius: from.radius || (activeMode.value === 'dock' ? 11 : 18),
    toRadius: to.radius || (nextMode === 'dock' ? 11 : 18),
    fadeOut: nextMode === 'hidden',
  })
  await motion
  if (id !== transitionId) return
  activeMode.value = nextMode
  transitioning.value = false
  overlayStyle.value = {}
  lastVisibleRect = to
  emit('transition-settled', { id, mode: nextMode, animated: true })
}

function onResize() {
  if (!transitioning.value || resizeFrame) return
  resizeFrame = requestAnimationFrame(() => {
    resizeFrame = 0
    if (!transitioning.value) return
    const from = currentRect()
    void moveTo(props.mode, from)
  })
}

watch(() => props.mode, (nextMode, previousMode) => {
  // This pre-flush snapshot survives parent slot conditions changing in the same render.
  const from = currentRect() || readRect(surfaceFor(previousMode))
  const focused = document.activeElement
  if (focused && root.value?.contains(focused) && nextMode !== activeMode.value) focused.blur?.()
  if (from) lastVisibleRect = from
  void moveTo(nextMode, from)
}, { flush: 'pre' })

watch(root, element => {
  resizeObserver?.disconnect()
  if (!element || typeof ResizeObserver === 'undefined') return
  resizeObserver = new ResizeObserver(onResize)
  resizeObserver.observe(element)
  element.querySelectorAll('.composer, .dock-input-surface').forEach(node => resizeObserver.observe(node))
})

onBeforeUnmount(() => {
  transitionId += 1
  motionController.dispose()
  resizeObserver?.disconnect()
  if (resizeFrame) cancelAnimationFrame(resizeFrame)
})

onMounted(() => {
  emit('transition-settled', { id: transitionId, mode: activeMode.value, animated: false, initial: true })
})
</script>

<template>
  <div
    ref="root"
    class="composer-surface-root"
    :class="{ 'is-transitioning': transitioning }"
    :data-mode="activeMode"
  >
    <div class="composer-surface-slot composer-surface-chat" :class="{ 'is-inactive': activeMode !== 'chat' }" :inert="activeMode !== 'chat' || transitioning" :aria-hidden="activeMode !== 'chat' || transitioning">
      <slot name="chat" />
    </div>
    <div class="composer-surface-slot composer-surface-dock" :class="{ 'is-inactive': activeMode !== 'dock' }" :inert="activeMode !== 'dock' || transitioning" :aria-hidden="activeMode !== 'dock' || transitioning">
      <slot name="dock" />
    </div>
    <Teleport to="body">
      <div v-if="transitioning" class="composer-surface-motion" aria-hidden="true">
        <div ref="overlay" class="composer-surface-motion-shape" :style="overlayStyle" />
      </div>
    </Teleport>
  </div>
</template>
