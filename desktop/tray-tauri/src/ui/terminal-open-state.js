export function terminalOpenIntent(sessionId = '') {
  const id = String(sessionId || '').trim()
  return id ? { kind: 'resume', sessionId: id } : { kind: 'choose-target' }
}

export function isTerminalDialogRequestCurrent({ requestEpoch, currentEpoch, ownerId, activeAgentId, dialogOpen }) {
  return Boolean(dialogOpen && requestEpoch === currentEpoch && ownerId && ownerId === activeAgentId)
}

export function runningTerminalSessions(sessions) {
  const seen = new Set()
  return (Array.isArray(sessions) ? sessions : []).filter(session => {
    const id = String(session?.terminal_id || '')
    if (!id || session?.status !== 'running' || seen.has(id)) return false
    seen.add(id)
    return true
  })
}

export function findTerminalInstanceForSession(instances, agentId, sessionId) {
  const owner = String(agentId || '')
  const session = String(sessionId || '')
  if (!owner || !session) return null
  return (Array.isArray(instances) ? instances : []).find(item => item?.agentId === owner && item?.sessionId === session) || null
}

export function reusableTerminalConnection(terminal) {
  return terminal?.connectionId && ['connecting', 'reconnecting', 'connected'].includes(terminal.status) ? terminal : null
}

export function isWindowsPlatform(platform) {
  const os = String(platform || '').toLowerCase()
  return os === 'windows' || os.startsWith('windows') || ['win32', 'win64', 'winnt'].includes(os)
}

export function terminalDialogChoices(platform, channels = []) {
  const localLabel = isWindowsPlatform(platform) ? 'Node 本机 PowerShell' : 'Node 本机默认终端'
  const choices = [{ kind: 'local', label: localLabel, hint: '由当前连接的 Node 主机启动' }]
  for (const channel of Array.isArray(channels) ? channels : []) {
    if (!channel?.channel_id || channel.enabled === false) continue
    const address = [channel.username, channel.host].filter(Boolean).join('@')
    choices.push({
      kind: 'linux_channel',
      targetId: String(channel.channel_id),
      shell: '',
      label: String(channel.display_name || channel.channel_id),
      hint: [address, channel.port ? `:${channel.port}` : '', channel.is_default ? '默认主机' : ''].filter(Boolean).join(' · '),
    })
  }
  return choices
}

export function terminalOpenArguments(choice, platform) {
  if (!choice || choice.kind === 'local') {
    return { targetKind: 'local', targetId: null, shell: isWindowsPlatform(platform) ? 'powershell' : null }
  }
  if (choice.kind !== 'linux_channel' || !/^[A-Za-z0-9_-]{1,120}$/.test(String(choice.targetId || ''))) {
    throw new Error('请选择有效的 Linux 主机')
  }
  return { targetKind: 'linux_channel', targetId: String(choice.targetId), shell: null }
}
