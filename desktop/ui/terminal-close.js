export function terminalCloseTransport(terminal, connectionId, commandType = 'detach') {
  if (!terminal) return 'none'
  // Removing a view or disconnecting only releases the Desktop relay. The Node
  // PTY is destroyed only by the explicit terminate action.
  if (commandType === 'terminate') return terminal.sessionId ? 'node-api' : 'none'
  return connectionId ? 'relay' : 'none'
}

// Run the backend close against captured Agent/session identifiers. Callbacks
// let the UI commit only if the same resource is still selected after awaits.
export async function closeTerminalFlow({
  agentId,
  terminal,
  connectionId,
  commandType = 'detach',
  closeRelay,
  closeSession,
  selectionIsCurrent = () => true,
  onSuccess = () => {},
  onFailure = () => {},
}) {
  const transport = terminalCloseTransport(terminal, connectionId, commandType)
  try {
    if (transport === 'relay') {
      await closeRelay({ agentId, connectionId, commandType: 'detach' })
    } else if (transport === 'node-api') {
      await closeSession({ agentId, terminalId: terminal.sessionId })
      if (connectionId) {
        // The Node DELETE is authoritative. Relay cleanup is best effort and
        // cannot turn a confirmed session deletion into a reported failure.
        try { await closeRelay({ agentId, connectionId, commandType: 'detach' }) } catch { /* Node already confirmed termination. */ }
      }
    }
  } catch (error) {
    onFailure(error, selectionIsCurrent())
    return false
  }
  onSuccess(selectionIsCurrent())
  return true
}
