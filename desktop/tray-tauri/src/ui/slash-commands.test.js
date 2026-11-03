import assert from 'node:assert/strict'
import test from 'node:test'
import { dispatchDesktopSlashCommand } from './slash-commands.js'

test('Desktop /clear reuses the confirmation result and refreshes only after clearing', async () => {
  const calls = []
  const notices = []
  const consumed = await dispatchDesktopSlashCommand('/clear', {
    agentId: 'agent-a',
    clearContext: async id => calls.push(['clear', id]),
    refresh: async id => calls.push(['refresh', id]),
    onNotice: notice => notices.push(notice),
  })

  assert.equal(consumed, true)
  assert.deepEqual(calls, [['clear', 'agent-a'], ['refresh', 'agent-a']])
  assert.deepEqual(notices, ['已清空对话上下文'])

  const canceled = []
  await dispatchDesktopSlashCommand('/clear', {
    agentId: 'agent-a',
    clearContext: async () => false,
    refresh: async () => canceled.push('refresh'),
    onNotice: () => canceled.push('notice'),
  })
  assert.deepEqual(canceled, [])
})

test('Desktop /clear reports a busy or pending-approval block while keeping confirmation cancellation quiet', async () => {
  const errors = []
  const calls = []
  const consumed = await dispatchDesktopSlashCommand('/clear', {
    agentId: 'agent-a',
    clearContext: async () => { throw new Error('请先处理待审批请求后再清空对话') },
    refresh: async () => calls.push('refresh'),
    onError: error => errors.push(error),
    onNotice: notice => calls.push(notice),
  })

  assert.equal(consumed, true)
  assert.deepEqual(errors, ['请先处理待审批请求后再清空对话'])
  assert.deepEqual(calls, [])
})

test('Desktop /compress reports the applied result or that no compaction was needed', async () => {
  const notices = []
  await dispatchDesktopSlashCommand('/compress', {
    agentId: 'agent-a',
    compressContext: async () => ({ status: 'applied', compressed_message_count: 4 }),
    refresh: async () => {},
    onNotice: notice => notices.push(notice),
  })
  await dispatchDesktopSlashCommand('/compress', {
    agentId: 'agent-a',
    compressContext: async () => ({ status: 'noop' }),
    onNotice: notice => notices.push(notice),
  })

  assert.deepEqual(notices, ['上下文压缩完成，合并了 4 条消息', '当前上下文无需压缩'])
})

test('Desktop no longer treats /thinking or /upload as supported commands', async () => {
  const errors = []
  for (const command of ['/thinking on', '/upload skill package.zip example 1.0.0']) {
    const consumed = await dispatchDesktopSlashCommand(command, {
      agentId: 'agent-a',
      onError: error => errors.push(error),
    })
    assert.equal(consumed, true)
  }

  assert.deepEqual(errors, [
    '未知命令: /thinking on',
    '未知命令: /upload skill package.zip example 1.0.0',
  ])
})

test('Desktop consumes unknown slash commands as errors instead of sending them to the model', async () => {
  const errors = []
  const consumed = await dispatchDesktopSlashCommand('/unknown', {
    agentId: 'agent-a',
    onError: error => errors.push(error),
  })

  assert.equal(consumed, true)
  assert.deepEqual(errors, ['未知命令: /unknown'])
})
