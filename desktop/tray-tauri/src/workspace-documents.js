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
