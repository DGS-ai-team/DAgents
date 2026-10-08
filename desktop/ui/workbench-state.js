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

export function tabForRoute(path) {
  return String(path || '').startsWith('/settings') ? 'settings' : 'chat';
}

export function routeForTab(tab) {
  return tab === 'settings' ? '/settings/general' : '/chat';
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
