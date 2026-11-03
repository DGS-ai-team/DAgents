const snapshots = new Map()
const MAX_SNAPSHOTS = 32

export function workspaceViewKey(agentId, scope) { return `${agentId}\u0000${scope}` }

export function rememberWorkspaceView(agentId, scope, snapshot) {
  if (!agentId) return
  const key = workspaceViewKey(agentId, scope)
  snapshots.delete(key)
  snapshots.set(key, snapshot)
  while (snapshots.size > MAX_SNAPSHOTS) snapshots.delete(snapshots.keys().next().value)
}

export function getWorkspaceView(agentId, scope) {
  const key = workspaceViewKey(agentId, scope)
  const value = snapshots.get(key)
  if (value) { snapshots.delete(key); snapshots.set(key, value) }
  return value || null
}

export function clearWorkspaceViewsForTests() { snapshots.clear() }
