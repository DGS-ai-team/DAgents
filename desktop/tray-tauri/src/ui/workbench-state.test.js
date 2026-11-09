import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { topSlotState } from './top-slot-state.js'
import { findTerminalInstanceForSession, isTerminalDialogRequestCurrent, reusableTerminalConnection, runningTerminalSessions, terminalDialogChoices, terminalOpenArguments, terminalOpenIntent } from './terminal-open-state.js'
import { agentErrorFor, agentSelectionViewTransition, applyAgentNotificationProjection, canCancelTurnPhase, cancelledTurnSummary, chooseActiveAgentId, closeResourceTransition, createDesktopMenuModel, desktopChatEntries, desktopComposerStatus, desktopHitlQueue, desktopMenuNames, formatApprovalArguments, handleAgentNotificationEvent, hasPersistedPartialMessage, isAgentBusy, mergeAgentNotificationSummaries, menuFocusIndex, recoverFailedSend, routeForTab, shouldReconnectTerminal, shouldShowNodeFirstRun, shouldShowTurnStopAction, shouldShowWorkgroupSidebar, switchDraft, tabForRoute, turnErrorSummary, updateAgentBusy, updateAgentError, workgroupCollaborationEnabled } from './workbench-state.js'

test('notification_changed uses notify_seq, preserves integer counts, and ignores stale per-Agent events', () => {
  const agents = [{ agent_id: 'agent-a', has_pending_hitl: true, pending_hitl_items: 2, notify_seq: 5 }]
  const cleared = applyAgentNotificationProjection(agents, {
    event_type: 'notification_changed',
    agent_id: 'agent-a',
    data: { notify_seq: 6, has_unread: true, pending_hitl_items: 3 },
  })
  assert.equal(cleared[0].has_pending_hitl, false)
  assert.equal(cleared[0].has_unread, true)
  assert.equal(cleared[0].pending_hitl_items, 3)
  assert.equal(Number.isInteger(cleared[0].pending_hitl_items), true)
  assert.equal(cleared[0].notify_seq, 6)
  assert.equal('notification_seq' in cleared[0], false)

  const stale = applyAgentNotificationProjection(cleared, {
    event_type: 'notification_changed',
    agent_id: 'agent-a',
    data: { notify_seq: 5, has_pending_hitl: true, pending_hitl_items: 1 },
  })
  assert.equal(stale[0].has_pending_hitl, false)
  assert.equal(applyAgentNotificationProjection(cleared, { event_type: 'turn_state', agent_id: 'agent-a' }), cleared)
})

test('notification handler keeps seq 20 monotonic across late seq 18 and GET seq 19', () => {
  let agents = [{ agent_id: 'agent-a', display_name: 'Agent A', notify_seq: 0, has_pending_hitl: false, has_unread: false, pending_hitl_items: 0 }]
  let projections = {}
  let version = 0
  const seq20 = { event_type: 'notification_changed', agent_id: 'agent-a', data: { notify_seq: 20, has_pending_hitl: true, has_unread: true, pending_hitl_items: 2 } }
  let result = handleAgentNotificationEvent(agents, projections, seq20, version)
  agents = result.agents
  projections = result.projectionsByAgent
  version = result.version
  assert.equal(agents[0].notify_seq, 20)
  assert.equal(agents[0].pending_hitl_items, 2)

  const seq18 = { event_type: 'notification_changed', agent_id: 'agent-a', data: { notify_seq: 18, has_pending_hitl: false, has_unread: false, pending_hitl_items: 0 } }
  result = handleAgentNotificationEvent(agents, projections, seq18, version)
  assert.equal(result.accepted, false)
  assert.equal(result.agents[0].notify_seq, 20)
  assert.equal(result.projectionsByAgent['agent-a'].event.data.notify_seq, 20)
  assert.equal(result.version, version)

  const getStartedAt = version
  const staleGet = [{ agent_id: 'agent-a', display_name: 'Agent A', notify_seq: 19, has_pending_hitl: false, has_unread: false, pending_hitl_items: 0 }]
  const merged = mergeAgentNotificationSummaries(staleGet, result.projectionsByAgent, getStartedAt)
  assert.equal(merged[0].notify_seq, 20)
  assert.equal(merged[0].has_pending_hitl, true)
  assert.equal(merged[0].pending_hitl_items, 2)
})

test('unknown notification is cached and schedules full membership refresh without fabricating a sidebar row', () => {
  const event = { event_type: 'notification_changed', agent_id: 'agent-new', data: { notify_seq: 1, has_unread: true, pending_hitl_items: 2 } }
  const handled = handleAgentNotificationEvent([], {}, event, 0)
  assert.deepEqual(handled.agents, [])
  assert.equal(handled.shouldRefreshSummaries, true)
  assert.equal(handled.projectionsByAgent['agent-new'].event, event)
  assert.equal(handled.projectionsByAgent['agent-new'].event.data.pending_hitl_items, 2)

  const staleGetWithoutMember = mergeAgentNotificationSummaries([], handled.projectionsByAgent, 0)
  assert.deepEqual(staleGetWithoutMember, [])
  const fullGet = [{ agent_id: 'agent-new', display_name: 'Readable Agent', agent_type: 'auto', notify_seq: 0, pending_hitl_items: 0 }]
  const mergedFullGet = mergeAgentNotificationSummaries(fullGet, handled.projectionsByAgent, 0)
  assert.equal(mergedFullGet.length, 1)
  assert.equal(mergedFullGet[0].display_name, 'Readable Agent')
  assert.equal(mergedFullGet[0].agent_type, 'auto')
  assert.equal(mergedFullGet[0].notify_seq, 1)
  assert.equal(mergedFullGet[0].pending_hitl_items, 2)
  assert.deepEqual(mergeAgentNotificationSummaries([], handled.projectionsByAgent, handled.version), [])
})

test('cancellation notice stays concise and only mentions completed file writes', () => {
  assert.equal(cancelledTurnSummary({ phase: 'completed' }), '')
  assert.equal(cancelledTurnSummary({ phase: 'cancelled' }), '已停止。')
  assert.equal(cancelledTurnSummary({
    phase: 'cancelled',
    toolExecutions: [{ toolName: 'write_file', status: 'completed' }],
  }), '已停止。已完成的文件写入仍保留。')
})

test('turn-level stop remains available while awaiting tool approval or user input', () => {
  for (const phase of ['queued', 'model_generating', 'tool_executing', 'tool_waiting', 'waiting_user']) {
    assert.equal(canCancelTurnPhase(phase), true, `${phase} should be cancellable`)
  }
  for (const phase of ['idle', 'completed', 'failed', 'cancelled']) {
    assert.equal(canCancelTurnPhase(phase), false, `${phase} should be terminal or idle`)
  }
})

test('waiting_user remains cancellable without switching the composer from send to stop', () => {
  assert.equal(canCancelTurnPhase('waiting_user'), true)
  assert.equal(shouldShowTurnStopAction('waiting_user'), false)
  assert.equal(shouldShowTurnStopAction('waiting_user', true), true, 'a request already being submitted stays locked')
  assert.equal(shouldShowTurnStopAction('tool_waiting'), true, 'approval wait still exposes stop')
  assert.equal(shouldShowTurnStopAction('tool_executing'), true, 'tool execution still exposes stop')

  const source = readFileSync(new URL('./DesktopApp.vue', import.meta.url), 'utf8')
  assert.match(source, /const agentGenerating = computed\(\(\) => shouldShowTurnStopAction\(turnStateStore\.phase, busy\.value, cancellingTurn\.value\)\)/)
  assert.match(source, /const canCancelTurn = computed\(\(\) => canCancelTurnPhase\(turnStateStore\.phase\)\)/)
  assert.match(source, /if \(hitlQueue\.value\.some\(item => item\.kind === 'user_information'\)\) \{ void submitUserInfo\(undefined, 'chat'\); return \}/)
  assert.match(source, /const dockPendingKind = computed\(\(\) => \{[\s\S]*?turnStateStore\.phase === 'waiting_user'[\s\S]*?return 'user_input'[\s\S]*?return 'approval'[\s\S]*?return 'request'/)
  assert.match(source, /:pending-kind="dockPendingKind"/)
})

test('Desktop menubar groups actions by user task and disables unavailable operations', () => {
  const menus = createDesktopMenuModel({ activeAgentId: 'agent-a', activeAgentName: 'autotest', activeTab: 'browser', online: true, generating: false, hasPendingApproval: true, nodeVersion: '0.11.0' })
  assert.deepEqual(desktopMenuNames, ['DAgents', '资源', '视图', '智能体', '帮助'])
  assert.equal(menus.DAgents.find(item => item.action === 'settings-general').label, '通用设置')
  assert.equal(menus.DAgents.some(item => item.status), false)
  assert.equal(menus.DAgents.find(item => item.action === 'refresh').label, '检查本地服务连接')
  assert.equal(menus.DAgents.find(item => item.action === 'quit-desktop').label, '退出桌面应用')
  assert.equal(menus.DAgents.find(item => item.action === 'quit-desktop').hint, '本地服务将继续运行')
  assert.equal(menus['资源'].find(item => item.action === 'open-browser').disabled, false)
  assert.equal(menus['资源'].find(item => item.action === 'create-terminal').label, '打开终端…')
  assert.equal(menus['资源'].find(item => item.action === 'close-resource').hint, '浏览器')
  assert.equal(menus['视图'].find(item => item.action === 'agent-chat').label, '显示当前对话')
  assert.equal(menus['视图'].find(item => item.action === 'agent-chat').disabled, false)
  assert.equal(menus['视图'].some(item => item.action === 'switch-resource'), false)
  assert.equal(menus['智能体'].find(item => item.action === 'approval').disabled, false)
  assert.equal(menus['智能体'].find(item => item.action === 'compress-context').disabled, true)
  assert.equal(menus['智能体'].some(item => item.action === 'delete-agent'), false)
  assert.deepEqual(menus['智能体'].filter(item => item.header).map(item => item.label), ['autotest', '当前会话', '管理'])
  assert.equal(menus['帮助'].find(item => item.action === 'feedback').label, '提交反馈')

  const settings = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'settings', online: true, isWorkgroup: false })
  assert.equal(settings['资源'].find(item => item.action === 'close-resource').disabled, true)
  assert.equal(settings['智能体'].find(item => item.action === 'current-agent-settings').disabled, false)
  assert.equal(settings['智能体'].some(item => item.action === 'delete-agent'), false)
  assert.equal(settings['视图'].find(item => item.action === 'agent-chat').disabled, false)
  assert.equal(settings['智能体'].some(item => ['select', 'clear'].includes(item.action)), false)
  const workgroup = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'workgroups', online: true, isWorkgroup: true })
  assert.equal(workgroup['资源'].find(item => item.action === 'create-terminal').disabled, true)
  assert.equal(workgroup['资源'].find(item => item.action === 'create-terminal').hint, '工作组暂不支持此操作')
  assert.equal(workgroup['智能体'].some(item => item.action === 'delete-agent'), false)
  assert.equal(workgroup['智能体'].find(item => item.action === 'current-agent-settings').hint, '此操作仅适用于单个智能体')
  assert.equal(workgroup['视图'].find(item => item.action === 'agent-chat').disabled, true)
  assert.equal(workgroup['视图'].find(item => item.action === 'agent-chat').hint, '工作组没有独立的智能体对话')
  const offline = createDesktopMenuModel({ activeTab: 'chat', online: false })
  assert.equal(offline.DAgents.some(item => item.status), false)
  assert.equal(offline['资源'].find(item => item.action === 'open-workspace').disabled, true)
  assert.equal(offline['资源'].find(item => item.action === 'open-workspace').hint, '本地服务未连接')
  assert.equal(offline['资源'].find(item => item.action === 'create-terminal').disabled, true)
  assert.equal(offline['资源'].find(item => item.action === 'create-terminal').hint, '本地服务未连接')
  assert.equal(offline['智能体'].find(item => item.action === 'current-agent-settings').disabled, true)
  assert.equal(offline['智能体'].find(item => item.action === 'current-agent-settings').hint, '本地服务未连接')
  assert.equal(offline['智能体'].some(item => item.action === 'delete-agent'), false)
  const setup = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'chat', online: true, setupRequired: true })
  assert.equal(setup['资源'].find(item => item.action === 'open-workspace').disabled, true)
  assert.equal(setup['资源'].find(item => item.action === 'open-workspace').hint, '请先完成本地服务首次设置')
  assert.equal(setup['智能体'].find(item => item.action === 'create-agent').disabled, true)
  assert.equal(setup['智能体'].find(item => item.action === 'create-agent').hint, '请先完成本地服务首次设置')
  const nonApprovalHitl = createDesktopMenuModel({ activeAgentId: 'agent-a', activeAgentName: 'Writer', online: true, hasPendingHitl: true, hasPendingApproval: false })
  assert.equal(nonApprovalHitl['智能体'][0].label, 'Writer')
  assert.equal(nonApprovalHitl['智能体'].find(item => item.action === 'approval').disabled, true)
  assert.equal(nonApprovalHitl['智能体'].find(item => item.action === 'approval').hint, '当前没有待审批请求')
  assert.equal(nonApprovalHitl['智能体'].find(item => item.action === 'clear-context').disabled, true)
  assert.equal(nonApprovalHitl['智能体'].find(item => item.action === 'clear-context').hint, '请先处理待审批请求')
  assert.equal(nonApprovalHitl['智能体'].find(item => item.action === 'compress-context').disabled, true)
  const compressBusy = createDesktopMenuModel({ activeAgentId: 'agent-a', online: true, generating: true })
  assert.equal(compressBusy['智能体'].find(item => item.action === 'compress-context').disabled, true)
  const compressReady = createDesktopMenuModel({ activeAgentId: 'agent-a', online: true })
  assert.equal(compressReady['智能体'].find(item => item.action === 'compress-context').disabled, false)
})

test('resource menu only exposes draft recovery when the active file panel offers it', () => {
  const fileMenu = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'file', online: true, resourceActions: { canRecoverDraft: true, recoveryLabel: '丢弃草稿并重试' } })['资源']
  assert.equal(fileMenu.find(item => item.action === 'discard-workspace-draft')?.label, '丢弃草稿并重试')
  const cleanFileMenu = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'file', online: true })['资源']
  assert.equal(cleanFileMenu.some(item => item.action === 'discard-workspace-draft'), false)
  const browserMenu = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'browser', online: true, resourceActions: { canRecoverDraft: true } })['资源']
  assert.equal(browserMenu.some(item => item.action === 'discard-workspace-draft'), false)
  const terminalMenu = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'terminal', online: true, resourceActions: { canEnableTerminalInput: true, canEndTerminal: true } })['资源']
  assert.equal(terminalMenu.find(item => item.action === 'enable-terminal-input')?.label, '启用手动输入')
  assert.equal(terminalMenu.find(item => item.action === 'end-terminal')?.label, '结束当前终端…')
  const chatMenu = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'chat', online: true, resourceActions: { canEnableTerminalInput: true, canEndTerminal: true } })['资源']
  assert.equal(chatMenu.some(item => ['enable-terminal-input', 'end-terminal'].includes(item.action)), false)
  const offlineFileMenu = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'file', online: false, resourceActions: { canRecoverDraft: true } })['资源']
  assert.equal(offlineFileMenu.some(item => item.action === 'discard-workspace-draft'), false)
  const setupTerminalMenu = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'terminal', online: true, setupRequired: true, resourceActions: { canEnableTerminalInput: true, canEndTerminal: true } })['资源']
  assert.equal(setupTerminalMenu.some(item => ['enable-terminal-input', 'end-terminal'].includes(item.action)), false)
})

test('terminal creation asks for a target, while resuming an existing session bypasses the chooser', () => {
  assert.deepEqual(terminalOpenIntent(), { kind: 'choose-target' })
  assert.deepEqual(terminalOpenIntent('session-123'), { kind: 'resume', sessionId: 'session-123' })
  const channels = [
    { channel_id: 'enabled-a', enabled: true, display_name: 'Staging', host: 'stage.example', port: 22, username: 'deploy' },
    { channel_id: 'disabled', enabled: false, display_name: 'Disabled' },
  ]
  const windows = terminalDialogChoices('Windows', channels)
  assert.equal(windows[0].label, '本机 PowerShell')
  assert.deepEqual(windows.map(choice => choice.targetId), [undefined, 'enabled-a'])
  assert.deepEqual(terminalOpenArguments(windows[0], 'Windows'), { targetKind: 'local', targetId: null, shell: 'powershell' })
  assert.deepEqual(terminalOpenArguments(windows[1], 'Windows'), { targetKind: 'linux_channel', targetId: 'enabled-a', shell: null })
  assert.equal(terminalDialogChoices('linux', [])[0].label, '本机默认终端')
  assert.equal(terminalDialogChoices('darwin', [])[0].label, '本机默认终端')
  assert.equal(terminalDialogChoices('unknown', [])[0].label, '本机默认终端')
  assert.deepEqual(terminalOpenArguments(terminalDialogChoices('darwin', [])[0], 'darwin'), { targetKind: 'local', targetId: null, shell: null })
  assert.throws(() => terminalOpenArguments({ kind: 'linux_channel', targetId: 'invalid/id' }, 'Windows'), /请选择有效的 Linux 主机/)
})

test('terminal chooser ignores stale loads after cancellation, retry, or Agent switch', () => {
  const request = { requestEpoch: 3, currentEpoch: 3, ownerId: 'agent-a', activeAgentId: 'agent-a', dialogOpen: true }
  assert.equal(isTerminalDialogRequestCurrent(request), true)
  assert.equal(isTerminalDialogRequestCurrent({ ...request, currentEpoch: 4 }), false)
  assert.equal(isTerminalDialogRequestCurrent({ ...request, dialogOpen: false }), false)
  assert.equal(isTerminalDialogRequestCurrent({ ...request, activeAgentId: 'agent-b' }), false)
})

test('running terminal list removes non-running and duplicate rows, and de-duplicates only within its Agent', () => {
  const sessions = [
    { terminal_id: 'one', status: 'running' },
    { terminal_id: 'one', status: 'running', name: 'duplicate' },
    { terminal_id: 'two', status: 'closed' },
    { terminal_id: 'three', status: 'running' },
  ]
  assert.deepEqual(runningTerminalSessions(sessions), [sessions[0], sessions[3]])
  const a = { id: 'a-one', agentId: 'agent-a', sessionId: 'one' }
  const b = { id: 'b-one', agentId: 'agent-b', sessionId: 'one' }
  assert.equal(findTerminalInstanceForSession([a, b], 'agent-a', 'one'), a)
  assert.equal(findTerminalInstanceForSession([a, b], 'agent-b', 'one'), b)
  assert.equal(findTerminalInstanceForSession([a, b], 'agent-c', 'one'), null)
  assert.equal(reusableTerminalConnection({ ...a, connectionId: 'relay-a', status: 'connected' }).id, a.id)
  assert.equal(reusableTerminalConnection({ ...a, connectionId: 'relay-a', status: 'error' }), null)
  assert.equal(reusableTerminalConnection({ ...a, status: 'connected' }), null)
})

test('DesktopApp supplies connection and active workspace state to the top-menu model', () => {
  const source = readFileSync(new URL('./DesktopApp.vue', import.meta.url), 'utf8')
  const menuContext = source.match(/const menuEntries = computed\(\(\) => createDesktopMenuModel\(\{([\s\S]*?)\}\)\)/)?.[1]
  assert.ok(menuContext, 'menu model call is present')
})

test('DesktopApp renders composer status in the visible footer and follows Turn and stream state', () => {
  const source = readFileSync(new URL('./DesktopApp.vue', import.meta.url), 'utf8')
  assert.match(source, /desktopComposerStatus\(\{[\s\S]*?turnState: turnStateStore/)
  assert.match(source, /<ComposerToolbar[^>]*:status-label="composerRuntimeStatus"/)
  assert.doesNotMatch(source, /desktop-composer-runtime-pill[^>]*>\{\{ composerRuntimeStatus \}\}/)
  assert.match(source, /applyDesktopTurnEvent\(event, activeId\.value\)/)
  assert.match(source, /const acceptedTurnState = applyHydrateTurnState\(snapshot\)/)
  assert.match(source, /if \(!acceptedTurnState && turnStateStore\.turnId\) return/)

  assert.equal(desktopComposerStatus({ generating: true }), '正在等待回复')
  assert.equal(desktopComposerStatus({ generating: true, streamingText: '首个 token', turnState: { phase: 'model_generating' } }), '正在生成回复')
  assert.equal(desktopComposerStatus({ generating: true, streamingText: 'reasoning', turnState: { phase: 'model_generating', outputChannel: 'reasoning' } }), '思考中')
  assert.equal(desktopComposerStatus({ generating: true, turnState: { phase: 'tool_executing' } }), '工具执行中')
  assert.equal(desktopComposerStatus({ generating: true, turnState: { phase: 'waiting_user' } }), '等待你的输入')
  assert.equal(desktopComposerStatus({ generating: true, turnState: { phase: 'model_generating', modelAttempt: { status: 'retrying', attempt: 1, maxAttempts: 3 } } }), '正在重试 1/2')
  assert.equal(desktopComposerStatus({ generating: true, turnState: { phase: 'completed', terminal: true } }), '')
  assert.equal(desktopComposerStatus({ generating: false, streamingText: 'late text' }), '')
})

test('DesktopApp notification handler applies reducer output and refreshes unknown Agent membership', () => {
  const source = readFileSync(new URL('./DesktopApp.vue', import.meta.url), 'utf8')
  const notificationHandler = source.indexOf("if (event.event_type === 'notification_changed')")
  const activeAgentBranch = source.indexOf("if (event.agent_id !== activeId.value)")
  assert.ok(notificationHandler >= 0)
  assert.ok(activeAgentBranch > notificationHandler)
  assert.match(source.slice(notificationHandler, activeAgentBranch), /handleAgentNotificationEvent\(/)
  assert.match(source.slice(notificationHandler, activeAgentBranch), /agents\.value = projection\.agents/)
  assert.match(source.slice(notificationHandler, activeAgentBranch), /agentNotificationProjections\.value = projection\.projectionsByAgent/)
  assert.match(source.slice(notificationHandler, activeAgentBranch), /agentNotificationChangeVersion/)
  assert.match(source.slice(notificationHandler, activeAgentBranch), /if \(projection\.shouldRefreshSummaries\)/)
  assert.match(source.slice(notificationHandler, activeAgentBranch), /refreshAgentSummaries\(\)/)
  assert.doesNotMatch(source.slice(notificationHandler, activeAgentBranch), /agentListRefreshSequence\s*\+=/)
  assert.match(source, /mergeAgentNotificationSummaries\(Array\.isArray\(latest\) \? latest : \[\], agentNotificationProjections\.value, notificationVersionAtStart\)/)
  assert.match(source.slice(notificationHandler, activeAgentBranch), /return/)
})

test('DesktopApp exposes one resource icon menu by the tabs and keeps error reasons visible', () => {
  const source = readFileSync(new URL('./DesktopApp.vue', import.meta.url), 'utf8')
  assert.match(source, /class="menu-anchor resource-menu-anchor" data-menu-anchor="资源"/)
  assert.match(source, /data-menubar-trigger="资源" aria-label="资源"/)
  assert.match(source, /desktopMenuNames\.slice\(1\)\.filter\(item => item !== '资源'\)/)
  assert.match(source, /const index = visibleTriggers\.indexOf\(trigger\)/)
  assert.match(source, /visibleTriggers\.findIndex\(item => item\.dataset\.menubarTrigger === name\)/)
  assert.match(source, /class="desktop-error-hint__message">\{\{ error \}\}/)
  assert.doesNotMatch(source, /desktop-chat-error-detail|desktop-resource-error-detail/)
  assert.doesNotMatch(source, /真实会话/)
})

test('turn error summaries use structured provider codes and keep unknown failures generic', () => {
  assert.equal(turnErrorSummary({ code: 'provider_unavailable', message: 'safe backend text' }, { retryCount: 2, partial: true }), '模型服务暂时不可用，已重试2次。请稍后重试或切换模型。 已保留本轮已生成的部分内容。')
  assert.equal(turnErrorSummary({ code: 'provider_authentication', message: '鉴权凭据无效，请检查模型配置。' }), '鉴权凭据无效，请检查模型配置。')
  assert.equal(turnErrorSummary({ code: 'provider_quota', message: '服务商账户额度不足，请检查余额。' }), '服务商账户额度不足，请检查余额。')
  assert.equal(turnErrorSummary({ code: 'provider_configuration', message: '模型请求或配置无效，请检查模型名称、接口地址和请求参数。' }), '模型请求或配置无效，请检查模型名称、接口地址和请求参数。')
  assert.equal(turnErrorSummary({ code: 'provider_http_error', message: '模型服务请求失败（HTTP 403）。' }), '模型服务请求失败（HTTP 403）。')
  assert.equal(turnErrorSummary({ code: 'unknown', message: '{"secret":"do not show"}' }), '本轮未能完成，请稍后重试或切换模型。')
})

test('partial assistant hydrate rows are identified after the current user message', () => {
  assert.equal(hasPersistedPartialMessage([{ kind: 'user' }, { kind: 'assistant', partial: true }]), true)
  assert.equal(hasPersistedPartialMessage([{ kind: 'assistant', partial: true }, { kind: 'user' }]), false)
  assert.equal(hasPersistedPartialMessage([{ kind: 'user' }, { kind: 'assistant', partial: false }]), false)
})

test('Node first-run gate leaves Desktop-local settings accessible without opening Node settings', () => {
  const base = { online: true, setupRequired: true, activeTab: 'settings' }
  assert.equal(shouldShowNodeFirstRun({ ...base, routePath: '/settings/desktop' }), false)
  assert.equal(shouldShowNodeFirstRun({ ...base, routePath: '/settings/general' }), true)
  assert.equal(shouldShowNodeFirstRun({ ...base, routePath: '/settings/connection' }), true)
  assert.equal(shouldShowNodeFirstRun({ ...base, online: false, routePath: '/settings/desktop' }), false)
})

test('menubar focus moves with arrows and Home/End among enabled entries', () => {
  assert.equal(menuFocusIndex(0, 'ArrowUp', 4), 3)
  assert.equal(menuFocusIndex(-1, 'ArrowUp', 4), 3)
  assert.equal(menuFocusIndex(-1, 'ArrowDown', 4), 0)
  assert.equal(menuFocusIndex(3, 'ArrowDown', 4), 0)
  assert.equal(menuFocusIndex(2, 'Home', 4), 0)
  assert.equal(menuFocusIndex(0, 'End', 4), 3)
  assert.equal(menuFocusIndex(0, 'ArrowDown', 0), -1)
})

test('Agent list refresh preserves the selection or safely chooses the next available Agent', () => {
  const rows = [{ agent_id: 'agent-a' }, { agent_id: 'agent-b' }]
  assert.equal(chooseActiveAgentId(rows, 'agent-b'), 'agent-b')
  assert.equal(chooseActiveAgentId(rows, 'archived-agent'), 'agent-a')
  assert.equal(chooseActiveAgentId([], 'archived-agent'), '')
})

test('same-Agent event refresh preserves its resource view, while an explicit sidebar click opens chat', () => {
  assert.deepEqual(agentSelectionViewTransition('file', false), { activeTab: 'file', enterChat: false })
  assert.deepEqual(agentSelectionViewTransition('terminal', false), { activeTab: 'terminal', enterChat: false })
  assert.deepEqual(agentSelectionViewTransition('workgroups', false, true), { activeTab: 'chat', enterChat: true })
  assert.deepEqual(agentSelectionViewTransition('browser', true), { activeTab: 'chat', enterChat: true })
})

test('approval details prefer structured arguments and fall back to raw_arguments', () => {
  assert.equal(formatApprovalArguments({ arguments: { command: 'rm -rf tmp' } }), '{\n  "command": "rm -rf tmp"\n}')
  assert.equal(formatApprovalArguments({ arguments: null, raw_arguments: '{"path":"a.txt"}' }), '{\n  "path": "a.txt"\n}')
  assert.equal(formatApprovalArguments({ raw_arguments: 'not json' }), 'not json')
  assert.equal(formatApprovalArguments({}), '')
})

test('route and visible tab stay aligned on initial load and navigation', () => {
  assert.equal(tabForRoute('/settings/general'), 'settings')
  assert.equal(tabForRoute('/settings/agents/worker'), 'settings')
  assert.equal(tabForRoute('/chat'), 'chat')
  assert.equal(routeForTab('settings'), '/settings/general')
  assert.equal(routeForTab('chat'), '/chat')
  assert.equal(routeForTab('file'), '/chat')
  assert.equal(routeForTab('browser'), '/chat')
  assert.equal(routeForTab('terminal'), '/chat')
})

test('shared chat route preserves an explicitly selected resource across settings round trips', () => {
  let tab = 'file'
  tab = tabForRoute('/chat', tab)
  assert.equal(tab, 'file')
  tab = tabForRoute('/settings/general', tab)
  assert.equal(tab, 'settings')
  tab = 'browser'
  tab = tabForRoute('/chat', tab)
  assert.equal(tab, 'browser')
  tab = 'terminal'
  tab = tabForRoute('/chat', tab)
  assert.equal(tab, 'terminal')
  assert.equal(tabForRoute('/chat', 'chat'), 'chat')
})

test('the fixed top-left slot keeps page identity and sidebar state across settings', () => {
  assert.deepEqual(topSlotState({ tab: 'chat', collapsed: false }), {
    icon: 'panel', page: 'chat', pageLabel: '对话', interactive: true,
    actionLabel: '对话 · 收起侧栏', expanded: true,
  })
  const settings = topSlotState({ tab: 'settings', collapsed: true })
  assert.equal(settings.icon, 'settings')
  assert.equal(settings.interactive, false)
  assert.equal(settings.actionLabel, '设置')
  const returned = topSlotState({ tab: 'chat', collapsed: true })
  assert.equal(returned.icon, 'panel')
  assert.equal(returned.expanded, false)
  assert.equal(returned.actionLabel, '对话 · 展开侧栏')
  assert.equal(topSlotState({ tab: 'browser', collapsed: true }).icon, 'browser')
  assert.equal(topSlotState({ tab: 'workgroups' }).icon, 'workgroup')
  assert.equal(topSlotState({ tab: 'chat', autoOverview: true }).interactive, false)
  assert.equal(topSlotState({ tab: 'chat', autoOverview: true }).icon, 'auto')
})

test('closing a resource clears its matching open state and returns to the shared chat route', () => {
  assert.deepEqual(closeResourceTransition('file'), { tab: 'chat', workspaceOpened: false })
  assert.deepEqual(closeResourceTransition('browser'), { tab: 'chat', browserOpened: false })
  assert.deepEqual(closeResourceTransition('terminal'), { tab: 'terminal', closeTerminal: true })
})

test('a retained disconnected terminal reconnects when activated, but not when closing its tab', () => {
  const detached = { sessionId: 'session-a', connectionId: '', status: 'disconnected' }
  assert.equal(shouldReconnectTerminal(detached), true)
  assert.equal(shouldReconnectTerminal(detached, false), false)
  assert.equal(shouldReconnectTerminal({ ...detached, connectionId: 'connection-a' }), false)
  assert.equal(shouldReconnectTerminal({ ...detached, sessionId: '' }), false)
})

test('Desktop chat adapter keeps hydrate tools and gives id-less history stable per-agent keys', () => {
  const transcript = [
    { kind: 'user', text: 'Inspect this' },
    { kind: 'tool_call', blockId: 'call-1', data: { tool_name: 'read_file', arguments: { path: 'a.txt' } } },
    { kind: 'tool_result', blockId: 'call-1', text: 'contents' },
    { kind: 'assistant', text: 'Done' },
  ]
  const entries = desktopChatEntries('agent-a', transcript)
  assert.deepEqual(entries.map(entry => entry.kind), ['user', 'tool_call', 'tool_result', 'assistant'])
  assert.deepEqual(entries.map(entry => entry.id), [
    'agent-a:history:0', 'agent-a:history:1', 'agent-a:history:2', 'agent-a:history:3',
  ])
  assert.deepEqual(entries[1].data.arguments, { path: 'a.txt' })
  assert.notEqual(desktopChatEntries('agent-b', transcript)[0].id, entries[0].id)
})

test('Desktop chat adapter preserves the safe conversation boundary in hydrated history', () => {
  const entries = desktopChatEntries('agent-a', [
    { kind: 'user', text: 'Earlier' },
    { kind: 'assistant', text: 'Earlier answer' },
    { kind: 'context_boundary', boundary_id: 'stable-id' },
    { kind: 'user', text: 'New question' },
  ])
  assert.deepEqual(entries.map(entry => entry.kind), ['user', 'assistant', 'context_boundary', 'user'])
  assert.equal(entries[2].boundary_id, 'stable-id')
  assert.equal(entries[2].text, undefined)
})

test('pending user send is visible before hydrate and is not duplicated once persisted', () => {
  const pending = { id: 'send-1', kind: 'user', text: '2 + 2?', baseTranscriptLength: 1 }
  const beforeHydrate = desktopChatEntries('agent-a', [{ kind: 'user', text: 'Earlier' }], pending)
  assert.deepEqual(beforeHydrate.map(entry => entry.text), ['Earlier', '2 + 2?'])
  assert.equal(beforeHydrate[1].id, 'agent-a:pending-user:send-1')

  const afterHydrate = desktopChatEntries('agent-a', [
    { kind: 'user', text: 'Earlier' },
    { kind: 'user', text: '2 + 2?' },
  ], pending)
  assert.deepEqual(afterHydrate.map(entry => entry.text), ['Earlier', '2 + 2?'])
})


test('Desktop HITL adapter preserves every supported request type and queue order', () => {
  const userInfo = { content: 'Which environment?', user_information_args: { question: 'Which environment?' } }
  const memoryConflict = { id: 'memory-call', content: 'Conflict' }
  const approval = { approval_id: 'approval-1', approval_args: { tool_calls: [{ id: 'call-1' }] } }
  assert.deepEqual(desktopHitlQueue({ userInfos: [userInfo], memoryConflicts: [memoryConflict], approval }), [
    { kind: 'user_information', data: userInfo },
    { kind: 'memory_conflict', data: memoryConflict },
    { kind: 'approval', data: approval },
  ])
})

test('composer drafts remain with the Agent that owns them', () => {
  const drafts = {}
  assert.equal(switchDraft(drafts, '', 'agent-a', ''), '')
  assert.equal(switchDraft(drafts, 'agent-a', 'agent-b', 'draft for A'), '')
  assert.equal(switchDraft(drafts, 'agent-b', 'agent-a', 'draft for B'), 'draft for A')
  assert.equal(switchDraft(drafts, 'agent-a', 'agent-b', 'updated draft for A'), 'draft for B')
})

test('failed send during an Agent switch is retained for the sending Agent', () => {
  const retries = {}
  const currentValue = recoverFailedSend(retries, 'agent-a', 'agent-b', 'draft for B', 'failed message from A')
  assert.equal(currentValue, 'draft for B')
  assert.equal(retries['agent-a'], 'failed message from A')
})

test('failed send never overwrites text typed while the request was pending', () => {
  const retries = {}
  const currentValue = recoverFailedSend(retries, 'agent-a', 'agent-a', 'new draft for A', 'failed earlier message')
  assert.equal(currentValue, 'new draft for A')
  assert.equal(retries['agent-a'], 'failed earlier message')
})

test('failed send returns to an empty composer for the same Agent', () => {
  const retries = {}
  assert.equal(recoverFailedSend(retries, 'agent-a', 'agent-a', '', 'retry me'), 'retry me')
  assert.equal(retries['agent-a'], undefined)
})

test('pending send and its failure stay with Agent A after switching to Agent B', () => {
  let busyByAgent = updateAgentBusy({}, 'agent-a', true)
  assert.equal(isAgentBusy(busyByAgent, 'agent-a'), true)
  assert.equal(isAgentBusy(busyByAgent, 'agent-b'), false)

  // B can send while A remains pending; completing or failing A cannot clear B's busy state.
  busyByAgent = updateAgentBusy(busyByAgent, 'agent-b', true)
  busyByAgent = updateAgentBusy(busyByAgent, 'agent-a', false)
  assert.equal(isAgentBusy(busyByAgent, 'agent-a'), false)
  assert.equal(isAgentBusy(busyByAgent, 'agent-b'), true)

  const errors = updateAgentError({}, 'agent-a', 'A 的请求失败')
  assert.equal(agentErrorFor(errors, 'agent-b'), '')
  assert.equal(agentErrorFor(errors, 'agent-a'), 'A 的请求失败')
})

test('workgroup sidebar appears only for enabled collaboration, existing groups, or its active view', () => {
  assert.equal(workgroupCollaborationEnabled({ enabled: true, workgroup_enabled: false }), false)
  assert.equal(workgroupCollaborationEnabled({ enabled: false, workgroup_enabled: true }), false)
  assert.equal(workgroupCollaborationEnabled({ enabled: true, workgroup_enabled: true }), true)
  assert.equal(shouldShowWorkgroupSidebar({ collaborationEnabled: false }), false)
  assert.equal(shouldShowWorkgroupSidebar({ collaborationEnabled: true }), true)
  assert.equal(shouldShowWorkgroupSidebar({ hasWorkgroups: true }), true)
  assert.equal(shouldShowWorkgroupSidebar({ isWorkgroupView: true }), true)
  const menu = createDesktopMenuModel().DAgents
  assert.equal(menu.find(item => item.action === 'workgroups-settings').label, '工作组 / 协作设置')
})
