import assert from 'node:assert/strict'
import test from 'node:test'
import { dispatchDesktopSlashCommand } from './slash-commands.js'

test('Desktop consumes /clear and refreshes the selected Agent without sending chat text', async () => {
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
  assert.match(notices[0], /已清空对话上下文/)
})

test('Desktop executes /compress and reports the result', async () => {
  const notices = []
  const consumed = await dispatchDesktopSlashCommand('/compress', {
    agentId: 'agent-a',
    compressContext: async () => ({ status: 'queued' }),
    refresh: async () => {},
    onNotice: notice => notices.push(notice),
  })

  assert.equal(consumed, true)
  assert.deepEqual(notices, ['压缩：queued'])
})

test('Desktop reports the current thinking setting and routes Manage uploads', async () => {
  const notices = []
  const uploads = []
  await dispatchDesktopSlashCommand('/thinking', {
    agentId: 'agent-a',
    getLLMSettings: async () => ({ thinking: 'enabled' }),
    onNotice: notice => notices.push(notice),
  })
  await dispatchDesktopSlashCommand('/upload skill C:/skills/demo.zip demo 1.0.0 Demo', {
    agentId: 'agent-a',
    upload: async spec => {
      uploads.push(spec)
      return { kind: 'skill', id: 'demo', version: '1.0.0', status: 'uploaded' }
    },
    onNotice: notice => notices.push(notice),
  })

  assert.deepEqual(notices, ['thinking: enabled', '已上传 skill demo@1.0.0（uploaded）'])
  assert.deepEqual(uploads.map(({ kind, path, id, version }) => ({ kind, path, id, version })), [
    { kind: 'skill', path: 'C:/skills/demo.zip', id: 'demo', version: '1.0.0' },
  ])
})

test('Desktop applies /thinking options and rejects invalid syntax', async () => {
  const patches = []
  const notices = []
  await dispatchDesktopSlashCommand('/thinking effort max', {
    agentId: 'agent-a',
    patchLLMSettings: async patch => {
      patches.push(patch)
      return { thinking: 'enabled', reasoning_effort: 'max' }
    },
    onNotice: notice => notices.push(notice),
  })
  assert.deepEqual(patches, [{ reasoning_effort: 'max' }])
  assert.deepEqual(notices, ['thinking: enabled'])

  const errors = []
  await dispatchDesktopSlashCommand('/thinking maybe', {
    agentId: 'agent-a',
    onError: error => errors.push(error),
  })
  assert.match(errors[0], /用法: \/thinking/)
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
