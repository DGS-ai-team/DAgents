import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ExcelJS from 'exceljs'
import { convertDocxToHtml, readXlsxPreview, workspaceDocumentPreviewLimits } from './workspace-documents.js'

test('DOCX preview converts paragraphs and tables to readable HTML', async () => {
  const bytes = await readFile(new URL('../node_modules/mammoth/test/test-data/single-paragraph.docx', import.meta.url))
  const result = await convertDocxToHtml(bytes)
  assert.match(result.html, /<p>Walking on imported air<\/p>/)
  assert.deepEqual(result.warnings, [])
})

test('XLSX preview returns sheet names, cell values, and bounded rows/columns', async () => {
  const source = new ExcelJS.Workbook()
  const sheet = source.addWorksheet('Quarterly')
  sheet.addRow(['Month', 'Revenue'])
  sheet.addRow(['January', 120])
  sheet.addRow(['February', { formula: '60*2', result: 120 }])
  const result = await readXlsxPreview(new Uint8Array(await source.xlsx.writeBuffer()))
  assert.deepEqual(result, [{ name: 'Quarterly', rows: [['Month', 'Revenue'], ['January', '120'], ['February', '120']], truncated: false }])

  const largeWorkbook = new ExcelJS.Workbook()
  const large = largeWorkbook.addWorksheet('Large')
  for (let row = 0; row < workspaceDocumentPreviewLimits.rows + 2; row += 1) large.addRow([row, 'value'])
  const bounded = await readXlsxPreview(new Uint8Array(await largeWorkbook.xlsx.writeBuffer()))
  assert.equal(bounded[0].rows.length, workspaceDocumentPreviewLimits.rows)
  assert.equal(bounded[0].truncated, true)
})
