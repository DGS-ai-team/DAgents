<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { getCurrentWindow } from '@tauri-apps/api/window'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { formatApprovalArguments, recoverFailedSend, routeForTab, switchDraft, tabForRoute } from './workbench-state.js'
import { createTerminalOutputDecoder } from './terminal-output.js'
import { agentStore } from '../../node/webui/frontend/src/stores/agent.js'
import normalAgentIcon from './assets/icons/agent.svg?url'
import autoAgentIcon from './assets/icons/auto.svg?url'
import navAgentsIcon from './assets/icons/nav-agents.svg?url'
import navAutoIcon from './assets/icons/nav-auto.svg?url'
import navWorkgroupsIcon from './assets/icons/nav-workgroups.svg?url'
import panelOpenIcon from './assets/icons/panel-left-open.svg?url'
import panelCloseIcon from './assets/icons/panel-left-close.svg?url'
import terminalIcon from './assets/icons/terminal.svg?url'
import browserIcon from './assets/icons/browser.svg?url'
import fileIcon from './assets/icons/file.svg?url'
import createIcon from './assets/icons/create.svg?url'
import folderOpenIcon from './assets/icons/folder-open.svg?url'
import circleXIcon from './assets/icons/circle-x.svg?url'
import messageIcon from './assets/icons/message.svg?url'
import sendIcon from './assets/icons/send.svg?url'
import playIcon from './assets/icons/play.svg?url'
import chevronDownIcon from './assets/icons/chevron-down.svg?url'
import chevronUpIcon from './assets/icons/chevron-up.svg?url'
import './desktop-ui.css'

const maskStyle = (icon) => ({
  backgroundImage: `url("${icon}")`,
})

const status = ref({ online: false, setup_required: false, version: '', error: '正在连接 Node…' })
const agents = ref([])
const activeId = ref('')
const hydrate = ref(null)
const input = ref('')
const busy = ref(false)
const error = ref('')
const streamingText = ref('')
const scrollBox = ref(null)
const textarea = ref(null)
const activeTab = ref('chat')
const terminalOpened = ref(false)
const collapsedGroup = ref('')
const terminalConnectionId = ref('')
const terminalSessionByAgent = ref({})
const terminalSessions = ref([])
const terminalOutput = ref('')
const terminalStatus = ref('idle')
const terminalError = ref('')
const terminalCommand = ref('')
const dockDraft = ref('')
const inputDrafts = ref({})
const dockDrafts = ref({})
const failedSendDrafts = ref({})
const dockExpanded = ref(false)
const isTauri = !!window.__TAURI_INTERNALS__
const router = useRouter()
const route = useRoute()

watch(() => route.path, path => { activeTab.value = tabForRoute(path) }, { immediate: true })
watch(activeId, id => { agentStore.agentId = id || '' }, { immediate: true })

const previouslyOnline = ref(false)
const sidebarCollapsed = ref(false)
const openMenu = ref('')
const dialog = ref('')
let unlisten = null
let unlistenTerminal = null
let selectionEpoch = 0
let pollTimer = null
let terminalOutputDecoder = createTerminalOutputDecoder()
const currentWindow = isTauri ? getCurrentWindow() : null

const activeAgent = computed(() => agents.value.find(a => a.agent_id === activeId.value))
const failedSendDraft = computed(() => failedSendDrafts.value[activeId.value] || '')
const normalAgents = computed(() => agents.value.filter(a => a.agent_type !== 'auto'))
const autoAgents = computed(() => agents.value.filter(a => a.agent_type === 'auto'))
const transcript = computed(() => (hydrate.value?.transcript || []).filter(m => ['user', 'assistant'].includes(m.kind)))
const fullTranscript = computed(() => hydrate.value?.transcript || [])
const lastToolActivity = computed(() => [...fullTranscript.value].reverse().find(m => ['tool_call', 'tool_result'].includes(m.kind)))
const lastConversationMessage = computed(() => [...transcript.value].reverse().find(m => m.kind === 'user' || m.kind === 'assistant'))
const pending = computed(() => hydrate.value?.pending_hitl || null)
const approvalCalls = computed(() => (pending.value?.items || []).filter(item => item.hitl_type === 'execute_tool' && item.id).map(item => ({ id: item.id, name: item.name, reason: item.approval_reason || '', argumentsText: formatApprovalArguments(item) })))
const phase = computed(() => hydrate.value?.turn_state?.phase || (busy.value ? 'running' : 'idle'))
const agentGenerating = computed(() => busy.value || ['running', 'streaming', 'generating', 'working'].includes(phase.value))
const menuEntries = computed(() => {
  const map = {
    DAgents: [{ label: `Node · ${status.value.online ? '在线' : '离线'}`, hint: status.value.version || '本机', disabled: true }, { label: '检查连接', action: 'refresh' }, { separator: true }, { label: '设置', action: 'settings' }, { separator: true }, { label: '关于 Desktop MVP', action: 'about' }],
    文件: [{ label: '新建对话', disabled: true }, { separator: true }, { label: '打开文件', hint: '未接入', disabled: true }, { label: '打开浏览器', hint: '未接入', disabled: true }, { label: '新建终端', action: 'open-terminal', disabled: !activeId.value || !status.value.online }, { separator: true }, { label: '关闭当前资源', action: 'close-resource', hint: activeTab.value === 'terminal' ? '关闭终端' : '无活动资源', disabled: activeTab.value !== 'terminal' }],
    编辑: [{ label: '全选当前输入', action: 'select' }, { label: '清空当前输入', action: 'clear' }, { separator: true }, { label: '撤销', hint: '未接入', disabled: true }, { label: '重做', hint: '未接入', disabled: true }],
    视图: [{ label: sidebarCollapsed.value ? '展开侧栏' : '收起侧栏', action: 'sidebar' }, { label: '对话', hint: '当前页', disabled: true }, { label: '跳转到待审批', action: 'approval', disabled: !pending.value }],
    运行: [{ label: `当前 Agent · ${agentGenerating.value ? '生成中' : '就绪'}`, disabled: true }, { label: '查看待审批', action: 'approval', disabled: !pending.value }, { separator: true }, { label: '打开终端', action: 'open-terminal', disabled: !activeId.value || !status.value.online }],
    工具: [{ label: '检查 Node 连接', action: 'refresh' }, { separator: true }, { label: '打开终端', action: 'open-terminal', disabled: !activeId.value || !status.value.online }, { label: '浏览器与文件资源', hint: '尚未接入', disabled: true }],
    帮助: [{ label: '关于 Desktop MVP', action: 'about' }],
  }
  return map[openMenu.value] || []
})

async function refreshHealth() {
  try { status.value = await invoke('desktop_node_status'); return status.value }
  catch (e) { status.value = { online: false, error: String(e) }; return status.value }
}
async function minimizeWindow() { if (currentWindow) await currentWindow.minimize() }
async function toggleMaximizeWindow() { if (currentWindow) await currentWindow.toggleMaximize() }
async function closeWindow() { if (currentWindow) await currentWindow.close() }
async function loadAgents() {
  error.value = ''
  await refreshHealth()
  if (!status.value.online) return
  if (status.value.setup_required) { error.value = '请先完成 Node 首次配置。使用托盘菜单“打开控制台”进入首配。'; return }
  try {
    agents.value = await invoke('desktop_list_agents')
    if (!activeId.value || !agents.value.some(a => a.agent_id === activeId.value)) activeId.value = agents.value[0]?.agent_id || ''
    if (activeId.value) await selectAgent(activeId.value)
  } catch (e) { error.value = String(e) }
}
async function selectAgent(id) {
  const changed = activeId.value !== id
  collapsedGroup.value = ''
  if (changed) { terminalOpened.value = false; activeTab.value = 'chat' }
  if (changed && terminalConnectionId.value) {
    void invoke('desktop_terminal_command', { connectionId: terminalConnectionId.value, agentId: activeId.value, commandType: 'detach' }).catch(() => {})
    terminalConnectionId.value = ''
    terminalStatus.value = 'idle'
    terminalOutput.value = ''
    activeTab.value = 'chat'
    terminalOpened.value = false
  }
  if (changed && activeId.value) {
    input.value = switchDraft(inputDrafts.value, activeId.value, id, input.value)
    dockDraft.value = switchDraft(dockDrafts.value, activeId.value, id, dockDraft.value)
  }
  if (changed) {
    streamingText.value = ''
    if (route.path !== '/chat') void router.push('/chat')
  }
  activeId.value = id
  const epoch = ++selectionEpoch
  if (changed) hydrate.value = null
  error.value = ''
  try {
    const snapshot = await invoke('desktop_hydrate_agent', { agentId: id })
    if (epoch === selectionEpoch && activeId.value === id) hydrate.value = snapshot
  } catch (e) { if (epoch === selectionEpoch) error.value = String(e) }
}
async function send() {
  const text = input.value.trim()
  if (!text || !activeId.value || busy.value) return
  const agentId = activeId.value
  input.value = ''
  inputDrafts.value[agentId] = ''
  const sent = await sendMessageToActiveAgent(text)
  if (!sent) input.value = recoverFailedSend(failedSendDrafts.value, agentId, activeId.value, input.value, text)
}
async function sendMessageToActiveAgent(text) {
  text = String(text || '').trim()
  if (!text || !activeId.value || busy.value) return false
  const agentId = activeId.value
  error.value = ''; busy.value = true
  try {
    await invoke('desktop_send_message', { agentId, content: text })
    if (activeId.value === agentId) await selectAgent(agentId)
    return true
  } catch (e) { error.value = String(e) }
  finally { busy.value = false; if (activeId.value === agentId) await selectAgent(agentId) }
  return false
}
async function sendFromDock() {
  const text = dockDraft.value
  if (!text.trim() || !activeId.value || busy.value) return
  const agentId = activeId.value
  dockDraft.value = ''
  dockDrafts.value[agentId] = ''
  const sent = await sendMessageToActiveAgent(text)
  if (!sent) dockDraft.value = recoverFailedSend(failedSendDrafts.value, agentId, activeId.value, dockDraft.value, text)
}
function restoreFailedSendDraft(target) {
  const text = failedSendDraft.value
  if (!text) return
  const draft = target === 'dock' ? dockDraft : input
  if (draft.value.trim()) return
  draft.value = text
  inputDrafts.value[activeId.value] = target === 'chat' ? text : inputDrafts.value[activeId.value]
  dockDrafts.value[activeId.value] = target === 'dock' ? text : dockDrafts.value[activeId.value]
  delete failedSendDrafts.value[activeId.value]
}
async function showTerminal() {
  terminalOpened.value = true
  activeTab.value = 'terminal'
  terminalError.value = ''
  if (terminalConnectionId.value) {
    try {
      await invoke('desktop_terminal_command', { connectionId: terminalConnectionId.value, agentId: activeId.value, commandType: 'resize', rows: 24, cols: 80 })
    } catch {
      terminalConnectionId.value = ''
      if (terminalStatus.value === 'connected') terminalStatus.value = 'disconnected'
    }
  }
  if (!terminalConnectionId.value && activeId.value && status.value.online) {
    try {
      terminalSessions.value = await invoke('desktop_terminal_list', { agentId: activeId.value })
      if (terminalSessionByAgent.value[activeId.value] && !terminalSessions.value.some(item => item.terminal_id === terminalSessionByAgent.value[activeId.value])) {
        terminalSessionByAgent.value[activeId.value] = ''
      }
    } catch (e) { terminalError.value = String(e) }
  }
}
async function openTerminal(sessionId = '') {
  if (!activeId.value || !status.value.online) return
  const agentId = activeId.value
  terminalError.value = ''
  terminalOutput.value = ''
  terminalOutputDecoder = createTerminalOutputDecoder()
  const connectionId = globalThis.crypto?.randomUUID?.() || `term-${Date.now()}-${Math.random().toString(16).slice(2)}`
  terminalConnectionId.value = connectionId
  terminalStatus.value = sessionId ? 'reconnecting' : 'connecting'
  activeTab.value = 'terminal'
  terminalOpened.value = true
  try {
    await invoke('desktop_terminal_open', {
      agentId,
      connectionId,
      sessionId: sessionId || null,
    })
  } catch (e) {
    if (terminalConnectionId.value === connectionId) {
      terminalConnectionId.value = ''
      terminalStatus.value = 'error'
      terminalError.value = String(e)
    }
  }
}
async function terminalSendCommand() {
  const command = terminalCommand.value
  if (!command.trim() || !terminalConnectionId.value || terminalStatus.value !== 'connected') return
  terminalCommand.value = ''
  try {
    await invoke('desktop_terminal_command', { connectionId: terminalConnectionId.value, agentId: activeId.value, commandType: 'input', data: `${command}\r\n` })
  } catch (e) { terminalCommand.value = command; terminalError.value = String(e); terminalStatus.value = 'error' }
}
async function closeTerminal(commandType = 'close') {
  const connectionId = terminalConnectionId.value
  terminalConnectionId.value = ''
  if (connectionId) {
    try { await invoke('desktop_terminal_command', { connectionId, agentId: activeId.value, commandType }) }
    catch (e) { terminalError.value = String(e) }
  }
  if (commandType === 'close' || commandType === 'terminate') delete terminalSessionByAgent.value[activeId.value]
  if (commandType === 'close') { terminalOpened.value = false; activeTab.value = 'chat' }
  terminalStatus.value = commandType === 'detach' ? 'disconnected' : 'closed'
  if (commandType !== 'detach') terminalOutput.value = ''
  if (activeId.value && status.value.online) {
    try { terminalSessions.value = await invoke('desktop_terminal_list', { agentId: activeId.value }) }
    catch { terminalSessions.value = [] }
  }
}
async function decideApproval(approve) {
  const hitl = pending.value
  if (!hitl || busy.value) return
  const agentId = activeId.value
  const ids = approvalCalls.value.map(call => call.id)
  const resumeValue = ids.length
    ? { type: 'selection', approved: approve ? ids : [], rejected: approve ? [] : ids }
    : { type: 'reject' }
  if (hitl.hitl_id) resumeValue.approval_id = hitl.hitl_id
  if (hitl.child_agent_id) resumeValue.child_agent_id = hitl.child_agent_id
  busy.value = true
  try { await invoke('desktop_resume_hitl', { agentId, resumeValue }); if (activeId.value === agentId) await selectAgent(agentId) }
  catch (e) { error.value = String(e) }
  finally { busy.value = false; if (activeId.value === agentId) await selectAgent(agentId) }
}
async function scrollBottom() { await nextTick(); if (scrollBox.value) scrollBox.value.scrollTop = scrollBox.value.scrollHeight }
function runMenuAction(action) {
  openMenu.value = ''
  if (action === 'refresh') loadAgents()
  else if (action === 'settings') showSettings()
  else if (action === 'sidebar') { sidebarCollapsed.value = !sidebarCollapsed.value; collapsedGroup.value = '' }
  else if (action === 'clear') input.value = ''
  else if (action === 'select') textarea.value?.select()
  else if (action === 'approval') document.querySelector('.approval')?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  else if (action === 'open-terminal') void showTerminal()
  else if (action === 'close-resource') void closeTerminal('close')
  else if (action === 'about') dialog.value = action
}
function onDocumentClick(event) { if (!event.target.closest('.menu-anchor')) openMenu.value = '' }
function onKeydown(event) { if (event.key === 'Escape') { openMenu.value = ''; dialog.value = '' } }
function onDesktopBackToChat() { showChat() }
function onDesktopUseChatCommand(event) {
  input.value = String(event.detail?.command || '')
  showChat()
  void nextTick(() => textarea.value?.focus())
}
function showSettings() { void router.push(routeForTab('settings')) }
function showChat() { activeTab.value = 'chat'; void router.push(routeForTab('chat')) }
onMounted(async () => {
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('keydown', onKeydown)
  window.addEventListener('desktop-back-to-chat', onDesktopBackToChat)
  window.addEventListener('desktop-use-chat-command', onDesktopUseChatCommand)
  if (!window.__TAURI_INTERNALS__) {
    status.value = { online: false, setup_required: false, version: '', error: '浏览器预览模式：请在 Tauri Desktop MVP 窗口中连接真实 Node。' }
    error.value = '预览模式仅展示界面；Node 状态、Agent 与消息仅从 Tauri Rust transport 获取。'
    return
  }
  await loadAgents()
  previouslyOnline.value = status.value.online
  unlisten = await listen('desktop-node-event', async ev => {
    const event = ev.payload
    if (event.agent_id !== activeId.value) return
    if (event.event_type === 'assistant' && event.data?.display_type === 'delta') {
      streamingText.value += event.data.content || ''
      await scrollBottom()
      return
    }
    if (['turn_state', 'turn_finished', 'hitl_required', 'tool_call', 'tool_result', 'error'].includes(event.event_type)) {
      const eventAgentId = event.agent_id
      if (activeId.value !== eventAgentId) return
      await selectAgent(eventAgentId)
      if (activeId.value === eventAgentId && ['turn_finished', 'error', 'hitl_required'].includes(event.event_type)) streamingText.value = ''
      await scrollBottom()
    }
  })
  unlistenTerminal = await listen('desktop-terminal-event', ev => {
    const payload = ev.payload || {}
    if (payload.connection_id !== terminalConnectionId.value || payload.agent_id !== activeId.value) return
    const event = payload.event || {}
    if (event.type === 'started') {
      terminalStatus.value = 'connected'
      if (event.session_id) terminalSessionByAgent.value[activeId.value] = event.session_id
    } else if (event.type === 'output') {
      terminalOutput.value = (terminalOutput.value + terminalOutputDecoder.push(event.data)).slice(-120000)
      void nextTick(() => { const el = document.querySelector('.terminal-output'); if (el) el.scrollTop = el.scrollHeight })
    } else if (event.type === 'replay_gap') {
      terminalOutput.value += '\r\n[Node 提示：部分历史输出已过期]\r\n'
    } else if (event.type === 'error') {
      terminalError.value = String(event.error || 'Node 终端错误')
      terminalStatus.value = 'error'
    } else if (event.type === 'exited' || event.type === 'terminated') {
      terminalStatus.value = 'exited'
    } else if (event.type === 'closed' || event.type === 'relay_closed') {
      if (terminalStatus.value !== 'closed' && terminalStatus.value !== 'exited') terminalStatus.value = 'disconnected'
      void invoke('desktop_terminal_command', { connectionId: terminalConnectionId.value, agentId: activeId.value, commandType: 'detach' }).catch(() => {})
      terminalConnectionId.value = ''
    }
  })
  pollTimer = window.setInterval(async () => {
    const current = await refreshHealth()
    if (!previouslyOnline.value && current.online) await loadAgents()
    previouslyOnline.value = current.online
    if (current.online && activeId.value && agentGenerating.value) await selectAgent(activeId.value)
  }, 2500)
})
onUnmounted(() => {
  window.removeEventListener('desktop-back-to-chat', onDesktopBackToChat)
  window.removeEventListener('desktop-use-chat-command', onDesktopUseChatCommand)
  if (terminalConnectionId.value) void invoke('desktop_terminal_command', { connectionId: terminalConnectionId.value, agentId: activeId.value, commandType: 'detach' }).catch(() => {})
  unlisten?.(); unlistenTerminal?.(); clearInterval(pollTimer); document.removeEventListener('click', onDocumentClick); document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="desktop-shell" :class="{ 'settings-mode': activeTab === 'settings' }">
    <header class="topbar" data-tauri-drag-region><div class="top-left" data-tauri-drag-region><div class="menu-anchor"><button class="menu-brand" @click.stop="openMenu = openMenu === 'DAgents' ? '' : 'DAgents'"><span class="brand-mark">D</span><span class="brand-word">DAgents</span><span class="icon-mask menu-chevron" :style="{ maskImage: `url(${chevronDownIcon})` }"></span></button><div v-if="openMenu === 'DAgents'" class="menu-popover"><template v-for="(item, i) in menuEntries" :key="i"><div v-if="item.separator" class="menu-separator"></div><button v-else class="menu-item" :disabled="item.disabled" @click="runMenuAction(item.action)"><span>{{ item.label }}</span><small>{{ item.hint || '' }}</small></button></template></div></div><button class="top-sidebar-toggle" :title="sidebarCollapsed ? '展开侧栏' : '收起侧栏'" :aria-label="sidebarCollapsed ? '展开侧栏' : '收起侧栏'" @click="sidebarCollapsed = !sidebarCollapsed"><span class="icon-mask" :style="maskStyle(sidebarCollapsed ? panelOpenIcon : panelCloseIcon)"></span></button><nav class="app-menus"><div v-for="name in ['文件','编辑','视图','运行','工具','帮助']" :key="name" class="menu-anchor"><button class="menu-trigger" :class="{ active: openMenu === name }" @click.stop="openMenu = openMenu === name ? '' : name">{{ name }}</button><div v-if="openMenu === name" class="menu-popover"><template v-for="(item, i) in menuEntries" :key="i"><div v-if="item.separator" class="menu-separator"></div><button v-else class="menu-item" :disabled="item.disabled" @click="runMenuAction(item.action)"><span>{{ item.label }}</span><small>{{ item.hint || '' }}</small></button></template></div></div></nav><span class="workspace-title"><strong>{{ activeAgent?.display_name || 'DAgents Desktop' }}</strong><span class="crumb"> / {{ activeTab === 'chat' ? '对话' : activeTab === 'settings' ? '设置' : '终端' }}</span></span></div><div class="top-status"><span :class="['status-dot', status.online ? 'good' : 'bad']"></span>{{ status.online ? '已连接' : '未连接' }}</div><div v-if="isTauri" class="window-controls"><button aria-label="最小化" title="最小化" @click="minimizeWindow">−</button><button aria-label="最大化或还原" title="最大化或还原" @click="toggleMaximizeWindow">□</button><button aria-label="关闭到托盘" title="关闭到托盘" @click="closeWindow">×</button></div></header>
    <aside class="rail" :class="{ collapsed: sidebarCollapsed }">
      <div class="sidebar-scroll">
        <section class="side-section">
          <button v-if="sidebarCollapsed" class="collapsed-group" aria-label="智能体" title="智能体" @click="collapsedGroup = collapsedGroup === 'agents' ? '' : 'agents'"><span class="group-icon" :style="maskStyle(navAgentsIcon)"></span></button>
          <template v-else><div class="group-title"><span class="group-icon" :style="maskStyle(navAgentsIcon)"></span><span>智能体</span><small class="group-count">{{ normalAgents.length }}</small></div><button v-for="agent in normalAgents" :key="agent.agent_id" class="agent-item" :class="{ selected: activeId === agent.agent_id }" @click="selectAgent(agent.agent_id)"><span class="agent-icon" :style="maskStyle(normalAgentIcon)"></span><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button></template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'agents'" class="sidebar-popover"><b>智能体 <small>{{ normalAgents.length }}</small></b><button v-for="agent in normalAgents" :key="agent.agent_id" @click="selectAgent(agent.agent_id)"><span class="agent-icon" :style="maskStyle(normalAgentIcon)"></span>{{ agent.display_name || agent.agent_id }}</button><p v-if="!normalAgents.length">暂无智能体</p></div>
        </section>
        <section class="side-section">
          <button v-if="sidebarCollapsed" class="collapsed-group" aria-label="工作组" title="工作组" @click="collapsedGroup = collapsedGroup === 'workgroups' ? '' : 'workgroups'"><span class="group-icon" :style="maskStyle(navWorkgroupsIcon)"></span></button>
          <template v-else><div class="group-title"><span class="group-icon" :style="maskStyle(navWorkgroupsIcon)"></span><span>工作组</span><small class="group-count">0</small></div><p class="group-empty">工作组尚未接入 Desktop</p></template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'workgroups'" class="sidebar-popover"><b>工作组 <small>0</small></b><p>工作组尚未接入 Desktop</p></div>
        </section>
        <section class="side-section">
          <button v-if="sidebarCollapsed" class="collapsed-group" aria-label="自主智能体" title="自主智能体" @click="collapsedGroup = collapsedGroup === 'auto' ? '' : 'auto'"><span class="group-icon" :style="maskStyle(navAutoIcon)"></span></button>
          <template v-else><div class="group-title"><span class="group-icon" :style="maskStyle(navAutoIcon)"></span><span>自主智能体</span><small class="group-count">{{ autoAgents.length }}</small></div><button v-for="agent in autoAgents" :key="agent.agent_id" class="agent-item" :class="{ selected: activeId === agent.agent_id }" @click="selectAgent(agent.agent_id)"><span class="agent-icon" :style="maskStyle(autoAgentIcon)"></span><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button></template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'auto'" class="sidebar-popover"><b>自主智能体 <small>{{ autoAgents.length }}</small></b><button v-for="agent in autoAgents" :key="agent.agent_id" @click="selectAgent(agent.agent_id)"><span class="agent-icon" :style="maskStyle(autoAgentIcon)"></span>{{ agent.display_name || agent.agent_id }}</button><p v-if="!autoAgents.length">暂无自主智能体</p></div>
        </section>
      </div>
      <div class="node-indicator" :class="status.online ? 'online' : 'offline'" :title="status.online ? 'Node 在线' : 'Node 离线'"><span>{{ status.online ? `Node · 在线${status.version ? ' · v' + status.version : ''}` : 'Node · 离线' }}</span><i></i></div>
    </aside>
    <main class="main-panel">
      <RouterView v-if="activeTab === 'settings'" />
      <div v-else class="desktop-chat-workspace">
      <div class="resource-tabs">
        <button class="conversation-tab" :class="{ active: activeTab === 'chat' }" @click="showChat"><span class="icon-mask tab-icon" :style="maskStyle(activeAgent?.agent_type === 'auto' ? autoAgentIcon : normalAgentIcon)"></span><b>{{ activeAgent?.display_name || '当前智能体' }}</b><span>对话</span></button>
        <div class="open-resource-tabs"><div v-if="terminalOpened" class="open-resource-tab" :class="{ active: activeTab === 'terminal' }"><button class="resource-tab-main" @click="showTerminal"><span class="icon-mask tab-icon" :style="maskStyle(terminalIcon)"></span><span>终端 1 · {{ terminalSessions.find(item => item.terminal_id === terminalSessionByAgent[activeId])?.shell || 'shell' }}</span></button><button class="tab-close" aria-label="关闭终端" title="关闭终端" @click="closeTerminal('close')"><span class="icon-mask" :style="maskStyle(circleXIcon)"></span></button></div></div>
        <div class="resource-nav-actions">
          <div class="menu-anchor"><button class="resource-entry" :aria-expanded="openMenu === 'resources'" @click.stop="openMenu = openMenu === 'resources' ? '' : 'resources'"><span class="icon-mask" :style="maskStyle(folderOpenIcon)"></span>资源 · {{ terminalOpened ? 1 : 0 }}<span class="icon-mask chevron-icon" :style="maskStyle(chevronDownIcon)"></span></button><div v-if="openMenu === 'resources'" class="menu-popover resource-popover"><strong>切换资源</strong><button v-if="terminalOpened" class="menu-item" @click="showTerminal(); openMenu = ''">终端 1</button><span v-else class="empty-resource-menu">暂无已打开资源</span></div></div>
          <div class="menu-anchor"><button class="add-resource" aria-label="新增资源" title="新增资源" :aria-expanded="openMenu === 'add-resource'" @click.stop="openMenu = openMenu === 'add-resource' ? '' : 'add-resource'"><span class="icon-mask" :style="maskStyle(createIcon)"></span></button><div v-if="openMenu === 'add-resource'" class="menu-popover resource-popover"><strong>新建资源</strong><button class="menu-item" :disabled="!status.online || !activeId" @click="showTerminal(); openMenu = ''"><span class="icon-mask" :style="maskStyle(terminalIcon)"></span>新建终端</button><button class="menu-item" disabled title="浏览器尚未接入"><span class="icon-mask" :style="maskStyle(browserIcon)"></span>打开浏览器</button><button class="menu-item" disabled title="文件尚未接入"><span class="icon-mask" :style="maskStyle(fileIcon)"></span>打开文件</button></div></div>
        </div>
      </div>
      <div v-if="error || status.setup_required" class="notice" :class="status.setup_required ? 'setup' : ''"><span>{{ error || 'Node 需要首次配置' }}</span><button v-if="status.setup_required" @click="error='请从托盘菜单选择“打开控制台”完成 Node 首次配置。'">查看说明</button><button v-else-if="isTauri" @click="loadAgents">重试</button></div>
      <section v-if="activeTab === 'chat'" class="conversation" ref="scrollBox">
        <div v-if="!status.online" class="empty-state"><div class="empty-icon"><i></i></div><h2>等待本地 Node</h2><p>{{ status.error || '启动 DAgents Node 后，Desktop 会自动连接。' }}</p></div>
        <div v-else-if="status.setup_required" class="empty-state"><div class="empty-icon"><i></i></div><h2>完成 Node 首次配置</h2><p>请从系统托盘菜单打开控制台，完成配置后返回此窗口。</p></div>
        <template v-else-if="activeAgent">
          <div v-if="!transcript.length" class="welcome"><div class="welcome-mark"><span class="icon-mask" :style="maskStyle(activeAgent?.agent_type === 'auto' ? autoAgentIcon : normalAgentIcon)"></span></div><h1>{{ activeAgent.display_name }}</h1><p>与此 Agent 的真实对话会保存在连接的 Node 中。</p><span class="real-tag">真实 Node 会话</span></div>
          <article v-for="(message, index) in transcript" :key="index" :class="['message', message.kind]">
            <div class="speaker">{{ message.kind === 'user' ? '你' : activeAgent.display_name }}</div><div class="message-body">{{ message.text || message.content || '' }}</div>
          </article>
          <article v-if="streamingText" class="message assistant"><div class="speaker">{{ activeAgent.display_name }}</div><div class="message-body">{{ streamingText }}</div></article>
          <div v-if="agentGenerating" class="working"><span class="pulse"></span>Agent 正在生成…</div>
          <div v-if="pending" class="approval"><div class="approval-head"><span class="warning">待处理</span><strong>{{ pending.message || 'Agent 请求用户确认' }}</strong></div><div v-for="call in approvalCalls" :key="call.id" class="tool-row"><code>{{ call.name }}</code><span>{{ call.reason }}</span><pre v-if="call.argumentsText" class="approval-arguments">{{ call.argumentsText }}</pre></div><div v-if="!approvalCalls.length" class="approval-note">此请求需要在 Node 控制台中回答；Desktop MVP 目前只处理工具审批。</div><div v-else class="approval-actions"><button class="quiet" :disabled="busy" @click="decideApproval(false)">拒绝</button><button class="approve" :disabled="busy" @click="decideApproval(true)">批准并继续</button></div></div>
        </template>
      </section>
      <section v-else class="terminal-resource" :class="{ 'dock-expanded': dockExpanded }">
        <div class="terminal-resource-head"><div><strong>本机终端</strong><span class="terminal-status" :class="`state-${terminalStatus}`"><i></i>{{ ({idle:'未连接',connecting:'连接中',reconnecting:'恢复会话中',connected:'已连接',disconnected:'连接已断开',error:'连接错误',exited:'进程已退出',closed:'已关闭'})[terminalStatus] || terminalStatus }}</span></div><div class="terminal-actions"><button v-if="terminalConnectionId" @click="closeTerminal('terminate')">终止进程</button></div></div>
        <div class="terminal-output" aria-live="polite"><div class="terminal-notice">真实 Node PTY · 当前 Agent 独立会话 · 切换到对话页时终端保持连接；切换 Agent 时会脱离连接并保留进程</div><div v-if="!terminalConnectionId && ['idle','disconnected','error','closed','exited'].includes(terminalStatus)" class="terminal-session-choices"><p>创建新终端，或尝试恢复当前 Agent 的已有会话。恢复会尝试附加到原会话；若已有连接占用，Node 会拒绝并显示错误。</p><button @click="openTerminal()">新建终端</button><button v-for="session in terminalSessions.filter(item => item.status === 'running')" :key="session.terminal_id" @click="openTerminal(session.terminal_id)">恢复 {{ session.shell || '终端' }} · {{ session.terminal_id.slice(0, 8) }}</button></div><pre>{{ terminalOutput || (terminalStatus === 'connected' ? '终端已连接。输入命令并按 Enter 执行。' : '') }}</pre><p v-if="terminalError" class="terminal-error">{{ terminalError }}</p></div>
        <form class="terminal-composer" @submit.prevent="terminalSendCommand"><label>PS&gt;</label><input v-model="terminalCommand" :disabled="terminalStatus !== 'connected'" aria-label="终端命令" placeholder="输入 PowerShell / shell 命令" autocomplete="off"><button :disabled="terminalStatus !== 'connected' || !terminalCommand.trim()"><span class="icon-mask play-icon" :style="maskStyle(playIcon)"></span>执行</button></form>
      </section>
      <footer v-if="activeTab === 'chat'" class="composer-wrap"><form class="composer" @submit.prevent="send"><textarea ref="textarea" v-model="input" :disabled="!activeId || !status.online || status.setup_required" placeholder="发送消息…（Enter 发送，Shift+Enter 换行）" @keydown.enter.exact.prevent="send"></textarea><button v-if="failedSendDraft" type="button" class="restore-failed-draft" :disabled="!!input.trim()" @click="restoreFailedSendDraft('chat')">恢复失败消息</button><button class="send-button" :disabled="busy || !input.trim() || !activeId" :aria-label="busy ? '生成中' : '发送消息'" :title="busy ? '生成中' : '发送消息'"><span class="icon-mask send-icon" :style="maskStyle(sendIcon)"></span></button></form><div class="composer-foot"><span>内容发送到当前 Agent</span><span>{{ agentGenerating ? '生成中' : '就绪' }}</span></div></footer>
      <aside v-if="activeTab === 'terminal'" class="terminal-agent-dock" :class="{ expanded: dockExpanded }">
        <button class="dock-heading" @click="dockExpanded = !dockExpanded" :aria-expanded="dockExpanded"><i :class="['dock-dot', { pending: pending, busy: agentGenerating && !pending }]"/><b>{{ activeAgent?.display_name || '当前 Agent' }}</b><span>{{ pending ? '待审批' : agentGenerating ? '生成中' : '空闲' }}</span><span class="icon-mask dock-chevron" :style="maskStyle(dockExpanded ? chevronDownIcon : chevronUpIcon)"></span></button>
        <form class="dock-composer" @submit.prevent="sendFromDock"><input v-model="dockDraft" :disabled="!activeId || !status.online || status.setup_required" aria-label="在终端页向 Agent 发送消息" placeholder="发消息给当前 Agent…"><button v-if="failedSendDraft" type="button" class="restore-failed-draft" :disabled="!!dockDraft.trim()" @click="restoreFailedSendDraft('dock')">恢复</button><button :disabled="busy || !dockDraft.trim()"><span class="icon-mask send-icon" :style="maskStyle(sendIcon)"></span></button></form>
          <div v-if="dockExpanded" class="dock-details">
          <div class="dock-detail-row"><small>最近消息</small><span>{{ lastConversationMessage?.text || '暂无对话消息' }}</span></div>
          <div class="dock-detail-row"><small>最近工具</small><span>{{ lastToolActivity?.summary || lastToolActivity?.data?.tool_name || '暂无工具活动' }}</span></div>
          <div v-if="pending" class="dock-approval"><strong>{{ pending.message || '有待处理的审批' }}</strong><div v-for="call in approvalCalls" :key="call.id" class="dock-approval-call"><code>{{ call.name }}</code><span>{{ call.reason }}</span><pre v-if="call.argumentsText" class="approval-arguments">{{ call.argumentsText }}</pre></div><div v-if="approvalCalls.length" class="approval-actions"><button class="quiet" :disabled="busy" @click.prevent="decideApproval(false)">拒绝</button><button class="approve" :disabled="busy" @click.prevent="decideApproval(true)">批准并继续</button></div><p v-else>此类请求请在 Node 控制台处理。</p></div>
          <button class="dock-return" @click="showChat"><span class="icon-mask message-icon" :style="maskStyle(messageIcon)"></span>返回完整对话</button>
        </div>
      </aside>
      </div>
    </main>
    <div v-if="dialog" class="dialog-scrim" @click.self="dialog = ''"><section class="dialog"><button class="dialog-close" aria-label="关闭" @click="dialog = ''">×</button><h2>关于 Desktop MVP</h2><p>DAgents Desktop MVP · 实验纵向切片</p><p>真实功能：本地 Node 状态、Agent 列表、历史消息、发送、工具审批与终端。</p><p>浏览器、文件与工作组 API 尚未接入。</p><button class="approve" @click="dialog = ''">关闭</button></section></div>
  </div>
</template>
