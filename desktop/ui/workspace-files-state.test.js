import test from 'node:test'
import assert from 'node:assert/strict'
import { agentMediaId, deserializeWorkspaceDraft, isWorkspaceRequestCurrent, serializeWorkspaceDraft, shouldClearWorkspaceUploadError, toNodeFileReference, workspaceContentType, workspaceDraftKey, workspaceUploadPath } from './workspace-files-state.js'
import { listWorkspace, readWorkspaceFile, saveWorkspaceFile, uploadWorkspaceFile } from './workspace-files-api.js'

test('workspace uploads target the complete relative filename and preserve directories', () => {
  assert.equal(workspaceUploadPath('', 'image.png'), 'image.png')
  assert.equal(workspaceUploadPath('src/assets/', '/image.png'), 'src/assets/image.png')
})

test('successful workspace uploads only clear the exact stale upload error', () => {
  const priorError = 'input.txt：同名文件已存在，请换名后重新上传。'
  assert.equal(shouldClearWorkspaceUploadError(priorError, priorError), true)
  assert.equal(shouldClearWorkspaceUploadError('网络连接失败', priorError), false)
  assert.equal(shouldClearWorkspaceUploadError(priorError, ''), false)
  // A prior file failing in this batch must remain visible after a later upload succeeds.
  assert.equal(shouldClearWorkspaceUploadError(priorError, priorError, true), false)
  assert.equal(shouldClearWorkspaceUploadError('first.txt：上传失败', priorError, true), false)
})

test('only explicitly selected file refs are serialized in the Node API object contract', () => {
  assert.deepEqual(toNodeFileReference({ path: 'C:/node/private/notes.md', name: 'notes.md' }), { path: 'C:/node/private/notes.md', name: 'notes.md' })
})

test('workspace binary sniffing previews actual raster images and treats source-like files as inert text', () => {
  const png = Uint8Array.from([137,80,78,71,13,10,26,10,0])
  assert.equal(workspaceContentType('preview.png', 'application/octet-stream', png), 'image/png')
  assert.equal(workspaceContentType('readme.md', 'application/octet-stream', new TextEncoder().encode('# safe')), 'text/plain')
  assert.equal(workspaceContentType('page.html', 'text/html', new TextEncoder().encode('<script>')), 'text/plain')
  assert.equal(workspaceContentType('spoof.png', 'application/octet-stream', new TextEncoder().encode('not an image')), 'text/plain')
  assert.equal(workspaceContentType('binary.bin', 'application/octet-stream', Uint8Array.from([0, 255])), 'application/octet-stream')
})

test('workspace request epochs reject stale Agent and file responses; drafts are Agent/path scoped', () => {
  const current = { epoch: 4, agentId: 'agent-b', path: 'src/new.md' }
  assert.equal(isWorkspaceRequestCurrent({ epoch: 3, agentId: 'agent-a', path: 'src/old.md' }, current), false)
  assert.equal(isWorkspaceRequestCurrent({ epoch: 4, agentId: 'agent-b', path: 'src/new.md' }, current), true)
  assert.notEqual(workspaceDraftKey('agent-a', 'src/new.md'), workspaceDraftKey('agent-b', 'src/new.md'))
  assert.notEqual(workspaceDraftKey('agent-b', 'src/old.md'), workspaceDraftKey('agent-b', 'src/new.md'))
})

test('workspace drafts retain their base ETag and surface external edits as conflicts', () => {
  const saved = serializeWorkspaceDraft('local edits', '"revision-a"')
  assert.deepEqual(deserializeWorkspaceDraft(saved, '"revision-a"'), { content: 'local edits', revision: '"revision-a"', conflict: false })
  assert.deepEqual(deserializeWorkspaceDraft(saved, '"revision-b"'), { content: 'local edits', revision: '"revision-a"', conflict: true })
})

test('media URLs are accepted only for the owning Agent and the Node media route', () => {
  assert.equal(agentMediaId('/v1/agents/agent-a/media/media_123?thumbnail=1', 'agent-a'), 'media_123')
  assert.equal(agentMediaId('/v1/agents/agent-b/media/media_123', 'agent-a'), '')
  assert.equal(agentMediaId('https://example.test/v1/agents/agent-a/media/media_123', 'agent-a'), '')
  assert.equal(agentMediaId('/v1/agents/agent-a/media/../secret', 'agent-a'), '')
})

test('workspace transport uses authenticated Node JSON listing and bounded binary commands with file_ref payload shape', async () => {
  const calls = []
  globalThis.__DAGENTS_API_REQUEST__ = async options => { calls.push(['json', options]); return { path: 'src', entries: [] } }
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    calls.push([command, args])
    const payload = JSON.stringify({ path: 'src/a.md', file_ref: 'C:/private/agent/src/a.md' })
    return { status: 200, content_type: 'application/json', etag: '"r1"', body_base64: btoa(payload) }
  }
  try {
    const listing = await listWorkspace('agent/1', 'src')
    const file = await readWorkspaceFile('agent/1', 'src/a.md')
    const uploaded = await uploadWorkspaceFile('agent/1', 'src', { name: 'a.md', arrayBuffer: async () => new TextEncoder().encode('hello').buffer })
    assert.equal(listing.path, 'src')
    assert.equal(file.etag, '"r1"')
    assert.equal(uploaded.file_ref, 'C:/private/agent/src/a.md')
    assert.match(calls[0][1].path, /agents%2F1|agents\/agent%2F1/)
    assert.match(calls[2][1].path, /workspace\/upload\?path=src%2Fa.md$/)
    assert.equal(calls[2][1].method, 'POST')
  } finally {
    delete globalThis.__DAGENTS_API_REQUEST__
    delete globalThis.__DAGENTS_DESKTOP_INVOKE__
  }
})

test('workspace upload collisions have a rename message while revision conflicts keep the reload message', async () => {
  const errors = [
    { error: { code: 'workspace_revision_mismatch', message: 'file revision changed' } },
    { error: { code: 'workspace_file_exists', message: 'file already exists' } },
  ]
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async () => ({
    status: 412,
    content_type: 'application/json',
    etag: '',
    body_base64: btoa(JSON.stringify(errors.shift())),
  })
  try {
    await assert.rejects(
      saveWorkspaceFile('agent-1', 'notes.txt', new TextEncoder().encode('changed'), '"r1"'),
      error => error.status === 412 && error.message === '文件已被其他操作修改，请重新载入后再保存。',
    )
    await assert.rejects(
      uploadWorkspaceFile('agent-1', '', { name: 'notes.txt', arrayBuffer: async () => new ArrayBuffer(0) }),
      error => error.status === 412 && error.message === '同名文件已存在，请换名后重新上传。',
    )
  } finally {
    delete globalThis.__DAGENTS_DESKTOP_INVOKE__
  }
})
