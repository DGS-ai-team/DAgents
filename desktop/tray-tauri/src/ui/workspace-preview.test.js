import test from 'node:test'
import assert from 'node:assert/strict'
import { workspaceCodeLanguage, workspaceFilePreviewNotice, workspacePreviewKind, workspacePreviewLimits } from './workspace-files-state.js'

const text = value => new TextEncoder().encode(value)

test('workspace preview recognizes supported formats by extension and file signature', () => {
  assert.equal(workspacePreviewKind('notes.pdf', 'application/octet-stream', text('%PDF-1.7')), 'pdf')
  assert.equal(workspacePreviewKind('report.docx', 'application/octet-stream', Uint8Array.from([0x50, 0x4b, 0x03, 0x04])), 'docx')
  assert.equal(workspacePreviewKind('budget.xlsx', 'application/octet-stream', Uint8Array.from([0x50, 0x4b, 0x03, 0x04])), 'xlsx')
  assert.equal(workspacePreviewKind('page.html', 'text/plain', text('<h1>safe</h1>')), 'html')
  assert.equal(workspacePreviewKind('graph.svg', 'application/octet-stream', text('<svg xmlns="http://www.w3.org/2000/svg"></svg>')), 'svg')
  assert.equal(workspacePreviewKind('picture.png', 'application/octet-stream', Uint8Array.from([137,80,78,71,13,10,26,10])), 'image')
  assert.equal(workspacePreviewKind('picture.bmp', 'application/octet-stream', Uint8Array.from([0x42,0x4d,0,0,0,0,0,0,0,0,0,0,0,0])), 'image')
  assert.equal(workspacePreviewKind('favicon.ico', 'application/octet-stream', Uint8Array.from([0,0,1,0])), 'image')
  assert.equal(workspacePreviewKind('main.go', 'text/plain', text('package main')), 'code')
  assert.equal(workspacePreviewKind('table.csv', 'text/plain', text('name,value\na,1')), 'csv')
  assert.equal(workspacePreviewKind('notes.md', 'text/plain', text('# Notes')), 'markdown')
  assert.equal(workspacePreviewLimits.bytes, 16 * 1024 * 1024)
  assert.equal(workspacePreviewLimits.textChunkBytes, 256 * 1024)
})

test('workspace preview reports malformed and legacy Office files as unsupported', () => {
  assert.equal(workspacePreviewKind('bad.pdf', 'application/pdf', text('not a pdf')), 'unsupported-invalid')
  assert.match(workspaceFilePreviewNotice('bad.pdf', 'unsupported-invalid'), /格式无效或内容不完整/)
  assert.equal(workspacePreviewKind('old.doc', 'application/msword', Uint8Array.from([0xd0,0xcf,0x11,0xe0])), 'unsupported-legacy-office')
  assert.equal(workspacePreviewKind('old.xls', 'application/vnd.ms-excel', Uint8Array.from([0xd0,0xcf,0x11,0xe0])), 'unsupported-legacy-office')
  assert.match(workspaceFilePreviewNotice('old.doc', 'unsupported-legacy-office'), /另存为 \.docx/)
  assert.match(workspaceFilePreviewNotice('archive.bin', 'unsupported'), /\.bin 格式/)
})

test('workspace source preview maps common source files to readable syntax languages', () => {
  assert.equal(workspaceCodeLanguage('main.go'), 'go')
  assert.equal(workspaceCodeLanguage('component.vue'), '')
  assert.equal(workspaceCodeLanguage('settings.yaml'), 'yaml')
  assert.equal(workspaceCodeLanguage('script.ps1'), 'powershell')
})
