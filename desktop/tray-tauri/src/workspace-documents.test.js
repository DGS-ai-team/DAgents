import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ExcelJS from 'exceljs'
import JSZip from 'jszip'
import { convertDocxToHtml, readXlsxPreview, validateOfficeZipLimits, workspaceDocumentPreviewLimits, workspaceOfficeZipLimits } from './workspace-documents.js'

async function zipBytes(files, options = {}) {
  const archive = new JSZip()
  for (const [name, data] of Object.entries(files)) archive.file(name, data)
  return new Uint8Array(await archive.generateAsync({ type: 'uint8array', ...options }))
}

test('DOCX preview converts paragraphs and tables to readable HTML', async () => {
  const bytes = await readFile(new URL('../node_modules/mammoth/test/test-data/single-paragraph.docx', import.meta.url))
  const result = await convertDocxToHtml(bytes)
  assert.match(result.html, /<p>Walking on imported air<\/p>/)
  assert.deepEqual(result.warnings, [])
})

test('Office preview errors use the shared 16 MiB limit', async () => {
  const oversized = new Uint8Array(workspaceDocumentPreviewLimits.bytes + 1)
  await assert.rejects(() => convertDocxToHtml(oversized), /超过 16 MiB/)
  await assert.rejects(() => readXlsxPreview(oversized), /超过 16 MiB/)
})

test('Office ZIP preflight limits entry count and declared expanded size', async () => {
  const bytes = await zipBytes({ 'a.xml': '<a/>', 'b.xml': '<b/>' })
  assert.throws(() => validateOfficeZipLimits(bytes, { ...workspaceOfficeZipLimits, entries: 1 }), /过多 ZIP 条目/)
  assert.throws(() => validateOfficeZipLimits(bytes, { ...workspaceOfficeZipLimits, totalUncompressedBytes: 4 }), /展开后超过/)
  assert.throws(() => validateOfficeZipLimits(new Uint8Array([1, 2, 3])), /ZIP 目录无效/)
})

test('Office ZIP preflight rejects suspicious compression ratios before parsers run', async () => {
  const bytes = await zipBytes({ 'xl/worksheets/sheet1.xml': 'x'.repeat(2048) }, { compression: 'DEFLATE' })
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  let eocd = -1
  for (let index = bytes.length - 22; index >= Math.max(0, bytes.length - 22 - 0xffff); index -= 1) {
    if (view.getUint32(index, true) === 0x06054b50) { eocd = index; break }
  }
  let central = view.getUint32(eocd + 16, true)
  const directoryEntries = view.getUint16(eocd + 10, true)
  for (let index = 0; index < directoryEntries; index += 1) {
    const nameLength = view.getUint16(central + 28, true)
    const extraLength = view.getUint16(central + 30, true)
    const commentLength = view.getUint16(central + 32, true)
    const name = new TextDecoder().decode(bytes.subarray(central + 46, central + 46 + nameLength))
    if (name === 'xl/worksheets/sheet1.xml') break
    central += 46 + nameLength + extraLength + commentLength
  }
  assert.notEqual(central, -1)
  const compressed = view.getUint32(central + 20, true)
  view.setUint32(central + 24, compressed * (workspaceOfficeZipLimits.compressionRatio + 1), true)
  assert.throws(() => validateOfficeZipLimits(bytes), /压缩比超过/)
})

test('XLSX preview returns sheet names, cell values, and bounded rows/columns', async () => {
  const source = new ExcelJS.Workbook()
  const sheet = source.addWorksheet('Quarterly')
  sheet.addRow(['Month', 'Revenue'])
  sheet.addRow(['January', 120])
  sheet.addRow(['February', { formula: '60*2', result: 120 }])
  const result = await readXlsxPreview(new Uint8Array(await source.xlsx.writeBuffer()))
  assert.deepEqual(result, [{ name: 'Quarterly', rows: [['Month', 'Revenue'], ['January', '120'], ['February', '120']], merges: [], truncated: false }])

  const largeWorkbook = new ExcelJS.Workbook()
  const large = largeWorkbook.addWorksheet('Large')
  for (let row = 0; row < workspaceDocumentPreviewLimits.rows + 2; row += 1) large.addRow([row, 'value'])
  const bounded = await readXlsxPreview(new Uint8Array(await largeWorkbook.xlsx.writeBuffer()))
  assert.equal(bounded[0].rows.length, workspaceDocumentPreviewLimits.rows)
  assert.equal(bounded[0].truncated, true)
})

test('XLSX preview preserves merged ranges and the blank cells covered by them', async () => {
  const source = new ExcelJS.Workbook()
  const sheet = source.addWorksheet('Summary')
  sheet.mergeCells('B2:D3')
  sheet.getCell('B2').value = 'Quarterly total'
  sheet.getCell('A4').value = 'Total'
  sheet.getCell('D4').value = 42
  const result = await readXlsxPreview(new Uint8Array(await source.xlsx.writeBuffer()))
  assert.deepEqual(result, [{
    name: 'Summary',
    rows: [
      ['', '', '', ''],
      ['', 'Quarterly total', 'Quarterly total', 'Quarterly total'],
      ['', 'Quarterly total', 'Quarterly total', 'Quarterly total'],
      ['Total', '', '', '42'],
    ],
    merges: [{ startRow: 2, endRow: 3, startColumn: 2, endColumn: 4 }],
    truncated: false,
  }])
})

test('XLSX preview keeps cell data when an unsupported default-namespace chart drawing is present', async () => {
  const source = new ExcelJS.Workbook()
  const sheet = source.addWorksheet('Summary')
  sheet.addRow(['Topic', 'Count'])
  sheet.addRow(['Open items', 4])

  const archive = await JSZip.loadAsync(await source.xlsx.writeBuffer())
  const sheetPath = 'xl/worksheets/sheet1.xml'
  const sheetXml = await archive.file(sheetPath).async('string')
  archive.file(sheetPath, sheetXml.replace('</worksheet>', '<drawing r:id="rIdDrawing"/></worksheet>'))
  archive.file('xl/worksheets/_rels/sheet1.xml.rels',
    '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="/xl/drawings/drawing1.xml" Id="rIdDrawing"/></Relationships>')
  archive.file('xl/drawings/drawing1.xml',
    '<wsDr xmlns="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"><oneCellAnchor><from><col>0</col><row>0</row></from><ext cx="1" cy="1"/><graphicFrame><xfrm/><graphic><graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"/></graphic></graphicFrame></oneCellAnchor></wsDr>')
  const bytes = await archive.generateAsync({ type: 'uint8array' })

  const result = await readXlsxPreview(bytes)
  assert.deepEqual(result, [{ name: 'Summary', rows: [['Topic', 'Count'], ['Open items', '4']], merges: [], truncated: false }])
})
