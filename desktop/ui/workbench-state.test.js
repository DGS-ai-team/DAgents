import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { topSlotState } from './top-slot-state.js'
import { findTerminalInstanceForSession, isTerminalDialogRequestCurrent, reusableTerminalConnection, runningTerminalSessions, terminalDialogChoices, terminalOpenArguments, terminalOpenIntent } from './terminal-open-state.js'
import { agentErrorFor, agentSelectionViewTransition, chooseActiveAgentId, closeResourceTransition, createDesktopMenuModel, desktopChatEntries, desktopHitlQueue, desktopMenuNames, formatApprovalArguments, isAgentBusy, menuFocusIndex, recoverFailedSend, routeForTab, shouldReconnectTerminal, shouldShowNodeFirstRun, switchDraft, tabForRoute, updateAgentBusy, updateAgentError } from './workbench-state.js'

test('Desktop menubar groups actions by user task and disables unavailable operations', () => {
  const menus = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'browser', online: true, generating: false, hasPendingApproval: true, nodeVersion: '0.11.0' })
  assert.deepEqual(desktopMenuNames, ['DAgents', '资源', '视图', '智能体', '帮助'])
  assert.equal(menus.DAgents.find(item => item.action === 'settings-general').label, '通用设置')
  assert.equal(menus.DAgents.find(item => item.label.startsWith('Node ·')).hint, '0.11.0')
  assert.equal(menus.DAgents.find(item => item.action === 'quit-desktop').label, '退出 Desktop')
  assert.equal(menus.DAgents.find(item => item.action === 'quit-desktop').hint, 'Node 将继续运行')
  assert.equal(menus['资源'].find(item => item.action === 'open-browser').disabled, false)
  assert.equal(menus['资源'].find(item => item.action === 'create-terminal').label, '打开终端…')
  assert.equal(menus['资源'].find(item => item.action === 'close-resource').hint, '浏览器')
  assert.equal(menus['视图'].find(item => item.action === 'agent-chat').disabled, false)
  assert.equal(menus['视图'].some(item => item.action === 'switch-resource'), false)
  assert.equal(menus['智能体'].find(item => item.action === 'approval').disabled, false)
  assert.equal(menus['帮助'].find(item => item.action === 'feedback').label, '提交反馈')

  const settings = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'settings', online: true, isWorkgroup: false })
  assert.equal(settings['资源'].find(item => item.action === 'close-resource').disabled, true)
  assert.equal(settings['智能体'].find(item => item.action === 'current-agent-settings').disabled, false)
  assert.equal(settings['视图'].find(item => item.action === 'agent-chat').disabled, false)
  assert.equal(settings['智能体'].some(item => ['select', 'clear'].includes(item.action)), false)
  const workgroup = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'workgroups', online: true, isWorkgroup: true })
  assert.equal(workgroup['资源'].find(item => item.action === 'create-terminal').disabled, true)
  assert.equal(workgroup['智能体'].find(item => item.action === 'delete-agent').disabled, true)
  assert.equal(workgroup['视图'].find(item => item.action === 'agent-chat').disabled, true)
  const offline = createDesktopMenuModel({ activeTab: 'chat', online: false })
  assert.equal(offline.DAgents.find(item => item.label.startsWith('Node ·')).hint, '本机')
  const tunnelOffline = createDesktopMenuModel({ activeTab: 'chat', online: false, connectionMode: 'tunnel' })
  assert.equal(tunnelOffline.DAgents.find(item => item.label.startsWith('Node ·')).hint, 'SSH 隧道')
  assert.equal(offline['资源'].find(item => item.action === 'open-workspace').disabled, true)
  assert.equal(offline['资源'].find(item => item.action === 'create-terminal').disabled, true)
  assert.equal(offline['智能体'].find(item => item.action === 'current-agent-settings').disabled, true)
  assert.equal(offline['智能体'].find(item => item.action === 'delete-agent').disabled, true)
  const setup = createDesktopMenuModel({ activeAgentId: 'agent-a', activeTab: 'chat', online: true, setupRequired: true })
  assert.equal(setup['资源'].find(item => item.action === 'open-workspace').disabled, true)
  assert.equal(setup['智能体'].find(item => item.action === 'create-agent').disabled, true)
  const nonApprovalHitl = createDesktopMenuModel({ activeAgentId: 'agent-a', activeAgentName: 'Writer', online: true, hasPendingHitl: true, hasPendingApproval: false })
  assert.equal(nonApprovalHitl['智能体'][0].label, 'Writer')
  assert.equal(nonApprovalHitl['智能体'].find(item => item.action === 'approval').disabled, true)
  assert.equal(nonApprovalHitl['智能体'].find(item => item.action === 'clear-context').disabled, true)
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
  assert.equal(windows[0].label, 'Node 本机 PowerShell')
  assert.deepEqual(windows.map(choice => choice.targetId), [undefined, 'enabled-a'])
  assert.deepEqual(terminalOpenArguments(windows[0], 'Windows'), { targetKind: 'local', targetId: null, shell: 'powershell' })
  assert.deepEqual(terminalOpenArguments(windows[1], 'Windows'), { targetKind: 'linux_channel', targetId: 'enabled-a', shell: null })
  assert.equal(terminalDialogChoices('linux', [])[0].label, 'Node 本机默认终端')
  assert.equal(terminalDialogChoices('darwin', [])[0].label, 'Node 本机默认终端')
  assert.equal(terminalDialogChoices('unknown', [])[0].label, 'Node 本机默认终端')
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

test('DesktopApp wires the active connection mode into the top-menu model', () => {
  const source = readFileSync(new URL('./DesktopApp.vue', import.meta.url), 'utf8')
  const menuContext = source.match(/const menuEntries = computed\(\(\) => createDesktopMenuModel\(\{([\s\S]*?)\}\)\)/)?.[1]
  assert.ok(menuContext, 'menu model call is present')
  assert.match(menuContext, /connectionMode:\s*status\.value\.connection_mode/)
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
  const entries = desktopChatEntries('agent-a', transcript, 'Continuing')
  assert.deepEqual(entries.map(entry => entry.kind), ['user', 'tool_call', 'tool_result', 'assistant', 'assistant'])
  assert.deepEqual(entries.map(entry => entry.id), [
    'agent-a:history:0', 'agent-a:history:1', 'agent-a:history:2', 'agent-a:history:3', 'agent-a:history:4',
  ])
  assert.deepEqual(entries[1].data.arguments, { path: 'a.txt' })
  assert.equal(entries[4].streaming, true)
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

test('streaming assistant reserves its history index and terminal hydrate keeps keys stable across turns', () => {
  const live = desktopChatEntries('agent-a', [{ kind: 'user', text: 'Question' }], 'Answer')
  const completedTranscript = [
    { kind: 'user', text: 'Question' },
    { kind: 'assistant', text: 'Answer' },
  ]
  const terminalState = { phase: 'completed', terminal: true }
  const terminal = desktopChatEntries('agent-a', completedTranscript, 'Answer', terminalState)
  assert.equal(live.at(-1).id, 'agent-a:history:1')
  assert.equal(terminal.filter(entry => entry.kind === 'assistant').length, 1)
  assert.equal(terminal.at(-1).id, live.at(-1).id)
  assert.equal(terminal.at(-1).streaming, undefined)
  assert.deepEqual(desktopChatEntries('agent-a', completedTranscript, '', terminalState).map(entry => entry.id), terminal.map(entry => entry.id))

  const nextTurn = [...completedTranscript, { kind: 'user', text: 'Next question' }]
  const nextLive = desktopChatEntries('agent-a', nextTurn, 'Next answer', { phase: 'model_generating', terminal: false })
  assert.equal(nextLive[1].id, 'agent-a:history:1')
  assert.equal(nextLive.at(-1).id, 'agent-a:history:3')
})

test('pending user send is visible before hydrate and is not duplicated once persisted', () => {
  const pending = { id: 'send-1', kind: 'user', text: '2 + 2?', baseTranscriptLength: 1 }
  const beforeHydrate = desktopChatEntries('agent-a', [{ kind: 'user', text: 'Earlier' }], '', null, pending, true)
  assert.deepEqual(beforeHydrate.map(entry => entry.text), ['Earlier', '2 + 2?', '正在生成回复…'])
  assert.equal(beforeHydrate[1].id, 'agent-a:pending-user:send-1')
  assert.equal(beforeHydrate[2].id, 'agent-a:pending-response:send-1')

  const afterHydrate = desktopChatEntries('agent-a', [
    { kind: 'user', text: 'Earlier' },
    { kind: 'user', text: '2 + 2?' },
  ], '', { phase: 'model_generating' }, pending, true)
  assert.deepEqual(afterHydrate.map(entry => entry.text), ['Earlier', '2 + 2?', '正在生成回复…'])

  const firstToken = desktopChatEntries('agent-a', [
    { kind: 'user', text: 'Earlier' },
    { kind: 'user', text: '2 + 2?' },
  ], '4', { phase: 'model_generating' }, null, true)
  assert.deepEqual(firstToken.map(entry => entry.text), ['Earlier', '2 + 2?', '4'])
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
