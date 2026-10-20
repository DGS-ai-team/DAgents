<script setup>
import { computed, nextTick, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { getCurrentWindow } from '@tauri-apps/api/window'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { agentErrorFor, agentSelectionViewTransition, chooseActiveAgentId, closeResourceTransition, createDesktopMenuModel, desktopChatEntries, desktopHitlQueue, desktopMenuNames, formatApprovalArguments, isAgentBusy, menuFocusIndex, recoverFailedSend, routeForTab, shouldReconnectTerminal, shouldShowNodeFirstRun, switchDraft, tabForRoute, updateAgentBusy, updateAgentError } from './workbench-state.js'
import { ResourceRenderContextKey, validateWorkspacePath } from '../../node/webui/frontend/src/utils/resourceContext.js'
import { closeTerminalFlow } from './terminal-close.js'
import { composerPageFadeOutCompletion, composerPageHandoffNeedsFade, composerPageHandoffReady, composerPageSettlementMatches, composerPageVisibility, createComposerPageHandoff } from './composer-surface-motion.js'
import { findTerminalInstanceForSession, isTerminalDialogRequestCurrent, isWindowsPlatform, reusableTerminalConnection, runningTerminalSessions, terminalDialogChoices, terminalOpenArguments, terminalOpenIntent } from './terminal-open-state.js'
import { agentMediaId, isWorkspaceRequestCurrent, shouldClearWorkspaceUploadError, toNodeFileReference, workspaceContentType, workspaceImageBlob } from './workspace-files-state.js'
import { agentStore } from '../../node/webui/frontend/src/stores/agent.js'
import { expandHitlRequired, extractToolApprovals, buildApprovalOneResume, buildApprovalResume, buildMemoryConflictResume, buildUserInfoSubmitResume } from '../../node/webui/frontend/src/stores/hitl.js'
import { applyHydrateTurnState } from '../../node/webui/frontend/src/stores/turnState.js'
import MainChatPanel from '../../node/webui/frontend/src/components/MainChatPanel.vue'
import ResourceMarkdown from '../../node/webui/frontend/src/components/ResourceMarkdown.vue'
import UiIcon from '../../node/webui/frontend/src/components/UiIcon.vue'
import AgentCreatePage from '../../node/webui/frontend/src/components/AgentCreatePage.vue'
import FirstRunNodeProfile from '../../node/webui/frontend/src/views/FirstRunNodeProfile.vue'
import ComposerToolbar from '../../node/webui/frontend/src/components/ComposerToolbar.vue'
import ImageLightbox from '../../node/webui/frontend/src/components/ImageLightbox.vue'
import TerminalConsole from './components/TerminalConsole.vue'
import TerminalOpenDialog from './components/TerminalOpenDialog.vue'
import TerminalEndDialog from './components/TerminalEndDialog.vue'
import ComposerSurface from './components/ComposerSurface.vue'
import ResourceAgentDock from './components/ResourceAgentDock.vue'
import WorkspaceFiles from './components/WorkspaceFiles.vue'
import BrowserViewer from './components/BrowserViewer.vue'
import WorkgroupWorkspace from './components/WorkgroupWorkspace.vue'
import AutoOverviewPanel from './components/AutoOverviewPanel.vue'
import TopLeftSlotIcon from './components/TopLeftSlotIcon.vue'
import { cancelAgentTurn, clearContext, closeAgentTerminal, compressContext, deleteAgent, getAgent, getLLMSettings, getSetupConfig, patchAgent, patchLLMSettings, submitMessage, uploadExternalToolToManage, uploadPluginToManage, uploadSkillToManage } from '../../node/webui/frontend/src/api/node.js'
import { agentActiveProfile } from '../../node/webui/frontend/src/utils/agentLLM.js'
import { dispatchDesktopSlashCommand } from './slash-commands.js'
import { openLightbox } from '../../node/webui/frontend/src/stores/lightbox.js'
import { readAgentMedia, readWorkspaceFile, uploadWorkspaceFile } from './workspace-files-api.js'
import normalAgentIcon from '@shared-icons/desktop/agent.svg?url'
import autoAgentIcon from '@shared-icons/desktop/auto.svg?url'
import navAgentsIcon from '@shared-icons/desktop/agent.svg?url'
import navAutoIcon from '@shared-icons/desktop/auto.svg?url'
import navWorkgroupsIcon from '@shared-icons/desktop/nav-workgroups.svg?url'
import terminalIcon from '@shared-icons/desktop/terminal.svg?url'
import browserIcon from '@shared-icons/desktop/browser.svg?url'
import fileIcon from '@shared-icons/desktop/file.svg?url'
import attachmentIcon from '@shared-icons/desktop/attachment.svg?url'
import folderOpenIcon from '@shared-icons/desktop/folder-open.svg?url'
import circleXIcon from '@shared-icons/desktop/circle-x.svg?url'
import sendIcon from '@shared-icons/desktop/send.svg?url'
import stopIcon from '@shared-icons/dagents/stop.svg?url'
import './desktop-ui.css'

const maskStyle = (icon) => ({
  maskImage: `url("${icon}")`,
  WebkitMaskImage: `url("${icon}")`,
})

function setAgentBusy(agentId, value) {
  busyByAgent.value = updateAgentBusy(busyByAgent.value, agentId, value)
}
function setAgentCancelling(agentId, value) {
  cancellingByAgent.value = updateAgentBusy(cancellingByAgent.value, agentId, value)
}
function setAgentHitlBusyIndex(agentId, index) {
  const next = { ...hitlBusyIndexByAgent.value }
  if (index < 0) delete next[agentId]
  else next[agentId] = index
  hitlBusyIndexByAgent.value = next
}
function setAgentError(agentId, message) {
  agentErrors.value = updateAgentError(agentErrors.value, agentId, message)
}

const status = ref({ online: false, setup_required: false, version: '', error: '正在连接 Node…' })
const agents = ref([])
const activeId = ref('')
const hydrate = ref(null)
const llmSettings = ref(null)
const input = ref('')
const busyByAgent = ref({})
const cancellingByAgent = ref({})
const hitlBusyIndexByAgent = ref({})
const agentErrors = ref({})
const globalError = ref('')
const commandNotice = ref('')
const busy = computed(() => isAgentBusy(busyByAgent.value, activeId.value))
const cancellingTurn = computed(() => isAgentBusy(cancellingByAgent.value, activeId.value))
const hitlBusyIndex = computed(() => Number.isInteger(hitlBusyIndexByAgent.value[activeId.value]) ? hitlBusyIndexByAgent.value[activeId.value] : -1)
const error = computed({
  get: () => activeId.value ? agentErrorFor(agentErrors.value, activeId.value) : globalError.value,
  set: message => {
    if (!activeId.value) { globalError.value = String(message || ''); return }
    agentErrors.value = updateAgentError(agentErrors.value, activeId.value, message)
  },
})
const workspaceUploadErrors = new Map()
const streamingText = ref('')
const textarea = ref(null)
const composerExpanded = ref(false)
const userInfoSelected = ref([])
const chatPanel = ref(null)
const activeTab = ref('chat')
const displayedTab = ref(activeTab.value)
const visitedTabs = ref(new Set(['chat']))
const pendingComposerPage = ref(null)
const composerPageFadeInTab = ref('')
const composerWorkspace = ref(null)
const workspaceFilesPanel = ref(null)
const browserViewerPanel = ref(null)
const resourceWindowMinimized = ref(false)
const workspaceRequestGeneration = ref(1)
const workspaceNodeIdentity = ref('node:unknown')
let composerPageHandoffSequence = 0
const workspaceOpenedByAgent = ref({})
const workspaceNameByAgent = ref({})
const browserOpenedByAgent = ref({})
const browserNameByAgent = ref({})
const workspaceOpened = computed({ get: () => Boolean(workspaceOpenedByAgent.value[activeId.value]), set: value => { if (activeId.value) workspaceOpenedByAgent.value[activeId.value] = Boolean(value) } })
const workspaceName = computed({ get: () => workspaceNameByAgent.value[activeId.value] || '工作区', set: value => { if (activeId.value) workspaceNameByAgent.value[activeId.value] = String(value || '工作区') } })
const browserOpened = computed({ get: () => Boolean(browserOpenedByAgent.value[activeId.value]), set: value => { if (activeId.value) browserOpenedByAgent.value[activeId.value] = Boolean(value) } })
const browserName = computed({ get: () => browserNameByAgent.value[activeId.value] || '浏览器', set: value => { if (activeId.value) browserNameByAgent.value[activeId.value] = String(value || '浏览器') } })
const collapsedGroup = ref('')
const terminalConnectionId = ref('')
const terminalInstances = ref([])
const activeTerminalInstanceId = ref('')
const terminalSessionByAgent = ref({})
const terminalSessions = ref([])
const terminalResourceName = computed(() => {
  const active = terminalInstances.value.find(item => item.id === activeTerminalInstanceId.value)
  if (active?.name) return active.name
  const sessionId = terminalSessionByAgent.value[activeId.value]
  const session = terminalSessions.value.find(item => item.terminal_id === sessionId)
  return String(session?.display_name || session?.name || session?.shell || 'Shell')
})
const terminalOutput = ref('')
const terminalStatus = ref('idle')
const terminalError = ref('')
const dockDraft = ref('')
const inputDrafts = ref({})
const dockDrafts = ref({})
const failedSendDrafts = ref({})
const pendingUserByAgent = ref({})
const dockExpanded = ref(false)
const dockStatus = computed(() => !status.value.online || status.value.setup_required ? 'offline' : pending.value ? 'pending' : agentGenerating.value ? 'generating' : 'idle')
const isTauri = !!window.__TAURI_INTERNALS__
const router = useRouter()
const route = useRoute()
const showNodeFirstRun = computed(() => shouldShowNodeFirstRun({
  online: status.value.online,
  setupRequired: status.value.setup_required,
  activeTab: activeTab.value,
  routePath: route.path,
}))
provide('desktopNodeSetupRequired', computed(() => status.value.online && status.value.setup_required))
provide('desktopFirstRunCompleted', onFirstRunCompleted)
provide(ResourceRenderContextKey, { agentId: activeId, readImage: readResourceImage, openResource })

watch(() => route.path, path => { activeTab.value = tabForRoute(path, activeTab.value) }, { immediate: true })
watch(activeId, id => { agentStore.agentId = id || '' }, { immediate: true })
watch(activeId, (id, previousId) => {
  if (id !== previousId) commandNotice.value = ''
  if (id !== previousId) resourceNavigationError.value = ''
  if (previousId && id !== previousId) terminalEndConfirmation.value = null
  if (previousId && id !== previousId && terminalOpenDialog.value) void closeTerminalOpenDialog()
})
watch([input, activeId], () => { void nextTick(resizeComposer) }, { flush: 'post' })

function resizeComposer() {
  const field = textarea.value
  if (!field) return
  const form = field.form
  const tracks = form ? window.getComputedStyle(form).gridTemplateColumns.trim().split(/\s+/) : []
  const compactWidth = Number.parseFloat(tracks[1]) || field.clientWidth
  const previousWidth = field.style.width
  field.style.width = `${compactWidth}px`
  const style = window.getComputedStyle(field)
  const lineHeight = Number.parseFloat(style.lineHeight) || 20
  const verticalPadding = (Number.parseFloat(style.paddingTop) || 0) + (Number.parseFloat(style.paddingBottom) || 0)
  const oneLineHeight = lineHeight + verticalPadding
  const maxHeight = lineHeight * 6 + verticalPadding
  field.style.height = `${oneLineHeight}px`
  const compactContentHeight = field.scrollHeight
  field.style.width = previousWidth
  field.style.height = `${oneLineHeight}px`
  const contentHeight = field.scrollHeight
  field.style.height = `${Math.max(oneLineHeight, Math.min(contentHeight, maxHeight))}px`
  field.style.overflowY = contentHeight > maxHeight ? 'auto' : 'hidden'
  composerExpanded.value = compactContentHeight > oneLineHeight + 1
}

function observeComposerField() {
  composerResizeObserver?.disconnect()
  composerResizeObserver = null
  if (typeof ResizeObserver === 'undefined' || !textarea.value) return
  composerResizeObserver = new ResizeObserver(() => resizeComposer())
  composerResizeObserver.observe(textarea.value)
}

watch(activeTab, async tab => {
  if (tab !== 'chat') {
    composerResizeObserver?.disconnect()
    composerResizeObserver = null
    composerExpanded.value = false
    return
  }
  await nextTick()
  resizeComposer()
  observeComposerField()
}, { flush: 'post' })

const previouslyOnline = ref(false)
const sidebarCollapsed = ref(false)
const openMenu = ref('')
const dialog = ref('')
const windowMaximized = ref(false)
const showAgentCreate = ref(false)
const workgroupList = ref([])
const selectedWorkgroupId = ref('')
const selectedWorkgroupName = ref('')
const pendingImagesByAgent = ref({})
const pendingFilesByAgent = ref({})
const pendingFiles = computed(() => pendingFilesByAgent.value[activeId.value] || [])
const pendingImages = computed({
  get: () => pendingImagesByAgent.value[activeId.value] || [],
  set: images => { if (activeId.value) pendingImagesByAgent.value[activeId.value] = images },
})
const attachmentInput = ref(null)
const lightboxImage = ref('')
let unlisten = null
let unlistenTerminal = null
let unlistenWindowResize = null
let unlistenWindowFocus = null
let selectionEpoch = 0
let resourceNavigationEpoch = 0
const resourceNavigationError = ref('')
let sendSequence = 0
let modelRequestSequence = 0
let pollTimer = null
let summaryRefreshTimer = 0
let resourceRefreshTimer = 0
let workspaceConnectionState = ''
let composerResizeObserver = null
let terminalDialogRequestEpoch = 0
let terminalSessionListEpoch = 0
let terminalChannelRequestEpoch = 0
const terminalDecoders = new Map()
const mediaObjectUrls = new Map()
const currentWindow = isTauri ? getCurrentWindow() : null
const workspaceScope = computed(() => workspaceNodeIdentity.value)

const activeAgent = computed(() => agents.value.find(a => a.agent_id === activeId.value))
const activeWorkgroupName = computed(() => workgroupList.value.find(group => group.workgroup_id === selectedWorkgroupId.value)?.display_name || selectedWorkgroupName.value || selectedWorkgroupId.value || '工作组')
const isWorkgroupView = computed(() => activeTab.value === 'workgroups')
const llmProfileId = computed(() => String(llmSettings.value?.active_profile || ''))
const failedSendDraft = computed(() => failedSendDrafts.value[activeId.value] || '')
const normalAgents = computed(() => agents.value.filter(a => a.agent_type !== 'auto'))
const autoAgents = computed(() => agents.value.filter(a => a.agent_type === 'auto'))
const fullTranscript = computed(() => hydrate.value?.transcript || [])
const chatEntries = computed(() => desktopChatEntries(activeId.value, fullTranscript.value, streamingText.value, hydrate.value?.turn_state, pendingUserByAgent.value[activeId.value], agentGenerating.value))
const hitlQueue = computed(() => {
  const expanded = expandHitlRequired(pending.value)
  return desktopHitlQueue(expanded)
})
const canSendMessage = computed(() => !hitlQueue.value.length || hitlQueue.value.some(item => item.kind === 'user_information'))
const lastToolActivity = computed(() => [...fullTranscript.value].reverse().find(m => ['tool_call', 'tool_result'].includes(m.kind)))
const lastConversationMessage = computed(() => [...fullTranscript.value].reverse().find(m => m.kind === 'user' || m.kind === 'assistant'))
const dockLatestMessage = computed(() => {
  const text = streamingText.value || lastConversationMessage.value?.text || ''
  return text ? { kind: streamingText.value ? 'assistant' : lastConversationMessage.value?.kind || 'assistant', text, agent_id: activeId.value } : null
})
const pending = computed(() => hydrate.value?.pending_hitl || null)
watch([activeId, fullTranscript], ([id, transcript]) => {
  const pendingUser = pendingUserByAgent.value[id]
  if (!pendingUser) return
  const start = Math.max(0, Number(pendingUser.baseTranscriptLength) || 0)
  if (!transcript.slice(start).some(entry => entry?.kind === 'user' && String(entry.text || '') === String(pendingUser.text || ''))) return
  const next = { ...pendingUserByAgent.value }
  delete next[id]
  pendingUserByAgent.value = next
}, { flush: 'post' })
const approvalQueueIndex = computed(() => hitlQueue.value.findIndex(item => item.kind === 'approval'))
const approvalCalls = computed(() => {
  const data = hitlQueue.value[approvalQueueIndex.value]?.data
  return extractToolApprovals(data).map(call => ({
    id: call.callId,
    name: call.name,
    reason: call.reason,
    argumentsText: formatApprovalArguments({ arguments: call.arguments, raw_arguments: call.rawArgs }),
  }))
})
const phase = computed(() => hydrate.value?.turn_state?.phase || (busy.value ? 'running' : 'idle'))
const canCancelTurn = computed(() => ['queued', 'model_generating', 'tool_executing'].includes(hydrate.value?.turn_state?.phase))
const agentGenerating = computed(() => busy.value || cancellingTurn.value || canCancelTurn.value)
function activeResourceMenuActions() {
  if (!status.value.online || status.value.setup_required || isWorkgroupView.value || pendingComposerPage.value || activeTab.value === 'settings') return {}
  if (activeTab.value === 'file') return workspaceFilesPanel.value?.resourceMenuActions?.() || {}
  if (activeTab.value !== 'terminal') return {}
  const terminal = terminalInstances.value.find(item => item.id === activeTerminalInstanceId.value && item.agentId === activeId.value)
  return {
    canEnableTerminalInput: Boolean(terminal?.connectionId && !terminal.manualInputEnabled),
    canEndTerminal: Boolean(terminal?.sessionId),
  }
}
const menuEntries = computed(() => createDesktopMenuModel({
  online: status.value.online,
  connectionMode: status.value.connection_mode,
  setupRequired: status.value.setup_required,
  nodeVersion: status.value.version,
  activeAgentId: activeId.value,
  activeAgentName: activeAgent.value?.display_name || '尚未选择智能体',
  isWorkgroup: isWorkgroupView.value,
  activeTab: activeTab.value,
  generating: agentGenerating.value,
  hasPendingApproval: approvalQueueIndex.value >= 0,
  hasPendingHitl: Boolean(pending.value),
  sidebarCollapsed: sidebarCollapsed.value,
  resourceActions: activeResourceMenuActions(),
}))
const terminalOpenDialog = ref(false)
const terminalOpenChoices = ref([])
const terminalSessionLoading = ref(false)
const terminalSessionError = ref('')
const terminalChannelLoading = ref(false)
const terminalChannelError = ref('')
const terminalDialogPlatform = ref('')
const terminalDialogAgentId = ref('')
const terminalDialogReturnFocus = ref(null)
const terminalEndConfirmation = ref(null)
const terminalEndReturnFocus = ref(null)
function composerModeForTab(tab) { return tab === 'chat' ? 'chat' : ['terminal', 'file', 'browser'].includes(tab) ? 'dock' : 'hidden' }
const composerMode = computed(() => composerModeForTab(activeTab.value))

watch(activeTab, (tab, previousTab) => {
  if (['chat', 'terminal', 'file', 'browser'].includes(tab)) {
    const next = new Set(visitedTabs.value)
    next.add(tab)
    visitedTabs.value = next
  }
  const decision = composerPageVisibility({
    requestedTab: tab,
    displayedTab: displayedTab.value,
    modeForTab: composerModeForTab,
    composerMounted: previousTab != null && !showNodeFirstRun.value && previousTab !== 'settings',
    pendingTransition: Boolean(pendingComposerPage.value),
  })
  if (decision.kind === 'show') {
    composerPageFadeInTab.value = ''
    pendingComposerPage.value = null
    displayedTab.value = tab
  } else {
    const current = pendingComposerPage.value
    const fade = composerPageHandoffNeedsFade(displayedTab.value, tab, composerModeForTab, prefersReducedMotion())
    composerPageFadeInTab.value = ''
    pendingComposerPage.value = createComposerPageHandoff({
      current,
      mode: decision.mode,
      id: null,
      displayedTab: displayedTab.value,
      fadeOut: fade,
      sequence: ++composerPageHandoffSequence,
    })
  }
}, { flush: 'sync', immediate: true })

watch(showNodeFirstRun, visible => {
  if (!visible) return
  composerPageFadeInTab.value = ''
  pendingComposerPage.value = null
  displayedTab.value = activeTab.value
}, { flush: 'sync' })

function prefersReducedMotion() {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true
}

function composerPageVisible(tab) {
  const pending = pendingComposerPage.value
  return displayedTab.value === tab && !(pending?.fadeOut && pending.fromTab === tab)
}

function resourcePageActive(tab) {
  return !resourceWindowMinimized.value && document.visibilityState !== 'hidden'
    && activeTab.value === tab && displayedTab.value === tab && !pendingComposerPage.value
    && composerPageVisible(tab) && activeTab.value !== 'settings'
}

function composerPageTransitionEnabled(tab) {
  const pending = pendingComposerPage.value
  return !prefersReducedMotion() && ((pending?.fadeOut && pending.fromTab === tab) || composerPageFadeInTab.value === tab)
}

function onComposerPageAfterLeave(tab, element) {
  const pending = pendingComposerPage.value
  if (element?.dataset?.composerPage !== tab || !element.isConnected || !composerWorkspace.value?.contains(element) || !pending?.fadeOut || pending.fromTab !== tab) return
  const completed = composerPageFadeOutCompletion(pending, pending.key)
  pendingComposerPage.value = completed
  if (composerPageHandoffReady(completed)) revealPendingComposerPage(completed.key)
}

function onComposerPageAfterEnter(tab, element) {
  if (element?.dataset?.composerPage !== tab || !element.isConnected || !composerWorkspace.value?.contains(element)) return
  if (composerPageFadeInTab.value === tab && displayedTab.value === tab && activeTab.value === tab) composerPageFadeInTab.value = ''
}

function revealPendingComposerPage(key) {
  const pending = pendingComposerPage.value
  if (!pending || pending.key !== key || !composerPageHandoffReady(pending)) return
  const tab = activeTab.value
  const fadeIn = pending.fadeOut && !prefersReducedMotion()
  composerPageFadeInTab.value = fadeIn ? tab : ''
  pendingComposerPage.value = null
  displayedTab.value = tab
}

function onComposerTransitionStart({ id, mode }) {
  if (pendingComposerPage.value?.mode === mode) pendingComposerPage.value = { ...pendingComposerPage.value, id }
}

function onComposerTransitionSettled({ id, mode }) {
  if (!pendingComposerPage.value || !composerPageSettlementMatches({
    eventId: id,
    eventMode: mode,
    pendingId: pendingComposerPage.value.id,
    pendingMode: pendingComposerPage.value.mode,
    currentMode: composerMode.value,
  })) return
  pendingComposerPage.value = { ...pendingComposerPage.value, composerSettled: true }
  if (composerPageHandoffReady(pendingComposerPage.value)) revealPendingComposerPage(pendingComposerPage.value.key)
}

async function refreshHealth() {
  try {
    const next = await invoke('desktop_node_status')
    const identity = `${Boolean(next.online)}:${next.connection_mode || 'local'}`
    const connectionChanged = Boolean(workspaceConnectionState && workspaceConnectionState !== identity)
    if (connectionChanged) workspaceRequestGeneration.value++
    workspaceConnectionState = identity
    status.value = next
    if (next.online && (!workspaceNodeIdentity.value || workspaceNodeIdentity.value === 'node:unknown' || connectionChanged)) {
      try {
        const info = await invoke('desktop_node_info')
        const stableIdentity = String(info?.node_id || info?.endpoint || '').trim()
        if (stableIdentity && workspaceNodeIdentity.value !== stableIdentity) workspaceNodeIdentity.value = stableIdentity
      } catch { /* The generation still isolates requests if identity lookup is unavailable. */ }
    }
    return status.value
  } catch (e) {
    if (workspaceConnectionState !== 'false:local') workspaceRequestGeneration.value++
    workspaceConnectionState = 'false:local'
    status.value = { online: false, error: String(e) }
    return status.value
  }
}
async function minimizeWindow() { if (currentWindow) await currentWindow.minimize() }
async function syncWindowMaximized() {
  if (!currentWindow) return
  try { windowMaximized.value = await currentWindow.isMaximized() } catch { /* Preview builds may not expose window state. */ }
}
async function syncResourceWindowState() {
  if (!currentWindow) return
  try {
    const minimized = await currentWindow.isMinimized()
    const restored = resourceWindowMinimized.value && !minimized
    resourceWindowMinimized.value = minimized
    if (restored) void nextTick(() => {
      if (resourcePageActive('file')) void workspaceFilesPanel.value?.refreshResources?.('return')
      if (resourcePageActive('browser')) void browserViewerPanel.value?.refreshResources?.('return')
    })
  } catch { /* Window state is unavailable in browser preview mode. */ }
}
async function toggleMaximizeWindow() {
  if (!currentWindow) return
  await currentWindow.toggleMaximize()
  await syncWindowMaximized()
}
async function closeWindow() { if (currentWindow) await currentWindow.close() }
async function loadAgents() {
  error.value = ''
  await refreshHealth()
  if (!status.value.online) return
  if (status.value.setup_required) { error.value = '请先完成 Node 首次配置。请打开 Desktop 设置中的连接配置。'; return }
  try {
    const latest = await invoke('desktop_list_agents')
    agents.value = Array.isArray(latest) ? latest : []
    await selectAgent(chooseActiveAgentId(agents.value, activeId.value))
  } catch (e) { error.value = String(e) }
}
async function refreshAgentSummaries() {
  if (!status.value.online) return
  try {
    const latest = await invoke('desktop_list_agents')
    if (!Array.isArray(latest)) return
    agents.value = latest
    if (!latest.some(agent => agent?.agent_id === activeId.value)) {
      await selectAgent(chooseActiveAgentId(latest, activeId.value))
    }
  } catch { /* Keep the last known sidebar state until the next Node event or reconnect. */ }
}
async function refreshAgentModel(id) {
  const requestSequence = ++modelRequestSequence
  if (!id) { llmSettings.value = null; return }
  try {
    const [settings, response, setup] = await Promise.all([getLLMSettings(), getAgent(id), getSetupConfig().catch(() => null)])
    const agent = response?.agent || response
    if (activeId.value !== id || requestSequence !== modelRequestSequence) return
    const profile = agentActiveProfile(agent)
    const profileConfig = setup?.llm?.profiles?.find(item => item?.id === profile)
    llmSettings.value = {
      ...settings,
      ...(profileConfig ? { provider: profileConfig.provider, model: profileConfig.model } : {}),
      ...(profile ? { active_profile: profile } : {}),
    }
  } catch {
    if (activeId.value === id && requestSequence === modelRequestSequence) llmSettings.value = null
  }
}
async function restoreAgentMedia(snapshot, agentId) {
  const transcript = Array.isArray(snapshot?.transcript) ? snapshot.transcript : []
  const loadSource = async source => {
    const raw = String(source || '')
    if (raw.startsWith('data:') || raw.startsWith('blob:')) return raw
    const mediaId = agentMediaId(source, agentId)
    if (!mediaId) return ''
    const key = `${agentId}/${mediaId}`
    if (mediaObjectUrls.has(key)) return mediaObjectUrls.get(key)
    const blob = await readAgentMedia(agentId, mediaId)
    const objectUrl = URL.createObjectURL(blob)
    mediaObjectUrls.set(key, objectUrl)
    if (mediaObjectUrls.size > 64) {
      const [oldKey, oldUrl] = mediaObjectUrls.entries().next().value
      mediaObjectUrls.delete(oldKey)
      URL.revokeObjectURL(oldUrl)
    }
    return objectUrl
  }
  const rows = await Promise.all(transcript.map(async entry => {
    const out = { ...entry }
    if (Array.isArray(entry.images)) out.images = await Promise.all(entry.images.map(loadSource))
    if (Array.isArray(entry.media)) out.media = await Promise.all(entry.media.map(async item => item && typeof item === 'object' && item.url ? { ...item, url: await loadSource(item.url) } : item))
    return out
  }))
  return { ...snapshot, transcript: rows }
}
async function switchLLMProfile(profileId) {
  const id = activeId.value
  if (!id || !profileId || agentGenerating.value) return
  error.value = ''
  try {
    await patchAgent(id, { defaults: { llm: { active: profileId } } })
    await refreshAgentModel(id)
  } catch (e) {
    if (activeId.value === id) error.value = String(e.message || e)
  }
}
async function selectAgent(id, { clearError = true, openChat = false } = {}) {
  const changed = activeId.value !== id
  if (changed) resourceNavigationEpoch += 1
  const viewTransition = agentSelectionViewTransition(activeTab.value, changed, openChat)
  collapsedGroup.value = ''
  if (viewTransition.enterChat) activeTab.value = 'chat'
  if (changed) {
    const previousId = activeId.value
    for (const terminal of terminalInstances.value.filter(item => item.agentId === previousId && item.connectionId)) {
      const connectionId = terminal.connectionId
      void invoke('desktop_terminal_command', { connectionId, agentId: previousId, commandType: 'detach' }).catch(() => {})
      terminal.connectionId = ''
      terminal.status = 'disconnected'
    }
    activeTerminalInstanceId.value = ''
    terminalConnectionId.value = ''
    terminalStatus.value = 'idle'
    terminalOutput.value = ''
    terminalError.value = ''
    terminalSessions.value = []
  }
  if (changed && activeId.value) {
    input.value = switchDraft(inputDrafts.value, activeId.value, id, input.value)
    dockDraft.value = switchDraft(dockDrafts.value, activeId.value, id, dockDraft.value)
  }
  if (changed) {
    streamingText.value = ''
    userInfoSelected.value = []
    applyHydrateTurnState({ turn_state: {} })
  }
  if (viewTransition.enterChat && route.path !== '/chat') void router.push('/chat')
  activeId.value = id
  const epoch = ++selectionEpoch
  if (changed) hydrate.value = null
  if (!id) {
    hydrate.value = null
    llmSettings.value = null
    input.value = ''
    dockDraft.value = ''
    streamingText.value = ''
    userInfoSelected.value = []
    applyHydrateTurnState({ turn_state: {} })
    if (clearError) error.value = ''
    return
  }
  if (clearError) error.value = ''
  try {
    const snapshot = await invoke('desktop_hydrate_agent', { agentId: id })
    if (epoch === selectionEpoch && activeId.value === id) {
      hydrate.value = snapshot
      applyHydrateTurnState(snapshot)
      void restoreAgentMedia(snapshot, id).then(withMedia => {
        if (epoch === selectionEpoch && activeId.value === id) hydrate.value = withMedia
      }).catch(e => { if (epoch === selectionEpoch && activeId.value === id) error.value = `无法读取会话图片：${String(e.message || e)}` })
      void refreshAgentModel(id)
      if (snapshot.turn_state?.terminal || ['completed', 'failed', 'cancelled', 'interrupted', 'budget_exhausted'].includes(snapshot.turn_state?.phase)) {
        streamingText.value = ''
      }
    }
  } catch (e) { if (epoch === selectionEpoch) error.value = String(e) }
}
async function send() {
  const text = input.value.trim()
  const images = pendingImages.value.slice()
  const files = pendingFiles.value.slice()
  if ((!text && !images.length && !files.length) || !activeId.value) return
  if (text.startsWith('/')) {
    const agentId = activeId.value
    input.value = ''
    inputDrafts.value[agentId] = ''
    pendingImagesByAgent.value[agentId] = (pendingImagesByAgent.value[agentId] || []).filter(image => !images.includes(image))
    pendingFilesByAgent.value[agentId] = (pendingFilesByAgent.value[agentId] || []).filter(file => !files.includes(file))
    await executeSlashCommand(text, agentId)
    return
  }
  commandNotice.value = ''
  if (hitlQueue.value.some(item => item.kind === 'user_information')) { void submitUserInfo(undefined, 'chat'); return }
  if (hitlQueue.value.length) { error.value = '请先处理当前对话中的待处理请求。'; return }
  if (agentGenerating.value) return
  const agentId = activeId.value
  input.value = ''
  inputDrafts.value[agentId] = ''
  const sent = await sendMessageToActiveAgent(text, images, files)
  if (!sent) input.value = recoverFailedSend(failedSendDrafts.value, agentId, activeId.value, input.value, text)
  else pendingImagesByAgent.value[agentId] = (pendingImagesByAgent.value[agentId] || []).filter(image => !images.includes(image))
  if (sent) pendingFilesByAgent.value[agentId] = (pendingFilesByAgent.value[agentId] || []).filter(file => !files.includes(file))
}
async function sendMessageToActiveAgent(text, images = [], files = []) {
  text = String(text || '').trim()
  if ((!text && !images.length && !files.length) || !activeId.value || agentGenerating.value) return false
  const agentId = activeId.value
  const pendingUser = {
    id: ++sendSequence,
    kind: 'user',
    text,
    images: images.map(image => image.url),
    file_refs: files.map(toNodeFileReference),
    baseTranscriptLength: fullTranscript.value.length,
  }
  pendingUserByAgent.value = { ...pendingUserByAgent.value, [agentId]: pendingUser }
  setAgentError(agentId, '')
  setAgentBusy(agentId, true)
  try {
    const contentParts = images.map(image => ({ type: 'image_url', image_url: { url: image.url } }))
    await submitMessage(agentId, text, contentParts, files.map(toNodeFileReference))
    if (activeId.value === agentId) {
      await selectAgent(agentId)
      await nextTick()
      chatPanel.value?.scrollToTail()
    }
    return true
  } catch (e) {
    if (pendingUserByAgent.value[agentId]?.id === pendingUser.id) {
      const next = { ...pendingUserByAgent.value }
      delete next[agentId]
      pendingUserByAgent.value = next
    }
    setAgentError(agentId, String(e))
  }
  finally { setAgentBusy(agentId, false) }
  return false
}
async function executeSlashCommand(text, agentId) {
  error.value = ''
  commandNotice.value = ''
  const consumed = await dispatchDesktopSlashCommand(text, {
    agentId,
    clearContext,
    compressContext,
    getLLMSettings,
    refresh: async id => {
      if (activeId.value !== id) return
      streamingText.value = ''
      await selectAgent(id)
    },
    patchLLMSettings: async patch => {
      const settings = await patchLLMSettings(patch)
      llmSettings.value = settings
      return settings
    },
    upload: async spec => {
      if (spec.kind === 'skill' || spec.kind === 'skills') return uploadSkillToManage({ path: spec.path, skillId: spec.id, version: spec.version, name: spec.name, publish: spec.publish })
      if (['externaltool', 'externaltools', 'tool', 'tools'].includes(spec.kind)) return uploadExternalToolToManage({ path: spec.path, toolId: spec.id, version: spec.version, name: spec.name, platform: spec.platform, publish: spec.publish })
      if (spec.kind === 'plugin' || spec.kind === 'plugins') return uploadPluginToManage({ path: spec.path, pluginId: spec.id, version: spec.version, name: spec.name, platform: spec.platform, publish: spec.publish })
      throw new Error(`未知 upload 类型: ${spec.kind}`)
    },
    onError: message => { error.value = message },
    onNotice: message => { commandNotice.value = message },
  })
  if (!consumed) error.value = '无法处理此斜杠命令'
}
function attachWorkspaceFile(fileRef) {
  if (!activeId.value || !fileRef) return
  const path = typeof fileRef === 'string' ? fileRef : fileRef.path
  const name = typeof fileRef === 'string' ? fileRef.split(/[\\/]/).pop() : fileRef.name
  if (!path || !name) return
  const current = pendingFilesByAgent.value[activeId.value] || []
  if (!current.some(file => file.path === path)) pendingFilesByAgent.value[activeId.value] = [...current, { path, name }]
  showChat()
}
function readImage(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error || new Error('无法读取图片'))
    reader.readAsDataURL(file)
  })
}
async function addImages(event) {
  const ownerId = activeId.value
  if (!ownerId) { event.target.value = ''; return }
  const files = Array.from(event.target.files || [])
  event.target.value = ''
  await processAttachments(files, ownerId)
}
async function processAttachments(files, ownerId) {
  const remaining = 8 - (pendingImagesByAgent.value[ownerId] || []).length - (pendingFilesByAgent.value[ownerId] || []).length
  const priorUploadError = workspaceUploadErrors.get(ownerId)
  let batchHadFailure = false
  for (const file of files.slice(0, Math.max(0, remaining))) {
    if (file.size > 16 * 1024 * 1024) { batchHadFailure = true; workspaceUploadErrors.delete(ownerId); if (activeId.value === ownerId) error.value = `${file.name}：文件不能超过 16 MiB`; continue }
    if (['image/jpeg', 'image/png', 'image/gif', 'image/webp'].includes(file.type)) {
      if (file.size > 10 * 1024 * 1024) { batchHadFailure = true; workspaceUploadErrors.delete(ownerId); if (activeId.value === ownerId) error.value = `${file.name}：图片不能超过 10 MB`; continue }
      try {
      const image = { name: file.name, url: await readImage(file) }
      pendingImagesByAgent.value[ownerId] = [...(pendingImagesByAgent.value[ownerId] || []), image]
      } catch (e) { batchHadFailure = true; workspaceUploadErrors.delete(ownerId); if (activeId.value === ownerId) error.value = String(e.message || e) }
    } else {
      try {
        const uploaded = await uploadWorkspaceFile(ownerId, '', file)
        if (activeId.value === ownerId && shouldClearWorkspaceUploadError(error.value, priorUploadError, batchHadFailure)) {
          error.value = ''
          workspaceUploadErrors.delete(ownerId)
        }
        const refs = pendingFilesByAgent.value[ownerId] || []
        if (!refs.some(item => item.path === uploaded.file_ref)) pendingFilesByAgent.value[ownerId] = [...refs, { path: uploaded.file_ref, name: uploaded.name || file.name }]
      } catch (e) {
        batchHadFailure = true
        if (activeId.value === ownerId) {
          const message = `${file.name}：${String(e.message || e)}`
          workspaceUploadErrors.set(ownerId, message)
          error.value = message
        }
      }
    }
  }
}
function handleAttachmentDrop(event) { void processAttachments(Array.from(event.dataTransfer?.files || []), activeId.value) }
function handleAttachmentPaste(event) { const files = Array.from(event.clipboardData?.files || []); if (files.length) void processAttachments(files, activeId.value) }
function removeImage(index) { pendingImages.value.splice(index, 1) }
function previewImage(image) { openLightbox([{ src: image.url, alt: image.name }], 0) }
async function stopCurrentTurn() {
  if (!activeId.value || !canCancelTurn.value || cancellingTurn.value) return
  const id = activeId.value
  setAgentCancelling(id, true)
  try { await cancelAgentTurn(id); if (activeId.value === id) await selectAgent(id) }
  catch (e) { setAgentError(id, String(e.message || e)) }
  finally { setAgentCancelling(id, false) }
}
async function clearCurrentContext() {
  if (!activeId.value || agentGenerating.value || !window.confirm('清空当前 Agent 的对话上下文？此操作会从当前会话移除消息。')) return
  const id = activeId.value
  try { await clearContext(id); if (activeId.value === id) { streamingText.value = ''; await selectAgent(id) } }
  catch (e) { if (activeId.value === id) setAgentError(id, String(e.message || e)) }
}
async function deleteCurrentAgent() {
  const agent = activeAgent.value
  if (!agent || agentGenerating.value || !window.confirm(`删除 Agent「${agent.display_name || agent.agent_id}」？`)) return
  try { await deleteAgent(agent.agent_id); activeId.value = ''; hydrate.value = null; await loadAgents() }
  catch (e) { if (activeId.value === agent.agent_id) setAgentError(agent.agent_id, String(e.message || e)) }
}
async function onAgentCreated(agent) {
  showAgentCreate.value = false
  await loadAgents()
  const createdId = agent?.agent_id || agent?.id
  if (createdId && agents.value.some(item => item.agent_id === createdId)) await selectAgent(createdId)
}
function showAutoOverview() { dialog.value = 'auto-overview' }
function showWorkgroups() { selectedWorkgroupId.value = ''; selectedWorkgroupName.value = ''; activeTab.value = 'workgroups' }
function openWorkgroup(id) { selectedWorkgroupId.value = String(id || ''); selectedWorkgroupName.value = workgroupList.value.find(group => group.workgroup_id === selectedWorkgroupId.value)?.display_name || ''; activeTab.value = 'workgroups' }
function openWorkgroupConnectionSettings() { void router.push('/settings/connection') }
function openBrowserCapabilitySettings() { void router.push('/settings/capabilities') }
async function openLinuxChannelSettings() { await closeTerminalOpenDialog(); await router.push('/settings/linux-channels') }
function openAutoChat(id) { dialog.value = ''; void selectAgent(id) }
async function openAutoConfig(id) { dialog.value = ''; await selectAgent(id); await router.push(`/settings/agents/${encodeURIComponent(id)}`) }
async function openAgentSettings(id) { await selectAgent(id); await router.push(`/settings/agents/${encodeURIComponent(id)}`) }
async function sendFromDock() {
  const text = dockDraft.value
  if (text.trim().startsWith('/') && activeId.value) {
    dockDraft.value = ''
    dockDrafts.value[activeId.value] = ''
    await executeSlashCommand(text.trim(), activeId.value)
    return
  }
  commandNotice.value = ''
  if (hitlQueue.value.some(item => item.kind === 'user_information')) { void submitUserInfo(undefined, 'dock'); return }
  if (hitlQueue.value.length) { error.value = '请先返回完整对话处理待处理请求。'; return }
  if (agentGenerating.value) return
  if (!text.trim() || !activeId.value || agentGenerating.value) return
  const agentId = activeId.value
  dockDraft.value = ''
  dockDrafts.value[agentId] = ''
  const sent = await sendMessageToActiveAgent(text)
  if (!sent) dockDraft.value = recoverFailedSend(failedSendDrafts.value, agentId, activeId.value, dockDraft.value, text)
}
async function requestNewTerminal(sessionId = '') {
  const intent = terminalOpenIntent(sessionId)
  if (intent.kind === 'resume') { await openTerminal(intent.sessionId, {}, activeId.value); return }
  if (!activeId.value || !status.value.online || status.value.setup_required) return
  const agentId = activeId.value
  terminalDialogRequestEpoch += 1
  if (!terminalOpenDialog.value) {
    const activeElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const menuTrigger = activeElement?.closest('[role="menu"]')?.closest('[data-menu-anchor]')?.querySelector('[data-menubar-trigger]')
    terminalDialogReturnFocus.value = menuTrigger instanceof HTMLElement ? menuTrigger : activeElement
  }
  terminalOpenDialog.value = true
  terminalDialogAgentId.value = agentId
  terminalDialogPlatform.value = ''
  terminalOpenChoices.value = terminalDialogChoices('', [])
  terminalSessionError.value = ''
  terminalChannelError.value = ''
  terminalSessionLoading.value = true
  await Promise.all([loadTerminalDialogSessions(agentId), loadTerminalDialogChannels(agentId)])
}
function terminalDialogRequestIsCurrent(agentId, requestEpoch = terminalDialogRequestEpoch) {
  return isTerminalDialogRequestCurrent({ requestEpoch, currentEpoch: terminalDialogRequestEpoch, ownerId: agentId, activeAgentId: activeId.value, dialogOpen: terminalOpenDialog.value }) && terminalDialogAgentId.value === agentId
}
async function loadTerminalDialogSessions(agentId) {
  const listEpoch = ++terminalSessionListEpoch
  terminalSessionLoading.value = true
  terminalSessionError.value = ''
  try {
    const sessions = await invoke('desktop_terminal_list', { agentId })
    if (listEpoch !== terminalSessionListEpoch || !terminalDialogRequestIsCurrent(agentId)) return
    terminalSessions.value = runningTerminalSessions(sessions)
  } catch (e) {
    if (listEpoch === terminalSessionListEpoch && terminalDialogRequestIsCurrent(agentId)) terminalSessionError.value = String(e?.message || e)
  } finally {
    if (listEpoch === terminalSessionListEpoch && terminalDialogRequestIsCurrent(agentId)) terminalSessionLoading.value = false
  }
}
async function loadTerminalDialogChannels(agentId) {
  const channelEpoch = ++terminalChannelRequestEpoch
  terminalChannelLoading.value = true
  terminalChannelError.value = ''
  terminalOpenChoices.value = terminalDialogChoices(terminalDialogPlatform.value, [])
  const [agentResult, channelsResult] = await Promise.allSettled([
      getAgent(agentId),
      isTauri ? invoke('desktop_linux_terminal_channels', { agentId }) : Promise.resolve([]),
    ])
  if (channelEpoch !== terminalChannelRequestEpoch || !terminalDialogRequestIsCurrent(agentId)) return
  if (agentResult.status === 'fulfilled') {
    const agent = agentResult.value?.agent || agentResult.value
    terminalDialogPlatform.value = String(agent?.host?.os_kind || agent?.host?.sys_platform || '')
  }
  if (channelsResult.status === 'fulfilled') terminalOpenChoices.value = terminalDialogChoices(terminalDialogPlatform.value, channelsResult.value)
  else {
    terminalOpenChoices.value = terminalDialogChoices(terminalDialogPlatform.value, [])
    terminalChannelError.value = String(channelsResult.reason?.message || channelsResult.reason)
  }
  if (channelEpoch === terminalChannelRequestEpoch && terminalDialogRequestIsCurrent(agentId)) terminalChannelLoading.value = false
}
async function closeTerminalOpenDialog({ preserveOwner = false } = {}) {
  terminalDialogRequestEpoch += 1
  terminalSessionListEpoch += 1
  terminalChannelRequestEpoch += 1
  terminalOpenDialog.value = false
  terminalSessionLoading.value = false
  terminalChannelLoading.value = false
  if (!preserveOwner) terminalDialogAgentId.value = ''
  await nextTick()
  if (terminalDialogReturnFocus.value?.isConnected) terminalDialogReturnFocus.value.focus()
  terminalDialogReturnFocus.value = null
}
async function createTerminalFromDialog(choice) {
  const agentId = terminalDialogAgentId.value
  if (!agentId || activeId.value !== agentId || !status.value.online) return
  const availableChoice = terminalOpenChoices.value.find(item => item.kind === choice?.kind && (item.kind === 'local' || item.targetId === choice?.targetId))
  if (!availableChoice) return
  let target
  try { target = terminalOpenArguments(availableChoice, terminalDialogPlatform.value) }
  catch (e) { terminalChannelError.value = String(e?.message || e); return }
  const channel = terminalOpenChoices.value.find(item => item.kind === 'linux_channel' && item.targetId === target.targetId)
  const terminalName = target.targetKind === 'linux_channel' ? (channel?.label || 'SSH 终端') : (isWindowsPlatform(terminalDialogPlatform.value) ? 'PowerShell' : '终端')
  await closeTerminalOpenDialog({ preserveOwner: true })
  if (activeId.value !== agentId || terminalDialogAgentId.value !== agentId) return
  terminalDialogAgentId.value = ''
  await openTerminal('', { ...target, name: terminalName }, agentId)
}
async function resumeTerminalFromDialog(session) {
  const agentId = terminalDialogAgentId.value
  const available = terminalSessions.value.find(item => item.terminal_id === session?.terminal_id && item.status === 'running')
  if (!agentId || activeId.value !== agentId || !available) return
  await closeTerminalOpenDialog({ preserveOwner: true })
  if (activeId.value !== agentId || terminalDialogAgentId.value !== agentId) return
  terminalDialogAgentId.value = ''
  await openTerminal(available.terminal_id, {}, agentId)
}
function retryTerminalSessionList() {
  const agentId = terminalDialogAgentId.value
  if (agentId && terminalDialogRequestIsCurrent(agentId)) void loadTerminalDialogSessions(agentId)
}
function retryTerminalChannels() {
  const agentId = terminalDialogAgentId.value
  if (!agentId || !terminalDialogRequestIsCurrent(agentId)) return
  void loadTerminalDialogChannels(agentId)
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
function showWorkspace() {
  if (!activeId.value) return
  workspaceOpened.value = true
  showResourceTab('file')
  void nextTick(() => {})
}
function normalizeResourceTarget(target) {
  if (target?.type === 'terminal') {
    const id = String(target.id || '')
    return /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(id) ? { type: 'terminal', id } : null
  }
  if ((target?.type === 'file' || target?.type === 'image') && validateWorkspacePath(target.path)) {
    return { type: target.type, path: target.path }
  }
  return null
}
async function readResourceImage(agentId, path) {
  const target = normalizeResourceTarget({ type: 'image', path })
  if (!target || !agentId || agentId !== activeId.value) throw new Error('图片链接不属于当前 Agent 的工作区。')
  const request = { agentId, path: target.path, epoch: selectionEpoch, generation: workspaceRequestGeneration.value, scope: workspaceScope.value }
  const result = await readWorkspaceFile(agentId, target.path)
  if (!isWorkspaceRequestCurrent(request, { agentId: activeId.value, path: target.path, epoch: selectionEpoch, generation: workspaceRequestGeneration.value }) || request.scope !== workspaceScope.value) {
    throw new Error('Agent 已切换，图片读取已取消。')
  }
  return workspaceImageBlob(target.path, result)
}
async function openResource(targetValue, { agentId = '' } = {}) {
  const target = normalizeResourceTarget(targetValue)
  const ownerId = String(agentId || '')
  const requestEpoch = ++resourceNavigationEpoch
  const ownerEpoch = selectionEpoch
  const ownerGeneration = workspaceRequestGeneration.value
  const ownerScope = workspaceScope.value
  const current = () => requestEpoch === resourceNavigationEpoch && ownerEpoch === selectionEpoch && ownerId === activeId.value && ownerGeneration === workspaceRequestGeneration.value && ownerScope === workspaceScope.value
  if (!target) { resourceNavigationError.value = '此资源链接无效或不受支持。'; return }
  if (!ownerId || ownerId !== activeId.value) { resourceNavigationError.value = '此资源链接属于其他 Agent，无法在当前工作区中打开。'; return }
  if (!status.value.online) { resourceNavigationError.value = 'Node 尚未连接，无法打开此资源。'; return }
  resourceNavigationError.value = ''

  if (target.type === 'file') {
    workspaceOpenedByAgent.value[ownerId] = true
    showResourceTab('file')
    await nextTick()
    if (!current()) return
    const panel = workspaceFilesPanel.value
    if (!panel?.openPath) { resourceNavigationError.value = '工作区尚未准备好，请稍后重试。'; return }
    const result = await panel.openPath(target.path)
    if (!current()) return
    if (!result?.ok && !result?.canceled) resourceNavigationError.value = '无法打开此工作区资源，请查看工作区面板中的提示。'
    return
  }

  if (target.type !== 'terminal') { resourceNavigationError.value = '此资源类型暂不支持在工作区中打开。'; return }

  try {
    const sessions = runningTerminalSessions(await invoke('desktop_terminal_list', { agentId: ownerId }))
    if (!current()) return
    const session = sessions.find(item => item.terminal_id === target.id)
    if (!session) { resourceNavigationError.value = `未找到此 Agent 的运行中终端（${target.id}）；终端可能已结束或不存在。`; return }
    terminalSessions.value = sessions
    const existing = findTerminalInstanceForSession(terminalInstances.value, ownerId, target.id)
    if (existing) existing.manualInputEnabled = false
    if (existing && reusableTerminalConnection(existing)) activateTerminal(existing.id, { reconnect: false })
    else await openTerminal(target.id, {}, ownerId)
    if (current()) resourceNavigationError.value = ''
  } catch (e) {
    if (current()) resourceNavigationError.value = `无法打开终端（${target.id}）：${String(e?.message || e)}`
  }
}
function showBrowser() {
  if (!activeId.value) return
  browserOpened.value = true
  showResourceTab('browser')
}
function closeCurrentResource() {
  const transition = closeResourceTransition(activeTab.value)
  if (transition.closeTerminal) void closeTerminal('view-close', activeTerminalInstanceId.value)
  else if (transition.workspaceOpened === false) { workspaceOpened.value = false; showChat() }
  else if (transition.browserOpened === false) { browserOpened.value = false; showChat() }
}
function showResourceTab(tab) { activeTab.value = tab; void router.push(routeForTab('chat')) }
function activateTerminal(id, { reconnect = true } = {}) {
  const terminal = terminalInstances.value.find(item => item.id === id && item.agentId === activeId.value)
  if (!terminal) return
  activeTerminalInstanceId.value = terminal.id
  terminalConnectionId.value = terminal.connectionId || ''
  terminalStatus.value = terminal.status || 'disconnected'
  terminalOutput.value = terminal.output || ''
  terminalError.value = terminal.error || ''
  showResourceTab('terminal')
  // Agent switches detach the PTY while preserving its session. Reopening the
  // retained tab should attach to that session again instead of leaving the
  // user at the disconnected placeholder.
  if (shouldReconnectTerminal(terminal, reconnect)) void openTerminal(terminal.sessionId, {}, terminal.agentId)
}
async function openTerminal(sessionId = '', target = {}, ownerId = activeId.value) {
  const agentId = ownerId
  if (!agentId || activeId.value !== agentId || !status.value.online) return
  const requestedSessionId = String(sessionId || '')
  let existing = requestedSessionId ? findTerminalInstanceForSession(terminalInstances.value, agentId, requestedSessionId) : null
  if (reusableTerminalConnection(existing)) {
    activateTerminal(existing.id, { reconnect: false })
    return
  }
  terminalError.value = ''
  const connectionId = globalThis.crypto?.randomUUID?.() || `term-${Date.now()}-${Math.random().toString(16).slice(2)}`
  const resourceId = requestedSessionId || connectionId
  const session = terminalSessions.value.find(item => item.terminal_id === requestedSessionId)
  const terminal = existing || { id: resourceId, agentId, sessionId: requestedSessionId, name: session?.display_name || session?.name || session?.shell || target.name || '终端', output: '', error: '', manualInputEnabled: !requestedSessionId }
  terminal.connectionId = connectionId
  terminal.status = requestedSessionId ? 'reconnecting' : 'connecting'
  if (requestedSessionId) {
    terminal.output = ''
    terminal.manualInputEnabled = false
    terminal.error = ''
  }
  if (!terminal.name) terminal.name = session?.display_name || session?.name || session?.shell || target.name || '终端'
  if (!terminalInstances.value.includes(terminal)) terminalInstances.value.push(terminal)
  terminalDecoders.set(connectionId, new TextDecoder())
  activeTerminalInstanceId.value = resourceId
  terminalConnectionId.value = connectionId
  terminalOutput.value = terminal.output || ''
  terminalStatus.value = sessionId ? 'reconnecting' : 'connecting'
  showResourceTab('terminal')
  try {
    await invoke('desktop_terminal_open', {
      agentId,
      connectionId,
      sessionId: requestedSessionId || null,
      targetKind: requestedSessionId ? null : target.targetKind || 'local',
      targetId: requestedSessionId ? null : target.targetId || null,
      shell: requestedSessionId ? null : target.shell || null,
    })
  } catch (e) {
    terminal.connectionId = ''
    terminal.status = 'error'
    terminal.error = String(e?.message || e)
    if (activeId.value === agentId && activeTerminalInstanceId.value === terminal.id && terminalConnectionId.value === connectionId) {
      terminalConnectionId.value = ''
      terminalStatus.value = 'error'
      terminalError.value = terminal.error
    }
  }
}
async function sendTerminalInput(data) {
  const terminal = terminalInstances.value.find(item => item.id === activeTerminalInstanceId.value && item.agentId === activeId.value)
  if (!terminal?.manualInputEnabled || !terminalConnectionId.value || terminalStatus.value !== 'connected') return
  try { await invoke('desktop_terminal_command', { connectionId: terminalConnectionId.value, agentId: activeId.value, commandType: 'input', data }) }
  catch (e) { terminalError.value = String(e); terminalStatus.value = 'error' }
}
async function resizeTerminal({ rows, cols }) {
  if (!terminalConnectionId.value || terminalStatus.value !== 'connected') return
  try { await invoke('desktop_terminal_command', { connectionId: terminalConnectionId.value, agentId: activeId.value, commandType: 'resize', rows, cols }) }
  catch (e) { terminalError.value = String(e) }
}
async function closeTerminal(action = 'view-close', terminalId = activeTerminalInstanceId.value) {
  const current = terminalInstances.value.find(item => item.id === terminalId)
  const agentId = current?.agentId || activeId.value
  if (!agentId || !current) return
  const isCurrent = () => activeId.value === agentId && activeTerminalInstanceId.value === current.id
  const connectionId = current.connectionId || ''
  let selectionUnchangedAtCommit = false
  const closed = await closeTerminalFlow({
    agentId,
    terminal: current,
    connectionId: connectionId || current.connectionId,
    commandType: action === 'end' ? 'terminate' : 'detach',
    closeRelay: ({ agentId: ownerId, connectionId: relayId, commandType: action }) => invoke('desktop_terminal_command', { connectionId: relayId, agentId: ownerId, commandType: action }),
    closeSession: ({ agentId: ownerId, terminalId: sessionId }) => closeAgentTerminal(ownerId, sessionId),
    selectionIsCurrent: isCurrent,
    onFailure: (error, stillCurrent) => {
      current.error = `${action === 'end' ? '结束' : '断开'}终端失败：${String(error)}`
      if (stillCurrent) terminalError.value = current.error
    },
    onSuccess: stillCurrent => {
      selectionUnchangedAtCommit = stillCurrent
      if (action === 'view-close' || action === 'end') {
        if (current.sessionId) delete terminalSessionByAgent.value[agentId]
        terminalInstances.value = terminalInstances.value.filter(item => item !== current)
      } else { current.connectionId = ''; current.status = 'disconnected' }
      if (!stillCurrent) return
      terminalConnectionId.value = ''
      terminalError.value = ''
      if (action === 'view-close' || action === 'end') {
        const next = terminalInstances.value.find(item => item.agentId === agentId)
        if (next) activateTerminal(next.id)
        else {
          activeTab.value = 'chat'
          activeTerminalInstanceId.value = ''
          terminalStatus.value = 'closed'
          terminalOutput.value = ''
        }
      } else {
        terminalStatus.value = 'disconnected'
        terminalOutput.value = current.output || ''
      }
    },
  })
  if (!closed) return
  if (!selectionUnchangedAtCommit) {
    void invoke('desktop_terminal_list', { agentId }).then(sessions => {
      const retainedId = terminalSessionByAgent.value[agentId]
      if (retainedId && !sessions.some(item => item.terminal_id === retainedId)) delete terminalSessionByAgent.value[agentId]
    }).catch(() => {})
    return
  }
  const expectedTerminalId = activeTerminalInstanceId.value
  if (agentId && status.value.online) {
    try {
      const sessions = await invoke('desktop_terminal_list', { agentId })
      if (activeId.value === agentId && activeTerminalInstanceId.value === expectedTerminalId) terminalSessions.value = sessions
      const retainedId = terminalSessionByAgent.value[agentId]
      if (retainedId && !sessions.some(item => item.terminal_id === retainedId)) delete terminalSessionByAgent.value[agentId]
    } catch {
      if (activeId.value === agentId && activeTerminalInstanceId.value === expectedTerminalId) terminalSessions.value = []
    }
  }
}
function requestEndTerminal(terminalId = activeTerminalInstanceId.value) {
  const terminal = terminalInstances.value.find(item => item.id === terminalId && item.agentId === activeId.value)
  if (terminal?.sessionId) {
    terminalEndReturnFocus.value = document.activeElement instanceof HTMLElement ? document.activeElement : null
    terminalEndConfirmation.value = { agentId: terminal.agentId, terminalId: terminal.id, name: terminal.name }
  }
}
function returnFocusAfterEndDialog() {
  const target = terminalEndReturnFocus.value
  terminalEndReturnFocus.value = null
  void nextTick(() => { if (target?.isConnected) target.focus() })
}
function cancelEndTerminal() { terminalEndConfirmation.value = null; returnFocusAfterEndDialog() }
async function confirmEndTerminal() {
  const captured = terminalEndConfirmation.value
  terminalEndConfirmation.value = null
  returnFocusAfterEndDialog()
  if (!captured || captured.agentId !== activeId.value) return
  const terminal = terminalInstances.value.find(item => item.id === captured.terminalId && item.agentId === captured.agentId)
  if (!terminal) return
  await closeTerminal('end', terminal.id)
}
function reconnectActiveTerminal() {
  const terminal = terminalInstances.value.find(item => item.id === activeTerminalInstanceId.value && item.agentId === activeId.value)
  if (terminal?.sessionId && terminal.status !== 'exited') void openTerminal(terminal.sessionId, {}, terminal.agentId)
}
function enableTerminalInput() {
  const terminal = terminalInstances.value.find(item => item.id === activeTerminalInstanceId.value && item.agentId === activeId.value)
  if (terminal) terminal.manualInputEnabled = true
}
async function resumeHitl(index, resumeValue) {
  const hitl = hitlQueue.value[index]
  const agentId = activeId.value
  if (!hitl || !agentId || isAgentBusy(busyByAgent.value, agentId)) return
  if (hitl.kind === 'approval') {
    if (hitl.data.approval_id) resumeValue.approval_id = hitl.data.approval_id
  }
  for (const key of ['child_agent_id', 'hitl_scope', 'child_purpose']) {
    if (hitl.data[key]) resumeValue[key] = hitl.data[key]
  }
  setAgentBusy(agentId, true)
  setAgentHitlBusyIndex(agentId, index)
  let resumed = false
  try { await invoke('desktop_resume_hitl', { agentId, resumeValue }); resumed = true }
  catch (e) { setAgentError(agentId, String(e)) }
  finally { setAgentBusy(agentId, false); setAgentHitlBusyIndex(agentId, -1) }
  if (resumed && activeId.value === agentId) await selectAgent(agentId)
  return resumed
}
function approveAll(index) { const item = hitlQueue.value[index]; if (item?.kind === 'approval') void resumeHitl(index, buildApprovalResume(item.data, { approveAll: true })) }
function rejectAll(index) { const item = hitlQueue.value[index]; if (item?.kind === 'approval') void resumeHitl(index, buildApprovalResume(item.data, { approveAll: false })) }
function approveOne({ index, callId }) { const item = hitlQueue.value[index]; if (item?.kind === 'approval') void resumeHitl(index, buildApprovalOneResume(item.data, callId, true)) }
function rejectOne({ index, callId }) { const item = hitlQueue.value[index]; if (item?.kind === 'approval') void resumeHitl(index, buildApprovalOneResume(item.data, callId, false)) }
async function submitUserInfo(index = hitlQueue.value.findIndex(item => item.kind === 'user_information'), source = 'chat') {
  const item = hitlQueue.value[index]
  if (!item) return false
  const draft = source === 'dock' ? dockDraft : input
  const submittedText = draft.value
  const agentId = activeId.value
  const result = buildUserInfoSubmitResume(item.data, { text: submittedText, selected: userInfoSelected.value })
  if (!result.ok) { error.value = result.error; return true }
  const resumed = await resumeHitl(index, result.resume)
  if (resumed && activeId.value === agentId && draft.value === submittedText) {
    draft.value = ''
    if (source === 'dock') dockDrafts.value[agentId] = ''
    else inputDrafts.value[agentId] = ''
  }
  return true
}
function decideMemoryConflict({ index, decision }) { const item = hitlQueue.value[index]; if (item?.kind === 'memory_conflict') void resumeHitl(index, buildMemoryConflictResume(item.data, decision)) }
function cancelMemoryConflict(index) { const item = hitlQueue.value[index]; if (item?.kind === 'memory_conflict') void resumeHitl(index, buildMemoryConflictResume(item.data, '', { cancelled: true })) }
async function jumpToApproval() {
  showChat()
  await nextTick()
  chatPanel.value?.scrollToTail()
  await nextTick()
  const approvals = document.querySelectorAll('.desktop-chat-wrapper .msg--approval')
  approvals[approvals.length - 1]?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}
function topMenuTriggers() { return [...document.querySelectorAll('[data-menubar-trigger]')] }
let hoverOpenedMenu = ''
function toggleTopMenu(name) {
  if (hoverOpenedMenu === name) { hoverOpenedMenu = ''; openMenu.value = name; return }
  hoverOpenedMenu = ''
  openMenu.value = openMenu.value === name ? '' : name
}
function setTopMenuFromHover(name) {
  if (openMenu.value && openMenu.value !== name) { openMenu.value = name; hoverOpenedMenu = name }
}
function focusTopMenu(index, keepOpen = Boolean(openMenu.value)) {
  const triggers = topMenuTriggers()
  if (!triggers.length) return
  const targetIndex = (index + triggers.length) % triggers.length
  if (keepOpen) openMenu.value = triggers[targetIndex].dataset.menubarTrigger
  void nextTick(() => topMenuTriggers()[targetIndex]?.focus())
}
function focusMenuItem(menuName, key = 'ArrowDown') {
  void nextTick(() => {
    const anchor = [...document.querySelectorAll('.menu-anchor')].find(item => item.dataset.menuAnchor === menuName)
    const items = [...(anchor?.querySelectorAll('[role="menu"] .menu-item:not(:disabled)') || [])]
    if (!items.length) return
    const currentIndex = items.indexOf(document.activeElement)
    items[menuFocusIndex(currentIndex < 0 ? (key === 'ArrowUp' || key === 'End' ? 0 : -1) : currentIndex, key, items.length)]?.focus()
  })
}
function handleMenubarKeydown(event) {
  const trigger = event.target.closest?.('[data-menubar-trigger]')
  const menu = event.target.closest?.('[role="menu"]')
  const menuNames = desktopMenuNames
  if (trigger) {
    const name = trigger.dataset.menubarTrigger
    const index = menuNames.indexOf(name)
    if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
      event.preventDefault()
      focusTopMenu(index + (event.key === 'ArrowRight' ? 1 : -1))
      return true
    }
    if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      focusTopMenu(event.key === 'Home' ? 0 : menuNames.length - 1, Boolean(openMenu.value))
      return true
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      openMenu.value = name
      focusMenuItem(name, event.key === 'ArrowUp' ? 'End' : 'Home')
      return true
    }
    if (event.key === 'Escape' && openMenu.value) {
      event.preventDefault()
      openMenu.value = ''
      return true
    }
  }
  if (menu) {
    const anchor = menu.closest('.menu-anchor')
    const name = anchor?.dataset.menuAnchor || ''
    if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault()
      const items = [...menu.querySelectorAll('.menu-item:not(:disabled)')]
      const currentIndex = items.indexOf(document.activeElement)
      const key = currentIndex < 0 ? (event.key === 'ArrowUp' ? 'End' : 'Home') : event.key === 'ArrowUp' && currentIndex === 0 ? 'End' : event.key
      items[menuFocusIndex(currentIndex, key, items.length)]?.focus()
      return true
    }
    if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
      event.preventDefault()
      focusTopMenu(menuNames.indexOf(name) + (event.key === 'ArrowRight' ? 1 : -1))
      return true
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      openMenu.value = ''
      void nextTick(() => topMenuTriggers().find(button => button.dataset.menubarTrigger === name)?.focus())
      return true
    }
    if (event.key === 'Tab') openMenu.value = ''
  }
  return false
}

function runMenuAction(action) {
  openMenu.value = ''
  if (action === 'refresh') loadAgents()
  else if (action === 'settings-general') void router.push('/settings/general')
  else if (action === 'quit-desktop' && isTauri) void invoke('desktop_quit')
  else if (action === 'sidebar') { sidebarCollapsed.value = !sidebarCollapsed.value; collapsedGroup.value = '' }
  else if (action === 'approval') void jumpToApproval()
  else if (action === 'agent-chat') showChat()
  else if (action === 'create-agent') showAgentCreate.value = true
  else if (action === 'current-agent-settings' && activeId.value) void router.push(`/settings/agents/${encodeURIComponent(activeId.value)}`)
  else if (action === 'delete-agent') void deleteCurrentAgent()
  else if (action === 'stop-turn') void stopCurrentTurn()
  else if (action === 'clear-context') void clearCurrentContext()
  else if (action === 'auto-overview') void showAutoOverview()
  else if (action === 'create-terminal') void requestNewTerminal()
  else if (action === 'open-workspace') showWorkspace()
  else if (action === 'open-browser') showBrowser()
  else if (action === 'discard-workspace-draft' && activeTab.value === 'file' && resourcePageActive('file') && workspaceOpened.value && status.value.online && !status.value.setup_required && !isWorkgroupView.value) workspaceFilesPanel.value?.discardDraftAndReload?.()
  else if (action === 'enable-terminal-input' && activeTab.value === 'terminal' && resourcePageActive('terminal') && status.value.online && !status.value.setup_required && !isWorkgroupView.value) enableTerminalInput()
  else if (action === 'end-terminal' && activeTab.value === 'terminal' && resourcePageActive('terminal') && status.value.online && !status.value.setup_required && !isWorkgroupView.value) requestEndTerminal()
  else if (action === 'close-resource') closeCurrentResource()
  else if (action === 'feedback') void router.push('/settings/feedback')
  else if (action === 'help-about') void router.push('/settings/about')
}
function onDocumentClick(event) { if (!event.target.closest('.menu-anchor[data-menu-anchor]')) openMenu.value = '' }
function onKeydown(event) {
  if (handleMenubarKeydown(event)) return
  if (event.ctrlKey && event.shiftKey && event.key.toLowerCase() === 't' && !event.altKey && !terminalOpenDialog.value) {
    event.preventDefault()
    void requestNewTerminal()
    return
  }
  if (event.key === 'Escape') { openMenu.value = ''; dialog.value = ''; showAgentCreate.value = false }
}
function onDesktopBackToChat() { showChat() }
let pendingDesktopAgentId = ''
async function onDesktopOpenAgent(event) {
  const id = String(event.detail?.agentId || '')
  if (!id) return
  pendingDesktopAgentId = id
  if (!agents.value.some(agent => agent.agent_id === id)) return
  pendingDesktopAgentId = ''
  await selectAgent(id, { openChat: true })
}
function onDesktopOpenSettings(event) {
  const path = String(event.detail?.path || '/settings/desktop')
  void router.push(path)
}
function onDesktopUseChatCommand(event) {
  input.value = String(event.detail?.command || '')
  showChat()
  void nextTick(() => textarea.value?.focus())
}
function showSettings() { void router.push('/settings/connection') }
async function onFirstRunCompleted() {
  await loadAgents()
  if (!status.value.setup_required) showChat()
}
function showChat() { activeTab.value = 'chat'; void router.push(routeForTab('chat')) }
onMounted(async () => {
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', resizeComposer)
  void nextTick(resizeComposer)
  void nextTick(observeComposerField)
  window.addEventListener('desktop-back-to-chat', onDesktopBackToChat)
  window.addEventListener('desktop-use-chat-command', onDesktopUseChatCommand)
  window.addEventListener('desktop-open-agent', onDesktopOpenAgent)
  window.addEventListener('desktop-open-settings', onDesktopOpenSettings)
  if (currentWindow) {
    void syncWindowMaximized()
    void syncResourceWindowState()
    try { unlistenWindowResize = await currentWindow.onResized(() => { void syncWindowMaximized(); void syncResourceWindowState() }) } catch { /* Window resize events may be unavailable in preview mode. */ }
    try { unlistenWindowFocus = await currentWindow.onFocusChanged(() => { void syncResourceWindowState() }) } catch { /* Preview builds may not expose focus events. */ }
  }
  if (!window.__TAURI_INTERNALS__) {
    status.value = { online: false, setup_required: false, version: '', error: '浏览器预览模式：请在 Tauri Desktop MVP 窗口中连接真实 Node。' }
    error.value = '预览模式仅展示界面；Node 状态、Agent 与消息仅从 Tauri Rust transport 获取。'
    return
  }
  await loadAgents()
  if (pendingDesktopAgentId && agents.value.some(agent => agent.agent_id === pendingDesktopAgentId)) {
    const agentId = pendingDesktopAgentId
    pendingDesktopAgentId = ''
    await selectAgent(agentId, { openChat: true })
  }
  previouslyOnline.value = status.value.online
  unlisten = await listen('desktop-node-event', async ev => {
    const event = ev.payload
    if (event.agent_id !== activeId.value) {
      const row = agents.value.find(agent => agent.agent_id === event.agent_id)
      if (event.event_type === 'hitl_required' && row) row.has_pending_hitl = true
      if (['hitl_required', 'turn_finished', 'turn_state', 'error'].includes(event.event_type)) {
        if (summaryRefreshTimer) clearTimeout(summaryRefreshTimer)
        summaryRefreshTimer = window.setTimeout(() => { summaryRefreshTimer = 0; void refreshAgentSummaries() }, 250)
      }
      return
    }
    if (event.event_type === 'assistant' && event.data?.display_type === 'delta') {
      streamingText.value += event.data.content || ''
      return
    }
    if (event.event_type === 'runtime/notice' && event.data?.message_kind === 'context_boundary') {
      await selectAgent(event.agent_id)
      return
    }
    if (['turn_state', 'turn_finished', 'hitl_required', 'tool_call', 'tool_result', 'error'].includes(event.event_type)) {
      if (['tool_result', 'turn_finished'].includes(event.event_type)) {
        if (resourceRefreshTimer) clearTimeout(resourceRefreshTimer)
        resourceRefreshTimer = window.setTimeout(() => {
          resourceRefreshTimer = 0
          if (resourcePageActive('file')) void workspaceFilesPanel.value?.refreshResources?.('tool')
          if (resourcePageActive('browser')) void browserViewerPanel.value?.refreshResources?.('tool')
        }, 300)
      }
      const eventAgentId = event.agent_id
      if (activeId.value !== eventAgentId) return
      await selectAgent(eventAgentId)
      const finalToolCall = event.event_type === 'tool_call' && !event.data?.partial
      if (activeId.value === eventAgentId && (['turn_finished', 'error', 'hitl_required'].includes(event.event_type) || finalToolCall)) streamingText.value = ''
    }
  })
  unlistenTerminal = await listen('desktop-terminal-event', ev => {
    const payload = ev.payload || {}
    const instance = terminalInstances.value.find(item => item.connectionId === payload.connection_id && item.agentId === payload.agent_id)
    if (!instance) return
    const event = payload.event || {}
    if (event.type === 'started') {
      instance.status = 'connected'
      const oldId = instance.id
      instance.sessionId = event.session_id || instance.sessionId
      if (event.session_id && oldId !== event.session_id) {
        instance.id = event.session_id
        if (activeTerminalInstanceId.value === oldId) activeTerminalInstanceId.value = event.session_id
      }
      instance.name = event.display_name || event.name || event.shell || instance.name || 'Shell'
      if (event.session_id) terminalSessionByAgent.value[instance.agentId] = event.session_id
      if (activeTerminalInstanceId.value === instance.id) terminalStatus.value = 'connected'
    } else if (event.type === 'output') {
      const bytes = Uint8Array.from(atob(event.data || ''), char => char.charCodeAt(0))
      const decoder = terminalDecoders.get(payload.connection_id) || new TextDecoder()
      terminalDecoders.set(payload.connection_id, decoder)
      instance.output = (instance.output + decoder.decode(bytes, { stream: true })).slice(-1500000)
      if (activeTerminalInstanceId.value === instance.id) terminalOutput.value = instance.output
    } else if (event.type === 'replay_gap') {
      instance.output += '\r\n[Node 提示：部分历史输出已过期]\r\n'
      if (activeTerminalInstanceId.value === instance.id) terminalOutput.value = instance.output
    } else if (event.type === 'error') {
      instance.error = String(event.error || 'Node 终端错误')
      instance.status = 'error'
      if (activeTerminalInstanceId.value === instance.id) { terminalError.value = instance.error; terminalStatus.value = 'error' }
    } else if (event.type === 'exited' || event.type === 'terminated') {
      instance.status = 'exited'
      if (activeTerminalInstanceId.value === instance.id) terminalStatus.value = 'exited'
    } else if (event.type === 'closed' || event.type === 'relay_closed') {
      if (instance.status !== 'closed' && instance.status !== 'exited') instance.status = 'disconnected'
      instance.connectionId = ''
      if (activeTerminalInstanceId.value === instance.id) { terminalStatus.value = instance.status; terminalConnectionId.value = '' }
    }
  })
  pollTimer = window.setInterval(async () => {
    const current = await refreshHealth()
    if (!previouslyOnline.value && current.online) await loadAgents()
    previouslyOnline.value = current.online
  }, 2500)
})
onUnmounted(() => {
  window.removeEventListener('resize', resizeComposer)
  composerResizeObserver?.disconnect()
  window.removeEventListener('desktop-back-to-chat', onDesktopBackToChat)
  window.removeEventListener('desktop-use-chat-command', onDesktopUseChatCommand)
  window.removeEventListener('desktop-open-agent', onDesktopOpenAgent)
  window.removeEventListener('desktop-open-settings', onDesktopOpenSettings)
  unlistenWindowResize?.(); unlistenWindowFocus?.()
  for (const terminal of terminalInstances.value.filter(item => item.connectionId)) {
    void invoke('desktop_terminal_command', { connectionId: terminal.connectionId, agentId: terminal.agentId, commandType: 'detach' }).catch(() => {})
  }
  unlisten?.(); unlistenTerminal?.(); clearInterval(pollTimer); clearTimeout(summaryRefreshTimer); clearTimeout(resourceRefreshTimer); document.removeEventListener('click', onDocumentClick); document.removeEventListener('keydown', onKeydown)
  for (const url of mediaObjectUrls.values()) URL.revokeObjectURL(url)
  mediaObjectUrls.clear()
})
</script>

<template>
  <div class="desktop-shell" :class="{ 'settings-mode': activeTab === 'settings' }">
    <header class="topbar" data-tauri-drag-region>
      <div class="top-left" data-tauri-drag-region>
        <TopLeftSlotIcon :tab="activeTab" :collapsed="sidebarCollapsed" :auto-overview="dialog === 'auto-overview'" @toggle="sidebarCollapsed = !sidebarCollapsed; collapsedGroup = ''" />
        <nav class="app-menus" role="menubar" aria-label="主菜单">
          <div class="menu-anchor" data-menu-anchor="DAgents" @mouseenter="setTopMenuFromHover('DAgents')">
            <button class="menu-brand" type="button" role="menuitem" data-menubar-trigger="DAgents" aria-haspopup="menu" :aria-expanded="openMenu === 'DAgents'" @click.stop="toggleTopMenu('DAgents')"><span class="brand-word">DAgents</span></button>
            <div v-if="openMenu === 'DAgents'" class="menu-popover" role="menu" aria-label="DAgents 菜单">
              <template v-for="(item, i) in menuEntries.DAgents" :key="i"><div v-if="item.separator" class="menu-separator" role="separator"></div><button v-else class="menu-item" type="button" role="menuitem" :disabled="item.disabled" tabindex="-1" @click="runMenuAction(item.action)"><span>{{ item.label }}</span><small v-if="item.hint">{{ item.hint }}</small></button></template>
            </div>
          </div>
          <div v-for="name in desktopMenuNames.slice(1)" :key="name" class="menu-anchor" :data-menu-anchor="name" @mouseenter="setTopMenuFromHover(name)">
            <button class="menu-trigger" :class="{ active: openMenu === name }" type="button" role="menuitem" :data-menubar-trigger="name" aria-haspopup="menu" :aria-expanded="openMenu === name" @click.stop="toggleTopMenu(name)">{{ name }}</button>
            <div v-if="openMenu === name" class="menu-popover" role="menu" :aria-label="`${name}菜单`">
              <template v-for="(item, i) in menuEntries[name]" :key="i"><div v-if="item.separator" class="menu-separator" role="separator"></div><div v-else-if="item.header" class="menu-section-label">{{ item.label }}</div><button v-else class="menu-item" type="button" role="menuitem" :disabled="item.disabled" tabindex="-1" @click="runMenuAction(item.action)"><span>{{ item.label }}</span><small v-if="item.hint">{{ item.hint }}</small></button></template>
            </div>
          </div>
        </nav>
        <span class="workspace-title"><strong>{{ activeAgent?.display_name || 'DAgents Desktop' }}</strong><span class="crumb"> / {{ activeTab === 'chat' ? '对话' : activeTab === 'settings' ? '设置' : activeTab === 'file' ? workspaceName : activeTab === 'workgroups' ? '工作组' : activeTab === 'browser' ? browserName : '终端' }}</span></span>
      </div>
      <div class="top-status"><span :class="['status-dot', status.online ? 'good' : 'bad']"></span>{{ status.online ? '已连接' : '未连接' }}</div>
      <div v-if="isTauri" class="window-controls">
        <button type="button" aria-label="最小化" title="最小化" @click="minimizeWindow"><span class="window-control-icon minimize" aria-hidden="true"></span></button>
        <button type="button" :aria-label="windowMaximized ? '还原' : '最大化'" :title="windowMaximized ? '还原' : '最大化'" @click="toggleMaximizeWindow"><span class="window-control-icon" :class="windowMaximized ? 'restore' : 'maximize'" aria-hidden="true"></span></button>
        <button type="button" aria-label="关闭到托盘" title="关闭到托盘" @click="closeWindow"><span class="window-control-icon close" aria-hidden="true"></span></button>
      </div>
    </header>
    <aside class="rail" :class="{ collapsed: sidebarCollapsed }">
      <div class="sidebar-scroll">
        <section class="side-section">
          <button v-if="sidebarCollapsed" class="collapsed-group" :class="{ selected: !isWorkgroupView && Boolean(activeId) && activeAgent?.agent_type !== 'auto' }" aria-label="智能体" title="智能体" :aria-expanded="collapsedGroup === 'agents'" @click="collapsedGroup = collapsedGroup === 'agents' ? '' : 'agents'"><span class="group-icon" :style="maskStyle(navAgentsIcon)"></span></button>
          <template v-else>
            <div class="group-title"><span class="group-icon" :style="maskStyle(navAgentsIcon)"></span><span>智能体</span><button class="group-add" aria-label="新建 Agent" title="新建 Agent" @click="showAgentCreate = true">+</button></div>
            <div v-for="agent in normalAgents" :key="agent.agent_id" class="agent-row">
              <button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button>
              <button class="agent-row-action" type="button" :aria-label="`${agent.display_name || agent.agent_id} 设置`" title="智能体设置" @click="openAgentSettings(agent.agent_id)"><UiIcon name="settings" :size="15" /></button>
            </div>
          </template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'agents'" class="sidebar-popover"><div class="sidebar-popover-heading"><b>智能体</b><button class="group-add" type="button" aria-label="新建 Agent" title="新建 Agent" @click="showAgentCreate = true">+</button></div><div v-for="agent in normalAgents" :key="agent.agent_id" class="agent-row"><button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button><button class="agent-row-action" type="button" :aria-label="`${agent.display_name || agent.agent_id} 设置`" title="智能体设置" @click="openAgentSettings(agent.agent_id)"><UiIcon name="settings" :size="15" /></button></div></div>
        </section>
        <section class="side-section">
          <button v-if="sidebarCollapsed" class="collapsed-group" :class="{ selected: activeTab === 'workgroups' }" aria-label="工作组" title="工作组" :aria-expanded="collapsedGroup === 'workgroups'" @click="collapsedGroup = collapsedGroup === 'workgroups' ? '' : 'workgroups'"><span class="group-icon" :style="maskStyle(navWorkgroupsIcon)"></span></button>
          <template v-else><button class="group-title group-title-button" :class="{ selected: isWorkgroupView }" :aria-current="isWorkgroupView ? 'page' : undefined" @click="showWorkgroups"><span class="group-icon" :style="maskStyle(navWorkgroupsIcon)"></span><span>工作组</span></button><button v-for="group in workgroupList" :key="group.workgroup_id" class="agent-item" :class="{ selected: activeTab === 'workgroups' && selectedWorkgroupId === group.workgroup_id }" :title="group.display_name || group.workgroup_id" @click="openWorkgroup(group.workgroup_id)"><span class="agent-label">{{ group.display_name || group.workgroup_id }}</span></button></template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'workgroups'" class="sidebar-popover"><button class="sidebar-popover-title" type="button" @click="showWorkgroups">工作组</button><button v-for="group in workgroupList" :key="group.workgroup_id" :class="{ selected: activeTab === 'workgroups' && selectedWorkgroupId === group.workgroup_id }" @click="openWorkgroup(group.workgroup_id)"><span class="agent-label">{{ group.display_name || group.workgroup_id }}</span></button></div>
        </section>
        <section class="side-section">
          <button v-if="sidebarCollapsed" class="collapsed-group" :class="{ selected: !isWorkgroupView && Boolean(activeId) && activeAgent?.agent_type === 'auto' }" aria-label="自主智能体" title="自主智能体" :aria-expanded="collapsedGroup === 'auto'" @click="collapsedGroup = collapsedGroup === 'auto' ? '' : 'auto'"><span class="group-icon" :style="maskStyle(navAutoIcon)"></span></button>
          <template v-else>
            <div class="group-title"><span class="group-icon" :style="maskStyle(navAutoIcon)"></span><span>自主智能体</span></div>
            <div v-for="agent in autoAgents" :key="agent.agent_id" class="agent-row">
              <button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button>
              <button class="agent-row-action" type="button" :aria-label="`${agent.display_name || agent.agent_id} 设置`" title="智能体设置" @click="openAgentSettings(agent.agent_id)"><UiIcon name="settings" :size="15" /></button>
            </div>
          </template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'auto'" class="sidebar-popover"><b>自主智能体</b><div v-for="agent in autoAgents" :key="agent.agent_id" class="agent-row"><button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button><button class="agent-row-action" type="button" :aria-label="`${agent.display_name || agent.agent_id} 设置`" title="智能体设置" @click="openAgentSettings(agent.agent_id)"><UiIcon name="settings" :size="15" /></button></div></div>
        </section>
      </div>
      <div class="node-indicator" :class="status.online ? 'online' : 'offline'" :title="`Node · ${status.online ? '在线' : '离线'} · ${status.connection_mode === 'tunnel' ? 'SSH 隧道' : '本机'}${status.version ? ' · v' + status.version : ''}`" :aria-label="`Node · ${status.online ? '在线' : '离线'} · ${status.connection_mode === 'tunnel' ? 'SSH 隧道' : '本机'}${status.version ? ' · v' + status.version : ''}`"><span>{{ `Node · ${status.online ? '在线' : '离线'} · ${status.connection_mode === 'tunnel' ? 'SSH 隧道' : '本机'}${status.version ? ' · v' + status.version : ''}` }}</span><i></i></div>
    </aside>
    <main class="main-panel">
      <FirstRunNodeProfile v-if="showNodeFirstRun && activeTab !== 'settings'" class="desktop-first-run" @completed="onFirstRunCompleted" />
      <RouterView v-else-if="activeTab === 'settings'" />
      <div v-else ref="composerWorkspace" class="desktop-chat-workspace">
      <div class="resource-tabs">
        <button v-if="activeTab !== 'workgroups'" class="conversation-tab" :class="{ active: activeTab === 'chat' }" :title="activeAgent?.display_name || '当前智能体'" @click="showChat"><span class="icon-mask tab-icon" :style="maskStyle(activeAgent?.agent_type === 'auto' ? autoAgentIcon : normalAgentIcon)"></span><b>{{ activeAgent?.display_name || '当前智能体' }}</b></button>
        <div v-else class="conversation-tab active workgroup-conversation-tab" :title="activeWorkgroupName"><span class="icon-mask tab-icon" :style="maskStyle(navWorkgroupsIcon)"></span><b>{{ activeWorkgroupName }}</b></div>
        <div v-if="activeTab !== 'workgroups'" class="open-resource-tabs"><div v-if="workspaceOpened" class="open-resource-tab" :class="{ active: activeTab === 'file' }"><button class="resource-tab-main" :title="workspaceName" @click="showWorkspace"><span class="icon-mask tab-icon" :style="maskStyle(folderOpenIcon)"></span><span class="resource-tab-label">{{ workspaceName }}</span></button><button class="tab-close" aria-label="关闭工作区" title="关闭工作区" @click="workspaceOpened = false; showChat()"><span class="icon-mask" :style="maskStyle(circleXIcon)"></span></button></div><div v-if="browserOpened" class="open-resource-tab" :class="{ active: activeTab === 'browser' }"><button class="resource-tab-main" :title="browserName" @click="showBrowser"><span class="icon-mask tab-icon" :style="maskStyle(browserIcon)"></span><span class="resource-tab-label">{{ browserName }}</span></button><button class="tab-close" aria-label="关闭浏览器" title="关闭浏览器" @click="browserOpened = false; showChat()"><span class="icon-mask" :style="maskStyle(circleXIcon)"></span></button></div><div v-for="terminal in terminalInstances.filter(item => item.agentId === activeId)" :key="terminal.id" class="open-resource-tab" :class="{ active: activeTab === 'terminal' && activeTerminalInstanceId === terminal.id }"><button class="resource-tab-main" :title="terminal.name" @click="activateTerminal(terminal.id)"><span class="icon-mask tab-icon" :style="maskStyle(terminalIcon)"></span><span class="resource-tab-label">{{ terminal.name }}</span></button><button class="tab-close" :aria-label="`关闭 ${terminal.name}`" :title="`关闭 ${terminal.name}`" @click="closeTerminal('view-close', terminal.id)"><span class="icon-mask" :style="maskStyle(circleXIcon)"></span></button></div></div>
      </div>
      <div v-if="error || status.setup_required" class="notice" :class="status.setup_required ? 'setup' : ''"><span>{{ error || 'Node 需要首次配置' }}</span><button v-if="status.setup_required" @click="showSettings">打开连接配置</button><button v-else-if="isTauri" @click="loadAgents">重试</button></div>
      <div v-if="resourceNavigationError" class="notice resource-navigation-notice" role="alert"><span>{{ resourceNavigationError }}</span><button type="button" aria-label="关闭资源提示" @click="resourceNavigationError = ''">关闭</button></div>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('chat')" @after-leave="onComposerPageAfterLeave('chat', $event)" @after-enter="onComposerPageAfterEnter('chat', $event)">
      <section v-if="visitedTabs.has('chat')" v-show="composerPageVisible('chat')" data-composer-page="chat" class="conversation" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)">
        <div v-if="!status.online" class="empty-state"><div class="empty-icon"><i></i></div><h2>{{ status.connection_mode === 'tunnel' ? '等待隧道中的 Node' : '等待本机 Node' }}</h2><template v-if="status.connection_mode === 'tunnel'"><p>请确认 SSH 本地转发与远程 Node 正在运行。</p><small v-if="status.error" class="connection-diagnostic">诊断：{{ status.error }}</small></template><p v-else>{{ status.error || '启动 DAgents Node 后，Desktop 会自动连接。' }}</p></div>
        <div v-else-if="status.setup_required" class="empty-state"><div class="empty-icon"><i></i></div><h2>完成 Node 首次配置</h2><p>在 Desktop 的连接设置中完成 Node 配置后即可使用。</p><button class="quiet" @click="showSettings">打开连接配置</button></div>
        <div v-else-if="!activeAgent" class="empty-state"><h2>选择一个 Agent</h2><p>从左侧列表选择 Agent 开始对话。</p></div>
        <template v-else>
          <div v-if="!chatEntries.length && !hitlQueue.length" class="welcome"><div class="welcome-mark"><span class="icon-mask" :style="maskStyle(activeAgent?.agent_type === 'auto' ? autoAgentIcon : normalAgentIcon)"></span></div><h1>{{ activeAgent.display_name }}</h1><p>与此 Agent 的真实对话会保存在连接的 Node 中。</p><span class="real-tag">真实 Node 会话</span></div>
          <div v-else class="desktop-chat-wrapper app__body--chat-v61">
            <MainChatPanel ref="chatPanel" :key="activeId" :agent-id="activeId" :entries="chatEntries" :hitl-queue="hitlQueue" :agent-title="activeAgent.display_name" :hide-composer="true" :show-workspace-switcher="false" :sending="busy || agentGenerating" :hitl-busy="busy" :hitl-busy-index="hitlBusyIndex" @approve-all="approveAll" @reject-all="rejectAll" @approve-one="approveOne" @reject-one="rejectOne" @user-info-submit="submitUserInfo" @user-info-selected="userInfoSelected = $event" @memory-conflict-decide="decideMemoryConflict" @memory-conflict-cancel="cancelMemoryConflict" />
          </div>
        </template>
      </section>
      </Transition>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('terminal')" @after-leave="onComposerPageAfterLeave('terminal', $event)" @after-enter="onComposerPageAfterEnter('terminal', $event)">
      <section v-if="visitedTabs.has('terminal')" v-show="composerPageVisible('terminal')" data-composer-page="terminal" class="terminal-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)">
        <div class="terminal-resource-head"><div><strong>{{ terminalResourceName }}</strong><span class="terminal-status" :class="`state-${terminalStatus}`"><i></i>{{ ({idle:'未连接',connecting:'连接中',reconnecting:'恢复会话中',connected:'已连接',disconnected:'连接已断开',error:'连接错误',exited:'进程已退出',closed:'已关闭'})[terminalStatus] || terminalStatus }}</span></div><div class="terminal-actions"><button v-if="terminalConnectionId" class="icon-only" type="button" title="断开终端连接（保留进程）" aria-label="断开终端连接（保留进程）" @click="closeTerminal('disconnect')"><UiIcon name="plug" :size="16" /></button><button v-else-if="['disconnected','error','closed'].includes(terminalStatus) && terminalInstances.find(item => item.id === activeTerminalInstanceId)?.sessionId" class="icon-only" type="button" title="重新连接" aria-label="重新连接终端" @click="reconnectActiveTerminal"><UiIcon name="refresh-cw" :size="16" /></button></div></div>
        <div class="terminal-owner-note">此终端属于 {{ activeAgent?.display_name || activeAgent?.agent_id || '当前 Agent' }}；模型可能正在使用。{{ terminalInstances.find(item => item.id === activeTerminalInstanceId)?.sessionId && !terminalInstances.find(item => item.id === activeTerminalInstanceId)?.manualInputEnabled ? '恢复会话只读查看。' : '手动输入已启用。' }}</div>
        <div class="terminal-output" :class="{ 'terminal-output-empty': !terminalConnectionId }"><div v-if="!terminalConnectionId" class="terminal-empty-state"><span class="icon-mask" :style="{ ...maskStyle(terminalIcon), width: '24px', height: '24px' }" aria-hidden="true"></span><h2>终端尚未连接</h2><p>打开终端选择器以新建或恢复当前 Agent 的运行中会话。</p><div><button class="icon-only" type="button" title="打开终端" aria-label="打开终端选择器" @click="requestNewTerminal()"><UiIcon name="terminal" :size="17" /></button></div></div><TerminalConsole v-if="terminalConnectionId" :key="terminalConnectionId" :output="terminalOutput" :connected="terminalStatus === 'connected'" :read-only="!terminalInstances.find(item => item.id === activeTerminalInstanceId)?.manualInputEnabled" @input="sendTerminalInput" @resize="resizeTerminal" /><p v-if="terminalError" class="terminal-error">{{ terminalError }}</p></div>
      </section>
      </Transition>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('file')" @after-leave="onComposerPageAfterLeave('file', $event)" @after-enter="onComposerPageAfterEnter('file', $event)">
      <section v-if="visitedTabs.has('file') && (displayedTab === 'file' || workspaceOpened)" v-show="composerPageVisible('file')" data-composer-page="file" class="file-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)"><WorkspaceFiles ref="workspaceFilesPanel" :key="activeId" :agent-id="activeId" :workspace-scope="workspaceScope" :request-generation="workspaceRequestGeneration" :active="resourcePageActive('file')" @resource-name="workspaceName = $event" @attach-file="attachWorkspaceFile" /></section>
      </Transition>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('browser')" @after-leave="onComposerPageAfterLeave('browser', $event)" @after-enter="onComposerPageAfterEnter('browser', $event)">
      <section v-if="visitedTabs.has('browser') && (displayedTab === 'browser' || browserOpened)" v-show="composerPageVisible('browser')" data-composer-page="browser" class="file-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)"><BrowserViewer ref="browserViewerPanel" :key="activeId" :agent-id="activeId" :workspace-scope="workspaceScope" :request-generation="workspaceRequestGeneration" :active="resourcePageActive('browser')" @resource-name="browserName = $event" @open-connection-settings="openBrowserCapabilitySettings" /></section>
      </Transition>
      <section v-if="displayedTab === 'workgroups'" class="file-resource workgroup-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)"><button v-if="selectedWorkgroupId" class="workgroup-back" @click="selectedWorkgroupId = ''; selectedWorkgroupName = ''">返回工作组列表</button><WorkgroupWorkspace :workgroup-id="selectedWorkgroupId" @select="openWorkgroup" @list="workgroupList = $event" @name="selectedWorkgroupName = $event" @open-connection-settings="openWorkgroupConnectionSettings" /></section>
      <ComposerSurface :mode="composerMode" @transition-start="onComposerTransitionStart" @transition-settled="onComposerTransitionSettled">
      <template #chat>
      <footer class="composer-wrap"><div v-if="pendingImages.length || pendingFiles.length" class="composer-attachments"><button v-for="(image, index) in pendingImages" :key="image.name + index" class="composer-attachment" type="button" :title="image.name" @click="previewImage(image)"><img :src="image.url" :alt="image.name"><span>{{ image.name }}</span><i @click.stop="removeImage(index)">×</i></button><button v-for="(file,index) in pendingFiles" :key="file.path" class="composer-attachment file-attachment" type="button" :title="file.path"><span class="icon-mask tab-icon" :style="maskStyle(fileIcon)"></span><span>{{ file.name }}</span><i @click.stop="pendingFilesByAgent[activeId].splice(index,1)">×</i></button></div><form class="composer" :class="{ 'has-multiline': composerExpanded }" @submit.prevent="send" @dragover.prevent @drop.prevent="handleAttachmentDrop">
  <button class="attach-button" type="button" aria-label="添加附件" title="添加图片或文件" :disabled="pendingImages.length + pendingFiles.length >= 8" @click="attachmentInput?.click()"><span class="icon-mask" :style="maskStyle(attachmentIcon)"></span></button>
  <input ref="attachmentInput" class="visually-hidden" type="file" multiple @change="addImages">
  <textarea ref="textarea" v-model="input" class="composer-input" :disabled="!activeId || !status.online || status.setup_required" placeholder="发送消息…" title="Enter 发送，Shift+Enter 换行；可拖放或粘贴附件" @input="resizeComposer" @paste="handleAttachmentPaste" @keydown.enter.exact.prevent="send"></textarea>
  <ComposerToolbar class="desktop-composer-toolbar" :llm-settings="llmSettings" :desktop-compact="true" :disabled="agentGenerating || !activeId" @switch-profile="switchLLMProfile"/>
  <button v-if="failedSendDraft" type="button" class="restore-failed-draft composer-restore" :disabled="!!input.trim()" @click="restoreFailedSendDraft('chat')">恢复失败消息</button>
  <button v-if="agentGenerating" class="send-button" type="button" :disabled="!canCancelTurn || cancellingTurn" :aria-label="cancellingTurn ? '正在停止本轮' : '停止本轮'" :title="cancellingTurn ? '正在停止本轮…' : '停止本轮'" :aria-busy="cancellingTurn" @click="stopCurrentTurn"><span class="icon-mask send-icon" :style="maskStyle(stopIcon)"></span></button>
  <button v-else class="send-button" :disabled="!canSendMessage || (!input.trim() && !pendingImages.length && !pendingFiles.length) || !activeId" aria-label="发送消息" title="发送消息"><span class="icon-mask send-icon" :style="maskStyle(sendIcon)"></span></button>
</form><p v-if="error" class="composer-error" role="alert">{{ error }}</p><p v-if="commandNotice" class="composer-notice" role="status">{{ commandNotice }}</p><div class="composer-foot"><span>内容发送到当前 Agent</span><span>{{ agentGenerating ? '生成中' : '就绪' }}</span></div></footer>
      </template>
      <template #dock>
      <ResourceAgentDock
        v-model="dockDraft"
        v-model:expanded="dockExpanded"
        :status="dockStatus"
        :agent-name="activeAgent?.display_name || '当前智能体'"
        :can-send="canSendMessage"
        :disabled="!activeId || !status.online || status.setup_required"
        :can-stop="canCancelTurn"
        :cancelling="cancellingTurn"
        :failed-draft="Boolean(failedSendDraft)"
        :restore-disabled="Boolean(dockDraft.trim())"
        @send="sendFromDock"
        @stop="stopCurrentTurn"
        @restore="restoreFailedSendDraft('dock')"
        @back-to-chat="showChat"
      >
        <template #details>
          <div class="dock-details">
            <p v-if="error" class="composer-error" role="alert">{{ error }}</p>
            <p v-if="commandNotice" class="composer-notice" role="status">{{ commandNotice }}</p>
            <div v-if="dockLatestMessage" class="dock-detail-row dock-detail-row--message"><ResourceMarkdown :entry="dockLatestMessage" :agent-id="activeId" source="dock" /></div>
            <div v-if="lastToolActivity" class="dock-detail-row"><small>最近工具</small><span>{{ lastToolActivity.summary || lastToolActivity.data?.tool_name || '工具活动' }}</span></div>
            <div v-if="pending" class="dock-approval"><strong>{{ pending.message || '有待处理的请求' }}</strong><div v-for="call in approvalCalls" :key="call.id" class="dock-approval-call"><code>{{ call.name }}</code><span>{{ call.reason }}</span><pre v-if="call.argumentsText" class="approval-arguments">{{ call.argumentsText }}</pre></div><div v-if="approvalCalls.length" class="approval-actions"><button class="quiet" :disabled="busy" @click.prevent="rejectAll(approvalQueueIndex)">拒绝</button><button class="approve" :disabled="busy" @click.prevent="approveAll(approvalQueueIndex)">批准并继续</button></div><p v-else>请使用右上角按钮返回完整对话处理此请求。</p></div>
          </div>
        </template>
      </ResourceAgentDock>
      </template>
      </ComposerSurface>
      </div>
    </main>
    <TerminalOpenDialog :open="terminalOpenDialog" :choices="terminalOpenChoices" :sessions="terminalSessions" :sessions-loading="terminalSessionLoading" :sessions-error="terminalSessionError" :channels-loading="terminalChannelLoading" :channels-error="terminalChannelError" @cancel="closeTerminalOpenDialog" @create="createTerminalFromDialog" @resume="resumeTerminalFromDialog" @retry-sessions="retryTerminalSessionList" @retry-channels="retryTerminalChannels" @open-settings="openLinuxChannelSettings" />
    <TerminalEndDialog :open="Boolean(terminalEndConfirmation)" :terminal-name="terminalEndConfirmation?.name" @cancel="cancelEndTerminal" @confirm="confirmEndTerminal" />
    <div v-if="dialog === 'auto-overview'" class="dialog-scrim auto-overview-scrim" @click.self="dialog = ''"><section class="auto-overview-dialog"><button class="dialog-close" aria-label="关闭" @click="dialog = ''">×</button><AutoOverviewPanel @open-chat="openAutoChat" @open-config="openAutoConfig" /></section></div>
    <div v-if="showAgentCreate" class="agent-create-overlay"><AgentCreatePage @cancel="showAgentCreate = false" @created="onAgentCreated" /></div>
    <ImageLightbox />
  </div>
</template>
