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

export function canCancelTurnPhase(phase) {
  return ['queued', 'model_generating', 'tool_executing', 'tool_waiting', 'waiting_user'].includes(String(phase || ''))
}

export function shouldShowTurnStopAction(phase, busy = false, cancelling = false) {
  if (busy || cancelling) return true
  return canCancelTurnPhase(phase) && String(phase || '') !== 'waiting_user'
}

export function applyAgentNotificationProjection(agents, event) {
  if (event?.event_type !== 'notification_changed' || !event?.agent_id || !Array.isArray(agents)) return agents
  const projection = event.data || {}
  const sequence = Number(projection.notify_seq) || 0
  const index = agents.findIndex(agent => agent?.agent_id === event.agent_id)
  if (index < 0) return agents
  const currentSequence = Number(agents[index]?.notify_seq) || 0
  if (sequence && currentSequence > sequence) return agents
  const projectionFields = {
    has_pending_hitl: Boolean(projection.has_pending_hitl),
    has_unread: Boolean(projection.has_unread),
    pending_hitl_items: Math.max(0, Math.floor(Number(projection.pending_hitl_items) || 0)),
    notify_seq: sequence || currentSequence,
  }
  return agents.map((agent, rowIndex) => rowIndex === index ? { ...agent, ...projectionFields } : agent)
}

export function handleAgentNotificationEvent(agents, projectionsByAgent = {}, event, version = 0) {
  if (event?.event_type !== 'notification_changed' || !event?.agent_id || !Array.isArray(agents)) {
    return { agents, projectionsByAgent, version, accepted: false, shouldRefreshSummaries: false }
  }
  const agentId = event.agent_id
  const row = agents.find(agent => agent?.agent_id === agentId)
  const cached = projectionsByAgent[agentId]
  const incomingSequence = Number(event.data?.notify_seq) || 0
  const currentSequence = Math.max(
    Number(row?.notify_seq) || 0,
    Number(cached?.event?.data?.notify_seq) || 0,
  )
  if (incomingSequence && incomingSequence < currentSequence) {
    return { agents, projectionsByAgent, version, accepted: false, shouldRefreshSummaries: !row }
  }

  const nextVersion = version + 1
  const nextProjections = {
    ...projectionsByAgent,
    [agentId]: { event, version: nextVersion },
  }
  return {
    agents: applyAgentNotificationProjection(agents, event),
    projectionsByAgent: nextProjections,
    version: nextVersion,
    accepted: true,
    shouldRefreshSummaries: !row,
  }
}

export function mergeAgentNotificationSummaries(agents, projectionsByAgent = {}, afterVersion = 0) {
  if (!Array.isArray(agents)) return []
  let merged = agents
  for (const [agentId, record] of Object.entries(projectionsByAgent || {})) {
    const event = record?.event
    if (event?.event_type !== 'notification_changed' || event.agent_id !== agentId) continue
    const index = merged.findIndex(agent => agent?.agent_id === agentId)
    // The list endpoint owns membership and is authoritative for events that
    // predate the request, unless its notification sequence is stale.
    if (index < 0) continue
    const serverSequence = Number(merged[index]?.notify_seq) || 0
    const eventSequence = Number(event.data?.notify_seq) || 0
    const arrivedDuringRequest = Number(record?.version) > Number(afterVersion)
    if (arrivedDuringRequest || (eventSequence && eventSequence > serverSequence)) {
      merged = applyAgentNotificationProjection(merged, event)
    }
  }
  return merged
}

export function cancelledTurnSummary(turnState) {
  if (String(turnState?.phase || '') !== 'cancelled') return ''
  const hasCompletedFileWrite = Array.isArray(turnState?.toolExecutions)
    && turnState.toolExecutions.some(item =>
      ['write_file', 'search_replace', 'create_file', 'delete_file'].includes(String(item?.toolName || item?.tool_name || ''))
      && ['completed', 'succeeded', 'success'].includes(String(item?.status || '').toLowerCase()),
    )
  return hasCompletedFileWrite ? '已停止。已完成的文件写入仍保留。' : '已停止。'
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

export function shouldShowWorkgroupSidebar({ collaborationEnabled, hasWorkgroups, isWorkgroupView } = {}) {
  return Boolean(collaborationEnabled || hasWorkgroups || isWorkgroupView);
}

export function workgroupCollaborationEnabled(manageSettings) {
  return manageSettings?.enabled === true && manageSettings?.workgroup_enabled === true;
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

export function desktopComposerStatus({ generating = false, cancelling = false, streamingText = '', turnState = {} } = {}) {
  const phase = String(turnState?.phase || '')
  if (!generating || turnState?.terminal || ['completed', 'failed', 'cancelled', 'interrupted', 'budget_exhausted', 'finished'].includes(phase)) return ''
  if (cancelling) return '正在停止'
  const attempt = turnState?.modelAttempt || turnState?.model_attempt || {}
  if (attempt.status === 'retrying') {
    const count = Math.max(1, Number(attempt.attempt) || 1)
    const totalRequests = Number(attempt.maxAttempts ?? attempt.max_attempts) || count + 1
    const totalRetries = Math.max(1, totalRequests - 1)
    return `正在重试 ${count}/${totalRetries}`
  }
  if (phase === 'queued') return '等待执行'
  if (phase === 'tool_executing') return '工具执行中'
  if (phase === 'tool_waiting') return '等待工具审批'
  if (phase === 'waiting_user') return '等待你的输入'
  if (streamingText) {
    return phase === 'model_generating' && turnState?.outputChannel === 'reasoning'
      ? '思考中'
      : '正在生成回复'
  }
  return '正在等待回复'
}

export function turnErrorSummary(error, { partial = false, retryCount = 0 } = {}) {
  const code = String(error?.code || '').trim().toLowerCase()
  const raw = String(error?.message || '').trim()
  const retries = Math.max(0, Number(retryCount) || 0)
  let message
  if (code === 'provider_authentication') {
    message = raw || '模型鉴权失败，请到模型设置检查 API Key 和服务地址。'
  } else if (code === 'provider_quota') {
    message = raw || '模型账户余额或额度不足，请检查服务商账户额度后重试。'
  } else if (code === 'provider_unavailable') {
    message = `模型服务暂时不可用，已重试${retries}次。请稍后重试或切换模型。`
  } else if (code === 'provider_timeout') {
    message = `模型服务响应超时，已重试${retries}次。请稍后重试或切换模型。`
  } else if (code === 'provider_rate_limited') {
    message = `模型服务请求过于频繁，已重试${retries}次。请稍后重试或切换模型。`
  } else if (code === 'provider_configuration' || code === 'provider_error' || code === 'provider_http_error' || code === 'provider_network_error') {
    message = raw || '本轮未能完成，请稍后重试或切换模型。'
  } else if (code === 'turn_interrupted') {
    message = '本轮已中断，请检查已生成内容后再继续。'
  } else {
    message = '本轮未能完成，请稍后重试或切换模型。'
  }
  return partial ? `${message} 已保留本轮已生成的部分内容。` : message
}

export function hasPersistedPartialMessage(transcript) {
  if (!Array.isArray(transcript)) return false
  let lastUserIndex = -1
  for (let index = 0; index < transcript.length; index += 1) {
    if (transcript[index]?.kind === 'user' || transcript[index]?.role === 'user') lastUserIndex = index
  }
  return transcript.slice(lastUserIndex + 1).some(entry =>
    entry?.partial && (entry?.kind === 'assistant' || entry?.role === 'assistant'),
  )
}

export function desktopChatEntries(agentId, transcript = [], pendingUser = null) {
  const rows = Array.isArray(transcript) ? transcript : []
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
  return entries;
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
    ? '本地服务未连接'
    : setupRequired
      ? '请先完成本地服务首次设置'
      : isWorkgroup
        ? '工作组暂不支持此操作'
        : !activeAgentId
          ? '请先选择智能体'
          : ''
  const agentUnavailableReason = !online
    ? '本地服务未连接'
    : isWorkgroup
      ? '此操作仅适用于单个智能体'
      : !activeAgentId
        ? '请先选择智能体'
        : ''
  const setupUnavailableReason = !online ? '本地服务未连接' : '请先完成本地服务首次设置'
  const conversationUnavailableReason = !activeAgentId
    ? '请先选择智能体'
    : isWorkgroup
      ? '工作组没有独立的智能体对话'
      : activeTab === 'chat'
        ? '当前已在对话页'
        : ''
  return {
    DAgents: [
      item('检查本地服务连接', 'refresh'),
      separator,
      item('通用设置', 'settings-general'),
      item('工作组 / 协作设置', 'workgroups-settings'),
      separator,
      item('退出桌面应用', 'quit-desktop', false, { hint: '本地服务将继续运行' }),
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
      item('压缩上下文', 'compress-context', !hasAgent || !online || setupRequired || generating || hasPendingHitl || hasPendingApproval, { hint: !hasAgent || !online ? agentUnavailableReason : setupRequired ? setupUnavailableReason : generating ? '请等待当前生成结束' : hasPendingHitl || hasPendingApproval ? '请先处理待审批请求' : '' }),
      item('查看待审批', 'approval', !hasAgent || !online || !hasPendingApproval, { hint: !hasAgent || !online ? agentUnavailableReason : !hasPendingApproval ? '当前没有待审批请求' : '' }),
      separator,
      { header: true, label: '管理' },
      item('新建智能体', 'create-agent', !online || setupRequired, { hint: !online || setupRequired ? setupUnavailableReason : '' }),
      item('自主智能体总览', 'auto-overview', !online, { hint: !online ? '本地服务未连接' : '' }),
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
