<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import UiIcon from '@desktop-ui/components/UiIcon.vue'

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
  const focusables = [...(dialogRoot.value?.querySelectorAll('button:not(:disabled), input:not(:disabled)') || [])]
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
  <div v-if="open" class="terminal-open-scrim" @click.self="emit('cancel')">
    <section ref="dialogRoot" class="terminal-open-dialog" role="dialog" aria-modal="true" aria-labelledby="terminal-open-title" tabindex="-1" @keydown="trapFocus">
      <header><div><h2 id="terminal-open-title">打开终端</h2><p>恢复当前 Agent 的已有终端，或创建一个新终端。</p></div><button class="desktop-icon-button" type="button" aria-label="取消" title="关闭" @click="emit('cancel')"><UiIcon name="close" :size="16" /></button></header>
      <section class="terminal-open-section">
        <h3>已有终端</h3>
        <div v-if="sessionsLoading" class="terminal-open-state">正在读取当前 Agent 的运行中终端…</div>
        <div v-else-if="sessionsError" class="terminal-open-state terminal-open-error"><p>{{ sessionsError }}</p><button type="button" @click="emit('retry-sessions')">重试列表</button></div>
        <div v-else-if="sessions.length" class="terminal-open-sessions">
          <button v-for="session in sessions" :key="session.terminal_id" type="button" class="terminal-open-session" @click="emit('resume', session)">
            <span><strong>{{ session.display_name || session.name || session.shell || '终端' }}</strong><small>{{ session.target_kind === 'linux_channel' ? `SSH ${session.target_id || ''}` : 'Node 本机' }} · {{ session.cwd || '默认目录' }} · #{{ String(session.terminal_id || '').slice(-8) }}</small></span><span>恢复查看</span>
          </button>
        </div>
        <div v-else class="terminal-open-state">当前 Agent 没有可恢复的运行中终端。</div>
      </section>
      <section class="terminal-open-section">
        <h3>新建终端</h3>
        <div class="terminal-open-options" role="radiogroup" aria-label="终端目标">
          <label v-for="choice in selectable" :key="choiceKey(choice)" class="terminal-open-option" :class="{ selected: selectedChoice === choice }">
            <input v-model="selectedId" type="radio" name="terminal-target" :value="choiceKey(choice)">
            <span><strong>{{ choice.label }}</strong><small>{{ choice.hint }}</small></span>
          </label>
        </div>
        <div v-if="channelsLoading" class="terminal-open-state">正在读取当前 Agent 可用的 SSH 主机…</div>
        <div v-else-if="channelsError" class="terminal-open-state terminal-open-error"><p>{{ channelsError }}</p><button type="button" @click="emit('retry-channels')">重试 SSH 列表</button></div>
        <p v-else-if="!selectable.some(choice => choice.kind === 'linux_channel')" class="terminal-open-hint">没有为当前 Agent 启用的 SSH 主机。你可以在 Linux 设置中配置主机并绑定到此 Agent。</p>
      </section>
      <p class="terminal-open-idle-note">空闲 10 分钟后自动回收。</p>
      <footer>
        <button v-if="!selectable.some(choice => choice.kind === 'linux_channel')" type="button" class="terminal-open-settings" @click="emit('open-settings')">打开 Linux 设置</button>
        <span class="terminal-open-spacer"></span>
        <button type="button" @click="emit('cancel')">取消</button>
        <button type="button" class="terminal-open-create" :disabled="!selectedChoice" @click="selectedChoice && emit('create', selectedChoice)">创建终端</button>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.terminal-open-scrim{position:fixed;inset:0;z-index:1100;display:grid;place-items:center;padding:24px;background:#10191d99}
.terminal-open-dialog{width:min(520px,100%);border:1px solid #d6dfe2;border-radius:14px;background:#fff;color:#263740;box-shadow:0 20px 64px #10212b40;padding:20px}
.terminal-open-dialog header,.terminal-open-dialog footer{display:flex;align-items:center;gap:10px}
.terminal-open-dialog header{justify-content:space-between;margin-bottom:16px}
.terminal-open-dialog h2{margin:0;font-size:18px}.terminal-open-dialog p{margin:5px 0 0;color:#627581;font-size:13px}
.terminal-open-dialog button{min-height:34px;border:1px solid #d1d9de;border-radius:7px;background:#fff;color:#263740;padding:6px 11px;cursor:pointer}
.terminal-open-dialog header button{display:grid;place-items:center;flex:0 0 32px;width:32px;height:32px;padding:0;line-height:0}.terminal-open-dialog button:disabled{opacity:.5;cursor:default}
.terminal-open-options{display:grid;gap:8px}.terminal-open-option{display:flex;align-items:flex-start;gap:11px;padding:12px;border:1px solid #dce5eb;border-radius:9px;cursor:pointer}.terminal-open-option.selected{border-color:#70787c;background:#f3f4f4}.terminal-open-option input{margin-top:3px;accent-color:#353c40}.terminal-open-option span{display:grid;gap:4px;min-width:0}.terminal-open-option small{color:#667982;overflow-wrap:anywhere}
.terminal-open-dialog{max-height:min(88vh,760px);overflow:auto}.terminal-open-section{margin:12px 0}.terminal-open-section h3{margin:0 0 5px;font-size:14px}.terminal-open-section>p{margin:0 0 8px;color:#667982;font-size:12px}.terminal-open-sessions{display:grid;gap:6px;max-height:190px;overflow:auto}.terminal-open-session{display:flex;align-items:center;justify-content:space-between;text-align:left;gap:14px;width:100%;padding:9px 11px!important}.terminal-open-session span:first-child{display:grid;min-width:0;gap:3px}.terminal-open-session small{color:#667982;overflow-wrap:anywhere}.terminal-open-session span:last-child{white-space:nowrap;font-size:12px}.terminal-open-idle-note{padding:9px 11px;border-left:3px solid #aab3b7;background:#f6f7f7;color:#59666b;font-size:12px}
.terminal-open-hint,.terminal-open-state{padding:12px;border-radius:8px;background:#f3f6f7;color:#52616a;font-size:13px}.terminal-open-error{color:#9a332f;background:#fff5f3}.terminal-open-error p{margin:0}.terminal-open-dialog footer{margin-top:18px}.terminal-open-spacer{flex:1}.terminal-open-dialog .terminal-open-settings{margin-right:auto}.terminal-open-dialog .terminal-open-create{background:#353c40;color:#fff;border-color:#353c40}
</style>
