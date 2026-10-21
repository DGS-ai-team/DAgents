import mammoth from 'mammoth'
import ExcelJS from 'exceljs'
import JSZip from 'jszip'
import { workspacePreviewLimits } from '../../ui/workspace-files-state.js'

const { bytes: MAX_PREVIEW_BYTES, rows: MAX_PREVIEW_ROWS, columns: MAX_PREVIEW_COLUMNS } = workspacePreviewLimits

function toArrayBuffer(value) {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value || [])
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)
}

export async function convertDocxToHtml(bytes) {
  if (bytes.byteLength > MAX_PREVIEW_BYTES) throw new Error('Word 文件超过 32 MiB，暂不生成预览；可以下载原文件查看。')
  const converter = mammoth.images.imgElement(image => image.read('base64').then(base64 => ({
    src: `data:${image.contentType};base64,${base64}`,
  })))
  const input = typeof window === 'undefined' && typeof Buffer !== 'undefined'
    ? { buffer: Buffer.from(bytes) }
    : { arrayBuffer: toArrayBuffer(bytes) }
  const result = await mammoth.convertToHtml(input, { convertImage: converter })
  return { html: result.value, warnings: result.messages.map(message => message.message) }
}

function cellText(cell) {
  const value = cell?.value
  if (value == null) return ''
  if (value instanceof Date) return value.toLocaleString()
  if (typeof value !== 'object') return String(value)
  if (Array.isArray(value.richText)) return value.richText.map(part => part.text || '').join('')
  if ('text' in value) return String(value.text || '')
  if ('result' in value && value.result != null) return String(value.result)
  if ('formula' in value) return `=${value.formula}`
  if ('error' in value) return String(value.error)
  return ''
}

function columnNumber(label) {
  let value = 0
  for (const character of label.toUpperCase()) value = value * 26 + character.charCodeAt(0) - 64
  return value
}

function parseCellReference(value) {
  const match = /^\$?([A-Z]+)\$?(\d+)$/i.exec(String(value || ''))
  return match ? { row: Number(match[2]), column: columnNumber(match[1]) } : null
}

function worksheetPreview(worksheet) {
  const merges = (worksheet.model?.merges || []).flatMap(reference => {
    const [start, end = start] = String(reference).split(':')
    const first = parseCellReference(start)
    const last = parseCellReference(end)
    return first && last ? [{ startRow: first.row, endRow: last.row, startColumn: first.column, endColumn: last.column }] : []
  })
  const fullRowCount = Math.max(worksheet.rowCount || worksheet.actualRowCount || 0, ...merges.map(range => range.endRow))
  const fullColumnCount = Math.max(worksheet.columnCount || 0, ...merges.map(range => range.endColumn))
  const rowCount = Math.min(fullRowCount, MAX_PREVIEW_ROWS)
  const columnCount = Math.min(fullColumnCount, MAX_PREVIEW_COLUMNS)
  const rows = Array.from({ length: rowCount }, (_, rowIndex) =>
    Array.from({ length: columnCount }, (_, columnIndex) => cellText(worksheet.getCell(rowIndex + 1, columnIndex + 1))))
  const visibleMerges = merges.filter(range => range.startRow <= rowCount && range.startColumn <= columnCount)
    .map(range => ({ ...range, endRow: Math.min(range.endRow, rowCount), endColumn: Math.min(range.endColumn, columnCount) }))
  return {
    name: worksheet.name,
    rows,
    merges: visibleMerges,
    truncated: fullColumnCount > MAX_PREVIEW_COLUMNS || fullRowCount > MAX_PREVIEW_ROWS,
  }
}

export async function readXlsxPreview(bytes) {
  if (bytes.byteLength > MAX_PREVIEW_BYTES) throw new Error('Excel 文件超过 32 MiB，暂不生成预览；可以下载原文件查看。')
  let workbook = new ExcelJS.Workbook()
  try {
    await workbook.xlsx.load(toArrayBuffer(bytes))
  } catch (error) {
    if (!isUnsupportedDrawingError(error)) throw error
    const drawinglessBytes = await removeDrawingParts(bytes)
    workbook = new ExcelJS.Workbook()
    await workbook.xlsx.load(toArrayBuffer(drawinglessBytes))
  }
  return workbook.worksheets.map(worksheetPreview)
}

function isUnsupportedDrawingError(error) {
  return error instanceof TypeError && /reading ['"]anchors['"]/.test(error.message || '')
}

async function removeDrawingParts(bytes) {
  const archive = await JSZip.loadAsync(toArrayBuffer(bytes))
  let changed = false

  for (const path of Object.keys(archive.files)) {
    if (!/^xl\/worksheets\/sheet\d+\.xml$/i.test(path)) continue
    const file = archive.file(path)
    if (!file) continue
    const xml = await file.async('string')
    const withoutDrawings = xml.replace(/<(?:[\w.-]+:)?drawing\b[^>]*\/\s*>/gi, '')
    if (withoutDrawings !== xml) {
      archive.file(path, withoutDrawings)
      changed = true
    }

    const relationshipsPath = path.replace(/^xl\/worksheets\//i, 'xl/worksheets/_rels/') + '.rels'
    const relationshipsFile = archive.file(relationshipsPath)
    if (relationshipsFile) {
      const relationshipsXml = await relationshipsFile.async('string')
      const withoutDrawingRelationships = relationshipsXml.replace(/<Relationship\b[^>]*\/>/gi, relationship => {
        const type = relationship.match(/\bType\s*=\s*(["'])(.*?)\1/i)?.[2] || ''
        return /\/drawing$/i.test(type) ? '' : relationship
      })
      if (withoutDrawingRelationships !== relationshipsXml) {
        archive.file(relationshipsPath, withoutDrawingRelationships)
        changed = true
      }
    }
  }

  for (const path of Object.keys(archive.files)) {
    if (/^xl\/drawings\/(?:_rels\/)?[^/]+\.xml(?:\.rels)?$/i.test(path)) {
      archive.remove(path)
      changed = true
    }
  }

  if (!changed) throw new Error('Excel 文件包含不受支持的图形结构，暂时无法预览；可以下载原文件查看。')
  return archive.generateAsync({ type: 'uint8array' })
}

export const workspaceDocumentPreviewLimits = workspacePreviewLimits
