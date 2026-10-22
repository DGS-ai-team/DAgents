export function formatApprovalArguments(item) {
  const args = item?.arguments;
  if (args != null) {
    if (typeof args === 'string') {
      try { return JSON.stringify(JSON.parse(args), null, 2) } catch { return args }
    }
    try { return JSON.stringify(args, null, 2) } catch { /* fall through to raw payload */ }
  }
  const raw = item?.raw_arguments;
  if (typeof raw !== 'string' || !raw.trim()) return '';
  try { return JSON.stringify(JSON.parse(raw), null, 2) } catch { return raw }
}

export function tabForRoute(path, currentTab = 'chat') {
  if (String(path || '').startsWith('/settings')) return 'settings'
  if (['file', 'browser', 'terminal'].includes(currentTab)) return currentTab
  return 'chat'
}

export function routeForTab(tab) {
  return tab === 'settings' ? '/settings/general' : '/chat';
}

export function shouldShowNodeFirstRun({ online, setupRequired, activeTab, routePath }) {
  return Boolean(online && setupRequired && !(activeTab === 'settings' && routePath === '/settings/desktop'));
}

// Entering another Agent is navigation, while an event refresh for the
// selected Agent should retain its open resource. Explicit sidebar intent also
// returns to chat when that Agent is already selected (for example from a
// workgroup view).
export function agentSelectionViewTransition(activeTab, changed, openChat = false) {
  const enterChat = Boolean(changed || openChat)
  return { activeTab: enterChat ? 'chat' : activeTab, enterChat }
}

export function closeResourceTransition(tab) {
  if (tab === 'file') return { tab: 'chat', workspaceOpened: false }
  if (tab === 'browser') return { tab: 'chat', browserOpened: false }
  if (tab === 'terminal') return { tab: 'terminal', closeTerminal: true }
  return { tab }
}

export function shouldReconnectTerminal(terminal, reconnect = true) {
  return Boolean(reconnect && terminal?.sessionId && !terminal.connectionId && terminal.status === 'disconnected')
}

export function chooseActiveAgentId(agents, currentId) {
  const rows = Array.isArray(agents) ? agents : []
  const current = String(currentId || '')
  if (current && rows.some(agent => agent?.agent_id === current)) return current
  return String(rows[0]?.agent_id || '')
}

export function desktopChatEntries(agentId, transcript = [], streamingText = '', turnState = null, pendingUser = null, generating = false) {
  const rows = Array.isArray(transcript) ? transcript : []
  const terminal = isTerminalTurn(turnState)
  const entries = rows.map((entry, index) => ({
    ...entry,
    id: `${agentId || 'agent'}:history:${index}`,
    data: entry?.data && typeof entry.data === 'object' ? entry.data : {},
  }));
  const persistedPendingUser = pendingUser && rows.slice(Math.max(0, Number(pendingUser.baseTranscriptLength) || 0))
    .some(entry => entry?.kind === 'user' && String(entry.text || '') === String(pendingUser.text || ''))
  if (pendingUser && !persistedPendingUser) {
    const entry = { ...pendingUser }
    delete entry.baseTranscriptLength
    entries.push({ ...entry, id: `${agentId || 'agent'}:pending-user:${pendingUser.id}`, data: {} })
  }
  if (generating && !streamingText && !terminal) {
    entries.push({
      id: `${agentId || 'agent'}:pending-response:${pendingUser?.id || 'active'}`,
      kind: 'assistant',
      // Match the WebUI: an empty streaming assistant entry renders the
      // animated brand activity mark instead of a textual placeholder.
      text: '',
      streaming: true,
    })
  } else if (streamingText && !terminal) {
    entries.push({
      id: `${agentId || 'agent'}:history:${entries.length}`,
      kind: 'assistant',
      text: String(streamingText),
      streaming: true,
    });
  }
  return entries;
}

function isTerminalTurn(turnState) {
  return turnState?.terminal === true || ['completed', 'failed', 'cancelled', 'interrupted', 'budget_exhausted'].includes(turnState?.phase)
}

export function desktopHitlQueue(expanded = {}) {
  return [
    ...(expanded.userInfos || []).map(data => ({ kind: 'user_information', data })),
    ...(expanded.memoryConflicts || []).map(data => ({ kind: 'memory_conflict', data })),
    ...(expanded.approval ? [{ kind: 'approval', data: expanded.approval }] : []),
  ]
}


export function switchDraft(drafts, fromId, toId, currentValue) {
  if (fromId) drafts[fromId] = currentValue
  return drafts[toId] || ''
}

export function recoverFailedSend(retries, agentId, activeAgentId, currentDraft, failedText) {
  if (activeAgentId !== agentId || currentDraft.trim()) {
    retries[agentId] = failedText
    return currentDraft
  }
  delete retries[agentId]
  return failedText
}

export function updateAgentBusy(states, agentId, busy) {
  const id = String(agentId || '')
  if (!id) return states
  const next = { ...states }
  if (busy) next[id] = true
  else delete next[id]
  return next
}

export function isAgentBusy(states, agentId) {
  return Boolean(states?.[String(agentId || '')])
}

export function updateAgentError(errors, agentId, message) {
  const id = String(agentId || '')
  if (!id) return errors
  const next = { ...errors }
  const value = String(message || '')
  if (value) next[id] = value
  else delete next[id]
  return next
}

export function agentErrorFor(errors, agentId) {
  return String(errors?.[String(agentId || '')] || '')
}

export const desktopMenuNames = ['DAgents', '资源', '视图', '智能体', '帮助']

export function createDesktopMenuModel(context = {}) {
  const {
    online = false,
    setupRequired = false,
    nodeVersion = '',
    activeAgentId = '',
    activeAgentName = '尚未选择智能体',
    isWorkgroup = false,
    activeTab = 'chat',
    generating = false,
    hasPendingApproval = false,
    hasPendingHitl = false,
    sidebarCollapsed = false,
    resourceActions = {},
  } = context
  const hasAgent = Boolean(activeAgentId) && !isWorkgroup
  const canOpenResource = online && !setupRequired && hasAgent
  const closeableResource = ['terminal', 'file', 'browser'].includes(activeTab) && !isWorkgroup
  const separator = { separator: true }
  const item = (label, action, disabled = false, extra = {}) => ({ label, action, disabled, ...extra })
  const resourceUnavailableReason = !online
    ? 'Node 未连接'
    : setupRequired
      ? '请先完成 Node 首次设置'
      : isWorkgroup
        ? '工作组暂不支持此操作'
        : !activeAgentId
          ? '请先选择智能体'
          : ''
  const agentUnavailableReason = !online
    ? 'Node 未连接'
    : isWorkgroup
      ? '此操作仅适用于单个智能体'
      : !activeAgentId
        ? '请先选择智能体'
        : ''
  const setupUnavailableReason = !online ? 'Node 未连接' : '请先完成 Node 首次设置'
  const conversationUnavailableReason = !activeAgentId
    ? '请先选择智能体'
    : isWorkgroup
      ? '工作组没有独立的智能体对话'
      : activeTab === 'chat'
        ? '当前已在对话页'
        : ''
  return {
    DAgents: [
      { status: true, online, label: `Node · ${online ? '在线' : '离线'}`, hint: nodeVersion || '本机' },
      item('检查 Node 连接', 'refresh'),
      separator,
      item('通用设置', 'settings-general'),
      separator,
      item('退出 Desktop', 'quit-desktop', false, { hint: 'Node 将继续运行' }),
    ],
    资源: [
      item('打开工作区文件', 'open-workspace', !canOpenResource, canOpenResource ? {} : { hint: resourceUnavailableReason }),
      item('查看浏览器', 'open-browser', !canOpenResource, canOpenResource ? {} : { hint: resourceUnavailableReason }),
      item('打开终端…', 'create-terminal', !canOpenResource, canOpenResource ? {} : { hint: resourceUnavailableReason }),
      separator,
      ...(canOpenResource && activeTab === 'file' && resourceActions.canRecoverDraft
        ? [item(resourceActions.recoveryLabel || '丢弃草稿并重载', 'discard-workspace-draft')]
        : []),
      ...(canOpenResource && activeTab === 'terminal' && resourceActions.canEnableTerminalInput
        ? [item('启用手动输入', 'enable-terminal-input')]
        : []),
      ...(canOpenResource && activeTab === 'terminal' && resourceActions.canEndTerminal
        ? [separator, item('结束当前终端…', 'end-terminal')]
        : []),
      item('关闭当前资源', 'close-resource', !closeableResource, { hint: closeableResource ? ({ terminal: '终端', file: '工作区', browser: '浏览器' })[activeTab] : '当前没有可关闭的资源' }),
    ],
    视图: [
      item(sidebarCollapsed ? '展开侧栏' : '收起侧栏', 'sidebar'),
      item('显示当前对话', 'agent-chat', activeTab === 'chat' || !activeAgentId || isWorkgroup, { hint: conversationUnavailableReason }),
    ],
    智能体: [
      { header: true, label: activeAgentName },
      item('当前智能体设置', 'current-agent-settings', !hasAgent || !online, { hint: !hasAgent || !online ? agentUnavailableReason : '' }),
      separator,
      { header: true, label: '当前会话' },
      item('停止当前回合', 'stop-turn', !hasAgent || !online || !generating, { hint: !hasAgent || !online ? agentUnavailableReason : !generating ? '当前没有生成中的任务' : '' }),
      item('清空当前对话', 'clear-context', !hasAgent || !online || generating || hasPendingHitl, { hint: !hasAgent || !online ? agentUnavailableReason : generating ? '请先停止当前生成' : hasPendingHitl ? '请先处理待审批请求' : '' }),
      item('查看待审批', 'approval', !hasAgent || !online || !hasPendingApproval, { hint: !hasAgent || !online ? agentUnavailableReason : !hasPendingApproval ? '当前没有待审批请求' : '' }),
      separator,
      { header: true, label: '管理' },
      item('新建智能体', 'create-agent', !online || setupRequired, { hint: !online || setupRequired ? setupUnavailableReason : '' }),
      item('自主智能体总览', 'auto-overview', !online, { hint: !online ? 'Node 未连接' : '' }),
    ],
    帮助: [
      item('使用说明与版本', 'help-about'),
      item('提交反馈', 'feedback'),
    ],
  }
}

export function menuFocusIndex(index, key, count) {
  const size = Math.max(0, Number(count) || 0)
  if (!size) return -1
  if (key === 'Home') return 0
  if (key === 'End') return size - 1
  const current = Number.isFinite(Number(index)) ? Number(index) : -1
  if (key === 'ArrowDown') return current < 0 ? 0 : (current + 1 + size) % size
  if (key === 'ArrowUp') return current < 0 ? size - 1 : (current - 1 + size) % size
  return Math.min(size - 1, Math.max(0, Number(index) || 0))
}
