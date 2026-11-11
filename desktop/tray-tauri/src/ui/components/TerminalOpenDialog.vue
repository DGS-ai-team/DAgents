<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import UiIcon from '@desktop-ui/components/UiIcon.vue'
import UiSelect from '@desktop-ui/components/UiSelect.vue'

const props = defineProps({
  open: { type: Boolean, default: false },
  choices: { type: Array, default: () => [] },
  sessions: { type: Array, default: () => [] },
  sessionsLoading: { type: Boolean, default: false },
  sessionsError: { type: String, default: '' },
  channelsLoading: { type: Boolean, default: false },
  channelsError: { type: String, default: '' },
})
const emit = defineEmits(['cancel', 'create', 'resume', 'open-settings', 'retry-sessions', 'retry-channels'])
const selectedId = ref('')
const dialogRoot = ref(null)
const selectable = computed(() => props.choices || [])
const choiceKey = choice => choice?.kind === 'local' ? 'local' : String(choice?.targetId || '')
const choiceOptions = computed(() => selectable.value.map(choice => ({
  value: choiceKey(choice),
  label: choice.label,
  description: choice.kind === 'linux_channel' ? choice.hint : '',
  searchText: choice.kind === 'local' ? '本机 local' : 'Linux SSH',
})))
function sessionDescription(session) {
  const target = session.target_kind === 'linux_channel'
    ? selectable.value.find(choice => choice.targetId === session.target_id)?.label || 'Linux'
    : '本机'
  return [target, session.cwd].filter(Boolean).join(' · ')
}
watch(() => props.open, async open => {
  if (!open) { selectedId.value = ''; return }
  if (!selectable.value.some(choice => choiceKey(choice) === selectedId.value)) selectedId.value = choiceKey(selectable.value[0])
  await nextTick()
  dialogRoot.value?.focus()
}, { immediate: true })
watch(() => props.choices, choices => {
  if (!choices.some(choice => choiceKey(choice) === selectedId.value)) selectedId.value = choiceKey(choices[0])
}, { deep: true })
const selectedChoice = computed(() => selectable.value.find(choice => choice.kind === 'local' ? selectedId.value === 'local' : choice.targetId === selectedId.value))
function trapFocus(event) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); emit('cancel'); return }
  if (event.key !== 'Tab') return
  const focusables = [...(dialogRoot.value?.querySelectorAll('button:not(:disabled):not([tabindex="-1"]), input:not(:disabled)') || [])]
  if (!focusables.length) { event.preventDefault(); dialogRoot.value?.focus(); return }
  const first = focusables[0]
  const last = focusables[focusables.length - 1]
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialogRoot.value)) {
    event.preventDefault(); last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault(); first.focus()
  }
}
</script>

<template>
  <div v-if="open" class="ui-dialog-backdrop terminal-open-scrim" @click.self="emit('cancel')">
      <section ref="dialogRoot" class="ui-dialog terminal-open-dialog" role="dialog" aria-modal="true" aria-labelledby="terminal-open-title" tabindex="-1" @keydown="trapFocus">
      <header><h2 id="terminal-open-title">打开终端</h2><button class="btn btn--ghost btn--sm desktop-icon-button" type="button" aria-label="取消" title="关闭" @click="emit('cancel')"><UiIcon name="close" :size="16" /></button></header>
      <section v-if="sessionsLoading || sessionsError || sessions.length" class="terminal-open-section">
        <h3>已有终端</h3>
        <div v-if="sessionsLoading" class="terminal-open-state">正在加载终端…</div>
        <div v-else-if="sessionsError" class="terminal-open-state terminal-open-error"><span :title="sessionsError">终端列表加载失败</span><button class="btn btn--ghost btn--sm" type="button" @click="emit('retry-sessions')">重试</button></div>
        <div v-else-if="sessions.length" class="terminal-open-sessions">
          <button v-for="session in sessions" :key="session.terminal_id" type="button" class="btn btn--default terminal-open-session" @click="emit('resume', session)">
            <span><strong>{{ session.display_name || session.name || session.shell || '终端' }}</strong><small :title="sessionDescription(session)">{{ sessionDescription(session) }}</small></span><span>打开</span>
          </button>
        </div>
      </section>
      <section class="terminal-open-section">
        <h3>终端配置</h3>
        <UiSelect v-model="selectedId" :options="choiceOptions" aria-label="终端配置" search-placeholder="搜索名称或地址" searchable contained />
        <div v-if="channelsLoading" class="terminal-open-state">正在加载配置…</div>
        <div v-else-if="channelsError" class="terminal-open-state terminal-open-error"><span :title="channelsError">Linux 配置加载失败</span><button class="btn btn--ghost btn--sm" type="button" @click="emit('retry-channels')">重试</button></div>
      </section>
      <footer>
        <button type="button" class="btn btn--ghost btn--sm terminal-open-settings" @click="emit('open-settings')">Linux 设置</button>
        <span class="terminal-open-spacer"></span>
        <button class="btn btn--ghost btn--sm" type="button" @click="emit('cancel')">取消</button>
        <button type="button" class="btn btn--primary btn--sm terminal-open-create" :disabled="!selectedChoice" @click="selectedChoice && emit('create', selectedChoice)">打开</button>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.terminal-open-scrim{position:fixed;inset:0;z-index:1100;display:grid;place-items:center;padding:24px}
.terminal-open-dialog{width:min(520px,100%);max-height:min(88vh,760px);overflow:auto}
.terminal-open-dialog header,.terminal-open-dialog footer{display:flex;align-items:center;gap:10px}
.terminal-open-dialog header{justify-content:space-between;margin-bottom:20px}
.terminal-open-dialog h2{margin:0;font-size:18px}
.terminal-open-section{margin:16px 0}.terminal-open-section h3{margin:0 0 8px;font-size:13px;font-weight:500}.terminal-open-sessions{display:grid;gap:6px;max-height:190px;overflow:auto}.terminal-open-session{display:flex;align-items:center;justify-content:space-between;text-align:left;gap:14px;width:100%;padding:9px 11px}.terminal-open-session span:first-child{display:grid;min-width:0;gap:3px}.terminal-open-session small{color:#667982;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.terminal-open-session span:last-child{white-space:nowrap;font-size:12px}
.terminal-open-state{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 0;color:#667982;font-size:12px}.terminal-open-error{color:#9a332f}.terminal-open-dialog footer{margin-top:22px}.terminal-open-spacer{flex:1}.terminal-open-settings{margin-right:auto}
</style>
