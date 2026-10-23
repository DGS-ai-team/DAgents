<script setup>
import { nextTick, ref, watch } from 'vue'

const props = defineProps({ open: { type: Boolean, default: false }, terminalName: { type: String, default: '终端' } })
const emit = defineEmits(['cancel', 'confirm'])
const root = ref(null)
watch(() => props.open, async open => {
  if (!open) return
  await nextTick()
  root.value?.querySelector('button:not(:disabled)')?.focus()
}, { immediate: true })
function onKeydown(event) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); emit('cancel'); return }
  if (event.key !== 'Tab') return
  const buttons = [...(root.value?.querySelectorAll('button:not(:disabled)') || [])]
  if (!buttons.length) { event.preventDefault(); root.value?.focus(); return }
  if (event.shiftKey && (document.activeElement === buttons[0] || document.activeElement === root.value)) { event.preventDefault(); buttons.at(-1).focus() }
  else if (!event.shiftKey && document.activeElement === buttons.at(-1)) { event.preventDefault(); buttons[0].focus() }
}
</script>

<template>
  <div v-if="open" class="dialog-scrim" @click.self="emit('cancel')">
    <section ref="root" class="dialog terminal-end-dialog" role="alertdialog" aria-modal="true" aria-labelledby="terminal-end-title" tabindex="-1" @keydown="onKeydown">
      <h2 id="terminal-end-title">结束终端？</h2>
      <p>结束「{{ terminalName }}」会销毁 Node 上的终端会话并中断其中运行的命令。当前 Agent 或模型也可能正在使用它。</p>
      <div class="dialog-actions"><button @click="emit('cancel')">取消</button><button class="danger" @click="emit('confirm')">结束终端</button></div>
    </section>
  </div>
</template>
