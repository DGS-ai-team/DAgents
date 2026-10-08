import test from 'node:test'
import assert from 'node:assert/strict'
import { formatApprovalArguments, recoverFailedSend, routeForTab, switchDraft, tabForRoute } from './workbench-state.js'

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
