<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import UiIcon from '@desktop-ui/components/UiIcon.vue'
import { isCurrentContextMenuSequence, nextContextMenuSequence } from './uiContextMenuState.js'

const props = defineProps({
  modelValue: { type: Object, default: null },
  label: { type: String, default: '' },
  width: { type: Number, default: 240 },
})
const emit = defineEmits(['update:modelValue', 'select'])
const menu = ref(null)
const isOpen = ref(false)
const instance = Symbol('ui-context-menu')
let trigger = null
let position = { x: 8, y: 8 }
let openToken = 0
let disposed = false

function close(restoreFocus = false) {
  if (!props.modelValue || !isOpen.value) return
  const focusTarget = trigger
  isOpen.value = false
  emit('update:modelValue', null)
  if (restoreFocus && focusTarget?.isConnected && isCurrentContextMenuSequence(openToken)) focusTarget.focus?.()
}
function place() {
  const bounds = menu.value?.getBoundingClientRect()
  if (!bounds) return
  const margin = 8
  const width = Math.min(bounds.width, Math.max(0, window.innerWidth - margin * 2))
  const height = Math.min(bounds.height, Math.max(0, window.innerHeight - margin * 2))
  const x = Math.max(margin, Math.min(position.x, window.innerWidth - width - margin))
  const y = Math.max(margin, Math.min(position.y, window.innerHeight - height - margin))
  menu.value.style.left = `${x}px`
  menu.value.style.top = `${y}px`
  menu.value.style.maxHeight = `${Math.max(0, window.innerHeight - margin * 2)}px`
}
async function open() {
  const value = props.modelValue
  if (!value) return
  isOpen.value = true
  const token = nextContextMenuSequence()
  openToken = token
  trigger = value.trigger || document.activeElement
  position = { x: Number(value.x) || 0, y: Number(value.y) || 0 }
  window.dispatchEvent(new CustomEvent('ui-context-menu-open', { detail: instance }))
  await nextTick()
  if (disposed || props.modelValue !== value || !isOpen.value || openToken !== token || !isCurrentContextMenuSequence(token)) return
  place()
  const firstItem = menu.value?.querySelector('[role="menuitem"]:not(:disabled)')
  if (firstItem) firstItem.focus()
  else menu.value?.focus()
}
function onPointerDown(event) {
  if (menu.value && !menu.value.contains(event.target)) close()
}
function onWindowChange(event) {
  if (event.type === 'scroll' && event.target?.nodeType && menu.value?.contains(event.target)) return
  close()
}
function onOtherMenu(event) { if (event.detail !== instance) close() }
function onKeydown(event) {
  if (!props.modelValue || !isOpen.value) return
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close(true)
    return
  }
  if (event.key === 'Tab') { close(true); return }
  const items = [...(menu.value?.querySelectorAll('[role="menuitem"]:not(:disabled)') || [])]
  if (!items.length) return
  const index = items.indexOf(document.activeElement)
  let next = null
  if (event.key === 'ArrowDown') next = (index + 1 + items.length) % items.length
  else if (event.key === 'ArrowUp') next = index <= 0 ? items.length - 1 : index - 1
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = items.length - 1
  if (next !== null) { event.preventDefault(); event.stopPropagation(); items[next].focus() }
  else if (event.key === ' ' && index >= 0) { event.preventDefault(); event.stopPropagation(); items[index].click() }
}
async function select(item) {
  if (item.disabled || item.separator) return
  const snapshot = props.modelValue
  close(true)
  await nextTick()
  emit('select', item, snapshot)
}
watch(() => props.modelValue, value => { if (value) void open(); else isOpen.value = false })
onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown, true)
  document.addEventListener('keydown', onKeydown, true)
  window.addEventListener('resize', onWindowChange)
  window.addEventListener('scroll', onWindowChange, true)
  window.addEventListener('ui-context-menu-open', onOtherMenu)
  if (props.modelValue) void open()
})
onBeforeUnmount(() => {
  disposed = true
  document.removeEventListener('pointerdown', onPointerDown, true)
  document.removeEventListener('keydown', onKeydown, true)
  window.removeEventListener('resize', onWindowChange)
  window.removeEventListener('scroll', onWindowChange, true)
  window.removeEventListener('ui-context-menu-open', onOtherMenu)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue && isOpen" ref="menu" class="ui-context-menu" role="menu" tabindex="-1" :aria-label="label || modelValue.title || modelValue.context?.title || modelValue.context?.name" :style="{ left: '8px', top: '8px', width: `${width}px` }" @contextmenu.prevent>
      <div v-if="modelValue.title || modelValue.context" class="ui-context-menu__context">
        <strong v-if="modelValue.title || modelValue.context?.title" :title="modelValue.title || modelValue.context.title">{{ modelValue.title || modelValue.context.title }}</strong>
        <span v-if="(modelValue.context?.summary || modelValue.context?.path) && (modelValue.context.summary || modelValue.context.path) !== (modelValue.title || modelValue.context?.title)" :title="modelValue.context.summary || modelValue.context.path">{{ modelValue.context.summary || modelValue.context.path }}</span>
      </div>
      <div v-for="(item, index) in modelValue.items || []" :key="`${item.label || 'separator'}-${index}`">
        <div v-if="item.separator" class="ui-context-menu__separator" role="separator" />
        <button v-else type="button" role="menuitem" tabindex="-1" :disabled="item.disabled" :title="item.title" :class="{ 'is-danger': item.danger }" @click="select(item)">
          <span v-if="item.icon" class="ui-context-menu__icon"><UiIcon :name="item.icon" :size="15" /></span>
          <span class="ui-context-menu__label">{{ item.label }}</span>
          <small v-if="item.summary" class="ui-context-menu__summary">{{ item.summary }}</small>
        </button>
      </div>
    </div>
  </Teleport>
</template>
