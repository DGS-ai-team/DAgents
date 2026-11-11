import test from 'node:test'
import assert from 'node:assert/strict'
import { composeWorkspaceMessage, createWorkspaceReference, dispatchWorkspaceMessage, formatWorkspaceReferencesMessage, normalizeWorkspaceReferenceAnchor, settleWorkspaceReferences, textRangeAnchor, workspaceReferencesForAgent } from './workspace-references.js'

test('workspace references retain a normalized file and selected content anchor', () => {
  const reference = createWorkspaceReference({
    id: 'ref-1',
    path: './reports/plan.xlsx',
    fileRef: 'node-file-ref',
    revision: 'etag-12',
    draft: true,
    anchor: { type: 'sheet-range', label: 'Summary!B2:B4', quote: '10\n20\n30', location: { sheet: 'Summary', range: 'B2:B4', ignored: { value: 1 } } },
  })
  assert.equal(reference.id, 'ref-1')
  assert.equal(reference.path, 'reports/plan.xlsx')
  assert.equal(reference.fileRef, 'node-file-ref')
  assert.equal(reference.draft, true)
  assert.equal(reference.annotation, '')
  assert.deepEqual(reference.anchor.location, { sheet: 'Summary', range: 'B2:B4' })
})

test('workspace annotations stay in structured reference metadata instead of user-visible text', () => {
  const reference = createWorkspaceReference({
    path: 'reports/plan.xlsx',
    annotation: '检查这个单元格的公式\n保留原有格式',
    anchor: { type: 'sheet-range', label: 'Summary!B2', quote: '10', location: { sheet: 'Summary', range: 'B2' } },
  })
  assert.equal(reference.annotation, '检查这个单元格的公式\n保留原有格式')
  const message = formatWorkspaceReferencesMessage({ text: '请按批注处理', references: [reference] })
  assert.equal(message, '请按批注处理')
})

test('workspace image references only retain supported inline raster image data', () => {
  const raster = createWorkspaceReference({ path: 'images/mock.png', imageUrl: 'data:image/png;base64,aGVsbG8=' })
  const vector = createWorkspaceReference({ path: 'images/mock.svg', imageUrl: 'data:image/svg+xml;base64,PHN2Zz4=' })
  assert.equal(raster.imageUrl, 'data:image/png;base64,aGVsbG8=')
  assert.equal(vector.imageUrl, '')
})

test('text range anchors preserve a quote and one-based line and column locations', () => {
  assert.deepEqual(textRangeAnchor('first\nsecond line\nthird', 6, 12), {
    type: 'text-range',
    label: '文本选区 · 第 2–2 行',
    quote: 'second',
    location: { start: 6, end: 12, startLine: 2, startColumn: 1, endLine: 2, endColumn: 7 },
  })
  assert.equal(textRangeAnchor('text', 1, 1).type, 'file')
})

test('reference metadata is structured and the original user text remains unchanged', () => {
  const message = formatWorkspaceReferencesMessage({
    text: '请修改这个单元格的公式',
    references: [{ path: 'reports/plan.xlsx', revision: 'etag-12', anchor: { type: 'sheet-range', label: 'Summary!B2:B4', quote: '10\n20\n30', location: { sheet: 'Summary', range: 'B2:B4' } } }],
  })
  assert.equal(message, '请修改这个单元格的公式')
})

test('reference-only messages use a short fallback while keeping reference details structured', () => {
  const message = formatWorkspaceReferencesMessage({
    references: [{ path: 'notes.md', anchor: { type: 'text-range', label: '文本选区', quote: 'selected text' } }],
  })
  assert.equal(message, '请结合我引用的工作区内容处理。')
})

test('full-chat and dock sends preserve structured refs while deduplicating plain attachment paths', () => {
  const reference = createWorkspaceReference({ path: 'notes.md', fileRef: 'C:/workspace/notes.md', name: 'notes.md', anchor: { type: 'text-range', label: '文本选区', quote: 'selected text' } })
  const imageReference = createWorkspaceReference({ path: 'images/chart.png', fileRef: 'C:/workspace/images/chart.png', name: 'chart.png', imageUrl: 'data:image/png;base64,Y2hhcnQ=' })
  const message = composeWorkspaceMessage({
    references: [reference, imageReference],
    files: [{ path: 'notes.md', name: 'notes.md' }],
    images: [{ name: 'chart.png', url: imageReference.imageUrl }],
  })

  assert.equal(message.text, '请结合我引用的工作区内容处理。')
  assert.deepEqual(message.files, [
    { id: reference.id, path: 'C:/workspace/notes.md', displayPath: 'notes.md', name: 'notes.md', revision: '', draft: false, annotation: '', anchor: reference.anchor },
    { id: imageReference.id, path: 'C:/workspace/images/chart.png', displayPath: 'images/chart.png', name: 'chart.png', revision: '', draft: false, annotation: '', anchor: imageReference.anchor },
  ])
  assert.deepEqual(message.images, [{ name: 'chart.png', url: imageReference.imageUrl }])
})

test('durable file_ref is sent, display path stays relative, and attachments merge across path forms', () => {
  const reference = createWorkspaceReference({
    id: 'workspace-ref',
    path: 'reports/plan.csv',
    fileRef: 'C:/Users/Agent/.runtime/workspaces/agent/reports/plan.csv',
    name: 'plan.csv',
    anchor: { type: 'sheet-range', label: 'B2:B4', location: { range: 'B2:B4' } },
  })
  const message = composeWorkspaceMessage({
    references: [reference],
    files: [{ path: 'c:\\Users\\Agent\\.runtime\\workspaces\\agent\\reports\\plan.csv', name: 'plan.csv' }],
  })

  assert.equal(message.files.length, 1)
  assert.equal(message.files[0].path, reference.fileRef)
  assert.equal(message.files[0].displayPath, 'reports/plan.csv')
  assert.equal(message.files[0].anchor.label, 'B2:B4')
})

test('multiple anchors on one durable file remain distinct beside a plain attachment', () => {
  const fileRef = 'C:/workspace/reports/plan.csv'
  const first = createWorkspaceReference({ id: 'range-a', path: 'reports/plan.csv', fileRef, anchor: { type: 'sheet-range', label: 'A1:A2' } })
  const second = createWorkspaceReference({ id: 'range-b', path: 'reports/plan.csv', fileRef, anchor: { type: 'sheet-range', label: 'C1:C2' } })
  const message = composeWorkspaceMessage({ references: [first, second], files: [{ path: fileRef, name: 'plan.csv' }] })

  assert.equal(message.files.length, 2)
  assert.deepEqual(message.files.map(file => file.path), [fileRef, fileRef])
  assert.deepEqual(message.files.map(file => file.anchor.label), ['A1:A2', 'C1:C2'])
})

test('same file path with distinct anchors remains separate in structured file refs', () => {
  const first = createWorkspaceReference({ id: 'range-a', path: 'reports/plan.csv', anchor: { type: 'sheet-range', label: 'A1:A2', quote: '1\n2', location: { range: 'A1:A2' } } })
  const second = createWorkspaceReference({ id: 'range-b', path: 'reports/plan.csv', anchor: { type: 'sheet-range', label: 'C1:C2', quote: '3\n4', location: { range: 'C1:C2' } } })
  const message = composeWorkspaceMessage({ text: '对比两处', references: [first, second] })

  assert.equal(message.text, '对比两处')
  assert.equal(message.files.length, 2)
  assert.deepEqual(message.files.map(file => file.anchor.label), ['A1:A2', 'C1:C2'])
})

test('dispatch sends raw user text and structured metadata in the file_refs argument', async () => {
  const reference = createWorkspaceReference({
    id: 'cell-a1',
    path: 'reports/plan.csv',
    revision: 'rev-7',
    draft: true,
    annotation: '核对合计',
    anchor: { type: 'sheet-range', label: 'B2:B4', quote: '12\n14\n18', location: { sheet: 'Summary', range: 'B2:B4' } },
  })
  let submitted
  await dispatchWorkspaceMessage({ agentId: 'agent-a', text: '请复核这个范围', references: [reference] }, async (...args) => { submitted = args })

  assert.equal(submitted[0], 'agent-a')
  assert.equal(submitted[1], '请复核这个范围')
  assert.deepEqual(submitted[3], [{
    id: 'cell-a1',
    path: 'reports/plan.csv',
    name: 'plan.csv',
    revision: 'rev-7',
    draft: true,
    annotation: '核对合计',
    anchor: { type: 'sheet-range', label: 'B2:B4', quote: '12\n14\n18', location: { sheet: 'Summary', range: 'B2:B4' } },
  }])
})

test('pending references remain visible per agent, survive failed sends, and clear only sent references on success', () => {
  const first = createWorkspaceReference({ id: 'first', path: 'first.md' })
  const addedWhileSending = createWorkspaceReference({ id: 'later', path: 'later.md' })
  const otherAgent = createWorkspaceReference({ id: 'other', path: 'other.md' })
  const byAgent = { agentA: [first, addedWhileSending], agentB: [otherAgent] }

  assert.deepEqual(workspaceReferencesForAgent(byAgent, 'agentA'), [first, addedWhileSending])
  assert.deepEqual(workspaceReferencesForAgent(byAgent, 'agentB'), [otherAgent])
  assert.equal(settleWorkspaceReferences(byAgent, 'agentA', [first], false), byAgent)
  assert.deepEqual(settleWorkspaceReferences(byAgent, 'agentA', [first], true), {
    agentA: [addedWhileSending],
    agentB: [otherAgent],
  })
})

test('unsafe workspace paths are rejected and unknown anchors degrade to a whole file', () => {
  assert.throws(() => createWorkspaceReference({ path: '../private.txt' }), /路径无效/)
  assert.throws(() => createWorkspaceReference({ path: 'C:/private.txt' }), /路径无效/)
  assert.deepEqual(normalizeWorkspaceReferenceAnchor({ type: 'script', label: 'x', quote: 'y' }), {
    type: 'file', label: 'x', quote: 'y', location: {},
  })
})
