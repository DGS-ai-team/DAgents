<script setup>
import { computed, nextTick, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { getCurrentWindow } from '@tauri-apps/api/window'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { agentErrorFor, agentSelectionViewTransition, cancelledTurnSummary, canCancelTurnPhase, chooseActiveAgentId, closeResourceTransition, createDesktopMenuModel, desktopComposerStatus, desktopMenuNames, handleAgentNotificationEvent, hasPersistedPartialMessage, isAgentBusy, mergeAgentNotificationSummaries, menuFocusIndex, recoverFailedSend, routeForTab, shouldReconnectTerminal, shouldShowNodeFirstRun, shouldShowWorkgroupSidebar, shouldShowTurnStopAction, switchDraft, tabForRoute, turnErrorSummary, updateAgentBusy, updateAgentError, workgroupCollaborationEnabled } from './workbench-state.js'
import { ResourceRenderContextKey, validateWorkspacePath } from '@desktop-ui/utils/resourceContext.js'
import { closeTerminalFlow } from './terminal-close.js'
import { composerPageFadeOutCompletion, composerPageHandoffNeedsFade, composerPageHandoffReady, composerPageSettlementMatches, composerPageVisibility, createComposerPageHandoff } from './composer-surface-motion.js'
import { findTerminalInstanceForSession, isTerminalDialogRequestCurrent, isWindowsPlatform, reusableTerminalConnection, runningTerminalSessions, terminalDialogChoices, terminalOpenArguments, terminalOpenIntent } from './terminal-open-state.js'
import { agentMediaId, cancelWorkspaceAttachmentBatch, createWorkspaceAttachmentBatch, isWorkspaceAttachmentBatchCurrent, isWorkspaceRequestCurrent, shouldClearWorkspaceUploadError, workspaceImageBlob } from './workspace-files-state.js'
import { restoreTranscriptMedia } from './utils/agentMediaRestore.js'
import { createMediaUrlRegistry } from './utils/mediaUrlRegistry.js'
import { onConfigurationChanged } from './utils/configurationEvents.js'
import { createWorkspaceReference, dispatchWorkspaceMessage, hasWorkspaceMessageContent, settleWorkspaceReferences, workspaceReferencesForAgent } from './workspace-references.js'
import { agentStore } from '@desktop-ui/stores/agent.js'
import { createConversationStore } from '@desktop-ui/stores/conversationStore.js'
import { applyRemoteWorkerEvent, currentRemoteWorkerEventVersion, replaceChildrenFromHydrate, setRemoteWorkerParent } from '@desktop-ui/stores/remoteWorkers.js'
import { approvalRequestKey, buildApprovalSelectionResume, buildMemoryConflictResume, buildUserInfoSubmitResume } from '@desktop-ui/stores/hitl.js'
import { enqueueHydrateEvent } from '@desktop-ui/utils/hydrateEventBuffer.js'
import { canManuallyRetryHydrate, hydrateSyncNotice, isHydrateReadError } from '@desktop-ui/utils/hydrateSnapshot.js'
import { createConversationController } from '@desktop-ui/utils/conversationController.js'
import { reconcileAgentWatermarks } from '@desktop-ui/utils/conversationWatermarks.js'
import { createTurnWatchdog } from '@desktop-ui/stores/turnWatchdog.js'
import { installPerformanceDiagnosticsGlobal, recordSSEEvent, startPerformanceSpan } from '@desktop-ui/stores/performanceDiagnostics.js'
import { normalComposerIntent } from '@desktop-ui/utils/composerState.js'
import MainChatPanel from '@desktop-ui/components/MainChatPanel.vue'
import ResourceMarkdown from '@desktop-ui/components/ResourceMarkdown.vue'
import UiIcon from '@desktop-ui/components/UiIcon.vue'
import UiContextMenu from '@desktop-ui/components/UiContextMenu.vue'
import AgentCreatePage from '@desktop-ui/components/AgentCreatePage.vue'
import FirstRunNodeProfile from '@desktop-ui/views/FirstRunNodeProfile.vue'
import ComposerToolbar from '@desktop-ui/components/ComposerToolbar.vue'
import ImageLightbox from '@desktop-ui/components/ImageLightbox.vue'
import TerminalConsole from './components/TerminalConsole.vue'
import TerminalOpenDialog from './components/TerminalOpenDialog.vue'
import TerminalEndDialog from './components/TerminalEndDialog.vue'
import ScreenshotRegionDialog from './components/ScreenshotRegionDialog.vue'
import ComposerSurface from './components/ComposerSurface.vue'
import ResourceAgentDock from './components/ResourceAgentDock.vue'
import ApprovalBubble from '@desktop-ui/components/ApprovalBubble.vue'
import WorkspaceReferenceAttachments from './components/WorkspaceReferenceAttachments.vue'
import WorkspaceFiles from './components/WorkspaceFiles.vue'
import BrowserViewer from './components/BrowserViewer.vue'
import ConversationHistoryPanel from './components/ConversationHistoryPanel.vue'
import WorkgroupWorkspace from './components/WorkgroupWorkspace.vue'
import AutoOverviewPanel from './components/AutoOverviewPanel.vue'
import TopLeftSlotIcon from './components/TopLeftSlotIcon.vue'
import { cancelAgentTurn, clearContext, closeAgentTerminal, compressContext, deleteAgent, editQueuedMessage, getAgent, getAgentModelView, getAgentWatermarks, getSetupConfig, newClientRequestId, patchAgent, setInputQueuePaused, submitMessage, unsubscribeWorkgroup, withdrawQueuedMessage } from '@desktop-ui/api/node.js'
import { confirmDesktopAction } from '@desktop-ui/utils/desktopDialogs.js'
import { agentActiveProfile } from '@desktop-ui/utils/agentLLM.js'
import { dispatchDesktopSlashCommand } from './slash-commands.js'
import { openLightbox } from '@desktop-ui/stores/lightbox.js'
import { cancelWorkspaceWrite, newWorkspaceWriteRequestId, pickWorkspaceImport, readAgentMedia, readWorkspaceFile, uploadWorkspaceFile } from './workspace-files-api.js'
import normalAgentIcon from '@shared-icons/desktop/agent.svg?url'
import autoAgentIcon from '@shared-icons/desktop/auto.svg?url'
import navAgentsIcon from '@shared-icons/desktop/agent.svg?url'
import navAutoIcon from '@shared-icons/desktop/auto.svg?url'
import navWorkgroupsIcon from '@shared-icons/desktop/nav-workgroups.svg?url'
import terminalIcon from '@shared-icons/desktop/terminal.svg?url'
import browserIcon from '@shared-icons/desktop/browser.svg?url'
import fileIcon from '@shared-icons/desktop/file.svg?url'
import attachmentIcon from '@shared-icons/desktop/attachment.svg?url'
import uploadActionIcon from '@shared-icons/desktop/upload.svg?url'
import imageUploadActionIcon from '@shared-icons/desktop/image.svg?url'
import screenshotActionIcon from '@shared-icons/desktop/screenshot.svg?url'
import folderOpenIcon from '@shared-icons/desktop/folder-open.svg?url'
import sendIcon from '@shared-icons/desktop/send.svg?url'
import stopIcon from '@shared-icons/dagents/stop.svg?url'
import brandIconUrl from '@dagents-brand/brand-icon.png?url'

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

const status = ref({ online: false, setup_required: false, version: '', error: '正在连接本地服务…' })
const manageSettings = ref(null)
const workgroupSettingsLoaded = ref(false)
const agents = ref([])
const agentNotificationProjections = ref({})
const activeId = ref('')
const conversationStore = createConversationStore()
const turnStateStore = conversationStore.turnState
const hydrate = conversationStore.hydrate
const hydrateSyncStatus = conversationStore.syncStatus
const activeHydrateContentRevision = conversationStore.contentRevision
const activeHydrateConversationRevision = conversationStore.conversationRevision
const llmSettings = ref(null)
const llmProfileCatalog = ref([])
const composerToolbar = ref(null)
const input = ref('')
const busyByAgent = ref({})
const messageSubmitPendingByAgent = ref({})
const cancellingByAgent = ref({})
const queueControlPending = ref(false)
const hitlBusyIndexByAgent = ref({})
const agentErrors = ref({})
const globalError = ref('')
const commandNotice = ref('')
let commandNoticeTimer = 0
function clearCommandNotice() {
  if (commandNoticeTimer) window.clearTimeout(commandNoticeTimer)
  commandNoticeTimer = 0
  commandNotice.value = ''
}
function showCommandNotice(message) {
  clearCommandNotice()
  const notice = String(message || '')
  if (!notice) return
  commandNotice.value = notice
  commandNoticeTimer = window.setTimeout(clearCommandNotice, 4200)
}
const busy = computed(() => isAgentBusy(busyByAgent.value, activeId.value))
const messageSubmitPending = computed(() => Boolean(messageSubmitPendingByAgent.value[activeId.value]))
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
const activeAssistantDraftText = computed(() => conversationStore.assistantDraftText(activeId.value, activeHydrationSequence))
const hasAssistantOutput = computed(() => conversationStore.hasAssistantDraftOutput(activeId.value, activeHydrationSequence))
const activeReadingWindow = computed(() => conversationStore.readingWindow.value)
const chatItemsRevision = conversationStore.itemsRevision
const chatEntriesChange = conversationStore.entriesChange
const textarea = ref(null)
const attachmentActionsOpen = ref(false)
const attachmentActionAnchor = ref(null)
const screenshotInProgress = ref(false)
const screenshotSelection = ref(null)
let screenshotClosePromise = null
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
const historyOpenedByAgent = ref({})
const workspaceOpened = computed({ get: () => Boolean(workspaceOpenedByAgent.value[activeId.value]), set: value => { if (activeId.value) workspaceOpenedByAgent.value[activeId.value] = Boolean(value) } })
const workspaceName = computed({ get: () => workspaceNameByAgent.value[activeId.value] || '工作区', set: value => { if (activeId.value) workspaceNameByAgent.value[activeId.value] = String(value || '工作区') } })
const browserOpened = computed({ get: () => Boolean(browserOpenedByAgent.value[activeId.value]), set: value => { if (activeId.value) browserOpenedByAgent.value[activeId.value] = Boolean(value) } })
const browserName = computed({ get: () => browserNameByAgent.value[activeId.value] || '浏览器', set: value => { if (activeId.value) browserNameByAgent.value[activeId.value] = String(value || '浏览器') } })
const historyOpened = computed({ get: () => Boolean(historyOpenedByAgent.value[activeId.value]), set: value => { if (activeId.value) historyOpenedByAgent.value[activeId.value] = Boolean(value) } })
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
const dockExpanded = ref(false)
const dockCollapsed = ref(false)
const dockOccupiedHeight = ref(0)
const dockStatus = computed(() => !status.value.online || status.value.setup_required ? 'offline' : pending.value || turnStateStore.phase === 'waiting_user' ? 'pending' : turnInProgress.value ? 'generating' : 'idle')
const isTauri = !!window.__TAURI_INTERNALS__
const startupReady = ref(false)
const startupTimedOut = ref(false)
let startupTimeoutTimer = 0
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

watch(() => route.path, (path, previousPath) => {
  activeTab.value = tabForRoute(path, activeTab.value)
  if (previousPath?.startsWith('/settings') && !path.startsWith('/settings')) void refreshWorkgroupSettings()
}, { immediate: true })
watch(activeId, id => { agentStore.agentId = id || ''; setRemoteWorkerParent(id) }, { immediate: true })
watch(activeId, (id, previousId) => {
  if (activeAttachmentBatch && activeAttachmentBatch.ownerId !== id) {
    cancelWorkspaceAttachmentBatch(activeAttachmentBatch)
    activeAttachmentBatch = null
  }
  if (composerWriteOwner.value && id !== composerWriteOwner.value) {
    void Promise.resolve(cancelWorkspaceWrite(composerWriteRequest.value)).catch(() => {})
    composerWriteRequest.value = ''
    composerWriteOwner.value = ''
    composerWriteProgress.value = null
  }
  if (id !== previousId) clearCommandNotice()
  if (previousId && id !== previousId) void nextTick(() => {
    if (activeId.value !== previousId) mediaUrlRegistry.release(previousId)
  })
  if (id !== previousId) resourceNavigationError.value = ''
  if (previousId && id !== previousId) clearWorkspaceAnnotations(previousId)
  if (previousId && id !== previousId) terminalEndConfirmation.value = null
  if (previousId && id !== previousId && terminalOpenDialog.value) void closeTerminalOpenDialog()
})
watch([input, activeId], () => { void nextTick(resizeComposer) }, { flush: 'post' })

function resizeComposer() {
  const field = textarea.value
  if (!field) return
  const style = window.getComputedStyle(field)
  const lineHeight = Number.parseFloat(style.lineHeight) || 20
  const verticalPadding = (Number.parseFloat(style.paddingTop) || 0) + (Number.parseFloat(style.paddingBottom) || 0)
  const oneLineHeight = Math.max(lineHeight + verticalPadding, Number.parseFloat(style.minHeight) || 0)
  const maxHeight = Math.max(oneLineHeight, lineHeight * 6 + verticalPadding)
  field.style.height = `${oneLineHeight}px`
  const contentHeight = field.scrollHeight
  field.style.height = `${Math.max(oneLineHeight, Math.min(contentHeight, maxHeight))}px`
  field.style.overflowY = contentHeight > maxHeight ? 'auto' : 'hidden'
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
    return
  }
  await nextTick()
  resizeComposer()
  observeComposerField()
}, { flush: 'post' })

const previouslyOnline = ref(false)
const sidebarCollapsed = ref(false)
const openMenu = ref('')
const entryContextMenu = ref(null)
const removingWorkgroupId = ref('')
const dialog = ref('')
const queuedInputEdit = ref(null)
const queuedInputEditText = ref('')
const queuedInputEditError = ref('')
const queuedInputMutationId = ref('')
const queuedInputEditField = ref(null)
const autoOverviewPanel = ref(null)
const deletingAgentId = ref('')
const autoOverviewActionError = ref('')
const windowMaximized = ref(false)
const showAgentCreate = ref(false)
const workgroupList = ref([])
const selectedWorkgroupId = ref('')
const selectedWorkgroupName = ref('')
const pendingImagesByAgent = ref({})
const pendingFilesByAgent = ref({})
const composerWriteRequest = ref('')
const composerWriteOwner = ref('')
const composerWriteProgress = ref(null)
let activeAttachmentBatch = null
const pendingWorkspaceReferencesByAgent = ref({})
const pendingFiles = computed(() => pendingFilesByAgent.value[activeId.value] || [])
const pendingWorkspaceReferences = computed(() => workspaceReferencesForAgent(pendingWorkspaceReferencesByAgent.value, activeId.value))
const hasComposerContent = computed(() => hasWorkspaceMessageContent({ text: input.value, images: pendingImages.value, files: pendingFiles.value, references: pendingWorkspaceReferences.value }))
const pendingImages = computed({
  get: () => pendingImagesByAgent.value[activeId.value] || [],
  set: images => { if (activeId.value) pendingImagesByAgent.value[activeId.value] = images },
})
const attachmentLimitReached = computed(() => pendingImages.value.length + pendingFiles.value.length + pendingWorkspaceReferences.value.length >= 8)
const imageAttachmentInput = ref(null)
let unlistenWorkspaceWrite = null
let unlistenTerminal = null
let unlistenWindowResize = null
let unlistenWindowFocus = null
let unlistenConfigurationChanged = null
let selectionEpoch = 0
let activeHydrationSequence = 0
let nativeWebviewGeneration = 0
let manageSettingsRequestSequence = 0
let resourceNavigationEpoch = 0
const resourceNavigationError = ref('')
let modelRequestSequence = 0
let pollTimer = null
let summaryRefreshTimer = 0
let agentListRefreshSequence = 0
let agentNotificationChangeVersion = 0
let agentWatermarkTimer = 0
let agentWatermarkFlight = null
let agentWatermarkEpoch = ''
let agentWatermarkEpochVersion = 0
let agentWatermarkPollingEnabled = false
let desktopWindowFocused = true
let resourceRefreshTimer = 0
let activeHydrateNoticeTimer = 0
const conversationController = createConversationController({
  isCurrent: (agentId, generation) => activeId.value === agentId && activeHydrationSequence === generation,
  stageDeferredEvents: stageDeferredActiveAgentEvents,
  applyEvent: applyOrderedDesktopNodeEvent,
  run: (agentId, generation) => hydrateAgentSnapshot(agentId, generation).catch(e => {
    if (activeId.value === agentId && activeHydrationSequence === generation) error.value = String(e)
  }),
})
let workspaceConnectionState = ''
let composerResizeObserver = null
let terminalDialogRequestEpoch = 0
let terminalSessionListEpoch = 0
let terminalChannelRequestEpoch = 0
const terminalDecoders = new Map()
const mediaUrlRegistry = createMediaUrlRegistry()
const currentWindow = isTauri ? getCurrentWindow() : null
const workspaceScope = computed(() => workspaceNodeIdentity.value)

const activeAgent = computed(() => agents.value.find(a => a.agent_id === activeId.value))
const activeWorkgroupName = computed(() => workgroupList.value.find(group => group.workgroup_id === selectedWorkgroupId.value)?.display_name || selectedWorkgroupName.value || selectedWorkgroupId.value || '工作组')
const isWorkgroupView = computed(() => activeTab.value === 'workgroups')
const showWorkgroupSidebar = computed(() => shouldShowWorkgroupSidebar({ collaborationEnabled: workgroupCollaborationEnabled(manageSettings.value), hasWorkgroups: workgroupList.value.length > 0, isWorkgroupView: isWorkgroupView.value }))
const failedSendDraft = computed(() => failedSendDrafts.value[activeId.value] || '')
const normalAgents = computed(() => agents.value.filter(a => a.agent_type !== 'auto'))
const autoAgents = computed(() => agents.value.filter(a => a.agent_type === 'auto'))
const fullTranscript = computed(() => hydrate.value?.transcript || [])
const chatEntries = conversationStore.entries
function localPendingUsersForAgent(agentId) {
  return conversationStore.pendingInputsForAgent(agentId)
}
function setLocalPendingUsersForAgent(agentId, pending) {
  conversationStore.replacePendingInputs(agentId, pending)
}
function rebuildChatItemSnapshot(agentId = activeId.value) {
  return conversationStore.replaceVisibleItems(agentId, activeHydrationSequence)
}
const hitlQueue = conversationStore.hitlQueue
const lastToolActivity = computed(() => [...fullTranscript.value].reverse().find(m => ['tool_call', 'tool_result'].includes(m.kind)))
const lastConversationMessage = computed(() => [...fullTranscript.value].reverse().find(m => m.kind === 'user' || m.kind === 'assistant'))
const dockLatestMessage = computed(() => {
  const liveText = activeAssistantDraftText.value
  const text = liveText || lastConversationMessage.value?.text || ''
  return text ? { kind: liveText ? 'assistant' : lastConversationMessage.value?.kind || 'assistant', text, agent_id: activeId.value } : null
})
const pending = conversationStore.pendingHitl
watch([activeId, fullTranscript, () => hydrate.value?.pending_inputs], ([id, transcript, pendingInputs]) => {
  const local = localPendingUsersForAgent(id)
  if (!local.length) return
  const durablePendingIds = new Set((Array.isArray(pendingInputs) ? pendingInputs : []).map(item => String(item?.input_id || '')).filter(Boolean))
  const durableRequestIds = new Set((Array.isArray(pendingInputs) ? pendingInputs : []).map(item => String(item?.client_request_id || '')).filter(Boolean))
  const transcriptIds = new Set((Array.isArray(transcript) ? transcript : []).map(item => String(item?.item_id || '')).filter(Boolean))
  const transcriptRequestIds = new Set((Array.isArray(transcript) ? transcript : []).map(item => String(item?.client_request_id || '')).filter(Boolean))
  const nextLocal = local.filter(item => {
    const itemId = String(item?.item_id || item?.input_id || '')
    if (itemId && (durablePendingIds.has(itemId) || transcriptIds.has(itemId))) return false
    const requestId = String(item?.client_request_id || '')
    if (requestId && (durableRequestIds.has(requestId) || transcriptRequestIds.has(requestId))) return false
    return true
  })
  if (nextLocal.length === local.length) return
  setLocalPendingUsersForAgent(id, nextLocal)
}, { flush: 'post' })
watch([activeId, () => hydrate.value?.transcript, () => hydrate.value?.pending_inputs, () => conversationStore.pendingInputsRevision.value], () => {
  rebuildChatItemSnapshot()
}, { flush: 'post', immediate: true })
const approvalQueueIndex = computed(() => hitlQueue.value.findIndex(item => item.kind === 'approval'))
const dockPendingKind = computed(() => {
  if (turnStateStore.phase === 'waiting_user' || hitlQueue.value.some(item => item.kind === 'user_information')) return 'user_input'
  if (approvalQueueIndex.value >= 0) return 'approval'
  return 'request'
})
const canCancelTurn = computed(() => canCancelTurnPhase(turnStateStore.phase))
const turnInProgress = computed(() => busy.value || cancellingTurn.value || canCancelTurn.value)
const agentGenerating = computed(() => shouldShowTurnStopAction(turnStateStore.phase, busy.value, cancellingTurn.value))
const composerIntent = computed(() => normalComposerIntent({
  // waiting_user is still an active turn even though its composer action stays
  // in send mode; do not derive queue behavior from stop-button visibility.
  turnRunning: turnInProgress.value,
  interactionPending: hitlQueue.value.length > 0,
}))
const canSendMessage = computed(() => composerIntent.value !== 'blocked')
const canStopFromComposer = computed(() => canCancelTurn.value && turnStateStore.phase !== 'waiting_user')
const composerRuntimeStatus = computed(() => desktopComposerStatus({
  generating: turnInProgress.value,
  cancelling: cancellingTurn.value,
  hasAssistantOutput: hasAssistantOutput.value,
  turnState: turnStateStore,
}))
const hydrateSyncNoticeState = computed(() => hydrateSyncNotice(hydrateSyncStatus.value))
const hydrateSyncNoticeText = computed(() => hydrateSyncNoticeState.value === 'syncing'
  ? '正在同步结果'
  : hydrateSyncNoticeState.value === 'failed'
    ? '结果暂未同步'
    : hydrateSyncNoticeState.value === 'commit_failed'
      ? '本轮结果保存未成功'
      : '')
const canRetryHydrateSync = computed(() => canManuallyRetryHydrate(hydrateSyncStatus.value.phase))
const queuedInputsPaused = computed(() => Boolean(hydrate.value?.queue_paused) && (Array.isArray(hydrate.value?.pending_inputs) ? hydrate.value.pending_inputs.length : 0) > 0)
const composerStatusLabel = computed(() => hydrateSyncNoticeText.value || (queuedInputsPaused.value ? '待处理消息已暂停' : composerRuntimeStatus.value))
const turnError = computed(() => cancelledTurnSummary(turnStateStore)
  || (turnStateStore.error
    ? turnErrorSummary(turnStateStore.error, {
      partial: hasAssistantOutput.value || (turnStateStore.error.incomplete && hasPersistedPartialMessage(hydrate.value?.transcript)),
      retryCount: Math.max(0, (turnStateStore.modelAttempt?.attempt || 1) - 1),
    })
    : ''))
const modelRecoveryAvailable = computed(() => Boolean(turnStateStore.error?.code?.startsWith('provider_')))
const turnWatchdog = createTurnWatchdog({
  isAwaiting: () => Boolean(activeId.value && status.value.online),
  hasStuckStatus: () => turnInProgress.value || (hasAssistantOutput.value && !turnStateStore.terminal),
  onStuck: async () => {
    const id = activeId.value
    if (!id) return
    await hydrateAgentSnapshot(id)
    void refreshAgentSummaries()
  },
})
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
  setupRequired: status.value.setup_required,
  activeAgentId: activeId.value,
  activeAgentName: activeAgent.value?.display_name || '尚未选择智能体',
  isWorkgroup: isWorkgroupView.value,
  activeTab: activeTab.value,
  generating: turnInProgress.value,
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
  if (previousTab != null && tab !== previousTab) clearWorkspaceAnnotations()
  if (['chat', 'terminal', 'file', 'browser', 'history'].includes(tab)) {
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

function finishStartup() {
  startupReady.value = true
  startupTimedOut.value = false
  if (startupTimeoutTimer) window.clearTimeout(startupTimeoutTimer)
  startupTimeoutTimer = 0
}

function waitForNodeStartup() {
  if (startupTimeoutTimer) window.clearTimeout(startupTimeoutTimer)
  startupTimedOut.value = false
  startupTimeoutTimer = window.setTimeout(() => {
    startupTimeoutTimer = 0
    if (!startupReady.value) startupTimedOut.value = true
  }, 18000)
}

function retryNodeStartup() {
  status.value = { ...status.value, error: '正在重新连接本地服务…' }
  waitForNodeStartup()
  void loadAgents().then(() => {
    if (status.value.online) finishStartup()
  })
}

function continueOffline() {
  finishStartup()
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
    const identity = `${Boolean(next.online)}`
    const connectionChanged = Boolean(workspaceConnectionState && workspaceConnectionState !== identity)
    if (connectionChanged) workspaceRequestGeneration.value++
    workspaceConnectionState = identity
    status.value = next
    if (next.online && (!workgroupSettingsLoaded.value || connectionChanged)) void refreshWorkgroupSettings()
    if (next.online && (!workspaceNodeIdentity.value || workspaceNodeIdentity.value === 'node:unknown' || connectionChanged)) {
      try {
        const info = await invoke('desktop_node_request', {
          method: 'GET',
          path: '/v1/node/info',
          body: null,
        })
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
async function refreshWorkgroupSettings() {
  if (!status.value.online) return
  const requestSequence = ++manageSettingsRequestSequence
  try {
    const response = await getSetupConfig()
    if (requestSequence !== manageSettingsRequestSequence || !status.value.online) return
    manageSettings.value = response?.manage || null
    workgroupSettingsLoaded.value = true
  } catch {
    if (requestSequence === manageSettingsRequestSequence) workgroupSettingsLoaded.value = false
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
  if (status.value.setup_required) { error.value = '请先在桌面应用设置中完成本地服务的首次初始化。'; return }
  const requestSequence = ++agentListRefreshSequence
  const notificationVersionAtStart = agentNotificationChangeVersion
  try {
    const latest = await invoke('desktop_list_agents')
    if (requestSequence !== agentListRefreshSequence) return
    agents.value = mergeAgentNotificationSummaries(Array.isArray(latest) ? latest : [], agentNotificationProjections.value, notificationVersionAtStart)
    await selectAgent(chooseActiveAgentId(agents.value, activeId.value))
  } catch (e) { error.value = String(e) }
}
async function refreshAgentSummaries() {
  if (!status.value.online) return
  const requestSequence = ++agentListRefreshSequence
  const notificationVersionAtStart = agentNotificationChangeVersion
  try {
    const latest = await invoke('desktop_list_agents')
    if (requestSequence !== agentListRefreshSequence) return
    if (!Array.isArray(latest)) return
    agents.value = mergeAgentNotificationSummaries(latest, agentNotificationProjections.value, notificationVersionAtStart)
    if (!latest.some(agent => agent?.agent_id === activeId.value)) {
      await selectAgent(chooseActiveAgentId(latest, activeId.value))
    }
  } catch { /* Keep the last known sidebar state until the next Node event or reconnect. */ }
}
function processDesktopNodeEvent(event) {
  let applied = false
  try {
    (() => {
      if (event.event_type === 'resync_required') {
        if (summaryRefreshTimer) clearTimeout(summaryRefreshTimer)
        summaryRefreshTimer = window.setTimeout(() => { summaryRefreshTimer = 0; void refreshAgentSummaries() }, 0)
        if (activeId.value) scheduleActiveAgentHydrate(activeId.value, 0, null, true)
        return
      }
      if (event.event_type === 'notification_changed') {
        const projection = handleAgentNotificationEvent(
          agents.value,
          agentNotificationProjections.value,
          event,
          agentNotificationChangeVersion,
        )
        agents.value = projection.agents
        agentNotificationProjections.value = projection.projectionsByAgent
        agentNotificationChangeVersion = projection.version
        if (projection.shouldRefreshSummaries) {
          if (summaryRefreshTimer) clearTimeout(summaryRefreshTimer)
          summaryRefreshTimer = window.setTimeout(() => { summaryRefreshTimer = 0; void refreshAgentSummaries() }, 0)
        }
        return
      }
      if (event.agent_id !== activeId.value) {
        const row = agents.value.find(agent => agent.agent_id === event.agent_id)
        if (event.event_type === 'hitl_required' && row) row.has_pending_hitl = true
        if (['hitl_required', 'turn_finished', 'turn_state', 'error'].includes(event.event_type)) {
          if (summaryRefreshTimer) clearTimeout(summaryRefreshTimer)
          summaryRefreshTimer = window.setTimeout(() => { summaryRefreshTimer = 0; void refreshAgentSummaries() }, 250)
        }
        return
      }
      turnWatchdog.noteActivity()
      const reduction = conversationStore.applyActiveNodeEvent(event, {
        agentId: activeId.value,
        generation: activeHydrationSequence,
      })
      if (reduction.kind === 'ignored') return
      if (reduction.kind === 'patch') {
        if (reduction.accepted) {
          conversationController.coverWithPatch(
            activeId.value,
            activeHydrationSequence,
            event.data,
            event.agent_seq,
          )
        }
        return
      }
      if (reduction.kind === 'draft') {
        if (!reduction.accepted && reduction.reason === 'invalid_delta') {
          scheduleActiveAgentHydrate(activeId.value, 0, event.data, true)
          return
        }
        if (!reduction.accepted && reduction.reason === 'missing_sequence') {
          const recovery = conversationStore.claimDraftGapRecovery({
            agentId: activeId.value,
            generation: activeHydrationSequence,
            itemId: reduction.itemId,
            attemptId: reduction.attemptId,
            part: reduction.part,
          })
          if (recovery.shouldRecover) {
            scheduleActiveAgentHydrate(activeId.value, 0, event.data, true)
          }
        }
        return
      }
      if (reduction.stale) return
      applyRemoteWorkerEvent(event.event_type, event.data)
      if (reduction.retiredDrafts?.removedItemIds.length) {
        rebuildChatItemSnapshot(event.agent_id)
      }
      if (event.event_type === 'runtime/notice' && event.data?.message_kind === 'context_boundary') {
        void selectAgent(event.agent_id)
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
        if (conversationStore.conversationPatchCoversEvent(event, {
          agentId: eventAgentId,
          generation: activeHydrationSequence,
        })) return
        const terminalEvent = ['turn_finished', 'hitl_required', 'error'].includes(event.event_type)
        scheduleActiveAgentHydrate(eventAgentId, terminalEvent ? 0 : 120, event)
      }
    })()
    noteAppliedAgentSeq(event)
    applied = true
  } catch (e) {
    console.error('Failed to apply Node event:', e)
    if (event?.agent_id && activeId.value === event.agent_id) {
      scheduleActiveAgentHydrate(event.agent_id, 0, event.data, true)
    }
  }
  return applied
}

function drainActiveHydrateEventBuffer(buffer, snapshot = null, coverageValid = false) {
  return conversationController.drainEventBuffer(buffer, {
    agentId: activeId.value,
    generation: activeHydrationSequence,
    streamEpoch: snapshot?.stream_epoch || agentWatermarkEpoch || '',
    coveredAgentSeq: snapshot?.covered_agent_seq,
    coveredStreamSeq: snapshot?.covered_stream_seq,
    coverageValid,
  })
}

function drainDeferredActiveAgentEvents(agentId) {
  while (true) {
    const next = conversationStore.takeNextAgentEvent({
      agentId,
      generation: activeHydrationSequence,
      appliedSequence: conversationStore.appliedAgentSequence(agentId),
    })
    if (!next) break
    if (!processDesktopNodeEvent(next.event)) return
  }
  if (conversationStore.hasDeferredAgentEvents(agentId, activeHydrationSequence) && activeId.value === agentId) {
    scheduleActiveAgentHydrate(agentId, 0, null, true)
  }
}

function applyOrderedDesktopNodeEvent(event) {
  const agentId = String(event?.agent_id || '')
  if (!agentId || agentId !== activeId.value) return processDesktopNodeEvent(event)
  const eventEpoch = String(event?.stream_epoch || '')
  if (agentWatermarkEpoch && eventEpoch && eventEpoch !== agentWatermarkEpoch) {
    scheduleActiveAgentHydrate(agentId, 0, event?.data, true)
    return false
  }

  const result = conversationStore.offerAgentEvent(event, {
    agentId,
    generation: activeHydrationSequence,
    appliedSequence: conversationStore.appliedAgentSequence(agentId),
  })
  if (result.action === 'duplicate') return false
  if (result.action === 'buffered') {
    scheduleActiveAgentHydrate(agentId, 0, event?.data, true)
    return false
  }
  if (!processDesktopNodeEvent(event)) return false
  drainDeferredActiveAgentEvents(agentId)
  return true
}

function stageDeferredActiveAgentEvents(buffer) {
  if (!buffer) return
  const deferred = conversationStore.takeDeferredAgentEvents({ agentId: buffer.agentId, generation: buffer.generation })
  for (const event of deferred.events) enqueueHydrateEvent(buffer, event)
  if (deferred.overflow) buffer.overflow = true
}

function noteAppliedAgentSeq(event) {
  const agentId = String(event?.agent_id || '').trim()
  if (!agentId) return
  const cursor = conversationStore.noteAppliedAgentSequence(agentId, event?.agent_seq)
  if (cursor.gap && !cursor.gapWasPresent) {
    if (activeId.value === agentId) {
      conversationStore.clearAgentSequenceGap(agentId)
      scheduleActiveAgentHydrate(agentId, 0, { data: { agent_seq: cursor.sequence } }, true)
    } else {
      if (summaryRefreshTimer) clearTimeout(summaryRefreshTimer)
      summaryRefreshTimer = window.setTimeout(() => {
        summaryRefreshTimer = 0
        void refreshAgentSummaries()
      }, 0)
    }
  }
}
function adoptAgentWatermarkEpoch(epochValue) {
  const epoch = String(epochValue || '')
  if (!epoch) return false
  const changedFromKnownEpoch = Boolean(agentWatermarkEpoch && agentWatermarkEpoch !== epoch)
  if (agentWatermarkEpoch !== epoch) agentWatermarkEpochVersion += 1
  const changed = changedFromKnownEpoch
  if (changed) {
    conversationStore.resetAgentSequences()
    // Agent SSE cursors are process-local. Do not let a high sequence from the
    // previous Node process pin the active hydrate target after restart.
    conversationController.resetAgentSequenceTarget(activeId.value, activeHydrationSequence)
  }
  agentWatermarkEpoch = epoch
  return changed
}
async function pollAgentWatermarks() {
  if (!status.value.online || status.value.setup_required) return
  if (agentWatermarkFlight) return agentWatermarkFlight
  agentWatermarkFlight = (async () => {
    const response = await getAgentWatermarks()
    if (!agentWatermarkPollingEnabled) return
    const epoch = String(response?.stream_epoch || '')
    if (!epoch) return
    const epochChanged = adoptAgentWatermarkEpoch(epoch)
    reconcileAgentWatermarks(response, {
      activeAgentId: activeId.value,
      epochChanged,
      store: conversationStore,
      localConversationRevision: id => id === activeId.value
        ? Math.max(activeHydrateConversationRevision.value, Number(hydrate.value?.conversation_revision) || 0)
        : 0,
      hydrateActive: (id, watermark, force) => scheduleActiveAgentHydrate(
        id,
        0,
        watermark ? { data: watermark } : null,
        force,
      ),
      refreshBackground: () => {
      if (summaryRefreshTimer) clearTimeout(summaryRefreshTimer)
      summaryRefreshTimer = window.setTimeout(() => {
        summaryRefreshTimer = 0
        void refreshAgentSummaries()
      }, 0)
      },
    })
  })().catch(() => {
    // A missed watermark request is retried by the next connection-level poll;
    // the bounded hydrate retry state remains owned by hydrateAgentSnapshot.
  }).finally(() => {
    agentWatermarkFlight = null
  })
  return agentWatermarkFlight
}
function scheduleAgentWatermarkPoll(delay = null) {
  if (!agentWatermarkPollingEnabled) return
  if (agentWatermarkTimer) window.clearTimeout(agentWatermarkTimer)
  const background = document.visibilityState === 'hidden' || !desktopWindowFocused
  const nextDelay = delay == null ? (background ? 60000 : 15000) : Math.max(0, delay)
  agentWatermarkTimer = window.setTimeout(async () => {
    agentWatermarkTimer = 0
    await pollAgentWatermarks()
    if (agentWatermarkPollingEnabled) scheduleAgentWatermarkPoll()
  }, nextDelay)
}
function onDocumentVisibilityChange() {
  if (document.visibilityState !== 'hidden' && desktopWindowFocused) scheduleAgentWatermarkPoll(0)
  else scheduleAgentWatermarkPoll(60000)
}
async function refreshAgentModel(id) {
  const requestSequence = ++modelRequestSequence
  if (!id) { llmSettings.value = null; return }
  try {
    const response = await getAgentModelView(id)
    if (activeId.value !== id || requestSequence !== modelRequestSequence) return
    llmProfileCatalog.value = Array.isArray(response?.profile_catalog) ? response.profile_catalog : []
    llmSettings.value = response?.llm_settings || null
  } catch {
    if (activeId.value === id && requestSequence === modelRequestSequence) llmSettings.value = null
  }
}
async function restoreAgentMedia(snapshot, agentId) {
  const lease = mediaUrlRegistry.begin(agentId)
  const loadSource = async source => {
    const raw = String(source || '')
    if (raw.startsWith('data:') || raw.startsWith('blob:')) return raw
    const mediaId = agentMediaId(source, agentId)
    if (!mediaId) return ''
    return mediaUrlRegistry.acquire(lease, mediaId, () => readAgentMedia(agentId, mediaId))
  }
  try {
    return { snapshot: await restoreTranscriptMedia(snapshot, loadSource), lease }
  } catch (cause) {
    mediaUrlRegistry.discard(lease)
    throw cause
  }
}
function hydrateAgentSnapshot(id, ownerSequence = activeHydrationSequence) {
  if (!id || activeId.value !== id || ownerSequence !== activeHydrationSequence) return Promise.resolve()
  return conversationController.runSingleFlight(id, ownerSequence, async () => {
    conversationController.markDirty(id, ownerSequence)
    if (activeHydrateNoticeTimer) clearTimeout(activeHydrateNoticeTimer)
    const eventBuffer = conversationController.beginEventBuffer(id, ownerSequence)
    if (!eventBuffer) return
    conversationStore.beginSync(Date.now())
    activeHydrateNoticeTimer = window.setTimeout(() => {
      activeHydrateNoticeTimer = 0
      conversationStore.markSyncing()
    }, 600)
    try {
    while (conversationController.consumeDirty(id, ownerSequence) && activeId.value === id) {
      conversationController.stageDeferredEvents(eventBuffer)
      const requestSequence = activeHydrationSequence
      const requestEpochVersion = agentWatermarkEpochVersion
      const workerEventVersion = currentRemoteWorkerEventVersion()
      const requestSpan = startPerformanceSpan('hydrate.ipc')
      let snapshot
      try {
        snapshot = await invoke('desktop_hydrate_agent', { agentId: id })
      } finally {
        requestSpan.end()
      }
      if (requestSequence !== activeHydrationSequence || activeId.value !== id) return
      const snapshotEpoch = String(snapshot?.stream_epoch || '')
      if (agentWatermarkEpochVersion !== requestEpochVersion && snapshotEpoch && snapshotEpoch !== agentWatermarkEpoch) {
        conversationController.markDirty(id, ownerSequence)
        drainActiveHydrateEventBuffer(eventBuffer)
        continue
      }
      adoptAgentWatermarkEpoch(snapshotEpoch)
      const snapshotEpochCurrent = Boolean(snapshotEpoch && (!agentWatermarkEpoch || snapshotEpoch === agentWatermarkEpoch))
      const coveredAgentSeq = Number(snapshot?.covered_agent_seq)
      const snapshotCanReconcileDrafts = snapshotEpochCurrent
        && Number.isSafeInteger(coveredAgentSeq)
        && coveredAgentSeq >= 0
        && conversationStore.appliedAgentSequence(id) <= coveredAgentSeq
      const applySpan = startPerformanceSpan('hydrate.apply_to_dom', {
        transcriptItems: Array.isArray(snapshot?.transcript) ? snapshot.transcript.length : 0,
      })
      turnWatchdog.noteActivity()
      const mergedSnapshot = conversationStore.applyHydrateSnapshot(snapshot, {
        agentId: id,
        generation: requestSequence,
        lifecycleSeq: turnStateStore.lifecycleSeq,
        reconcileCoveredDrafts: snapshotCanReconcileDrafts,
      })
      const historyRevision = mergedSnapshot.historyRevision
      const conversationRevision = mergedSnapshot.conversationRevision
      const shouldApplyMessages = mergedSnapshot.transcriptAccepted
      if (mergedSnapshot.accepted) {
        if (snapshotCanReconcileDrafts && Array.isArray(snapshot?.assistant_drafts)) rebuildChatItemSnapshot(id)
        if (mergedSnapshot.lifecycleAccepted && Array.isArray(snapshot?.child_agents)) replaceChildrenFromHydrate(snapshot.child_agents, id, workerEventVersion)
      }
      if (snapshotEpochCurrent && Number.isSafeInteger(snapshot?.covered_agent_seq) && snapshot.covered_agent_seq >= 0) {
        conversationStore.applyAgentSequenceCoverage(id, snapshot.covered_agent_seq)
      }
      if (shouldApplyMessages) {
        // Restore media from the accepted, reference-merged transcript. Using
        // the raw IPC snapshot here reclones every history row on each hydrate,
        // even when only lifecycle state changed.
        void restoreAgentMedia(mergedSnapshot.snapshot, id).then(restored => {
          if (requestSequence === activeHydrationSequence && activeId.value === id && activeHydrateContentRevision.value === historyRevision && activeHydrateConversationRevision.value === conversationRevision && hydrate.value) {
            const conversationEpoch = Number(snapshot?.conversation_epoch) || 0
            const applied = conversationStore.applyMediaTranscript(id, requestSequence, {
              conversationEpoch,
              historyRevision,
              expectedConversationRevision: conversationRevision,
              transcript: restored.snapshot.transcript,
            })
            if (applied) mediaUrlRegistry.commit(restored.lease)
            else mediaUrlRegistry.discard(restored.lease)
          } else mediaUrlRegistry.discard(restored.lease)
        }).catch(e => {
          if (requestSequence === activeHydrationSequence && activeId.value === id) error.value = `无法读取会话图片：${String(e.message || e)}`
        })
      }
      const target = conversationController.getTarget(id, ownerSequence)
      if (target.historyRevision > historyRevision
        || target.conversationRevision > Number(snapshot?.conversation_revision || 0)
        || target.lifecycleSeq > (Number(snapshot?.turn_state?.lifecycle_seq) || 0)) {
        conversationController.markDirty(id, ownerSequence)
        await nextTick()
        drainActiveHydrateEventBuffer(eventBuffer, snapshot, snapshotEpochCurrent && Number.isSafeInteger(coveredAgentSeq) && coveredAgentSeq >= 0)
        applySpan.end({ accepted: false, transcriptItems: hydrate.value?.transcript?.length || 0 })
        throw new Error('Snapshot has not reached the requested conversation state')
      }
      await nextTick()
      drainActiveHydrateEventBuffer(eventBuffer, snapshot, snapshotEpochCurrent && Number.isSafeInteger(coveredAgentSeq) && coveredAgentSeq >= 0)
      applySpan.end({ accepted: true, transcriptItems: hydrate.value?.transcript?.length || 0 })
    }
    conversationController.markClean(id, ownerSequence)
    if (activeHydrateNoticeTimer) clearTimeout(activeHydrateNoticeTimer)
    activeHydrateNoticeTimer = 0
    conversationStore.completeSync(snapshotResultCommitStatus(hydrate.value))
    if (activeId.value === id && isHydrateReadError(error.value)) error.value = ''
    } catch (e) {
      if (activeId.value !== id || activeHydrationSequence !== ownerSequence) return
      drainActiveHydrateEventBuffer(eventBuffer)
      conversationController.markDirty(id, ownerSequence)
      conversationController.scheduleRetry(id, ownerSequence, () => {
        if (activeHydrateNoticeTimer) clearTimeout(activeHydrateNoticeTimer)
        activeHydrateNoticeTimer = 0
        conversationStore.failSync()
        error.value = `结果暂未同步：${String(e.message || e)}`
      })
    } finally {
      if (activeHydrationSequence === ownerSequence) {
        const bufferNeedsRepair = eventBuffer.overflow || eventBuffer.epochMismatch
        conversationController.releaseEventBuffer(eventBuffer)
        if (bufferNeedsRepair && activeId.value === id && !conversationController.hasRetry(id, ownerSequence)) {
          scheduleActiveAgentHydrate(id, 0, null, true)
        } else if (conversationController.isDirty(id, ownerSequence) && activeId.value === id && !conversationController.hasRetry(id, ownerSequence)) {
          scheduleActiveAgentHydrate(id, 0)
        }
      }
    }
  })
}
function snapshotResultCommitStatus(snapshot) {
  return String(snapshot?.turn_state?.result_commit_status || '')
}
function retryHydrateSync() {
  const id = activeId.value
  if (!id || !canManuallyRetryHydrate(hydrateSyncStatus.value.phase)) return
  if (isHydrateReadError(error.value)) error.value = ''
  conversationController.retryNow(id, activeHydrationSequence)
}
function scheduleActiveAgentHydrate(id, delay = 100, target = null, force = false) {
  if (!id || id !== activeId.value) return
  const scheduled = conversationController.request(id, {
    generation: activeHydrationSequence,
    delay,
    requestedTarget: target,
    force,
  })
  const changedTarget = Boolean(scheduled.targetChanged)
  if (changedTarget || force) {
    if (hydrateSyncStatus.value.phase === 'failed') conversationStore.clearFailedSync()
  }
}
async function switchLLMProfile(profileId) {
  const id = activeId.value
  if (!id || !profileId || turnInProgress.value) return
  error.value = ''
  try {
    const response = await patchAgent(id, { defaults: { llm: { active: profileId } } })
    if (activeId.value !== id) return
    const agent = response?.agent || response
    const profile = agentActiveProfile(agent) || profileId
    const profileConfig = llmProfileCatalog.value.find(item => item?.id === profile)
    llmSettings.value = {
      ...llmSettings.value,
      ...(profileConfig ? {
        provider: profileConfig.provider,
        model: profileConfig.model,
        mock: Boolean(profileConfig.mock),
        multimodal_enabled: Boolean(profileConfig.multimodal_enabled),
        thinking: profileConfig.thinking || '',
        reasoning_effort: profileConfig.reasoning_effort || '',
      } : {}),
      active_profile: profile,
    }
  } catch (e) {
    if (activeId.value === id) error.value = String(e.message || e)
  }
}
function openModelSettings(profileId) {
  const profile = String(profileId || agentActiveProfile(activeAgent.value) || '').trim()
  if (!profile) return
  void router.push({ path: '/settings/connection', query: { profile } })
}
async function openModelPicker() {
  if (activeTab.value !== 'chat') {
    showChat()
    await nextTick()
  }
  composerToolbar.value?.openPicker?.()
}
async function selectAgent(id, { clearError = true, openChat = false } = {}) {
  const changed = activeId.value !== id
  const epoch = ++selectionEpoch
  if (changed) {
    activeHydrationSequence += 1
    if (isTauri) {
      try {
        const scopeAccepted = await invoke('desktop_set_active_agent', {
          agentId: id,
          selectionGeneration: activeHydrationSequence,
          webviewGeneration: nativeWebviewGeneration,
        })
        if (!scopeAccepted) throw new Error('当前窗口会话已更新，请重试切换智能体。')
      } catch (cause) {
        if (epoch === selectionEpoch) {
          const message = `无法切换当前会话：${String(cause?.message || cause)}`
          if (activeId.value) setAgentError(activeId.value, message)
          else globalError.value = message
        }
        return
      }
      if (epoch !== selectionEpoch) return
    }
  }
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
    conversationController.reset(id, activeHydrationSequence)
    conversationStore.setActiveScope(id, activeHydrationSequence)
    if (activeHydrateNoticeTimer) clearTimeout(activeHydrateNoticeTimer)
    activeHydrateNoticeTimer = 0
    userInfoSelected.value = []
    conversationStore.resetTurnState()
  }
  if (viewTransition.enterChat && route.path !== '/chat') void router.push('/chat')
  activeId.value = id
  if (changed) conversationStore.replaceHydrateSnapshot(null)
  if (!id) {
    conversationStore.replaceHydrateSnapshot(null)
    llmSettings.value = null
    input.value = ''
    dockDraft.value = ''
    userInfoSelected.value = []
    conversationStore.resetTurnState()
    if (clearError) error.value = ''
    return
  }
  if (clearError) error.value = ''
  try {
    await hydrateAgentSnapshot(id)
    if (epoch === selectionEpoch && activeId.value === id) void refreshAgentModel(id)
  } catch (e) { if (epoch === selectionEpoch) error.value = String(e) }
}
async function send() {
  const text = input.value.trim()
  const images = pendingImages.value.slice()
  const files = pendingFiles.value.slice()
  const references = pendingWorkspaceReferences.value.slice()
  if (!hasWorkspaceMessageContent({ text, images, files, references }) || !activeId.value) return
  if (composerIntent.value === 'blocked') { error.value = '请在请求卡片中完成待处理交互，再发送新消息。'; return }
  if (messageSubmitPending.value) { error.value = '上一条消息仍在确认发送结果，请稍后再发送。'; return }
  if (text.startsWith('/') && !images.length && !files.length && !references.length) {
    if (composerIntent.value === 'queue') { error.value = '正在执行任务，应用命令不能加入等待队列。'; return }
    const agentId = activeId.value
    input.value = ''
    inputDrafts.value[agentId] = ''
    await executeSlashCommand(text, agentId)
    return
  }
  clearCommandNotice()
  const queueDuringTurn = composerIntent.value === 'queue'
  const agentId = activeId.value
  input.value = ''
  inputDrafts.value[agentId] = ''
  const sent = await sendMessageToActiveAgent(text, images, files, references, { allowDuringTurn: queueDuringTurn })
  if (!sent) input.value = recoverFailedSend(failedSendDrafts.value, agentId, activeId.value, input.value, text)
  else {
    pendingImagesByAgent.value[agentId] = (pendingImagesByAgent.value[agentId] || []).filter(image => !images.includes(image))
    pendingFilesByAgent.value[agentId] = (pendingFilesByAgent.value[agentId] || []).filter(file => !files.includes(file))
  }
}
async function sendMessageToActiveAgent(text, images = [], files = [], references = [], { allowDuringTurn = false } = {}) {
  text = String(text || '').trim()
  const turnWasActive = turnInProgress.value
  if (!hasWorkspaceMessageContent({ text, images, files, references }) || !activeId.value || (turnWasActive && !allowDuringTurn)) return false
  const agentId = activeId.value
  const conversationEpoch = Math.max(1, Number(hydrate.value?.conversation_epoch) || 1)
  if (messageSubmitPendingByAgent.value[agentId]) return false
  messageSubmitPendingByAgent.value = { ...messageSubmitPendingByAgent.value, [agentId]: true }
  let pendingUser
  let receipt
  setAgentError(agentId, '')
  setAgentBusy(agentId, true)
  try {
    await dispatchWorkspaceMessage({ agentId, text, images, files, references }, async (...args) => {
      receipt = await submitMessage(...args, {
        conversationEpoch,
        clientRequestId: pendingUser?.client_request_id,
        onClientRequestId: clientRequestId => {
          if (!pendingUser) return
          const previousId = pendingUser.id
          pendingUser = { ...pendingUser, id: `pending:${clientRequestId}`, client_request_id: clientRequestId }
          setLocalPendingUsersForAgent(agentId, localPendingUsersForAgent(agentId).map(item => item.id === previousId ? pendingUser : item))
        },
      })
      const inputSeq = Number(receipt?.input_seq)
      if (pendingUser && Number.isSafeInteger(inputSeq) && inputSeq > 0) {
        const inputId = `${agentId}/input/${inputSeq}`
        const previousId = pendingUser.id
        pendingUser = {
          ...pendingUser,
          id: receipt?.input_id || inputId,
          input_id: receipt?.input_id || inputId,
          item_id: receipt?.input_id || inputId,
          input_seq: inputSeq,
          queue_status: receipt?.status === 'queued' ? 'queued' : 'claimed',
        }
        const current = localPendingUsersForAgent(agentId).map(item => item.id === previousId ? pendingUser : item)
        setLocalPendingUsersForAgent(agentId, current)
      }
      return receipt
    }, message => {
      const clientRequestId = newClientRequestId()
      pendingUser = {
        id: `pending:${clientRequestId}`,
        client_request_id: clientRequestId,
        kind: 'user',
        text: message.text,
        images: message.images.map(image => image.url),
        file_refs: message.files.map(file => ({ ...file })),
        queue_status: turnWasActive ? 'queued' : 'claimed',
      }
      setLocalPendingUsersForAgent(agentId, [...localPendingUsersForAgent(agentId), pendingUser])
    })
    if (references.length) consumeWorkspaceReferences(agentId, references)
  } catch (e) {
    if (pendingUser) {
      const remaining = localPendingUsersForAgent(agentId).filter(item => item.id !== pendingUser.id)
      setLocalPendingUsersForAgent(agentId, remaining)
    }
    setAgentError(agentId, e?.unknownSubmission
      ? '发送结果暂时无法确认，消息草稿已保留。请恢复连接后重试同一条内容，系统会先核对接收回执，避免重复发送。'
      : String(e))
    return false
  } finally {
    setAgentBusy(agentId, false)
    const nextPending = { ...messageSubmitPendingByAgent.value }
    delete nextPending[agentId]
    messageSubmitPendingByAgent.value = nextPending
  }
  if (activeId.value === agentId) {
    // Keep one bounded snapshot fallback after the optimistic send. Re-running
    // selectAgent here also refreshes model metadata and can race the turn SSE,
    // producing several back-to-back full hydrate reads for one message.
    scheduleActiveAgentHydrate(agentId, 80)
    await nextTick()
    chatPanel.value?.scrollToTail()
  }
  return true
}
async function executeSlashCommand(text, agentId) {
  error.value = ''
  clearCommandNotice()
  const consumed = await dispatchDesktopSlashCommand(text, {
    agentId,
    clearContext: clearContextWithConfirmation,
    compressContext: async id => {
      if (!status.value.online || status.value.setup_required) throw new Error('本地服务未就绪，无法压缩上下文')
      if (activeId.value !== id || isWorkgroupView.value) throw new Error('请先选择一个智能体')
      if (turnInProgress.value) throw new Error('请等待当前生成结束后再压缩上下文')
      if (pending.value || approvalQueueIndex.value >= 0) throw new Error('请先处理待审批请求')
      return compressContext(id)
    },
    refresh: async id => {
      if (activeId.value !== id) return
      await selectAgent(id)
    },
    onError: message => { error.value = message },
    onNotice: showCommandNotice,
  })
  if (!consumed) error.value = '无法处理此斜杠命令'
}
function handleWorkspaceReference(payload) {
  const agentId = String(payload?.agentId || '')
  if (!agentId || agentId !== activeId.value || !payload.path) return
  const existing = pendingWorkspaceReferencesByAgent.value[agentId] || []
  try {
    const reference = createWorkspaceReference(payload)
    const index = existing.findIndex(item => item.id === reference.id || item.path === reference.path && item.anchor?.label === '整个文件' && reference.anchor.label === '整个文件' && !item.annotation && !reference.annotation)
    if (index < 0) {
      const total = (pendingImagesByAgent.value[agentId] || []).length + (pendingFilesByAgent.value[agentId] || []).length + existing.length
      if (total >= 8) { error.value = '一条消息最多添加 8 个附件或注解。'; return }
      pendingWorkspaceReferencesByAgent.value[agentId] = [...existing, reference]
      dockCollapsed.value = false
    } else {
      pendingWorkspaceReferencesByAgent.value[agentId] = existing.map((item, itemIndex) => itemIndex === index ? reference : item)
    }
    error.value = ''
  } catch (cause) { error.value = String(cause?.message || cause) }
}
function removeWorkspaceReferenceById(id, { removeBubble = true } = {}) {
  const agentId = activeId.value
  if (!agentId || !id) return
  pendingWorkspaceReferencesByAgent.value[agentId] = (pendingWorkspaceReferencesByAgent.value[agentId] || []).filter(reference => reference.id !== id)
  if (removeBubble) workspaceFilesPanel.value?.removeAnnotation?.(id)
}
function onWorkspaceAnnotationRemove(id) {
  removeWorkspaceReferenceById(id, { removeBubble: false })
}
function clearWorkspaceAnnotations(agentId = activeId.value) {
  if (!agentId) return
  pendingWorkspaceReferencesByAgent.value[agentId] = []
  if (agentId === activeId.value) workspaceFilesPanel.value?.clearAnnotations?.()
}
function consumeWorkspaceReferences(agentId, references) {
  pendingWorkspaceReferencesByAgent.value = settleWorkspaceReferences(pendingWorkspaceReferencesByAgent.value, agentId, references, true)
  const sentIds = new Set(references.map(reference => reference.id))
  if (!sentIds.size) return
  if (agentId === activeId.value) {
    for (const id of sentIds) workspaceFilesPanel.value?.removeAnnotation?.(id)
  }
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
async function toggleAttachmentActions() {
  if (attachmentActionsOpen.value) { closeAttachmentActions(); return }
  attachmentActionsOpen.value = true
  await nextTick()
  attachmentActionAnchor.value?.querySelector('.attachment-action-option:not(:disabled)')?.focus()
}
function closeAttachmentActions(restoreFocus = false) {
  attachmentActionsOpen.value = false
  if (restoreFocus) void nextTick(() => attachmentActionAnchor.value?.querySelector('.attach-button')?.focus())
}
function onAttachmentActionsKeydown(event) {
  if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
  const items = Array.from(attachmentActionAnchor.value?.querySelectorAll('.attachment-action-option:not(:disabled)') || [])
  if (!items.length) return
  event.preventDefault()
  const index = items.indexOf(document.activeElement)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : event.key === 'ArrowRight' ? (index + 1 + items.length) % items.length : (index <= 0 ? items.length - 1 : index - 1)
  items[next]?.focus()
}
function openFilePicker() {
  closeAttachmentActions()
  void importComposerFiles()
}
async function importComposerFiles() {
  const ownerId = activeId.value
  if (!ownerId || composerWriteRequest.value || activeAttachmentBatch) return
  const remaining = 8 - (pendingImagesByAgent.value[ownerId] || []).length - (pendingFilesByAgent.value[ownerId] || []).length - (pendingWorkspaceReferencesByAgent.value[ownerId] || []).length
  if (remaining <= 0) return
  const requestId = newWorkspaceWriteRequestId()
  composerWriteRequest.value = requestId
  composerWriteOwner.value = ownerId
  composerWriteProgress.value = { bytes: 0, total: 0, phase: 'preparing' }
  try {
    const result = await pickWorkspaceImport(ownerId, '', { requestId, maxFiles: Math.min(8, remaining) })
    if (activeId.value !== ownerId || composerWriteRequest.value !== requestId) return
    const refs = [...(pendingFilesByAgent.value[ownerId] || [])]
    for (const item of result.results || []) {
      if (item.status === 'saved') {
        if (item.file_ref && !refs.some(ref => ref.path === item.file_ref)) refs.push({ path: item.file_ref, name: item.name || '文件' })
      } else if (item.status !== 'cancelled') setAgentError(ownerId, `${item.name || '文件'}：${item.message || item.status}`)
    }
    pendingFilesByAgent.value[ownerId] = refs
  } catch (e) {
    if (activeId.value === ownerId && composerWriteRequest.value === requestId) setAgentError(ownerId, String(e.message || e))
  } finally {
    if (composerWriteRequest.value === requestId) { composerWriteRequest.value = ''; composerWriteOwner.value = ''; composerWriteProgress.value = null }
  }
}
async function cancelComposerImport() {
  const requestId = composerWriteRequest.value
  if (!requestId) return
  const batch = activeAttachmentBatch
  const cancellingBatch = Boolean(batch && batch.ownerId === composerWriteOwner.value && batch.currentRequestId === requestId)
  if (cancellingBatch) cancelWorkspaceAttachmentBatch(batch)
  try { await cancelWorkspaceWrite(requestId) }
  catch (e) { if (!cancellingBatch && activeId.value === composerWriteOwner.value) setAgentError(composerWriteOwner.value, String(e.message || e)) }
}
function openImagePicker() {
  closeAttachmentActions()
  imageAttachmentInput.value?.click()
}
async function captureScreenshot() {
  const ownerId = activeId.value
  if (!ownerId || screenshotInProgress.value || (pendingImagesByAgent.value[ownerId]?.length || 0) + (pendingFilesByAgent.value[ownerId]?.length || 0) + (pendingWorkspaceReferencesByAgent.value[ownerId]?.length || 0) >= 8) return
  closeAttachmentActions()
  screenshotInProgress.value = true
  try {
    const base64 = await invoke('desktop_capture_screenshot')
    const selection = {
      url: `data:image/png;base64,${base64}`,
      ownerId,
      previousFullscreen: currentWindow ? await currentWindow.isFullscreen() : false,
      previousAlwaysOnTop: currentWindow ? await currentWindow.isAlwaysOnTop() : false,
    }
    screenshotSelection.value = selection
    if (currentWindow) {
      try {
        await nextTick()
        await currentWindow.setAlwaysOnTop(true)
        if (!selection.previousFullscreen) await currentWindow.setFullscreen(true)
      } catch (error) {
        await closeScreenshotSelection()
        throw error
      }
    }
  } catch (e) {
    const name = String(e?.name || '')
    if (name !== 'AbortError' && activeId.value === ownerId) {
      setAgentError(ownerId, String(e?.message || e))
    }
  } finally {
    screenshotInProgress.value = false
  }
}
async function confirmScreenshotSelection(file) {
  const selection = screenshotSelection.value
  if (!selection || !file) return
  await closeScreenshotSelection()
  await processAttachments([file], selection.ownerId)
}
async function closeScreenshotSelection() {
  if (screenshotClosePromise) return screenshotClosePromise
  const selection = screenshotSelection.value
  if (!selection) return
  screenshotClosePromise = (async () => {
    try {
      if (currentWindow) {
        if (!selection.previousFullscreen) await currentWindow.setFullscreen(false)
        await currentWindow.setAlwaysOnTop(selection.previousAlwaysOnTop)
      }
    } finally {
      screenshotSelection.value = null
      if (selection.url) URL.revokeObjectURL(selection.url)
      screenshotClosePromise = null
    }
  })()
  return screenshotClosePromise
}
function onScreenshotSelectionError(message) {
  const ownerId = screenshotSelection.value?.ownerId
  if (ownerId && activeId.value === ownerId) setAgentError(ownerId, message)
}
async function processAttachments(files, ownerId) {
  if (activeAttachmentBatch || composerWriteRequest.value) return
  const batch = createWorkspaceAttachmentBatch(ownerId)
  activeAttachmentBatch = batch
  const isCurrent = () => isWorkspaceAttachmentBatchCurrent(batch, activeAttachmentBatch, activeId.value)
  const remaining = 8 - (pendingImagesByAgent.value[ownerId] || []).length - (pendingFilesByAgent.value[ownerId] || []).length - (pendingWorkspaceReferencesByAgent.value[ownerId] || []).length
  const priorUploadError = workspaceUploadErrors.get(ownerId)
  let batchHadFailure = false
  try {
    for (const file of files.slice(0, Math.max(0, remaining))) {
      if (!isCurrent()) break
      if (file.size > 16 * 1024 * 1024) { batchHadFailure = true; workspaceUploadErrors.delete(ownerId); if (isCurrent()) error.value = `${file.name}：文件不能超过 16 MiB`; continue }
      if (['image/jpeg', 'image/png', 'image/gif', 'image/webp'].includes(file.type)) {
        if (file.size > 10 * 1024 * 1024) { batchHadFailure = true; workspaceUploadErrors.delete(ownerId); if (isCurrent()) error.value = `${file.name}：图片不能超过 10 MB`; continue }
        try {
          const image = { name: file.name, url: await readImage(file) }
          if (!isCurrent()) { URL.revokeObjectURL(image.url); break }
          pendingImagesByAgent.value[ownerId] = [...(pendingImagesByAgent.value[ownerId] || []), image]
        } catch (e) { batchHadFailure = true; workspaceUploadErrors.delete(ownerId); if (isCurrent()) error.value = String(e.message || e) }
      } else {
        const requestId = newWorkspaceWriteRequestId()
        batch.currentRequestId = requestId
        composerWriteRequest.value = requestId
        composerWriteOwner.value = ownerId
        composerWriteProgress.value = { bytes: 0, total: file.size, phase: 'preparing' }
        try {
          const uploaded = await uploadWorkspaceFile(ownerId, '', file, { requestId, signal: batch.controller.signal })
          if (!isCurrent()) break
          if (shouldClearWorkspaceUploadError(error.value, priorUploadError, batchHadFailure)) {
            error.value = ''
            workspaceUploadErrors.delete(ownerId)
          }
          const refs = pendingFilesByAgent.value[ownerId] || []
          if (!refs.some(item => item.path === uploaded.file_ref)) pendingFilesByAgent.value[ownerId] = [...refs, { path: uploaded.file_ref, name: uploaded.name || file.name }]
        } catch (e) {
          if (e?.writeStatus === 'cancelled') batch.cancelled = true
          else if (isCurrent()) {
            batchHadFailure = true
            const message = `${file.name}：${String(e.message || e)}`
            workspaceUploadErrors.set(ownerId, message)
            error.value = message
          }
        } finally {
          if (composerWriteRequest.value === requestId) { composerWriteRequest.value = ''; composerWriteOwner.value = ''; composerWriteProgress.value = null }
          if (batch.currentRequestId === requestId) batch.currentRequestId = ''
        }
      }
    }
  } finally {
    if (activeAttachmentBatch === batch) activeAttachmentBatch = null
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
async function resumeQueuedInputs() {
  const id = activeId.value
  const expectedRevision = Number(hydrate.value?.queue_control_revision)
  if (!id || !queuedInputsPaused.value || !Number.isSafeInteger(expectedRevision) || expectedRevision <= 0 || queueControlPending.value) return
  queueControlPending.value = true
  try {
    await setInputQueuePaused(id, false, expectedRevision)
    if (activeId.value === id) await selectAgent(id)
  } catch (e) {
    if (activeId.value === id) {
      if (Number(e?.status) === 409) {
        await hydrateAgentSnapshot(id)
        setAgentError(id, '待处理队列状态已变化，已刷新当前状态。')
      } else {
        setAgentError(id, String(e.message || e))
      }
    }
  } finally {
    queueControlPending.value = false
  }
}
async function clearContextWithConfirmation(id) {
  if (!id || id !== activeId.value) throw new Error('请先选择一个智能体')
  if (!status.value.online || status.value.setup_required) throw new Error('本地服务未就绪，无法清空对话')
  if (turnInProgress.value) throw new Error('请等待当前回合结束后再清空对话')
  if (pending.value || approvalQueueIndex.value >= 0) throw new Error('请先处理待审批请求后再清空对话')
  if (!await confirmDesktopAction('清空当前 Agent 的对话上下文？此操作会从当前会话移除消息。')) return false
  await clearContext(id)
  setLocalPendingUsersForAgent(id, [])
  if (activeId.value === id) {
    conversationStore.resetDrafts()
    conversationStore.resetTurnState({ preserveLifecycle: true })
  }
  return true
}
async function clearCurrentContext() {
  const id = activeId.value
  try { if (await clearContextWithConfirmation(id) && activeId.value === id) await selectAgent(id) }
  catch (e) { if (activeId.value === id) setAgentError(id, String(e.message || e)) }
}
async function deleteAgentById(agentId) {
  const id = String(agentId || '').trim()
  const agent = agents.value.find(item => item.agent_id === id)
  if (!agent || !status.value.online || status.value.setup_required || deletingAgentId.value || agent.state === 'working' || (activeId.value === id && turnInProgress.value)) return
  const kind = agent.agent_type === 'auto' ? '自主智能体' : '智能体'
  const name = agent.display_name || id
  if (!await confirmDesktopAction(`删除${kind}「${name}」？\n该操作会在本地服务归档此智能体并移除其运行时。`)) return
  autoOverviewActionError.value = ''
  deletingAgentId.value = id
  try {
    await deleteAgent(id)
    if (activeId.value === id) await selectAgent('')
    await refreshAgentSummaries()
    await nextTick()
    await autoOverviewPanel.value?.refresh?.()
  } catch (e) {
    const message = String(e.message || e)
    setAgentError(id, message)
    if (dialog.value === 'auto-overview') autoOverviewActionError.value = message
  } finally { if (deletingAgentId.value === id) deletingAgentId.value = '' }
}
function openEntryContextMenu(event, { title = '', items = [], anchor = event?.currentTarget } = {}) {
  event?.preventDefault?.()
  event?.stopPropagation?.()
  if (!items.length) return
  openMenu.value = ''
  const focusAnchor = anchor instanceof HTMLElement ? anchor : null
  const rect = focusAnchor?.getBoundingClientRect?.()
  const hasPointerPosition = Number(event?.clientX) > 0 || Number(event?.clientY) > 0
  const x = hasPointerPosition ? Number(event.clientX) : (rect?.left || 0) + 12
  const y = hasPointerPosition ? Number(event.clientY) : (rect?.bottom || 0)
  entryContextMenu.value = { title, items, x, y, trigger: focusAnchor }
}
function openQueuedInputContextMenu({ event, entry, anchor = event?.currentTarget } = {}) {
  const inputSeq = Number(entry?.input_seq)
  const revision = Number(entry?.input_revision)
  const agentId = activeId.value
  if (!agentId || entry?.queue_status !== 'queued' || !Number.isSafeInteger(inputSeq) || inputSeq <= 0 || !Number.isSafeInteger(revision) || revision <= 0) return
  const mutationInProgress = Boolean(queuedInputMutationId.value)
  void openEntryContextMenu(event, {
    title: '待处理消息',
    anchor,
    items: [
      { label: '编辑待处理消息', icon: 'edit', disabled: mutationInProgress, run: () => beginQueuedInputEdit(agentId, entry) },
      { label: '撤回待处理消息', icon: 'trash', danger: true, disabled: mutationInProgress, run: () => withdrawPendingInput(agentId, entry) },
    ],
  })
}
async function beginQueuedInputEdit(agentId, entry) {
  queuedInputEdit.value = {
    agentId,
    inputId: String(entry?.input_id || entry?.item_id || `${agentId}/input/${Number(entry?.input_seq)}`),
    inputSeq: Number(entry?.input_seq),
    revision: Number(entry?.input_revision),
    status: 'queued',
    fileCount: Array.isArray(entry?.file_refs) ? entry.file_refs.length : 0,
    imageCount: Math.max(0, Number(entry?.image_count) || 0),
  }
  queuedInputEditText.value = String(entry?.text || '')
  queuedInputEditError.value = ''
  dialog.value = 'queued-input-edit'
  await nextTick()
  queuedInputEditField.value?.focus?.()
}
function closeQueuedInputEdit() {
  if (queuedInputMutationId.value) return
  dialog.value = ''
  queuedInputEdit.value = null
  queuedInputEditText.value = ''
  queuedInputEditError.value = ''
}
function pendingInputByIdentity(agentId, inputId) {
  if (activeId.value !== agentId || !inputId) return null
  return (Array.isArray(hydrate.value?.pending_inputs) ? hydrate.value.pending_inputs : [])
    .find(item => String(item?.input_id || '') === inputId) || null
}
function removeLocalPendingInput(agentId, inputId) {
  conversationStore.removePendingInput(agentId, inputId)
}
async function refreshQueuedInputState(agentId) {
  if (activeId.value === agentId) {
    await hydrateAgentSnapshot(agentId)
    await nextTick()
  }
}
async function saveQueuedInputEdit() {
  const target = queuedInputEdit.value
  if (!target || target.status !== 'queued' || queuedInputMutationId.value) return
  const identity = target.inputId || `${target.agentId}/input/${target.inputSeq}`
  queuedInputMutationId.value = identity
  queuedInputEditError.value = ''
  try {
    await editQueuedMessage(target.agentId, target.inputSeq, {
      expectedRevision: target.revision,
      content: queuedInputEditText.value,
    })
    if (dialog.value === 'queued-input-edit' && queuedInputEdit.value?.inputId === target.inputId) {
      dialog.value = ''
      queuedInputEdit.value = null
      queuedInputEditText.value = ''
      queuedInputEditError.value = ''
    }
    await refreshQueuedInputState(target.agentId)
  } catch (error) {
    await refreshQueuedInputState(target.agentId)
    const current = pendingInputByIdentity(target.agentId, target.inputId)
    if (current?.status === 'queued') {
      queuedInputEdit.value = { ...target, revision: Number(current.input_revision) || target.revision }
      if (Number(current.input_revision) !== target.revision) {
        queuedInputEditError.value = '这条消息已被更新。你的修改仍保留；再次保存会按最新版本提交。'
      } else if (Number(error?.status) === 409) {
        queuedInputEditError.value = '队列正在发生变化，请稍后再试。你的修改已保留。'
      } else {
        queuedInputEditError.value = `保存失败：${String(error?.message || error)}。你的修改已保留。`
      }
    } else {
      queuedInputEdit.value = { ...target, status: current?.status || 'unavailable' }
      queuedInputEditError.value = current?.status === 'claimed'
        ? '这条消息已经开始处理，无法再修改。你的修改仍保留在此处。'
        : '这条待处理消息已不存在。你的修改仍保留在此处。'
    }
  } finally {
    if (queuedInputMutationId.value === identity) queuedInputMutationId.value = ''
  }
}
async function copyQueuedEditToComposer() {
  if (input.value.trim() && !await confirmDesktopAction('输入栏已有未发送内容。要用这段修改替换它吗？')) return
  input.value = queuedInputEditText.value
  if (activeId.value) inputDrafts.value[activeId.value] = input.value
  closeQueuedInputEdit()
  await nextTick()
  resizeComposer()
  textarea.value?.focus?.()
}
async function withdrawPendingInput(agentId, entry) {
  const inputSeq = Number(entry?.input_seq)
  const expectedRevision = Number(entry?.input_revision)
  const inputId = String(entry?.input_id || entry?.item_id || `${agentId}/input/${inputSeq}`)
  if (!agentId || !Number.isSafeInteger(inputSeq) || inputSeq <= 0 || !Number.isSafeInteger(expectedRevision) || expectedRevision <= 0 || queuedInputMutationId.value) return
  queuedInputMutationId.value = inputId
  try {
    await withdrawQueuedMessage(agentId, inputSeq, expectedRevision)
    removeLocalPendingInput(agentId, inputId)
    await refreshQueuedInputState(agentId)
  } catch (error) {
    await refreshQueuedInputState(agentId)
    const current = pendingInputByIdentity(agentId, inputId)
    if (!current) removeLocalPendingInput(agentId, inputId)
    const message = current?.status === 'claimed'
      ? '消息已开始处理，无法撤回。'
      : Number(error?.status) === 409
        ? '待处理消息已变化，请查看刷新后的内容再操作。'
        : String(error?.message || error)
    setAgentError(agentId, message)
  } finally {
    if (queuedInputMutationId.value === inputId) queuedInputMutationId.value = ''
  }
}
function runEntryContextMenuItem(item) {
  if (!item || item.disabled) return
  void item.run?.()
}
function openAgentContextMenu(event, agent, anchor = event?.currentTarget) {
  const id = String(agent?.agent_id || '').trim()
  if (!id) return
  const focusAnchor = anchor instanceof HTMLElement && !anchor.matches('button, a, input, select, textarea, [tabindex]:not([tabindex="-1"])')
    ? anchor.querySelector('.agent-item') || anchor
    : anchor
  const kind = agent.agent_type === 'auto' ? '自主智能体' : '智能体'
  const isWorking = agent.state === 'working' || (activeId.value === id && turnInProgress.value)
  const disabledReason = !status.value.online ? '请先连接本地服务' : status.value.setup_required ? '请先完成本地服务初始化' : deletingAgentId.value ? '正在删除智能体' : isWorking ? '智能体工作中，暂不能删除' : ''
  void openEntryContextMenu(event, {
    title: agent.display_name || id,
    anchor: focusAnchor,
    items: [
      { label: '打开对话', icon: 'message-square', run: () => selectAgent(id, { openChat: true }) },
      { label: '智能体设置', icon: 'settings', run: () => openAgentSettings(id) },
      { separator: true },
      { label: `删除${kind}`, icon: 'trash', danger: true, disabled: Boolean(disabledReason), title: disabledReason || undefined, run: () => deleteAgentById(id) },
    ],
  })
}
function onAgentEntryKeydown(event, agent) {
  if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return
  event.preventDefault()
  openAgentContextMenu(event, agent)
}
function openAutoOverviewContextMenu(payload) {
  const item = payload?.item
  if (!item) return
  openAgentContextMenu(payload.event, { ...item, agent_type: 'auto' }, payload.anchor)
}
function openSidebarWorkgroupContextMenu(event, group, anchor = event?.currentTarget) {
  const id = String(group?.workgroup_id || '').trim()
  if (!id) return
  const disabledReason = !status.value.online ? '请先连接本地服务' : status.value.setup_required ? '请先完成本地服务初始化' : removingWorkgroupId.value ? '正在移除工作组' : ''
  void openEntryContextMenu(event, {
    title: group.display_name || id,
    anchor,
    items: [
      { label: '打开工作组', icon: 'workgroup', run: () => openWorkgroup(id) },
      { separator: true },
      { label: '从本地服务移除工作组', icon: 'trash', danger: true, disabled: Boolean(disabledReason), title: disabledReason || undefined, run: () => removeSidebarWorkgroup(group) },
    ],
  })
}
function openSidebarGroupContextMenu(event, groupName, anchor = event?.currentTarget) {
  const items = groupName === 'agents'
    ? [{ label: '新建智能体', icon: 'plus', run: () => { showAgentCreate.value = true } }]
    : groupName === 'auto'
      ? [{ label: '自主智能体总览', icon: 'auto', run: showAutoOverview }]
      : [{ label: '打开工作组列表', icon: 'workgroup', run: showWorkgroups }]
  void openEntryContextMenu(event, { title: groupName === 'agents' ? '智能体' : groupName === 'auto' ? '自主智能体' : '工作组', anchor, items })
}
function onSidebarGroupKeydown(event, groupName) {
  if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return
  event.preventDefault()
  openSidebarGroupContextMenu(event, groupName)
}
function onWorkgroupEntryKeydown(event, group) {
  if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return
  event.preventDefault()
  openSidebarWorkgroupContextMenu(event, group)
}
function openDelegatedContextMenu(payload) {
  if (payload?.event) void openEntryContextMenu(payload.event, payload)
}
async function removeSidebarWorkgroup(group) {
  const id = String(group?.workgroup_id || '').trim()
  if (!id || !status.value.online || status.value.setup_required || removingWorkgroupId.value) return
  const label = String(group?.display_name || id)
  if (!await confirmDesktopAction(`从本地服务移除工作组「${label}」？\n这会取消当前本地服务的订阅，管理后台中的工作组数据会保留。`)) return
  removingWorkgroupId.value = id
  resourceNavigationError.value = ''
  try {
    await unsubscribeWorkgroup(id)
    onWorkgroupRemoved(id)
  } catch (e) {
    const message = String(e?.message || e)
    if (activeId.value) setAgentError(activeId.value, message)
    else globalError.value = message
  } finally {
    if (removingWorkgroupId.value === id) removingWorkgroupId.value = ''
  }
}
async function onAgentCreated(agent) {
  showAgentCreate.value = false
  await loadAgents()
  const createdId = agent?.agent_id || agent?.id
  if (createdId && agents.value.some(item => item.agent_id === createdId)) {
    await selectAgent(createdId)
    window.dispatchEvent(new CustomEvent('desktop-agent-created', { detail: { agentId: createdId } }))
  }
}
function showAutoOverview() { autoOverviewActionError.value = ''; dialog.value = 'auto-overview' }
function showWorkgroups() { selectedWorkgroupId.value = ''; selectedWorkgroupName.value = ''; activeTab.value = 'workgroups' }
function openWorkgroup(id) { selectedWorkgroupId.value = String(id || ''); selectedWorkgroupName.value = workgroupList.value.find(group => group.workgroup_id === selectedWorkgroupId.value)?.display_name || ''; activeTab.value = 'workgroups' }
function onWorkgroupRemoved(id) {
  const removedId = String(id || '')
  workgroupList.value = workgroupList.value.filter(group => group.workgroup_id !== removedId)
  if (selectedWorkgroupId.value === removedId) { selectedWorkgroupId.value = ''; selectedWorkgroupName.value = '' }
}
function openWorkgroupConnectionSettings() { void router.push('/settings/connection') }
function openBrowserCapabilitySettings() { void router.push('/settings/capabilities') }
async function openLinuxChannelSettings() { await closeTerminalOpenDialog(); await router.push('/settings/linux-channels') }
function openAutoChat(id) { dialog.value = ''; void selectAgent(id) }
async function openAutoConfig(id) { dialog.value = ''; await selectAgent(id); await router.push(`/settings/agents/${encodeURIComponent(id)}`) }
async function openAgentSettings(id) { await selectAgent(id); await router.push(`/settings/agents/${encodeURIComponent(id)}`) }
async function sendFromDock() {
  const text = dockDraft.value
  const references = pendingWorkspaceReferences.value.slice()
  if (composerIntent.value === 'blocked') { error.value = '请返回消息卡片处理待处理交互，再发送新消息。'; return }
  if (messageSubmitPending.value) { error.value = '上一条消息仍在确认发送结果，请稍后再发送。'; return }
  if (text.trim().startsWith('/') && activeId.value && !references.length) {
    if (composerIntent.value === 'queue') { error.value = '正在执行任务，应用命令不能加入等待队列。'; return }
    dockDraft.value = ''
    dockDrafts.value[activeId.value] = ''
    await executeSlashCommand(text.trim(), activeId.value)
    return
  }
  clearCommandNotice()
  if ((!text.trim() && !references.length) || !activeId.value) return
  const queueDuringTurn = composerIntent.value === 'queue'
  const agentId = activeId.value
  dockDraft.value = ''
  dockDrafts.value[agentId] = ''
  const sent = await sendMessageToActiveAgent(text, [], [], references, { allowDuringTurn: queueDuringTurn })
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
  if (!status.value.online) { resourceNavigationError.value = '本地服务尚未连接，无法打开此资源。'; return }
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
  else if (transition.historyOpened === false) { historyOpened.value = false; showChat() }
}
function showResourceTab(tab) { activeTab.value = tab; void router.push(routeForTab('chat')) }
function showConversationHistory() {
  if (!activeId.value || isWorkgroupView.value || !status.value.online) return
  historyOpened.value = true
  showResourceTab('history')
}
function onChatReadingWindowChange(state = {}) {
  const agentId = activeId.value
  const currentEpoch = Number(hydrate.value?.conversation_epoch) || 0
  conversationStore.applyReadingWindowChange(agentId, activeHydrationSequence, state, currentEpoch)
}
async function onConversationResubmitted() {
  const agentId = activeId.value
  historyOpened.value = false
  showChat()
  if (agentId && activeId.value === agentId) {
    try { await hydrateAgentSnapshot(agentId) }
    catch (cause) { if (activeId.value === agentId) setAgentError(agentId, String(cause?.message || '重新发送已接收，正在等待同步对话。')) }
  }
}
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
function approveSelection({ index, approvedCallIds }) {
  const item = hitlQueue.value[index]
  if (item?.kind === 'approval') void resumeHitl(index, buildApprovalSelectionResume(item.data, approvedCallIds))
}
function rejectAll(index) { approveSelection({ index, approvedCallIds: [] }) }
async function submitUserInfo(index = hitlQueue.value.findIndex(item => item.kind === 'user_information'), submittedText = '') {
  const item = hitlQueue.value[index]
  if (!item) return false
  const result = buildUserInfoSubmitResume(item.data, { text: submittedText, selected: userInfoSelected.value })
  if (!result.ok) { error.value = result.error; return true }
  const resumed = await resumeHitl(index, result.resume)
  if (resumed) userInfoSelected.value = []
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
function topMenuTriggers() {
  return [...document.querySelectorAll('[data-menubar-trigger]')]
    .sort((left, right) => desktopMenuNames.indexOf(left.dataset.menubarTrigger) - desktopMenuNames.indexOf(right.dataset.menubarTrigger))
}
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
  const menu = event.target.closest?.('.menu-anchor [role="menu"]')
  const visibleTriggers = topMenuTriggers()
  if (trigger) {
    const name = trigger.dataset.menubarTrigger
    const index = visibleTriggers.indexOf(trigger)
    if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
      event.preventDefault()
      focusTopMenu(index + (event.key === 'ArrowRight' ? 1 : -1))
      return true
    }
    if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      focusTopMenu(event.key === 'Home' ? 0 : visibleTriggers.length - 1, Boolean(openMenu.value))
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
      focusTopMenu(visibleTriggers.findIndex(item => item.dataset.menubarTrigger === name) + (event.key === 'ArrowRight' ? 1 : -1))
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
  else if (action === 'workgroups-settings') void router.push('/settings/connection')
  else if (action === 'quit-desktop' && isTauri) void invoke('desktop_quit')
  else if (action === 'sidebar') { sidebarCollapsed.value = !sidebarCollapsed.value; collapsedGroup.value = '' }
  else if (action === 'approval') void jumpToApproval()
  else if (action === 'agent-chat') showChat()
  else if (action === 'create-agent') showAgentCreate.value = true
  else if (action === 'current-agent-settings' && activeId.value) void router.push(`/settings/agents/${encodeURIComponent(activeId.value)}`)
  else if (action === 'compress-context' && activeId.value) void executeSlashCommand('/compress', activeId.value)
  else if (action === 'stop-turn') void stopCurrentTurn()
  else if (action === 'clear-context') void clearCurrentContext()
  else if (action === 'auto-overview') void showAutoOverview()
  else if (action === 'open-history') showConversationHistory()
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
function onDocumentClick(event) {
  if (!event.target.closest('.menu-anchor[data-menu-anchor]')) openMenu.value = ''
  if (attachmentActionsOpen.value && !event.target.closest('.attachment-action')) closeAttachmentActions()
}
function onKeydown(event) {
  if (screenshotSelection.value && event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    closeScreenshotSelection()
    return
  }
  if (attachmentActionsOpen.value && event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    closeAttachmentActions(true)
    return
  }
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
function onDesktopOpenAgentCreate() { showAgentCreate.value = true }
function showSettings() { void router.push('/settings/connection') }
async function onFirstRunCompleted() {
  await loadAgents()
  if (!status.value.setup_required) showChat()
}
function showChat() { activeTab.value = 'chat'; void router.push(routeForTab('chat')) }
onMounted(async () => {
  installPerformanceDiagnosticsGlobal()
  if (window.__TAURI_INTERNALS__) {
    try {
      nativeWebviewGeneration = await invoke('desktop_event_scope_generation')
    } catch (cause) {
      error.value = `无法同步桌面窗口状态：${String(cause?.message || cause)}`
      finishStartup()
      return
    }
  }
  unlistenConfigurationChanged = onConfigurationChanged(({ kind }) => {
    if (kind === 'llm' && activeId.value) void refreshAgentModel(activeId.value)
    void refreshWorkgroupSettings()
  })
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('keydown', onKeydown)
  document.addEventListener('visibilitychange', onDocumentVisibilityChange)
  window.addEventListener('resize', resizeComposer)
  void nextTick(resizeComposer)
  void nextTick(observeComposerField)
  window.addEventListener('desktop-back-to-chat', onDesktopBackToChat)
  window.addEventListener('desktop-open-agent', onDesktopOpenAgent)
  window.addEventListener('desktop-open-settings', onDesktopOpenSettings)
  window.addEventListener('desktop-open-agent-create', onDesktopOpenAgentCreate)
  if (window.__TAURI_INTERNALS__) {
    unlistenWorkspaceWrite = await listen('workspace-write-progress', event => {
      const progress = event.payload
      if (!progress || progress.requestId !== composerWriteRequest.value || composerWriteOwner.value !== activeId.value) return
      composerWriteProgress.value = { bytes: Number(progress.bytes) || 0, total: Number(progress.total) || 0, phase: progress.phase || '' }
    })
  }
  if (currentWindow) {
    void syncWindowMaximized()
    void syncResourceWindowState()
    try { unlistenWindowResize = await currentWindow.onResized(() => { void syncWindowMaximized(); void syncResourceWindowState() }) } catch { /* Window resize events may be unavailable in preview mode. */ }
    try { unlistenWindowFocus = await currentWindow.onFocusChanged(event => {
      desktopWindowFocused = Boolean(event?.payload)
      void syncResourceWindowState()
      scheduleAgentWatermarkPoll(desktopWindowFocused ? 0 : 60000)
    }) } catch { /* Preview builds may not expose focus events. */ }
  }
  if (!window.__TAURI_INTERNALS__) {
    status.value = { online: false, setup_required: false, version: '', error: '浏览器预览模式：请在桌面应用窗口中连接本地服务。' }
    error.value = '预览模式仅展示界面；连接状态、智能体与消息仅从桌面应用获取。'
    finishStartup()
    return
  }
  waitForNodeStartup()
  await loadAgents()
  agentWatermarkPollingEnabled = true
  scheduleAgentWatermarkPoll(0)
  if (status.value.online) finishStartup()
  if (pendingDesktopAgentId && agents.value.some(agent => agent.agent_id === pendingDesktopAgentId)) {
    const agentId = pendingDesktopAgentId
    pendingDesktopAgentId = ''
    await selectAgent(agentId, { openChat: true })
  }
  previouslyOnline.value = status.value.online
  await conversationController.subscribe(
    handler => listen('desktop-node-event', event => handler(event.payload)),
    event => {
      // Count the event at Desktop ingress, before hydration may cover it or
      // the ordered reducer may intentionally defer its application.
      recordSSEEvent(event?.event_type || 'unknown', event?.agent_seq, event?.agent_id || 'global')
      if (conversationController.bufferEvent(event)) return
      applyOrderedDesktopNodeEvent(event)
    },
  )
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
      instance.output += '\r\n[本地服务提示：部分历史输出已过期]\r\n'
      if (activeTerminalInstanceId.value === instance.id) terminalOutput.value = instance.output
    } else if (event.type === 'error') {
      instance.error = String(event.error || '本地服务终端错误')
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
    if (!previouslyOnline.value && current.online) {
      await loadAgents()
      scheduleAgentWatermarkPoll(0)
      if (status.value.online) finishStartup()
    }
    previouslyOnline.value = current.online
  }, 2500)
  try {
    // Rust buffers SSE until both this JS listener and the Node stream are
    // ready, then requests one authoritative snapshot to cover startup gaps.
    await invoke('desktop_events_ready')
  } catch (e) {
    console.warn('Desktop event bridge readiness failed:', e)
  }
  turnWatchdog.start()
})
onUnmounted(() => {
  agentWatermarkPollingEnabled = false
  if (composerWriteRequest.value) void Promise.resolve(cancelWorkspaceWrite(composerWriteRequest.value)).catch(() => {})
  if (startupTimeoutTimer) window.clearTimeout(startupTimeoutTimer)
  closeScreenshotSelection()
  clearCommandNotice()
  window.removeEventListener('resize', resizeComposer)
  composerResizeObserver?.disconnect()
  window.removeEventListener('desktop-back-to-chat', onDesktopBackToChat)
  window.removeEventListener('desktop-open-agent', onDesktopOpenAgent)
  window.removeEventListener('desktop-open-settings', onDesktopOpenSettings)
  window.removeEventListener('desktop-open-agent-create', onDesktopOpenAgentCreate)
  document.removeEventListener('visibilitychange', onDocumentVisibilityChange)
  unlistenConfigurationChanged?.()
  unlistenWindowResize?.(); unlistenWindowFocus?.()
  for (const terminal of terminalInstances.value.filter(item => item.connectionId)) {
    void invoke('desktop_terminal_command', { connectionId: terminal.connectionId, agentId: terminal.agentId, commandType: 'detach' }).catch(() => {})
  }
  conversationController.dispose()
  unlistenWorkspaceWrite?.(); unlistenTerminal?.(); clearInterval(pollTimer); clearTimeout(agentWatermarkTimer); clearTimeout(summaryRefreshTimer); clearTimeout(resourceRefreshTimer); clearTimeout(activeHydrateNoticeTimer); document.removeEventListener('click', onDocumentClick); document.removeEventListener('keydown', onKeydown)
  turnWatchdog.stop()
  mediaUrlRegistry.dispose()
})
</script>

<template>
  <section v-if="isTauri && !startupReady" class="desktop-startup-screen" aria-live="polite">
    <header class="desktop-startup-topbar" data-tauri-drag-region>
      <strong data-tauri-drag-region>DAgents</strong>
      <div class="window-controls">
        <button type="button" aria-label="最小化" title="最小化" @click="minimizeWindow"><span class="window-control-icon minimize" aria-hidden="true"></span></button>
        <button type="button" :aria-label="windowMaximized ? '还原' : '最大化'" :title="windowMaximized ? '还原' : '最大化'" @click="toggleMaximizeWindow"><span class="window-control-icon" :class="windowMaximized ? 'restore' : 'maximize'" aria-hidden="true"></span></button>
        <button type="button" aria-label="关闭到托盘" title="关闭到托盘" @click="closeWindow"><span class="window-control-icon close" aria-hidden="true"></span></button>
      </div>
    </header>
    <div class="desktop-startup-card">
      <span class="desktop-startup-mark" aria-hidden="true"><img :src="brandIconUrl" alt=""></span>
      <p class="desktop-startup-brand">DAgents Desktop</p>
      <h1>{{ startupTimedOut ? '仍在等待本地服务' : '正在启动 DAgents' }}</h1>
      <p class="desktop-startup-description">{{ startupTimedOut ? (status.online ? '本地服务已连接，但工作区还没有完成加载。请重试，或离线打开桌面应用设置。' : status.error || '本地服务尚未就绪。你可以重试连接，或先离线打开桌面应用设置。') : '正在启动并连接本地服务，准备好后会自动打开工作区。' }}</p>
      <div v-if="startupTimedOut" class="desktop-startup-actions">
        <button class="desktop-startup-retry" type="button" @click="retryNodeStartup">重试连接</button>
        <button class="desktop-startup-continue" type="button" @click="continueOffline">离线打开</button>
      </div>
      <div v-else class="desktop-startup-progress"><span></span>连接本地服务</div>
    </div>
  </section>
  <div v-else class="desktop-shell" :class="{ 'settings-mode': activeTab === 'settings' }">
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
          <div v-for="name in desktopMenuNames.slice(1).filter(item => item !== '资源')" :key="name" class="menu-anchor" :data-menu-anchor="name" @mouseenter="setTopMenuFromHover(name)">
            <button class="menu-trigger" :class="{ active: openMenu === name }" type="button" role="menuitem" :data-menubar-trigger="name" aria-haspopup="menu" :aria-expanded="openMenu === name" @click.stop="toggleTopMenu(name)">{{ name }}</button>
            <div v-if="openMenu === name" class="menu-popover" role="menu" :aria-label="`${name}菜单`">
              <template v-for="(item, i) in menuEntries[name]" :key="i"><div v-if="item.separator" class="menu-separator" role="separator"></div><div v-else-if="item.header" class="menu-section-label" role="presentation">{{ item.label }}</div><button v-else class="menu-item" type="button" role="menuitem" :disabled="item.disabled" tabindex="-1" @click="runMenuAction(item.action)"><span>{{ item.label }}</span><small v-if="item.hint">{{ item.hint }}</small></button></template>
            </div>
          </div>
        </nav>
        <span class="workspace-title"><strong>{{ activeAgent?.display_name || 'DAgents Desktop' }}</strong><span class="crumb"> / {{ activeTab === 'chat' ? '对话' : activeTab === 'settings' ? '设置' : activeTab === 'file' ? workspaceName : activeTab === 'workgroups' ? '工作组' : activeTab === 'browser' ? browserName : activeTab === 'history' ? '对话历史' : '终端' }}</span></span>
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
          <button v-if="sidebarCollapsed" class="collapsed-group" :class="{ selected: !isWorkgroupView && Boolean(activeId) && activeAgent?.agent_type !== 'auto' }" aria-label="智能体" title="智能体" :aria-expanded="collapsedGroup === 'agents'" @click="collapsedGroup = collapsedGroup === 'agents' ? '' : 'agents'" @contextmenu="openSidebarGroupContextMenu($event, 'agents')" @keydown="onSidebarGroupKeydown($event, 'agents')"><span class="group-icon" :style="maskStyle(navAgentsIcon)"></span></button>
          <template v-else>
            <div class="group-title" tabindex="0" aria-label="智能体，右键查看操作" title="右键查看操作" @contextmenu="openSidebarGroupContextMenu($event, 'agents')" @keydown="onSidebarGroupKeydown($event, 'agents')"><span class="group-icon" :style="maskStyle(navAgentsIcon)"></span><span>智能体</span></div>
            <div v-for="agent in normalAgents" :key="agent.agent_id" class="agent-row" @contextmenu="openAgentContextMenu($event, agent)">
              <button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })" @keydown="onAgentEntryKeydown($event, agent)"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button>
            </div>
          </template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'agents'" class="sidebar-popover"><div class="sidebar-popover-heading" tabindex="0" aria-label="智能体，右键查看操作" title="右键查看操作" @contextmenu="openSidebarGroupContextMenu($event, 'agents')" @keydown="onSidebarGroupKeydown($event, 'agents')"><b>智能体</b></div><div v-for="agent in normalAgents" :key="agent.agent_id" class="agent-row" @contextmenu="openAgentContextMenu($event, agent)"><button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })" @keydown="onAgentEntryKeydown($event, agent)"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button></div></div>
        </section>
        <section class="side-section">
          <button v-if="sidebarCollapsed && showWorkgroupSidebar" class="collapsed-group" :class="{ selected: activeTab === 'workgroups' }" aria-label="工作组" title="工作组" :aria-expanded="collapsedGroup === 'workgroups'" @click="collapsedGroup = collapsedGroup === 'workgroups' ? '' : 'workgroups'" @contextmenu="openSidebarGroupContextMenu($event, 'workgroups')" @keydown="onSidebarGroupKeydown($event, 'workgroups')"><span class="group-icon" :style="maskStyle(navWorkgroupsIcon)"></span></button>
          <template v-if="!sidebarCollapsed && showWorkgroupSidebar"><button class="group-title group-title-button" :class="{ selected: isWorkgroupView }" :aria-current="isWorkgroupView ? 'page' : undefined" @click="showWorkgroups" @contextmenu="openSidebarGroupContextMenu($event, 'workgroups')" @keydown="onSidebarGroupKeydown($event, 'workgroups')"><span class="group-icon" :style="maskStyle(navWorkgroupsIcon)"></span><span>工作组</span></button><button v-for="group in workgroupList" :key="group.workgroup_id" class="agent-item" :class="{ selected: activeTab === 'workgroups' && selectedWorkgroupId === group.workgroup_id }" :title="group.display_name || group.workgroup_id" @click="openWorkgroup(group.workgroup_id)" @contextmenu="openSidebarWorkgroupContextMenu($event, group)" @keydown="onWorkgroupEntryKeydown($event, group)"><span class="agent-label">{{ group.display_name || group.workgroup_id }}</span></button></template>
          <div v-if="sidebarCollapsed && showWorkgroupSidebar && collapsedGroup === 'workgroups'" class="sidebar-popover"><button class="sidebar-popover-title" type="button" @click="showWorkgroups" @contextmenu="openSidebarGroupContextMenu($event, 'workgroups')" @keydown="onSidebarGroupKeydown($event, 'workgroups')">工作组</button><button v-for="group in workgroupList" :key="group.workgroup_id" :class="{ selected: activeTab === 'workgroups' && selectedWorkgroupId === group.workgroup_id }" @click="openWorkgroup(group.workgroup_id)" @contextmenu="openSidebarWorkgroupContextMenu($event, group)" @keydown="onWorkgroupEntryKeydown($event, group)"><span class="agent-label">{{ group.display_name || group.workgroup_id }}</span></button></div>
        </section>
        <section class="side-section">
          <button v-if="sidebarCollapsed" class="collapsed-group" :class="{ selected: !isWorkgroupView && Boolean(activeId) && activeAgent?.agent_type === 'auto' }" aria-label="自主智能体" title="自主智能体" :aria-expanded="collapsedGroup === 'auto'" @click="collapsedGroup = collapsedGroup === 'auto' ? '' : 'auto'" @contextmenu="openSidebarGroupContextMenu($event, 'auto')" @keydown="onSidebarGroupKeydown($event, 'auto')"><span class="group-icon" :style="maskStyle(navAutoIcon)"></span></button>
          <template v-else>
            <div class="group-title" tabindex="0" aria-label="自主智能体，右键查看操作" title="右键查看操作" @contextmenu="openSidebarGroupContextMenu($event, 'auto')" @keydown="onSidebarGroupKeydown($event, 'auto')"><span class="group-icon" :style="maskStyle(navAutoIcon)"></span><span>自主智能体</span></div>
            <div v-for="agent in autoAgents" :key="agent.agent_id" class="agent-row" @contextmenu="openAgentContextMenu($event, agent)">
              <button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })" @keydown="onAgentEntryKeydown($event, agent)"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button>
            </div>
          </template>
          <div v-if="sidebarCollapsed && collapsedGroup === 'auto'" class="sidebar-popover"><b tabindex="0" aria-label="自主智能体，右键查看操作" title="右键查看操作" @contextmenu="openSidebarGroupContextMenu($event, 'auto')" @keydown="onSidebarGroupKeydown($event, 'auto')">自主智能体</b><div v-for="agent in autoAgents" :key="agent.agent_id" class="agent-row" @contextmenu="openAgentContextMenu($event, agent)"><button class="agent-item" :class="{ selected: !isWorkgroupView && activeId === agent.agent_id }" :aria-current="!isWorkgroupView && activeId === agent.agent_id ? 'page' : undefined" :title="agent.display_name || agent.agent_id" @click="selectAgent(agent.agent_id, { openChat: true })" @keydown="onAgentEntryKeydown($event, agent)"><span class="agent-label">{{ agent.display_name || agent.agent_id }}</span><span v-if="agent.has_pending_hitl" class="pending-dot"></span></button></div></div>
        </section>
      </div>
      <div class="node-indicator" :class="status.online ? 'online' : 'offline'" :title="`本地服务 · ${status.online ? '在线' : '离线'}${status.version ? ' · v' + status.version : ''}`" :aria-label="`本地服务 · ${status.online ? '在线' : '离线'}${status.version ? ' · v' + status.version : ''}`"><span>{{ `本地服务 · ${status.online ? '在线' : '离线'}${status.version ? ' · v' + status.version : ''}` }}</span><i></i></div>
    </aside>
    <main class="main-panel">
      <FirstRunNodeProfile v-if="showNodeFirstRun && activeTab !== 'settings'" class="desktop-first-run" @completed="onFirstRunCompleted" />
      <RouterView v-else-if="activeTab === 'settings'" />
      <div v-else ref="composerWorkspace" class="desktop-chat-workspace">
      <div class="resource-tabs">
        <button v-if="activeTab !== 'workgroups'" class="conversation-tab" :class="{ active: activeTab === 'chat' }" :title="activeAgent?.display_name || '当前智能体'" @click="showChat"><span class="icon-mask tab-icon" :style="maskStyle(activeAgent?.agent_type === 'auto' ? autoAgentIcon : normalAgentIcon)"></span><b>{{ activeAgent?.display_name || '当前智能体' }}</b></button>
        <div v-else class="conversation-tab active workgroup-conversation-tab" :title="activeWorkgroupName"><span class="icon-mask tab-icon" :style="maskStyle(navWorkgroupsIcon)"></span><b>{{ activeWorkgroupName }}</b></div>
        <div v-if="activeTab !== 'workgroups'" class="menu-anchor resource-menu-anchor" data-menu-anchor="资源" @mouseenter="setTopMenuFromHover('资源')">
          <button class="resource-menu-trigger" :class="{ active: openMenu === '资源' }" type="button" role="menuitem" data-menubar-trigger="资源" aria-label="资源" title="资源" aria-haspopup="menu" :aria-expanded="openMenu === '资源'" @click.stop="toggleTopMenu('资源')"><UiIcon name="folder-open" :size="16" /></button>
          <div v-if="openMenu === '资源'" class="menu-popover" role="menu" aria-label="资源菜单">
            <template v-for="(item, i) in menuEntries.资源" :key="i"><div v-if="item.separator" class="menu-separator" role="separator"></div><button v-else class="menu-item" type="button" role="menuitem" :disabled="item.disabled" tabindex="-1" @click="runMenuAction(item.action)"><span>{{ item.label }}</span><small v-if="item.hint">{{ item.hint }}</small></button></template>
          </div>
        </div>
        <button v-if="activeTab !== 'workgroups' && historyOpened" class="conversation-tab conversation-history-tab" :class="{ active: activeTab === 'history' }" type="button" aria-label="对话历史" title="对话历史" @click="showResourceTab('history')"><UiIcon name="clock" :size="15" /><b>对话历史</b></button>
        <div v-if="activeTab !== 'workgroups'" class="open-resource-tabs"><div v-if="workspaceOpened" class="open-resource-tab" :class="{ active: activeTab === 'file' }"><button class="resource-tab-main" :title="workspaceName" @click="showWorkspace"><span class="icon-mask tab-icon" :style="maskStyle(folderOpenIcon)"></span><span class="resource-tab-label">{{ workspaceName }}</span></button><button class="tab-close" aria-label="关闭工作区" title="关闭工作区" @click="workspaceOpened = false; showChat()"><UiIcon name="close" :size="14" /></button></div><div v-if="browserOpened" class="open-resource-tab" :class="{ active: activeTab === 'browser' }"><button class="resource-tab-main" :title="browserName" @click="showBrowser"><span class="icon-mask tab-icon" :style="maskStyle(browserIcon)"></span><span class="resource-tab-label">{{ browserName }}</span></button><button class="tab-close" aria-label="关闭浏览器" title="关闭浏览器" @click="browserOpened = false; showChat()"><UiIcon name="close" :size="14" /></button></div><div v-for="terminal in terminalInstances.filter(item => item.agentId === activeId)" :key="terminal.id" class="open-resource-tab" :class="{ active: activeTab === 'terminal' && activeTerminalInstanceId === terminal.id }"><button class="resource-tab-main" :title="terminal.name" @click="activateTerminal(terminal.id)"><span class="icon-mask tab-icon" :style="maskStyle(terminalIcon)"></span><span class="resource-tab-label">{{ terminal.name }}</span></button><button class="tab-close" :aria-label="`关闭 ${terminal.name}`" :title="`关闭 ${terminal.name}`" @click="closeTerminal('view-close', terminal.id)"><UiIcon name="close" :size="14" /></button></div></div>
      </div>
      <div v-if="status.setup_required" class="notice setup"><span>本地服务需要首次配置</span><button @click="showSettings">打开设置</button></div>
      <div v-if="error && status.online && !status.setup_required && displayedTab !== 'chat'" class="desktop-error-hint desktop-error-hint--visible desktop-error-hint--resource" role="alert">
        <UiIcon name="alert" :size="14" aria-hidden="true" />
        <span>操作未完成</span>
        <span class="desktop-error-hint__message">{{ error }}</span>
      </div>
      <div v-if="resourceNavigationError" class="notice resource-navigation-notice" role="alert"><span>{{ resourceNavigationError }}</span><button type="button" aria-label="关闭资源提示" @click="resourceNavigationError = ''">关闭</button></div>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('chat')" @after-leave="onComposerPageAfterLeave('chat', $event)" @after-enter="onComposerPageAfterEnter('chat', $event)">
      <section v-if="visitedTabs.has('chat')" v-show="composerPageVisible('chat')" data-composer-page="chat" class="conversation" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)">
        <div v-if="!status.online" class="empty-state"><div class="empty-icon"><i></i></div><h2>等待本地服务</h2><p>{{ status.error || '启动 DAgents 本地服务后，桌面应用会自动连接。' }}</p></div>
        <div v-else-if="status.setup_required" class="empty-state"><div class="empty-icon"><i></i></div><h2>完成本地服务首次配置</h2><p>设置本地服务名称与模型后即可开始使用。</p><button class="quiet" @click="showSettings">打开设置</button></div>
        <div v-else-if="!activeAgent" class="empty-state"><h2>选择一个 Agent</h2><p>从左侧列表选择 Agent 开始对话。</p></div>
        <template v-else>
          <div v-if="!chatEntries.length && !hitlQueue.length && !turnError" class="welcome"><div class="welcome-mark"><span class="icon-mask" :style="maskStyle(activeAgent?.agent_type === 'auto' ? autoAgentIcon : normalAgentIcon)"></span></div><h1>{{ activeAgent.display_name }}</h1><p>输入任务开始；也可以从资源菜单打开工作区、浏览器或终端。</p></div>
          <div v-else class="desktop-chat-wrapper app__body--chat-v61">
            <MainChatPanel ref="chatPanel" :key="activeId" :agent-id="activeId" :entries="chatEntries" :entries-revision="chatItemsRevision" :entries-change="chatEntriesChange" :reading-window="activeReadingWindow" :turn-error="turnError" :has-earlier-history="Boolean(hydrate?.has_earlier_history)" :model-recovery-available="modelRecoveryAvailable" :model-name="llmSettings?.model || ''" :model-profile="llmSettings?.active_profile || agentActiveProfile(activeAgent)" :hitl-queue="hitlQueue" :turn-state="conversationStore.turnState" :agent-title="activeAgent.display_name" :show-workspace-switcher="false" :sending="busy || agentGenerating" :hitl-busy="busy" :hitl-busy-index="hitlBusyIndex" @approve-selection="approveSelection" @reject-all="rejectAll" @open-history="showConversationHistory" @reading-window-change="onChatReadingWindowChange" @open-model-picker="openModelPicker" @open-model-settings="openModelSettings(llmSettings?.active_profile || agentActiveProfile(activeAgent))" @user-info-submit="submitUserInfo" @user-info-selected="userInfoSelected = $event" @memory-conflict-decide="decideMemoryConflict" @memory-conflict-cancel="cancelMemoryConflict" @queued-input-context-menu="openQueuedInputContextMenu" />
          </div>
        </template>
        <div v-if="error && status.online && !status.setup_required" class="desktop-error-hint desktop-error-hint--visible" role="alert">
          <UiIcon name="alert" :size="14" aria-hidden="true" />
          <span>操作未完成</span>
          <button v-if="canRetryHydrateSync" class="desktop-error-hint__retry" type="button" aria-label="重试同步结果" title="重试同步结果" @click.stop="retryHydrateSync"><UiIcon name="refresh-cw" :size="13" /></button>
          <button v-if="isTauri && !activeAgent" class="desktop-error-hint__retry" type="button" aria-label="重试连接" title="重试" @click.stop="loadAgents"><UiIcon name="refresh-cw" :size="13" /></button>
          <span class="desktop-error-hint__message">{{ error }}</span>
        </div>
      </section>
      </Transition>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('terminal')" @after-leave="onComposerPageAfterLeave('terminal', $event)" @after-enter="onComposerPageAfterEnter('terminal', $event)">
      <section v-if="visitedTabs.has('terminal')" v-show="composerPageVisible('terminal')" data-composer-page="terminal" class="terminal-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)">
        <div class="terminal-resource-head"><div><strong>{{ terminalResourceName }}</strong><span class="terminal-status" :class="`state-${terminalStatus}`"><i></i>{{ ({idle:'未连接',connecting:'连接中',reconnecting:'恢复会话中',connected:'已连接',disconnected:'连接已断开',error:'连接错误',exited:'进程已退出',closed:'已关闭'})[terminalStatus] || terminalStatus }}</span><span v-if="terminalInstances.find(item => item.id === activeTerminalInstanceId)?.sessionId && !terminalInstances.find(item => item.id === activeTerminalInstanceId)?.manualInputEnabled" class="terminal-readonly" title="恢复的终端当前为只读查看">只读</span></div><div class="terminal-actions"><button v-if="terminalConnectionId" class="icon-only" type="button" title="断开连接（保留终端进程）" aria-label="断开终端连接，保留终端进程" @click="closeTerminal('disconnect')"><UiIcon name="unplug" :size="16" /></button><button v-else-if="['disconnected','error','closed'].includes(terminalStatus) && terminalInstances.find(item => item.id === activeTerminalInstanceId)?.sessionId" class="icon-only" type="button" title="重新连接" aria-label="重新连接终端" @click="reconnectActiveTerminal"><UiIcon name="refresh-cw" :size="16" /></button></div></div>
        <div class="terminal-output" :class="{ 'terminal-output-empty': !terminalConnectionId }"><div v-if="!terminalConnectionId" class="terminal-empty-state"><span class="icon-mask" :style="{ ...maskStyle(terminalIcon), width: '24px', height: '24px' }" aria-hidden="true"></span><h2>终端尚未连接</h2><p>打开终端选择器以新建或恢复当前 Agent 的运行中会话。</p><div><button class="icon-only" type="button" title="打开终端" aria-label="打开终端选择器" @click="requestNewTerminal()"><UiIcon name="terminal" :size="17" /></button></div></div><TerminalConsole v-if="terminalConnectionId" :key="terminalConnectionId" :output="terminalOutput" :connected="terminalStatus === 'connected'" :read-only="!terminalInstances.find(item => item.id === activeTerminalInstanceId)?.manualInputEnabled" @input="sendTerminalInput" @resize="resizeTerminal" /><p v-if="terminalError" class="terminal-error">{{ terminalError }}</p></div>
      </section>
      </Transition>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('file')" @after-leave="onComposerPageAfterLeave('file', $event)" @after-enter="onComposerPageAfterEnter('file', $event)">
      <section v-if="visitedTabs.has('file') && (displayedTab === 'file' || workspaceOpened)" v-show="composerPageVisible('file')" data-composer-page="file" class="file-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)"><WorkspaceFiles ref="workspaceFilesPanel" :key="activeId" :agent-id="activeId" :workspace-scope="workspaceScope" :workspace-location="activeAgent?.workspace?.path || activeAgent?.workspace_path || ''" :request-generation="workspaceRequestGeneration" :active="resourcePageActive('file')" :dock-collapsed="dockCollapsed" :dock-occupied-height="dockOccupiedHeight" :dock-status="dockStatus" :dock-agent-name="activeAgent?.display_name || '当前智能体'" :attachment-limit-reached="attachmentLimitReached" @resource-name="workspaceName = $event" @annotation-update="handleWorkspaceReference" @annotation-remove="onWorkspaceAnnotationRemove" @expand-agent-dock="dockCollapsed = false" /></section>
      </Transition>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('browser')" @after-leave="onComposerPageAfterLeave('browser', $event)" @after-enter="onComposerPageAfterEnter('browser', $event)">
      <section v-if="visitedTabs.has('browser') && (displayedTab === 'browser' || browserOpened)" v-show="composerPageVisible('browser')" data-composer-page="browser" class="file-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)"><BrowserViewer ref="browserViewerPanel" :key="activeId" :agent-id="activeId" :workspace-scope="workspaceScope" :request-generation="workspaceRequestGeneration" :active="resourcePageActive('browser')" @resource-name="browserName = $event" @open-connection-settings="openBrowserCapabilitySettings" /></section>
      </Transition>
      <Transition name="composer-page" :css="composerPageTransitionEnabled('history')" @after-leave="onComposerPageAfterLeave('history', $event)" @after-enter="onComposerPageAfterEnter('history', $event)">
      <section v-if="visitedTabs.has('history') && (displayedTab === 'history' || historyOpened)" v-show="composerPageVisible('history')" data-composer-page="history" class="file-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)"><ConversationHistoryPanel v-if="activeId && !isWorkgroupView" :key="activeId" :agent-id="activeId" :agent-name="activeAgent?.display_name || ''" @resubmitted="onConversationResubmitted" /></section>
      </Transition>
      <section v-if="displayedTab === 'workgroups'" class="file-resource workgroup-resource" :inert="Boolean(pendingComposerPage)" :aria-hidden="Boolean(pendingComposerPage)"><button v-if="selectedWorkgroupId" class="workgroup-back" @click="selectedWorkgroupId = ''; selectedWorkgroupName = ''">返回工作组列表</button><WorkgroupWorkspace :workgroup-id="selectedWorkgroupId" @select="openWorkgroup" @list="workgroupList = $event" @name="selectedWorkgroupName = $event" @removed="onWorkgroupRemoved" @context-menu="openDelegatedContextMenu" @open-connection-settings="openWorkgroupConnectionSettings" /></section>
      <ComposerSurface :mode="composerMode" @transition-start="onComposerTransitionStart" @transition-settled="onComposerTransitionSettled">
      <template #chat>
      <footer class="composer-wrap"><div v-if="composerWriteRequest" class="desktop-composer-progress-pill" role="status" aria-live="polite">{{ composerWriteProgress?.phase === 'committing' ? '正在提交，取消后结果可能未知。' : composerWriteProgress?.total ? `导入进度 ${Math.min(100, Math.floor(composerWriteProgress.bytes * 100 / composerWriteProgress.total))}%` : '正在准备导入…' }}<button type="button" @click="cancelComposerImport">请求取消</button></div><div v-if="pendingImages.length || pendingFiles.length || pendingWorkspaceReferences.length" class="composer-attachments" role="group" aria-label="待发送附件和引用"><button v-for="(image, index) in pendingImages" :key="image.name + index" class="composer-attachment" type="button" :title="image.name" @click="previewImage(image)"><img :src="image.url" :alt="image.name"><span>{{ image.name }}</span><i @click.stop="removeImage(index)">×</i></button><button v-for="(file,index) in pendingFiles" :key="file.path" class="composer-attachment file-attachment" type="button" :title="file.path"><span class="icon-mask tab-icon" :style="maskStyle(fileIcon)"></span><span>{{ file.name }}</span><i @click.stop="pendingFilesByAgent[activeId].splice(index,1)">×</i></button><WorkspaceReferenceAttachments :references="pendingWorkspaceReferences" @remove="removeWorkspaceReferenceById" /></div><form class="composer" @submit.prevent="send" @dragover.prevent @drop.prevent="handleAttachmentDrop">
  <input ref="imageAttachmentInput" class="visually-hidden" type="file" accept="image/jpeg,image/png,image/gif,image/webp" multiple @change="addImages">
      <textarea ref="textarea" v-model="input" class="composer-input" :disabled="!activeId || !status.online || status.setup_required" placeholder="发送消息…" :title="composerIntent === 'queue' ? 'Enter 将消息加入等待队列；Shift+Enter 换行' : 'Enter 发送，Shift+Enter 换行；可拖放或粘贴附件'" @input="resizeComposer" @paste="handleAttachmentPaste" @keydown.enter.exact.prevent="send"></textarea>
  <div class="composer-toolbar-row">
    <div class="composer-toolbar-left">
      <div ref="attachmentActionAnchor" class="attachment-action">
        <button class="attach-button" type="button" aria-label="添加附件操作" title="添加附件操作" :aria-expanded="attachmentActionsOpen" aria-controls="attachment-action-options" :disabled="attachmentLimitReached" @click.stop="toggleAttachmentActions"><span class="icon-mask" :style="maskStyle(attachmentIcon)"></span></button>
        <Transition name="attachment-actions"><div v-show="attachmentActionsOpen" id="attachment-action-options" class="attachment-action-expanded" role="group" aria-label="附件操作" @keydown="onAttachmentActionsKeydown">
          <button class="attachment-action-option" type="button" aria-label="上传文件" title="上传文件" @click.stop="openFilePicker"><span class="icon-mask" :style="maskStyle(uploadActionIcon)"></span></button>
          <button class="attachment-action-option" type="button" aria-label="上传图片" title="上传图片" @click.stop="openImagePicker"><span class="icon-mask" :style="maskStyle(imageUploadActionIcon)"></span></button>
          <button class="attachment-action-option" type="button" :aria-label="screenshotInProgress ? '正在截屏' : '截屏'" :title="screenshotInProgress ? '正在截屏…' : '截屏'" :disabled="screenshotInProgress || attachmentLimitReached" @click.stop="captureScreenshot"><span class="icon-mask" :style="maskStyle(screenshotActionIcon)"></span></button>
        </div></Transition>
      </div>
    </div>
    <div class="composer-toolbar-right">
      <button v-if="failedSendDraft" type="button" class="restore-failed-draft composer-restore" :disabled="!!input.trim()" @click="restoreFailedSendDraft('chat')">恢复失败消息</button>
      <ComposerToolbar ref="composerToolbar" class="desktop-composer-toolbar" :llm-settings="llmSettings" :profile-catalog="llmProfileCatalog" :settings-profile-id="agentActiveProfile(activeAgent)" :desktop-compact="true" :disabled="turnInProgress || !activeId" :status-label="composerStatusLabel" :status-action-label="canRetryHydrateSync ? '重试同步结果' : ''" @status-action="retryHydrateSync" @switch-profile="switchLLMProfile" @open-model-settings="openModelSettings"/>
      <button v-if="queuedInputsPaused" class="icon-only composer-queue-resume" type="button" :disabled="queueControlPending" :aria-label="queueControlPending ? '正在继续处理待处理消息' : '继续处理待处理消息'" :title="queueControlPending ? '正在继续处理…' : `继续处理 ${hydrate.pending_inputs.length} 条待处理消息`" @click="resumeQueuedInputs"><UiIcon name="play" :size="16" /></button>
      <button class="send-button composer-action" type="button" :disabled="!canSendMessage || messageSubmitPending || !hasComposerContent || !activeId || !status.online || status.setup_required" :aria-label="composerIntent === 'queue' ? '加入等待队列' : '发送消息'" :title="composerIntent === 'queue' ? '加入等待队列' : '发送消息'" @click="send"><span class="icon-mask send-icon" :style="maskStyle(sendIcon)" aria-hidden="true"></span></button>
      <button class="send-button composer-action composer-stop-action" :class="{ 'is-hidden': !agentGenerating }" type="button" :disabled="!agentGenerating || !canStopFromComposer || cancellingTurn" :aria-hidden="!agentGenerating" :tabindex="agentGenerating ? 0 : -1" :aria-label="cancellingTurn ? '正在停止本轮' : '停止本轮'" :title="cancellingTurn ? '正在停止本轮…' : '停止本轮'" :aria-busy="cancellingTurn" @click="stopCurrentTurn"><span class="icon-mask send-icon" :style="maskStyle(stopIcon)" aria-hidden="true"></span></button>
    </div>
  </div>
</form><Transition name="composer-notice"><p v-if="commandNotice" class="composer-notice" role="status" aria-live="polite">{{ commandNotice }}</p></Transition><div class="composer-foot"><span>{{ composerIntent === 'queue' ? '按 Enter 将消息加入等待队列' : '内容发送到当前 Agent' }}</span></div></footer>
      </template>
      <template #dock>
      <ResourceAgentDock
        v-model="dockDraft"
        v-model:expanded="dockExpanded"
        v-model:collapsed="dockCollapsed"
        :collapsed-in-toolbar="activeTab === 'file'"
        :status="dockStatus"
        :pending-kind="dockPendingKind"
        :turn-error="turnError"
        :model-recovery-available="modelRecoveryAvailable"
        :model-name="llmSettings?.model || ''"
        :model-profile="llmSettings?.active_profile || agentActiveProfile(activeAgent)"
        :agent-name="activeAgent?.display_name || '当前智能体'"
        :can-send="canSendMessage"
        :queueing="composerIntent === 'queue'"
        :references="pendingWorkspaceReferences"
        :disabled="!activeId || !status.online || status.setup_required"
        :send-pending="messageSubmitPending"
        :can-stop="canStopFromComposer"
        :cancelling="cancellingTurn"
        :failed-draft="Boolean(failedSendDraft)"
        :restore-disabled="Boolean(dockDraft.trim())"
        @send="sendFromDock"
        @stop="stopCurrentTurn"
        @restore="restoreFailedSendDraft('dock')"
        @remove-reference="removeWorkspaceReferenceById"
        @back-to-chat="showChat"
        @open-model-picker="openModelPicker"
        @open-model-settings="openModelSettings(llmSettings?.active_profile || agentActiveProfile(activeAgent))"
        @occupied-height="dockOccupiedHeight = $event"
      >
        <template #details>
          <div class="dock-details">
            <p v-if="commandNotice" class="dock-command-notice" role="status" aria-live="polite">{{ commandNotice }}</p>
            <div v-if="dockLatestMessage" class="dock-detail-row dock-detail-row--message"><ResourceMarkdown :entry="dockLatestMessage" :agent-id="activeId" source="dock" /></div>
            <div v-if="lastToolActivity && approvalQueueIndex < 0" class="dock-detail-row"><small>最近工具</small><span>{{ lastToolActivity.summary || lastToolActivity.data?.tool_name || '工具活动' }}</span></div>
            <div v-if="approvalQueueIndex >= 0" class="dock-approval"><ApprovalBubble :data="hitlQueue[approvalQueueIndex].data" :request-key="approvalRequestKey(hitlQueue[approvalQueueIndex].data)" :busy="hitlBusyIndex === approvalQueueIndex" @approve-selection="approvedCallIds => approveSelection({ index: approvalQueueIndex, approvedCallIds })" @reject-all="rejectAll(approvalQueueIndex)" /></div>
            <div v-else-if="pending" class="dock-approval dock-approval-fallback"><strong>{{ pending.message || '有待处理的请求' }}</strong><p>请返回完整对话处理此请求。</p></div>
          </div>
        </template>
      </ResourceAgentDock>
      </template>
      </ComposerSurface>
      </div>
    </main>
    <TerminalOpenDialog :open="terminalOpenDialog" :choices="terminalOpenChoices" :sessions="terminalSessions" :sessions-loading="terminalSessionLoading" :sessions-error="terminalSessionError" :channels-loading="terminalChannelLoading" :channels-error="terminalChannelError" @cancel="closeTerminalOpenDialog" @create="createTerminalFromDialog" @resume="resumeTerminalFromDialog" @retry-sessions="retryTerminalSessionList" @retry-channels="retryTerminalChannels" @open-settings="openLinuxChannelSettings" />
    <TerminalEndDialog :open="Boolean(terminalEndConfirmation)" :terminal-name="terminalEndConfirmation?.name" @cancel="cancelEndTerminal" @confirm="confirmEndTerminal" />
    <ScreenshotRegionDialog v-if="screenshotSelection" :image-url="screenshotSelection.url" @cancel="closeScreenshotSelection" @confirm="confirmScreenshotSelection" @error="onScreenshotSelectionError" />
    <div v-if="dialog === 'queued-input-edit' && queuedInputEdit" class="dialog-scrim queued-input-edit-scrim" @click.self="closeQueuedInputEdit">
      <section class="queued-input-edit-dialog" role="dialog" aria-modal="true" aria-labelledby="queued-input-edit-title" @keydown.esc.stop.prevent="closeQueuedInputEdit">
        <header class="queued-input-edit-dialog__head">
          <div><h2 id="queued-input-edit-title">编辑待处理消息</h2><p>仅修改尚未开始处理的消息。</p></div>
          <button class="desktop-icon-button" type="button" aria-label="关闭" title="关闭" :disabled="Boolean(queuedInputMutationId)" @click="closeQueuedInputEdit"><UiIcon name="close" :size="16" /></button>
        </header>
        <textarea ref="queuedInputEditField" v-model="queuedInputEditText" class="queued-input-edit-dialog__field" rows="7" aria-label="消息内容" :disabled="Boolean(queuedInputMutationId)" @keydown.ctrl.enter.prevent="saveQueuedInputEdit"></textarea>
        <p v-if="queuedInputEdit.fileCount || queuedInputEdit.imageCount" class="queued-input-edit-dialog__attachments">
          原消息的附件会保留：<span v-if="queuedInputEdit.fileCount">{{ queuedInputEdit.fileCount }} 个文件引用</span><span v-if="queuedInputEdit.fileCount && queuedInputEdit.imageCount"> · </span><span v-if="queuedInputEdit.imageCount">{{ queuedInputEdit.imageCount }} 张图片</span>
        </p>
        <p v-if="queuedInputEditError" class="queued-input-edit-dialog__error" role="alert">{{ queuedInputEditError }}</p>
        <footer class="queued-input-edit-dialog__actions">
          <button v-if="queuedInputEdit.status !== 'queued'" type="button" class="btn btn--sm" @click="copyQueuedEditToComposer">将修改内容放入输入栏</button>
          <span class="queued-input-edit-dialog__spacer"></span>
          <button type="button" class="btn btn--sm" :disabled="Boolean(queuedInputMutationId)" @click="closeQueuedInputEdit">取消</button>
          <button type="button" class="btn btn--primary btn--sm" :disabled="queuedInputEdit.status !== 'queued' || Boolean(queuedInputMutationId)" @click="saveQueuedInputEdit">{{ queuedInputMutationId ? '保存中…' : '保存修改' }}</button>
        </footer>
      </section>
    </div>
    <div v-if="dialog === 'auto-overview'" class="dialog-scrim auto-overview-scrim" @click.self="dialog = ''"><section class="auto-overview-dialog"><button class="dialog-close desktop-icon-button" type="button" aria-label="关闭" title="关闭" @click="dialog = ''"><UiIcon name="close" :size="16" /></button><AutoOverviewPanel ref="autoOverviewPanel" :action-error="autoOverviewActionError" @open-chat="openAutoChat" @open-config="openAutoConfig" @context-menu="openAutoOverviewContextMenu" /></section></div>
    <div v-if="showAgentCreate" class="agent-create-overlay"><AgentCreatePage @cancel="showAgentCreate = false" @created="onAgentCreated" /></div>
    <UiContextMenu v-model="entryContextMenu" :label="entryContextMenu?.title" @select="runEntryContextMenuItem" />
    <ImageLightbox />
  </div>
</template>
