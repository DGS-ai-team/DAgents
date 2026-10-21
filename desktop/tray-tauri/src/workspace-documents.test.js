import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ExcelJS from 'exceljs'
import JSZip from 'jszip'
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
  assert.deepEqual(result, [{ name: 'Summary', rows: [['Topic', 'Count'], ['Open items', '4']], truncated: false }])
})
