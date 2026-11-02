import test from 'node:test'
import assert from 'node:assert/strict'
import { agentMediaId, cancelWorkspaceAttachmentBatch, createWorkspaceAttachmentBatch, createWorkspaceCsvPreviewParser, decodeWorkspaceText, deserializeWorkspaceDraft, encodeWorkspaceText, isWorkspaceAttachmentBatchCurrent, isWorkspaceRequestCurrent, isWorkspaceWriteRequestCurrent, normalizeWorkspacePath, parseWorkspaceCsvPreview, serializeWorkspaceDraft, shouldClearWorkspaceUploadError, toNodeFileReference, workspaceContentType, workspaceDraftKey, workspaceFileLocation, workspaceImageBlob, workspaceRasterBlob, workspaceTextFormat, workspaceWriteProgressLabel } from './workspace-files-state.js'
import { createWorkspaceFile, listWorkspace, pickWorkspaceImport, readWorkspaceFile, readWorkspaceTextChunk, revealWorkspacePath, saveWorkspaceFile, saveWorkspaceFileAs, uploadWorkspaceFile } from './workspace-files-api.js'

test('text editor round-trips UTF-8 BOM and dominant line endings, including new files', () => {
  const encoder = new TextEncoder()
  const original = encoder.encode('\uFEFFfirst\r\nsecond\r\n')
  const format = workspaceTextFormat(original)
  assert.deepEqual(format, { hasBom: true, newline: '\r\n' })
  const decoded = new TextDecoder('utf-8').decode(original)
  assert.equal(decoded, 'first\r\nsecond\r\n')
  assert.equal(decodeWorkspaceText(original), 'first\nsecond\n')
  const saved = encodeWorkspaceText('first\nupdated\n', format)
  assert.deepEqual(Array.from(saved.subarray(0, 3)), [0xef, 0xbb, 0xbf])
  assert.equal(new TextDecoder().decode(saved), 'first\r\nupdated\r\n')

  const lfFormat = workspaceTextFormat(encoder.encode('one\ntwo\n'))
  assert.deepEqual(lfFormat, { hasBom: false, newline: '\n' })
  assert.equal(new TextDecoder().decode(encodeWorkspaceText('one\ntwo', lfFormat)), 'one\ntwo')
  assert.deepEqual(workspaceTextFormat(new Uint8Array()), { hasBom: false, newline: '\n' })
  assert.equal(new TextDecoder().decode(encodeWorkspaceText('new file\n')), 'new file\n')
})

test('attachment batches stop after cancellation and become stale when the active Agent changes', () => {
  const batch = createWorkspaceAttachmentBatch('agent-a')
  assert.equal(isWorkspaceAttachmentBatchCurrent(batch, batch, 'agent-a'), true)
  assert.equal(isWorkspaceAttachmentBatchCurrent(batch, batch, 'agent-b'), false)
  cancelWorkspaceAttachmentBatch(batch)
  assert.equal(batch.controller.signal.aborted, true)
  assert.equal(isWorkspaceAttachmentBatchCurrent(batch, batch, 'agent-a'), false)
})

test('workspace path normalization preserves dotfiles while removing only explicit current-directory prefixes', () => {
  assert.equal(normalizeWorkspacePath('.gitignore'), '.gitignore')
  assert.equal(normalizeWorkspacePath('.config/settings.json'), '.config/settings.json')
  assert.equal(normalizeWorkspacePath('./reports/summary.md'), 'reports/summary.md')
  assert.equal(normalizeWorkspacePath('.'), '')
  assert.deepEqual(workspaceFileLocation('reports/annual/summary.md'), { path: 'reports/annual/summary.md', parent: 'reports/annual', name: 'summary.md' })
  assert.deepEqual(workspaceFileLocation('.config/settings.json'), { path: '.config/settings.json', parent: '.config', name: 'settings.json' })
})

test('CSV preview parses quoted delimiters, escaped quotes, and multiline cells within preview bounds', () => {
  assert.deepEqual(parseWorkspaceCsvPreview('\uFEFFname,note\r\n"Doe, Jane","said ""hello"""\r\nnext,"line one\nline two"'), {
    rows: [['name', 'note'], ['Doe, Jane', 'said "hello"'], ['next', 'line one\nline two']],
    truncated: false,
  })
  assert.deepEqual(parseWorkspaceCsvPreview('a,b,c\n1,2,3', { rows: 1, columns: 2 }), {
    rows: [['a', 'b']],
    truncated: true,
  })
})

test('incremental CSV preview carries quote, CRLF, and escaped-quote state across chunks', () => {
  const parser = createWorkspaceCsvPreviewParser()
  parser.append('id,note\r\n1,"line "')
  parser.append('"quoted""\ncontinued"\r')
  const result = parser.append('\n2,last', { final: true })
  assert.deepEqual(result.rows, [
    ['id', 'note'],
    ['1', 'line "quoted"\ncontinued'],
    ['2', 'last'],
  ])
})

test('incremental CSV parser caps rows, columns, and retained cell length', () => {
  const parser = createWorkspaceCsvPreviewParser({ rows: 2, columns: 1, maxCellChars: 3 })
  const result = parser.append('long,value\na,2\nb,3', { final: true })
  assert.deepEqual(result.rows, [['lon'], ['a']])
  assert.equal(result.truncated, true)
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
  assert.equal(workspaceContentType('page.html', 'text/html', new TextEncoder().encode('<script>')), 'text/html')
  assert.equal(workspaceContentType('spoof.png', 'application/octet-stream', new TextEncoder().encode('not an image')), 'text/plain')
  assert.equal(workspaceContentType('binary.bin', 'application/octet-stream', Uint8Array.from([0, 255])), 'application/octet-stream')
})

test('resource image blobs trust raster signatures rather than filenames or response MIME', () => {
  const fixtures = [
    ['chart.png', 'image/svg+xml', [137,80,78,71,13,10,26,10], 'image/png'],
    ['photo.jpg', 'text/html', [0xff,0xd8,0xff,0x00], 'image/jpeg'],
    ['animation.gif', 'application/octet-stream', [...new TextEncoder().encode('GIF89a')], 'image/gif'],
    ['plot.webp', 'application/octet-stream', [...new TextEncoder().encode('RIFF') ,0,0,0,0,...new TextEncoder().encode('WEBP')], 'image/webp'],
  ]
  for (const [path, contentType, signature, expected] of fixtures) {
    assert.equal(workspaceRasterBlob(path, { contentType, bytes: Uint8Array.from(signature) }).type, expected)
  }
  for (const [path, contentType, bytes] of [
    ['spoof.png', 'image/png', new TextEncoder().encode('<html>not an image</html>')],
    ['vector.svg', 'image/svg+xml', new TextEncoder().encode('<svg xmlns="http://www.w3.org/2000/svg"></svg>')],
  ]) assert.throws(() => workspaceRasterBlob(path, { contentType, bytes }), /不是受支持的/)
  const svg = new TextEncoder().encode('<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>')
  assert.equal(workspaceImageBlob('chart.svg', { contentType: 'application/octet-stream', bytes: svg }).type, 'image/svg+xml')
  assert.throws(() => workspaceImageBlob('chart.svg', { contentType: 'text/html', bytes: svg }), /响应类型无效/)
  assert.throws(() => workspaceImageBlob('chart.svg', { contentType: 'application/octet-stream', bytes: Uint8Array.from([0xff, 0xfe]) }), /UTF-8/)
  assert.throws(() => workspaceImageBlob('chart.svg', { contentType: 'application/octet-stream', bytes: new Uint8Array(1024 * 1024 + 1) }), /1 MiB/)
})

test('workspace request epochs reject stale Agent, path, and connection responses; drafts include stable Node scope', () => {
  const current = { epoch: 4, agentId: 'agent-b', path: 'src/new.md', generation: 3 }
  assert.equal(isWorkspaceRequestCurrent({ epoch: 3, agentId: 'agent-a', path: 'src/old.md' }, current), false)
  assert.equal(isWorkspaceRequestCurrent({ epoch: 4, agentId: 'agent-b', path: 'src/new.md' }, current), true)
  assert.equal(isWorkspaceRequestCurrent({ epoch: 4, agentId: 'agent-b', path: 'src/new.md', generation: 2 }, current), false)
  assert.notEqual(workspaceDraftKey('agent-a', 'src/new.md'), workspaceDraftKey('agent-b', 'src/new.md'))
  assert.notEqual(workspaceDraftKey('agent-b', 'src/new.md', 'node-a'), workspaceDraftKey('agent-b', 'src/new.md', 'node-b'))
  assert.notEqual(workspaceDraftKey('agent-b', 'src/old.md'), workspaceDraftKey('agent-b', 'src/new.md'))
})

test('write progress rejects stale Agent/request scopes and distinguishes commit uncertainty', () => {
  const current = { requestId: 'req-2', agentId: 'agent-b', path: '', scope: 'node-b', generation: 3, epoch: 5 }
  assert.equal(isWorkspaceWriteRequestCurrent(current, current), true)
  assert.equal(isWorkspaceWriteRequestCurrent({ ...current, requestId: 'req-1' }, current), false)
  assert.equal(isWorkspaceWriteRequestCurrent({ ...current, agentId: 'agent-a' }, current), false)
  assert.equal(workspaceWriteProgressLabel('preparing'), '正在准备暂存文件…')
  assert.equal(workspaceWriteProgressLabel('copying', 25, 100), '正在导入 25%')
  assert.match(workspaceWriteProgressLabel('committing'), /结果可能未知/)
})

test('workspace drafts retain their base ETag and surface external edits as conflicts', () => {
  const saved = serializeWorkspaceDraft('local edits', '"revision-a"')
  assert.deepEqual(deserializeWorkspaceDraft(saved, '"revision-a"'), { content: 'local edits', revision: '"revision-a"', conflict: false })
  assert.deepEqual(deserializeWorkspaceDraft(saved, '"revision-b"'), { content: 'local edits', revision: '"revision-a"', conflict: true })
  const formatted = serializeWorkspaceDraft('local edits', '"revision-a"', { hasBom: true, newline: '\r\n' })
  assert.deepEqual(deserializeWorkspaceDraft(formatted, '"revision-a"'), { content: 'local edits', revision: '"revision-a"', conflict: false, textFormat: { hasBom: true, newline: '\r\n' } })
})

test('media URLs are accepted only for the owning Agent and the Node media route', () => {
  assert.equal(agentMediaId('/v1/agents/agent-a/media/media_123?thumbnail=1', 'agent-a'), 'media_123')
  assert.equal(agentMediaId('/v1/agents/agent-b/media/media_123', 'agent-a'), '')
  assert.equal(agentMediaId('https://example.test/v1/agents/agent-a/media/media_123', 'agent-a'), '')
  assert.equal(agentMediaId('/v1/agents/agent-a/media/../secret', 'agent-a'), '')
})

test('workspace listing and writes use Node while file reads use the native full-read command', async () => {
  const calls = []
  globalThis.__DAGENTS_API_REQUEST__ = async options => { calls.push(['json', options]); return { path: 'src', entries: [] } }
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    calls.push([command, args])
    if (command === 'desktop_workspace_import_bytes') return { status: 'saved', path: 'src/a.md', name: 'a.md', size: 5, revision: 'r2', fileRef: 'C:/private/agent/src/a.md' }
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
    assert.deepEqual(calls[1], ['desktop_workspace_read_file', { agentId: 'agent/1', path: 'src/a.md', ifNoneMatch: null }])
    assert.match(calls[0][1].path, /agents%2F1|agents\/agent%2F1/)
    assert.equal(calls[2][0], 'desktop_workspace_import_bytes')
    assert.equal(calls[2][1].directory, 'src')
    assert.equal(calls[2][1].fileName, 'a.md')
    assert.deepEqual(calls[2][1].bytes, [104, 101, 108, 108, 111])
  } finally {
    delete globalThis.__DAGENTS_API_REQUEST__
    delete globalThis.__DAGENTS_DESKTOP_INVOKE__
  }
})

test('conditional native workspace reads preserve the 304 status without decoding it as empty file content', async () => {
  let call
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    call = { command, args }
    return { status: 304, content_type: 'application/octet-stream', etag: '"r2"', body_base64: '' }
  }
  try {
    const response = await readWorkspaceFile('agent-a', 'notes.txt', { ifNoneMatch: '"r1"' })
    assert.equal(response.notModified, true)
    assert.equal(response.status, 304)
    assert.equal(response.bytes, null)
    assert.deepEqual(call, { command: 'desktop_workspace_read_file', args: { agentId: 'agent-a', path: 'notes.txt', ifNoneMatch: '"r1"' } })
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('workspace text chunks use the native bounded read command with revision guards', async () => {
  const calls = []
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    calls.push({ command, args })
    return { status: 200, content_type: 'text/plain', etag: 'W/"rev"', file_size: 300000, next_offset: 256, body_base64: btoa('hi') }
  }
  try {
    const text = await readWorkspaceTextChunk('agent-a', 'notes.txt', 0, 256, 'W/"rev"')
    assert.equal(new TextDecoder().decode(text.bytes), 'hi')
    assert.equal(text.fileSize, 300000)
    assert.deepEqual(calls, [
      { command: 'desktop_workspace_read_chunk', args: { agentId: 'agent-a', path: 'notes.txt', offset: 0, limit: 256, revision: 'W/"rev"', text: true } },
    ])
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('native Save As and path reveal commands receive only Agent-relative paths and optional draft text', async () => {
  const calls = []
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    calls.push({ command, args })
    return command === 'desktop_workspace_save_as' ? { status: 'cancelled', bytesWritten: 0 } : undefined
  }
  try {
    const result = await saveWorkspaceFileAs('agent-a', 'docs\\notes.md', 'draft text')
    await saveWorkspaceFileAs('agent-a', 'docs/clean.md', null)
    await revealWorkspacePath('agent-a', 'docs\\notes.md')
    assert.deepEqual(result, { status: 'cancelled', bytesWritten: 0 })
    assert.deepEqual(calls, [
      { command: 'desktop_workspace_save_as', args: { agentId: 'agent-a', path: 'docs/notes.md', draftContent: 'draft text' } },
      { command: 'desktop_workspace_save_as', args: { agentId: 'agent-a', path: 'docs/clean.md', draftContent: null } },
      { command: 'desktop_workspace_reveal', args: { agentId: 'agent-a', path: 'docs/notes.md' } },
    ])
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('creating a new text file preserves an explicit UTF-8 BOM', async () => {
  let call
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    call = { command, args }
    return { status: 'saved', path: 'notes.txt', revision: 'rev' }
  }
  try {
    const bytes = new TextEncoder().encode('\uFEFFhello')
    await createWorkspaceFile('agent-a', 'notes.txt', bytes, { requestId: 'create-bom' })
    assert.equal(call.command, 'desktop_workspace_create_text')
    assert.equal(call.args.content, '\uFEFFhello')
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('cancelling during Browser File arrayBuffer prevents the import IPC call', async () => {
  let finishRead
  const controller = new AbortController()
  let calls = 0
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async () => { calls++; return { status: 'saved' } }
  try {
    const task = uploadWorkspaceFile('agent-a', '', {
      name: 'slow.txt',
      arrayBuffer: () => new Promise(resolve => { finishRead = resolve }),
    }, { requestId: 'cancel-before-prepare', signal: controller.signal })
    controller.abort()
    finishRead(new TextEncoder().encode('bytes').buffer)
    await assert.rejects(task, error => error.writeStatus === 'cancelled')
    assert.equal(calls, 0)
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('composer native import sends only Agent, directory, request ID and count, then attaches metadata refs', async () => {
  let call
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    call = { command, args }
    return { status: 'complete', results: [{ status: 'saved', path: 'notes.bin', name: 'notes.bin', fileRef: { path: 'private/notes.bin', name: 'notes.bin' }, revision: 'r1', size: 4 }] }
  }
  try {
    const result = await pickWorkspaceImport('agent-a', '', { requestId: 'req-import', maxFiles: 5 })
    assert.equal(call.command, 'desktop_workspace_pick_import')
    assert.deepEqual(call.args, { agentId: 'agent-a', directory: '', requestId: 'req-import', maxFiles: 5 })
    assert.equal(result.results[0].file_ref.path, 'private/notes.bin')
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('text save and create reject oversized UTF-8 bytes before invoking native IPC', async () => {
  const calls = []
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (...args) => { calls.push(args); return { status: 'saved' } }
  try {
    const tooLarge = new Uint8Array(16 * 1024 * 1024 + 1)
    await assert.rejects(saveWorkspaceFile('agent-a', 'large.txt', tooLarge, 'rev'), /16 MiB/)
    await assert.rejects(createWorkspaceFile('agent-a', 'large.txt', tooLarge), /16 MiB/)
    assert.deepEqual(calls, [])
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('workspace upload collisions and revision conflicts preserve actionable messages', async () => {
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async command => command === 'desktop_workspace_save_text'
    ? { status: 'conflict', httpStatus: 412 }
    : { status: 'error', httpStatus: 409, errorCode: 'workspace_file_exists' }
  try {
    await assert.rejects(
      saveWorkspaceFile('agent-1', 'notes.txt', new TextEncoder().encode('changed'), '"r1"'),
      error => error.status === 412 && error.message === '文件已被其他操作修改，请重新载入后再保存。',
    )
    await assert.rejects(
      uploadWorkspaceFile('agent-1', '', { name: 'notes.txt', arrayBuffer: async () => new ArrayBuffer(0) }),
      error => error.status === 409 && error.message === '同名文件已存在，请换名后重新导入。',
    )
  } finally {
    delete globalThis.__DAGENTS_DESKTOP_INVOKE__
  }
})

test('successful staged commits keep the raw revision and expose a quoted strong ETag', async () => {
  const revision = 'a'.repeat(64)
  let call
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async (command, args) => {
    call = { command, args }
    return { status: 'saved', path: 'notes.txt', revision, size: 5 }
  }
  try {
    const response = await saveWorkspaceFile('agent-a', 'notes.txt', new TextEncoder().encode('saved'), '"old"', { requestId: 'save-request' })
    assert.equal(response.revision, revision)
    assert.equal(response.etag, `"${revision}"`)
    assert.equal(call.args.expectedRevision, '"old"')
  } finally { delete globalThis.__DAGENTS_DESKTOP_INVOKE__ }
})

test('workspace full-file limit explains read-only preview and native Save As', async () => {
  globalThis.__DAGENTS_DESKTOP_INVOKE__ = async () => ({
    status: 413,
    content_type: 'application/json',
    etag: '',
    body_base64: btoa(JSON.stringify({ error: { code: 'workspace_file_too_large' } })),
  })
  try {
    await assert.rejects(
      readWorkspaceFile('agent-1', 'archive.bin'),
      error => error.status === 413
        && error.message.includes('完整读取和文本保存上限为 16 MiB')
        && error.message.includes('分段只读预览')
        && error.message.includes('导入上限为 1 GiB')
        && error.message.includes('拖放或粘贴导入上限为 16 MiB')
        && error.message.includes('另存为原文件'),
    )
  } finally {
    delete globalThis.__DAGENTS_DESKTOP_INVOKE__
  }
})
