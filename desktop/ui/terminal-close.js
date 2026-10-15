export function terminalCloseTransport(terminal, connectionId, commandType = 'close') {
  if (!terminal) return 'none'
  if (commandType === 'detach') return connectionId ? 'relay' : 'none'
  if (connectionId) return 'relay'
  return terminal.sessionId ? 'node-api' : 'none'
}

// Run the backend close against captured Agent/session identifiers. Callbacks
// let the UI commit only if the same resource is still selected after awaits.
export async function closeTerminalFlow({
  agentId,
  terminal,
  connectionId,
  commandType = 'close',
  closeRelay,
  closeSession,
  selectionIsCurrent = () => true,
  onSuccess = () => {},
  onFailure = () => {},
}) {
  const transport = terminalCloseTransport(terminal, connectionId, commandType)
  try {
    if (transport === 'relay') {
      await closeRelay({ agentId, connectionId, commandType })
    } else if (transport === 'node-api') {
      await closeSession({ agentId, terminalId: terminal.sessionId })
    }
  } catch (error) {
    if (transport === 'relay' && terminal?.sessionId && ['close', 'terminate'].includes(commandType)) {
      try {
        await closeSession({ agentId, terminalId: terminal.sessionId })
      } catch (fallbackError) {
        onFailure(fallbackError, selectionIsCurrent())
        return false
      }
    } else {
      onFailure(error, selectionIsCurrent())
      return false
    }
  }
  onSuccess(selectionIsCurrent())
  return true
}
