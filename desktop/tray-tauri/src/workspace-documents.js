import mammoth from 'mammoth'
import ExcelJS from 'exceljs'
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

function worksheetPreview(worksheet) {
  const columnCount = Math.min(worksheet.columnCount || 0, MAX_PREVIEW_COLUMNS)
  const rows = []
  let truncated = (worksheet.columnCount || 0) > MAX_PREVIEW_COLUMNS || (worksheet.actualRowCount || 0) > MAX_PREVIEW_ROWS
  worksheet.eachRow({ includeEmpty: false }, (row, rowNumber) => {
    if (rowNumber > MAX_PREVIEW_ROWS) { truncated = true; return }
    rows.push(Array.from({ length: columnCount }, (_, index) => cellText(row.getCell(index + 1))))
  })
  return { name: worksheet.name, rows, truncated }
}

export async function readXlsxPreview(bytes) {
  if (bytes.byteLength > MAX_PREVIEW_BYTES) throw new Error('Excel 文件超过 32 MiB，暂不生成预览；可以下载原文件查看。')
  const workbook = new ExcelJS.Workbook()
  await workbook.xlsx.load(toArrayBuffer(bytes))
  return workbook.worksheets.map(worksheetPreview)
}

export const workspaceDocumentPreviewLimits = workspacePreviewLimits
