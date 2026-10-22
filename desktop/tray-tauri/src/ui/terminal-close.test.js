import test from 'node:test'
import assert from 'node:assert/strict'
import { closeTerminalFlow } from './terminal-close.js'

function deferred() {
  let resolve
  const promise = new Promise(res => { resolve = res })
  return { promise, resolve }
}

test('explicit end targets its captured Agent and does not overwrite a different selection', async () => {
  const terminalA = { id: 'session-a', sessionId: 'session-a', connectionId: '', agentId: 'agent-a', status: 'disconnected' }
  const terminalB = { id: 'session-b', sessionId: 'session-b', connectionId: 'relay-b', agentId: 'agent-b', status: 'connected' }
  const ui = { activeAgentId: 'agent-a', activeTerminalId: 'session-a', connectionId: '', status: 'disconnected', output: 'A output', resources: [terminalA, terminalB] }
  const pending = deferred()
  const closeCalls = []
  const task = closeTerminalFlow({
    agentId: ui.activeAgentId,
    terminal: terminalA,
    connectionId: ui.connectionId,
    commandType: 'terminate',
    closeRelay: async () => assert.fail('detached close must not reconnect through the UI relay'),
    closeSession: async request => { closeCalls.push(request); await pending.promise },
    selectionIsCurrent: () => ui.activeAgentId === 'agent-a' && ui.activeTerminalId === 'session-a',
    onSuccess: stillCurrent => {
      ui.resources = ui.resources.filter(item => item !== terminalA)
      if (stillCurrent) { ui.connectionId = ''; ui.status = 'closed'; ui.output = '' }
    },
  })

  ui.activeAgentId = 'agent-b'
  ui.activeTerminalId = 'session-b'
  ui.connectionId = 'relay-b'
  ui.status = 'connected'
  ui.output = 'B output'
  pending.resolve()

  assert.equal(await task, true)
  assert.deepEqual(closeCalls, [{ agentId: 'agent-a', terminalId: 'session-a' }])
  assert.deepEqual(ui.resources, [terminalB])
  assert.deepEqual({ activeAgentId: ui.activeAgentId, activeTerminalId: ui.activeTerminalId, connectionId: ui.connectionId, status: ui.status, output: ui.output }, {
    activeAgentId: 'agent-b', activeTerminalId: 'session-b', connectionId: 'relay-b', status: 'connected', output: 'B output',
  })
})

test('failed detached end keeps the original terminal available for retry', async () => {
  const terminal = { id: 'session-a', sessionId: 'session-a', connectionId: '', agentId: 'agent-a', status: 'disconnected' }
  const resources = [terminal]
  let terminalError = ''
  let globalError = ''
  const closed = await closeTerminalFlow({
    agentId: 'agent-a',
    terminal,
    commandType: 'terminate',
    closeRelay: async () => assert.fail('detached close must use the Node API'),
    closeSession: async () => { throw new Error('Node unavailable') },
    selectionIsCurrent: () => true,
    onFailure: (error, stillCurrent) => {
      terminal.error = String(error)
      if (stillCurrent) globalError = terminal.error
      terminalError = terminal.error
    },
    onSuccess: () => resources.splice(0, 1),
  })
  assert.equal(closed, false)
  assert.match(terminalError, /Node unavailable/)
  assert.equal(globalError, terminalError)
  assert.deepEqual(resources, [terminal])
})

test('explicit termination confirms Node deletion before best-effort relay detach', async () => {
  const calls = []
  const closed = await closeTerminalFlow({
    agentId: 'agent-a',
    terminal: { sessionId: 'session-a' },
    connectionId: 'relay-a',
    commandType: 'terminate',
    closeRelay: async request => { calls.push(['relay', request]); throw new Error('relay gone') },
    closeSession: async request => calls.push(['api', request]),
  })
  assert.equal(closed, true)
  assert.deepEqual(calls, [
    ['api', { agentId: 'agent-a', terminalId: 'session-a' }],
    ['relay', { agentId: 'agent-a', connectionId: 'relay-a', commandType: 'detach' }],
  ])
})

test('failed view-close relay does not close the retained PTY through the Node API', async () => {
  const calls = []
  const failures = []
  const terminal = { sessionId: 'session-a' }
  const closed = await closeTerminalFlow({
    agentId: 'agent-a',
    terminal,
    connectionId: 'relay-a',
    commandType: 'detach',
    closeRelay: async request => { calls.push(['relay', request]); throw new Error('relay gone') },
    closeSession: async request => calls.push(['api', request]),
    onFailure: error => failures.push(error.message),
  })
  assert.equal(closed, false)
  assert.deepEqual(calls, [
    ['relay', { agentId: 'agent-a', connectionId: 'relay-a', commandType: 'detach' }],
  ])
  assert.deepEqual(failures, ['relay gone'])
})

test('view close detaches the relay and never destroys a retained session', async () => {
  const calls = []
  const terminal = { id: 'session-a', sessionId: 'session-a', agentId: 'agent-a' }
  const detached = await closeTerminalFlow({
    agentId: 'agent-a', terminal, connectionId: 'relay-a',
    closeRelay: async request => calls.push(['relay', request]),
    closeSession: async request => calls.push(['api', request]),
  })
  assert.equal(detached, true)
  assert.deepEqual(calls, [['relay', { agentId: 'agent-a', connectionId: 'relay-a', commandType: 'detach' }]])
})

test('disconnect keeps the tab and a later explicit end uses Node destruction', async () => {
  const calls = []
  const terminal = { id: 'session-a', sessionId: 'session-a', agentId: 'agent-a' }
  await closeTerminalFlow({ agentId: 'agent-a', terminal, connectionId: 'relay-a', commandType: 'detach', closeRelay: async request => calls.push(request), closeSession: async request => calls.push(request) })
  await closeTerminalFlow({ agentId: 'agent-a', terminal, connectionId: '', commandType: 'terminate', closeRelay: async request => calls.push(request), closeSession: async request => calls.push(request) })
  assert.deepEqual(calls, [
    { agentId: 'agent-a', connectionId: 'relay-a', commandType: 'detach' },
    { agentId: 'agent-a', terminalId: 'session-a' },
  ])
})

test('failed Node deletion leaves terminal available and never pretends relay termination succeeded', async () => {
  const calls = []
  const terminal = { id: 'session-a', sessionId: 'session-a', agentId: 'agent-a' }
  const ended = await closeTerminalFlow({
    agentId: 'agent-a', terminal, connectionId: 'relay-a', commandType: 'terminate',
    closeRelay: async request => calls.push(['relay', request]),
    closeSession: async request => { calls.push(['api', request]); throw new Error('Node unavailable') },
  })
  assert.equal(ended, false)
  assert.deepEqual(calls, [['api', { agentId: 'agent-a', terminalId: 'session-a' }]])
})

test('legacy close command is normalized to detach and cannot destroy a PTY', async () => {
  const calls = []
  await closeTerminalFlow({ agentId: 'agent-a', terminal: { sessionId: 'session-a' }, connectionId: 'relay-a', commandType: 'close', closeRelay: async request => calls.push(request), closeSession: async request => calls.push(request) })
  assert.deepEqual(calls, [{ agentId: 'agent-a', connectionId: 'relay-a', commandType: 'detach' }])
})
