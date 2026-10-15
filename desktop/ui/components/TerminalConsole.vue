<script setup>
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

const props = defineProps({ output: { type: String, default: '' }, connected: { type: Boolean, default: false } })
const emit = defineEmits(['input', 'resize'])
const host = ref(null)
let terminal
let fitAddon
let resizeObserver
let outputSnapshot = ''

function fit() {
  if (!terminal || !fitAddon || !host.value?.clientWidth || !host.value?.clientHeight) return
  try {
    fitAddon.fit()
  } catch { /* WebView may be between layout passes. */ }
}

onMounted(async () => {
  terminal = new Terminal({
    cursorBlink: true,
    convertEol: false,
    scrollback: 10000,
    fontFamily: 'Cascadia Code, Consolas, monospace',
    fontSize: 13,
    theme: { background: '#111315', foreground: '#d8dedf', cursor: '#d8dedf', selectionBackground: '#50575d88' },
  })
  fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.open(host.value)
  if (props.output) {
    terminal.write(props.output)
    outputSnapshot = props.output
  }
  terminal.onData(data => emit('input', data))
  terminal.onResize(({ rows, cols }) => emit('resize', { rows, cols }))
  resizeObserver = new ResizeObserver(() => fit())
  resizeObserver.observe(host.value)
  await nextTick()
  fit()
})

watch(() => props.output, value => {
  const next = String(value || '')
  if (!terminal || next === outputSnapshot) return
  if (next.startsWith(outputSnapshot)) terminal.write(next.slice(outputSnapshot.length))
  else { terminal.reset(); terminal.write(next) }
  outputSnapshot = next
})
watch(() => props.connected, value => { if (value) void nextTick(fit) })

onUnmounted(() => {
  resizeObserver?.disconnect()
  terminal?.dispose()
})
</script>

<template><div ref="host" class="desktop-xterm" aria-label="终端输出" /></template>

<style scoped>
.desktop-xterm { width: 100%; height: 100%; min-width: 0; min-height: 0; padding: 9px 10px; background: #111315; }
</style>
