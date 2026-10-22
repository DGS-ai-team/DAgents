<script setup>
import { computed } from 'vue'
import { MorphIcon } from 'morphicons/vue'
import { svgToIcon } from 'morphicons/adapters'
import { topSlotState } from '../top-slot-state.js'
import panelOpenSvg from '@shared-icons/desktop/panel-left-open.svg?raw'
import panelCloseSvg from '@shared-icons/desktop/panel-left-close.svg?raw'
import settingsSvg from '@shared-icons/desktop/settings.svg?raw'
import terminalSvg from '@shared-icons/desktop/terminal.svg?raw'
import browserSvg from '@shared-icons/desktop/browser.svg?raw'
import folderOpenSvg from '@shared-icons/desktop/folder-open.svg?raw'
import panelOpenUrl from '@shared-icons/desktop/panel-left-open.svg?url'
import panelCloseUrl from '@shared-icons/desktop/panel-left-close.svg?url'
import autoUrl from '@shared-icons/desktop/auto.svg?url'
import workgroupUrl from '@shared-icons/desktop/nav-workgroups.svg?url'

const props = defineProps({
  tab: { type: String, default: 'chat' },
  collapsed: { type: Boolean, default: false },
  autoOverview: { type: Boolean, default: false },
})
const emit = defineEmits(['toggle'])

function parseStrokeIcon(markup, name) {
  try { return svgToIcon(markup) } catch (error) {
    console.error(`Top-left slot icon ${name} could not be morphed; using the canonical panel mask.`, error)
    return null
  }
}

// Parse the small, known-compatible set once. Filled/transform artwork stays as its approved mask.
const morphIcons = Object.freeze({
  panelOpen: parseStrokeIcon(panelOpenSvg, 'panel-left-open'),
  panelClose: parseStrokeIcon(panelCloseSvg, 'panel-left-close'),
  settings: parseStrokeIcon(settingsSvg, 'settings'),
  terminal: parseStrokeIcon(terminalSvg, 'terminal'),
  browser: parseStrokeIcon(browserSvg, 'browser'),
  'folder-open': parseStrokeIcon(folderOpenSvg, 'folder-open'),
})
const model = computed(() => topSlotState({ tab: props.tab, collapsed: props.collapsed, autoOverview: props.autoOverview }))
const targetIcon = computed(() => {
  if (model.value.icon === 'panel') return morphIcons[props.collapsed ? 'panelOpen' : 'panelClose']
  return morphIcons[model.value.icon] || null
})
const fallbackUrl = computed(() => {
  if (model.value.icon === 'auto') return autoUrl
  if (model.value.icon === 'workgroup') return workgroupUrl
  return targetIcon.value ? '' : (props.collapsed ? panelOpenUrl : panelCloseUrl)
})
const fallbackStyle = computed(() => ({
  maskImage: `url("${fallbackUrl.value}")`,
  WebkitMaskImage: `url("${fallbackUrl.value}")`,
}))
function activate(event) {
  if (!model.value.interactive) return
  if (event.type === 'keydown' && !['Enter', ' '].includes(event.key)) return
  if (event.type === 'keydown') event.preventDefault()
  emit('toggle')
}
</script>

<template>
  <div
    class="top-sidebar-slot"
    :class="{ 'is-interactive': model.interactive }"
    :role="model.interactive ? 'button' : 'img'"
    :tabindex="model.interactive ? 0 : undefined"
    :title="model.actionLabel"
    :aria-label="model.actionLabel"
    :aria-expanded="model.interactive ? model.expanded : undefined"
    :aria-current="model.interactive ? undefined : 'page'"
    @click="activate"
    @keydown="activate"
  >
    <MorphIcon
      class="top-slot-morph"
      :class="{ 'is-hidden': !targetIcon }"
      :icon="targetIcon || morphIcons.panelClose"
      :size="18"
      :stroke-width="1.8"
      reduced-motion="user"
      spring="snappy"
      aria-hidden="true"
    />
    <Transition name="top-slot-swap">
      <span v-if="fallbackUrl" :key="model.icon" class="top-slot-fallback" :style="fallbackStyle" aria-hidden="true"></span>
    </Transition>
  </div>
</template>
