<script setup>
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import UiIcon from '@desktop-ui/components/UiIcon.vue'
import { confirmDesktopAction } from '@desktop-ui/utils/desktopDialogs.js'
import DOMPurify from 'dompurify'
import { validateWorkspacePath } from '@desktop-ui/utils/resourceContext.js'
import folderOpenIcon from '@shared-icons/desktop/folder-open.svg?url'
import { createWorkspaceFile, downloadBytes, listWorkspace, readWorkspaceFile, saveWorkspaceFile, uploadWorkspaceFile } from '../workspace-files-api.js'
import { deserializeWorkspaceDraft, isWorkspaceRequestCurrent, normalizeWorkspacePath, parseWorkspaceCsvPreview, serializeWorkspaceDraft, workspaceCodeLanguage, workspaceContentType, workspaceDraftKey, workspaceFileLocation, workspaceFilePreviewNotice, workspacePreviewKind, workspacePreviewLimits } from '../workspace-files-state.js'
import { textRangeAnchor } from '../workspace-references.js'
const PdfPreview = defineAsyncComponent(() => import('../../components/WorkspacePdfPreview.vue'))

const props = defineProps({ agentId: { type: String, required: true }, active: { type: Boolean, default: false }, workspaceScope: { type: String, default: 'default' }, requestGeneration: { type: Number, default: 0 }, dockCollapsed: { type: Boolean, default: false }, dockStatus: { type: String, default: 'idle' }, dockAgentName: { type: String, default: '当前智能体' }, attachmentLimitReached: { type: Boolean, default: false } })
const emit = defineEmits(['resource-name', 'attach-file', 'annotation-update', 'annotation-remove', 'expand-agent-dock'])
const entries = ref([])
const currentPath = ref('')
const selectedPath = ref('')
const content = ref('')
const fileBytes = ref(null)
const fileContentType = ref('')
const revision = ref('')
const dirty = ref(false)
const draftConflict = ref(false)
const externalUpdate = ref(false)
const fileMissing = ref(false)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const refreshError = ref('')
const uploadInput = ref(null)
const toolbarMenu = ref(null)
const dockRestoreButton = ref(null)
const previewUrl = ref('')
const svgPreviewUrl = ref('')
const previewKind = ref('')
const previewMode = ref('preview')
const documentHtml = ref('')
const documentWarnings = ref([])
const documentPreviewError = ref('')
const highlightedContent = ref('')
const workbookSheets = ref([])
const workbookSheetName = ref('')
const spreadsheetSelection = ref(null)
const spreadsheetDragStart = ref(null)
const imageSelection = ref(null)
const imageDragStart = ref(null)
const documentFrame = ref(null)
const referenceMenu = ref(null)
const referenceMenuElement = ref(null)
const annotations = ref([])
const annotationInputRefs = new Map()
const textAnchor = ref(null)
const pdfAnchor = ref(null)
const fileRefs = ref({})
const editorRef = ref(null)
const createNameDialogOpen = ref(false)
const newFileName = ref('')
const newFileNameError = ref('')
const newFileNameInput = ref(null)
const draftOwnerId = ref('')
const draftOwnerScope = ref('')
const folderMaskStyle = { maskImage: `url("${folderOpenIcon}")`, WebkitMaskImage: `url("${folderOpenIcon}")` }
const dockStatusLabel = computed(() => ({ offline: '离线', pending: '待审批', generating: '生成中', idle: '空闲' }[props.dockStatus] || props.dockStatus))
let requestEpoch = 0
let contentEditVersion = 0
let refreshTimer = 0
let missingConfirmTimer = 0
let refreshInFlight = false
let refreshQueued = false
let refreshQueuedReason = ''
let refreshRetry = 0
let lastDirectoryCheck = 0
let lastFileCheck = 0
let disposed = false
let fileReadSequence = 0
let refreshAfterSave = false
let saveSequence = 0
let highlightSequence = 0
const filename = computed(() => selectedPath.value.split('/').filter(Boolean).at(-1) || '')
const canEditText = computed(() => Boolean(selectedPath.value) && ['text/plain', 'text/html', 'application/json', 'application/xml', 'application/javascript'].includes(fileContentType.value) && !['pdf', 'docx', 'xlsx', 'image', 'unsupported', 'unsupported-invalid', 'unsupported-legacy-office'].includes(previewKind.value))
const canTogglePreview = computed(() => canEditText.value && ['code', 'csv', 'html', 'svg'].includes(previewKind.value))
const canOverwriteDraft = computed(() => dirty.value && canEditText.value && !fileMissing.value && (draftConflict.value || externalUpdate.value))
const workbookPreview = computed(() => workbookSheets.value.find(sheet => sheet.name === workbookSheetName.value) || null)
const workbookColumnCount = computed(() => Math.max(0, ...(workbookPreview.value?.rows || []).map(row => row.length)))
const workbookGrid = computed(() => {
  if (!workbookPreview.value) return []
  const sheet = workbookPreview.value
  const rowCount = sheet.rows.length
  const columnCount = Math.max(0, ...sheet.rows.map(row => row.length))
  const covered = new Set()
  const masters = new Map()
  for (const merge of sheet.merges || []) {
    const startRow = Math.max(1, merge.startRow)
    const endRow = Math.min(rowCount, merge.endRow)
    const startColumn = Math.max(1, merge.startColumn)
    const endColumn = Math.min(columnCount, merge.endColumn)
    masters.set(`${startRow}:${startColumn}`, { rowspan: endRow - startRow + 1, colspan: endColumn - startColumn + 1 })
    for (let row = startRow; row <= endRow; row += 1) {
      for (let column = startColumn; column <= endColumn; column += 1) {
        if (row !== startRow || column !== startColumn) covered.add(`${row}:${column}`)
      }
    }
  }
  return sheet.rows.map((row, rowIndex) => Array.from({ length: columnCount }, (_, columnIndex) => {
    const value = row[columnIndex] ?? ''
    const rowNumber = rowIndex + 1
    const columnNumber = columnIndex + 1
    const address = `${xlsxColumnLabel(columnNumber)}${rowNumber}`
    const merge = masters.get(`${rowNumber}:${columnNumber}`)
    return { value, address, rowNumber, columnNumber, covered: covered.has(`${rowNumber}:${columnNumber}`), rowspan: merge?.rowspan || 1, colspan: merge?.colspan || 1, selected: spreadsheetCellSelected(rowNumber, columnNumber) }
  }))
})
const activeReferenceAnchor = computed(() => pdfAnchor.value || imageSelection.value || spreadsheetAnchor() || textAnchor.value || { type: 'file', label: '整个文件', quote: '', location: {} })
const unsupportedPreviewNotice = computed(() => previewKind.value.startsWith('unsupported') ? workspaceFilePreviewNotice(filename.value, previewKind.value) : '')
const draftStatusText = computed(() => fileMissing.value
  ? '工作区文件已移除，本地草稿仍保留。'
  : draftConflict.value || externalUpdate.value
    ? '工作区文件已有新版本，本地草稿仍保留。'
    : '有未保存的本地修改。')
const safeDocumentPreview = computed(() => {
  const raw = previewKind.value === 'docx' ? documentHtml.value : content.value
  if (!raw || !['docx', 'html'].includes(previewKind.value)) return ''
  const sanitized = DOMPurify.sanitize(raw, { USE_PROFILES: { html: true } })
  const contentSecurityPolicy = "default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; font-src data:;"
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${contentSecurityPolicy}"><meta name="referrer" content="no-referrer"><style>html,body{margin:0;min-height:100%;background:#fff;color:#30383c;font:14px/1.65 system-ui,"Segoe UI",sans-serif}body{box-sizing:border-box;padding:24px;overflow-wrap:anywhere}img{max-width:100%;height:auto}table{max-width:100%;border-collapse:collapse}td,th{padding:6px 8px;border:1px solid #d9e0e2}</style></head><body>${sanitized}</body></html>`
})
const breadcrumbs = computed(() => currentPath.value ? currentPath.value.split('/').filter(Boolean) : [])
const noticeDetails = computed(() => [
  draftConflict.value ? '本地草稿基于较旧文件版本；保存会被拒绝。可以从资源菜单丢弃草稿并重新载入。' : '',
  fileMissing.value ? '此文件已从工作区移除；本地草稿仍保留，保存不会自动重新创建文件。可以从资源菜单丢弃草稿并重试。' : '',
  error.value ? `文件操作错误：${error.value}` : '',
  refreshError.value ? `刷新错误：${refreshError.value}` : '',
  externalUpdate.value ? '工作区文件已在其他位置更新；当前编辑内容和版本基线已保留。可以从资源菜单丢弃草稿并重新载入。' : '',
].filter(Boolean))
const noticeSummary = computed(() => draftConflict.value ? '文件版本冲突' : fileMissing.value ? '文件已从工作区移除' : error.value || refreshError.value ? '工作区文件操作失败' : externalUpdate.value ? '工作区文件已更新' : '')

function workspacePath(parent, child) { return [parent, child].filter(Boolean).join('/') }
function revokePreview() {
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
  if (svgPreviewUrl.value) URL.revokeObjectURL(svgPreviewUrl.value)
  previewUrl.value = ''
  svgPreviewUrl.value = ''
}
function refreshSvgPreview() {
  if (svgPreviewUrl.value) URL.revokeObjectURL(svgPreviewUrl.value)
  svgPreviewUrl.value = ''
  if (previewKind.value !== 'svg' || !content.value) return
  const sanitized = DOMPurify.sanitize(content.value, { USE_PROFILES: { svg: true }, FORBID_TAGS: ['script', 'foreignObject'] })
  if (/<svg\b/i.test(sanitized)) svgPreviewUrl.value = URL.createObjectURL(new Blob([sanitized], { type: 'image/svg+xml' }))
}
function resetPreviewData() {
  previewKind.value = ''
  previewMode.value = 'preview'
  documentHtml.value = ''
  documentWarnings.value = []
  documentPreviewError.value = ''
  workbookSheets.value = []
  workbookSheetName.value = ''
  spreadsheetSelection.value = null
  spreadsheetDragStart.value = null
  imageSelection.value = null
  imageDragStart.value = null
  textAnchor.value = null
  pdfAnchor.value = null
  referenceMenu.value = null
}
function defaultPreviewMode(kind) { return ['code', 'csv', 'html', 'svg'].includes(kind) ? 'preview' : 'source' }
function xlsxColumnLabel(index) {
  let value = index
  let label = ''
  while (value > 0) { value--; label = String.fromCharCode(65 + value % 26) + label; value = Math.floor(value / 26) }
  return label
}
function cellAddress(row, column) { return `${xlsxColumnLabel(column)}${row}` }
function spreadsheetCellSelected(row, column) {
  const selection = spreadsheetSelection.value
  if (!selection) return false
  return row >= Math.min(selection.start.row, selection.end.row) && row <= Math.max(selection.start.row, selection.end.row)
    && column >= Math.min(selection.start.column, selection.end.column) && column <= Math.max(selection.start.column, selection.end.column)
}
function spreadsheetAnchor() {
  const selection = spreadsheetSelection.value
  if (!selection || !workbookPreview.value) return null
  const startRow = Math.min(selection.start.row, selection.end.row)
  const endRow = Math.max(selection.start.row, selection.end.row)
  const startColumn = Math.min(selection.start.column, selection.end.column)
  const endColumn = Math.max(selection.start.column, selection.end.column)
  const firstCell = cellAddress(startRow, startColumn)
  const lastCell = cellAddress(endRow, endColumn)
  const range = firstCell === lastCell ? firstCell : `${firstCell}:${lastCell}`
  const quote = workbookPreview.value.rows.slice(startRow - 1, endRow).map(row => row.slice(startColumn - 1, endColumn).join('\t')).join('\n')
  const sheetName = workbookPreview.value.name
  return {
    type: 'sheet-range',
    label: `${sheetName}!${range}`,
    quote,
    location: { sheet: sheetName, range, format: previewKind.value },
  }
}
function startSpreadsheetSelection(event, cell) {
  if (event.button !== 0) return
  event.preventDefault()
  const position = { row: cell.rowNumber, column: cell.columnNumber }
  spreadsheetDragStart.value = position
  const start = event.shiftKey && spreadsheetSelection.value ? spreadsheetSelection.value.start : position
  spreadsheetSelection.value = { start, end: position }
  textAnchor.value = null
  pdfAnchor.value = null
  imageSelection.value = null
}
function continueSpreadsheetSelection(cell) {
  if (!spreadsheetDragStart.value) return
  spreadsheetSelection.value = { start: spreadsheetDragStart.value, end: { row: cell.rowNumber, column: cell.columnNumber } }
}
function stopSpreadsheetSelection() { spreadsheetDragStart.value = null }
function captureTextSelection(event) {
  const target = event?.target
  if (!target || typeof target.selectionStart !== 'number' || typeof target.selectionEnd !== 'number') return
  const anchor = textRangeAnchor(content.value, target.selectionStart, target.selectionEnd)
  textAnchor.value = anchor.quote ? anchor : null
  spreadsheetSelection.value = null
  pdfAnchor.value = null
  imageSelection.value = null
}
function captureCodeSelection() {
  const quote = window.getSelection?.()?.toString().trim()
  if (!quote) { textAnchor.value = null; return }
  const start = content.value.indexOf(quote)
  textAnchor.value = start >= 0
    ? textRangeAnchor(content.value, start, start + quote.length, '代码选区')
    : { type: 'text-range', label: '代码选区', quote: quote.slice(0, 2000), location: {} }
  spreadsheetSelection.value = null
  pdfAnchor.value = null
  imageSelection.value = null
}
function captureRenderedTextSelection(text, type = 'document-text', location = {}) {
  const quote = String(text || '').trim()
  if (!quote) return
  const frameDocument = documentFrame.value?.contentDocument
  const fullText = frameDocument?.body?.innerText || ''
  const start = fullText.indexOf(quote)
  textAnchor.value = {
    type,
    label: type === 'html-region' ? 'HTML 预览文本' : '文档段落文本',
    quote: quote.slice(0, 2000),
    location: { ...location, ...(start >= 0 ? { start, end: start + quote.length, before: fullText.slice(Math.max(0, start - 100), start), after: fullText.slice(start + quote.length, start + quote.length + 100) } : {}) },
  }
  spreadsheetSelection.value = null
  pdfAnchor.value = null
  imageSelection.value = null
}
function domElementPath(element, root) {
  const segments = []
  let current = element
  while (current && current !== root && current.nodeType === 1 && segments.length < 8) {
    const tag = current.tagName.toLowerCase()
    const siblings = current.parentElement ? Array.from(current.parentElement.children).filter(child => child.tagName === current.tagName) : []
    segments.unshift(`${tag}:nth-of-type(${Math.max(1, siblings.indexOf(current) + 1)})`)
    current = current.parentElement
  }
  return segments.join(' > ')
}
function readDocumentFrameSelection() {
  const frameDocument = documentFrame.value?.contentDocument
  const selection = frameDocument?.getSelection?.()
  const quote = selection?.toString().trim()
  if (!quote) return
  const range = selection.getRangeAt(0)
  const element = range.startContainer.nodeType === 1 ? range.startContainer : range.startContainer.parentElement
  const type = previewKind.value === 'html' ? 'html-region' : 'document-text'
  captureRenderedTextSelection(quote, type, { block: domElementPath(element, frameDocument.body) })
}
function onDocumentFrameLoad() {
  try {
    const frameDocument = documentFrame.value?.contentDocument
    frameDocument?.addEventListener('mouseup', readDocumentFrameSelection)
    frameDocument?.addEventListener('contextmenu', event => {
      event.preventDefault()
      readDocumentFrameSelection()
      const bounds = documentFrame.value?.getBoundingClientRect()
      openReferenceMenu({
        clientX: (bounds?.left || 0) + event.clientX,
        clientY: (bounds?.top || 0) + event.clientY,
        preventDefault() {},
      })
    })
  } catch { /* Sandboxed previews may deny parent access. */ }
}
function capturePdfSelection(anchor) {
  pdfAnchor.value = anchor
  if (anchor) {
    spreadsheetSelection.value = null
    imageSelection.value = null
    textAnchor.value = null
  }
}
function imagePoint(event) {
  const bounds = event.currentTarget.getBoundingClientRect()
  return { x: Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width)), y: Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height)) }
}
function startImageSelection(event) {
  if (event.button !== 0) return
  event.preventDefault()
  event.currentTarget.setPointerCapture?.(event.pointerId)
  const point = imagePoint(event)
  imageDragStart.value = point
  imageSelection.value = { type: 'image-region', label: '图片区域', quote: '', location: { x: point.x, y: point.y, width: 0, height: 0 } }
  spreadsheetSelection.value = null
  textAnchor.value = null
  pdfAnchor.value = null
}
function moveImageSelection(event) {
  if (!imageDragStart.value) return
  const point = imagePoint(event)
  const x = Math.min(point.x, imageDragStart.value.x)
  const y = Math.min(point.y, imageDragStart.value.y)
  const width = Math.abs(point.x - imageDragStart.value.x)
  const height = Math.abs(point.y - imageDragStart.value.y)
  imageSelection.value = { type: 'image-region', label: '图片区域', quote: '', location: { x, y, width, height } }
}
function stopImageSelection(event) {
  if (!imageDragStart.value) return
  moveImageSelection(event)
  imageDragStart.value = null
  const location = imageSelection.value?.location
  if (!location || location.width < 0.012 || location.height < 0.012) imageSelection.value = null
  else imageSelection.value.label = `图片区域 · ${Math.round(location.x * 100)}%, ${Math.round(location.y * 100)}%`
}
function imageSelectionStyle() {
  const location = imageSelection.value?.location
  return location ? { left: `${location.x * 100}%`, top: `${location.y * 100}%`, width: `${location.width * 100}%`, height: `${location.height * 100}%` } : {}
}
function imageBytesAsDataUrl(bytes, mimeType) {
  if (!bytes?.byteLength || bytes.byteLength > 10 * 1024 * 1024 || !['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(mimeType)) return Promise.resolve('')
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error || new Error('无法读取图片引用'))
    reader.readAsDataURL(new Blob([new Uint8Array(bytes)], { type: mimeType }))
  })
}
function columnNumberFromLabel(label) {
  return [...String(label || '')].reduce((value, character) => value * 26 + character.charCodeAt(0) - 64, 0)
}
function openReferenceMenu(event) {
  if (!selectedPath.value || loading.value) return
  event?.preventDefault?.()
  const target = event?.target
  if (previewKind.value === 'xlsx' || previewKind.value === 'csv') {
    const cell = target?.closest?.('[data-cell-address]')
    const match = cell?.dataset?.cellAddress?.match(/^([A-Z]+)(\d+)$/)
    if (match) {
      const position = { row: Number(match[2]), column: columnNumberFromLabel(match[1]) }
      if (!spreadsheetCellSelected(position.row, position.column)) spreadsheetSelection.value = { start: position, end: position }
      textAnchor.value = null
      imageSelection.value = null
      pdfAnchor.value = null
    }
  } else if (target?.selectionStart !== undefined && target?.selectionEnd !== undefined) {
    captureTextSelection({ target })
  } else if (previewKind.value === 'code') {
    captureCodeSelection()
  } else if (['image', 'svg'].includes(previewKind.value) && !imageSelection.value) {
    imageSelection.value = { type: 'image-region', label: '整张图片', quote: '', location: { x: 0, y: 0, width: 1, height: 1 } }
  }
  const anchor = activeReferenceAnchor.value
  const x = Number(event?.clientX) || 0
  const y = Number(event?.clientY) || 0
  referenceMenu.value = {
    x: Math.max(8, Math.min(x, window.innerWidth - 224)),
    y: Math.max(8, Math.min(y, window.innerHeight - 92)),
    anchor: { ...anchor, location: { ...anchor.location } },
  }
  void nextTick(() => referenceMenuElement.value?.querySelector('button')?.focus())
}
function setAnnotationInput(id, element) {
  if (element) annotationInputRefs.set(id, element)
  else annotationInputRefs.delete(id)
}
function syncAnnotation(annotation) {
  emit('annotation-update', { ...annotation.reference, annotation: annotation.comment })
}
function updateAnnotation(annotation, event) {
  annotation.comment = String(event?.target?.value || '')
  syncAnnotation(annotation)
}
function removeAnnotation(id, { notify = false } = {}) {
  const existed = annotations.value.some(annotation => annotation.id === id)
  annotations.value = annotations.value.filter(annotation => annotation.id !== id)
  annotationInputRefs.delete(id)
  if (existed && notify) emit('annotation-remove', id)
}
function clearAnnotations({ notify = false } = {}) {
  const ids = annotations.value.map(annotation => annotation.id)
  annotations.value = []
  annotationInputRefs.clear()
  if (notify) for (const id of ids) emit('annotation-remove', id)
}
async function addAnnotation() {
  const selected = referenceMenu.value
  if (!selected || !selectedPath.value || props.attachmentLimitReached) return
  const path = selectedPath.value
  const payload = {
    agentId: props.agentId,
    path,
    name: filename.value,
    fileRef: fileRefs.value[path] || path,
    revision: revision.value,
    draft: dirty.value,
    anchor: selected.anchor,
  }
  const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const point = { x: selected.x, y: selected.y }
  const imageBytes = previewKind.value === 'image' ? fileBytes.value : null
  const imageType = fileContentType.value
  referenceMenu.value = null
  if (imageBytes) {
    try { payload.imageUrl = await imageBytesAsDataUrl(imageBytes, imageType) }
    catch { /* The workspace file reference remains attached if inline image conversion fails. */ }
  }
  if (props.agentId !== payload.agentId || selectedPath.value !== path) return
  const annotation = { id, x: point.x, y: point.y, comment: '', reference: { ...payload, id } }
  annotations.value = [...annotations.value, annotation]
  syncAnnotation(annotation)
  await nextTick()
  annotationInputRefs.get(id)?.focus()
}
function annotationStyle(annotation) {
  const width = Math.min(320, Math.max(0, window.innerWidth - 16))
  const height = 206
  return {
    left: `${Math.max(8, Math.min(annotation.x, window.innerWidth - width - 8))}px`,
    top: `${Math.max(8, Math.min(annotation.y, window.innerHeight - height - 8))}px`,
  }
}
async function buildFilePreview(path, suppliedType, bytes, draftContent, isCurrent) {
  if (!isCurrent()) return null
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  const type = workspaceContentType(path, suppliedType, data)
  const kind = workspacePreviewKind(path, suppliedType, data)
  const snapshot = { kind, contentType: kind === 'svg' ? 'text/plain' : type, content: '', url: '', html: '', warnings: [], sheets: [], error: '' }
  if (data.byteLength > workspacePreviewLimits.bytes) {
    snapshot.error = `文件超过 ${Math.round(workspacePreviewLimits.bytes / 1024 / 1024)} MiB，暂不生成预览；可以下载原文件查看。`
    return snapshot
  }
  try {
    if (kind === 'image') snapshot.url = URL.createObjectURL(new Blob([data], { type }))
    else if (kind === 'svg') {
      if (data.length > 1024 * 1024) throw new Error('SVG 图片超过 1 MiB 限制。')
      const source = new TextDecoder('utf-8', { fatal: true }).decode(data)
      const sanitized = DOMPurify.sanitize(source, { USE_PROFILES: { svg: true }, FORBID_TAGS: ['script', 'foreignObject'] })
      if (!/<svg\b/i.test(sanitized)) throw new Error('SVG 内容无效，无法预览。')
      snapshot.content = draftContent ?? source
    } else if (kind === 'docx') {
      const { convertDocxToHtml } = await import('../../workspace-documents.js')
      const converted = await convertDocxToHtml(data)
      snapshot.html = converted.html
      snapshot.warnings = converted.warnings
    } else if (kind === 'xlsx') {
      const { readXlsxPreview } = await import('../../workspace-documents.js')
      snapshot.sheets = await readXlsxPreview(data)
    } else if (kind === 'csv') {
      snapshot.content = draftContent ?? new TextDecoder('utf-8').decode(data)
      const parsed = parseWorkspaceCsvPreview(snapshot.content)
      snapshot.sheets = [{ name: String(path).split('/').at(-1) || 'CSV', rows: parsed.rows, merges: [], truncated: parsed.truncated }]
    }
    else if (['html', 'code', 'text'].includes(kind)) snapshot.content = draftContent ?? new TextDecoder('utf-8').decode(data)
    if (!isCurrent()) {
      if (snapshot.url) URL.revokeObjectURL(snapshot.url)
      return null
    }
  } catch (error) {
    if (snapshot.url) URL.revokeObjectURL(snapshot.url)
    snapshot.url = ''
    snapshot.error = String(error?.message || error)
  }
  return snapshot
}
function applyFilePreview(snapshot, { keepMode = false } = {}) {
  if (!snapshot) return
  const priorMode = previewMode.value
  revokePreview()
  previewKind.value = snapshot.kind
  fileContentType.value = snapshot.contentType
  previewUrl.value = snapshot.url
  content.value = snapshot.content
  refreshSvgPreview()
  documentHtml.value = snapshot.html
  documentWarnings.value = snapshot.warnings
  documentPreviewError.value = snapshot.error
  workbookSheets.value = snapshot.sheets
  workbookSheetName.value = snapshot.sheets[0]?.name || ''
  spreadsheetSelection.value = null
  imageSelection.value = null
  textAnchor.value = null
  pdfAnchor.value = null
  previewMode.value = keepMode && canTogglePreview.value ? priorMode : defaultPreviewMode(snapshot.kind)
}
function togglePreviewMode() { if (canTogglePreview.value) previewMode.value = previewMode.value === 'preview' ? 'source' : 'preview' }
function saveDraft() {
  if (!selectedPath.value || !draftOwnerId.value || !draftOwnerScope.value) return
  const key = workspaceDraftKey(draftOwnerId.value, selectedPath.value, draftOwnerScope.value)
  try {
    if (dirty.value) localStorage.setItem(key, serializeWorkspaceDraft(content.value, revision.value))
    else localStorage.removeItem(key)
  } catch { /* Workspace edits remain in memory when local storage is unavailable. */ }
}
async function confirmDiscard() { return !dirty.value || await confirmDesktopAction('当前文件有未保存修改。继续操作会保留本地草稿并离开编辑器，是否继续？') }
async function loadDirectory(path = currentPath.value) {
  if (!props.agentId) return { ok: false, canceled: false }
  if (!await confirmDiscard()) return { ok: false, canceled: true }
  const request = { epoch: ++requestEpoch, agentId: props.agentId, path, scope: props.workspaceScope, generation: props.requestGeneration }
  loading.value = true; error.value = ''
  try {
    const result = await listWorkspace(request.agentId, request.path)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: request.path, generation: props.requestGeneration }) || request.scope !== props.workspaceScope) return { ok: false, canceled: false }
    currentPath.value = normalizeWorkspacePath(result.path || path)
    entries.value = Array.isArray(result.entries) ? result.entries : []
    selectedPath.value = ''
    content.value = ''
    fileBytes.value = null
    dirty.value = false
    revokePreview()
    resetPreviewData()
    emit('resource-name', currentPath.value.split('/').filter(Boolean).at(-1) || '工作区')
    return { ok: true, canceled: false }
  } catch (e) {
    if (request.epoch === requestEpoch && request.agentId === props.agentId) {
      if (e?.status === 412) draftConflict.value = true
      error.value = String(e.message || e)
    }
    return { ok: false, canceled: false }
  }
  finally { if (request.epoch === requestEpoch && request.agentId === props.agentId) loading.value = false }
}
async function openPath(path) {
  const target = normalizeWorkspacePath(path)
  if (!validateWorkspacePath(target)) {
    error.value = '资源链接包含无效的工作区相对路径。'
    return { ok: false, canceled: false }
  }
  const { name, parent } = workspaceFileLocation(target)
  const loaded = await loadDirectory(parent)
  if (!loaded.ok) return loaded
  if (currentPath.value !== parent) {
    error.value = `无法定位工作区目录：${parent || '工作区'}`
    return { ok: false, canceled: false }
  }
  const entry = entries.value.find(item => item.path === target || item.name === name && currentPath.value === parent)
  if (!entry) {
    error.value = `未找到工作区文件：${target}`
    return { ok: false, canceled: false }
  }
  if (entry.is_dir) {
    error.value = `资源链接指向文件夹而不是文件：${target}`
    return { ok: false, canceled: false }
  }
  const opened = await openEntry(entry)
  return { ok: opened, canceled: !opened && !error.value }
}
async function openEntry(entry, discard = false) {
  if (entry.is_dir) return loadDirectory(entry.path)
  if (!discard && !await confirmDiscard()) return false
  const request = { epoch: ++requestEpoch, agentId: props.agentId, path: entry.path, scope: props.workspaceScope, generation: props.requestGeneration }
  loading.value = true; error.value = ''; revokePreview()
  resetPreviewData()
  try {
    const result = await readWorkspaceFile(request.agentId, request.path)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: request.path, generation: props.requestGeneration }) || request.scope !== props.workspaceScope) return false
    const draft = deserializeWorkspaceDraft(localStorage.getItem(workspaceDraftKey(request.agentId, request.path, request.scope)), result.etag)
    revision.value = draft?.conflict ? draft.revision : result.etag
    const snapshot = await buildFilePreview(request.path, result.contentType, result.bytes, draft?.content, () => isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: entry.path, generation: props.requestGeneration }) && request.scope === props.workspaceScope)
    if (!snapshot) return false
    selectedPath.value = entry.path
    draftOwnerId.value = request.agentId
    draftOwnerScope.value = request.scope
    contentEditVersion++
    fileBytes.value = result.bytes
    applyFilePreview(snapshot)
    draftConflict.value = Boolean(draft?.conflict)
    dirty.value = canEditText.value && draft !== null && draft.content !== new TextDecoder().decode(result.bytes)
    externalUpdate.value = false
    fileMissing.value = false
    lastFileCheck = Date.now()
    error.value = ''
    if (entry.file_ref) fileRefs.value[entry.path] = entry.file_ref
    emit('resource-name', entry.name)
    return true
  } catch (e) { if (request.epoch === requestEpoch && request.agentId === props.agentId) error.value = String(e.message || e); return false }
  finally { if (request.epoch === requestEpoch && request.agentId === props.agentId) loading.value = false }
}
function updateContent(value) {
  contentEditVersion++
  content.value = value
  if (previewKind.value === 'svg') refreshSvgPreview()
  if (previewKind.value === 'csv') {
    const parsed = parseWorkspaceCsvPreview(value)
    workbookSheets.value = [{ name: filename.value || 'CSV', rows: parsed.rows, merges: [], truncated: parsed.truncated }]
    workbookSheetName.value = filename.value || 'CSV'
    spreadsheetSelection.value = null
  }
  dirty.value = true
  saveDraft()
}
async function refreshHighlightedContent() {
  const sequence = ++highlightSequence
  const source = content.value
  if (previewKind.value !== 'code' || previewMode.value !== 'preview') { highlightedContent.value = ''; return }
  try {
    const { highlightWorkspaceSource } = await import('../../workspace-syntax.js')
    if (sequence !== highlightSequence || source !== content.value || previewKind.value !== 'code' || previewMode.value !== 'preview') return
    highlightedContent.value = highlightWorkspaceSource(source, workspaceCodeLanguage(filename.value))
  } catch { if (sequence === highlightSequence) highlightedContent.value = '' }
}
async function discardDraftAndReload({ confirm = true } = {}) {
  if (loading.value || saving.value || !selectedPath.value || !dirty.value && !draftConflict.value && !fileMissing.value && !externalUpdate.value) return
  if (confirm && !await confirmDesktopAction('丢弃当前本地草稿并重新读取工作区文件？')) return
  const path = selectedPath.value
  try { localStorage.removeItem(workspaceDraftKey(draftOwnerId.value, path, draftOwnerScope.value)) } catch { /* ignore unavailable storage */ }
  dirty.value = false
  draftConflict.value = false
  externalUpdate.value = false
  error.value = ''
  refreshError.value = ''
  const entry = entries.value.find(item => item.path === path)
  if (entry) return openEntry(entry, true)
  selectedPath.value = ''
  content.value = ''
  fileBytes.value = null
  revokePreview()
  resetPreviewData()
  return refreshResources('manual')
}
async function saveFile({ overwrite = false, confirm = true } = {}) {
  if (!selectedPath.value || !canEditText.value || !dirty.value || saving.value || fileMissing.value) return
  if (!overwrite && (draftConflict.value || externalUpdate.value)) return
  if (overwrite && confirm && !await confirmDesktopAction('覆盖工作区中的较新版本？此操作会用当前本地草稿替换文件内容。')) return
  const request = { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value, scope: props.workspaceScope, generation: props.requestGeneration, saveId: ++saveSequence }
  const text = content.value
  saving.value = true; error.value = ''
  try {
    const bytes = new TextEncoder().encode(text)
    let baseRevision = revision.value
    if (overwrite) {
      const current = await readWorkspaceFile(request.agentId, request.path)
      if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value, generation: props.requestGeneration }) || request.scope !== props.workspaceScope || request.saveId !== saveSequence) return
      baseRevision = current.etag
      if (!baseRevision) throw new Error('无法确认工作区文件版本，请重新载入后再保存。')
    }
    const response = await saveWorkspaceFile(request.agentId, request.path, bytes, baseRevision)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value, generation: props.requestGeneration }) || request.scope !== props.workspaceScope || request.saveId !== saveSequence) return
    revision.value = response.etag || baseRevision
    fileBytes.value = bytes
    dirty.value = content.value !== text
    draftConflict.value = false
    externalUpdate.value = false
    fileMissing.value = false
    if (!dirty.value) saveDraft()
    lastFileCheck = Date.now()
    lastDirectoryCheck = 0
    void refreshResources('local-write')
  } catch (e) {
    if (request.epoch === requestEpoch && request.agentId === props.agentId && request.scope === props.workspaceScope && request.generation === props.requestGeneration && request.saveId === saveSequence) {
      if (e?.status === 412) { draftConflict.value = true; externalUpdate.value = true }
      if (e?.status === 404) fileMissing.value = true
      error.value = String(e.message || e)
    }
  }
  finally { if (request.saveId === saveSequence) saving.value = false }
  if (refreshAfterSave && request.epoch === requestEpoch && request.agentId === props.agentId && request.scope === props.workspaceScope && request.generation === props.requestGeneration && request.saveId === saveSequence) {
    refreshAfterSave = false
    refreshQueued = false
    void refreshResources('local-write')
  }
}
async function createFile() {
  if (!await confirmDiscard()) return
  newFileName.value = ''
  newFileNameError.value = ''
  createNameDialogOpen.value = true
  await nextTick()
  newFileNameInput.value?.focus()
}
function cancelCreateFile() {
  createNameDialogOpen.value = false
  newFileNameError.value = ''
}
async function submitCreateFile() {
  const name = newFileName.value.trim()
  if (!name) { newFileNameError.value = '请输入文件名。'; newFileNameInput.value?.focus(); return }
  if (name.includes('/') || name.includes('\\') || name === '.' || name === '..') { newFileNameError.value = '请输入单个文件名。'; newFileNameInput.value?.focus(); return }
  cancelCreateFile()
  const ownerId = props.agentId
  try {
    const path = workspacePath(currentPath.value, name)
    await createWorkspaceFile(ownerId, path, new TextEncoder().encode(''))
    if (ownerId !== props.agentId) return
    await loadDirectory(currentPath.value)
    await openEntry({ name, path, is_dir: false })
  } catch (e) { if (ownerId === props.agentId) error.value = String(e.message || e) }
}
async function onUpload(event) {
  const files = Array.from(event.target.files || [])
  const ownerId = props.agentId
  const directory = currentPath.value
  event.target.value = ''
  for (const file of files) {
    if (file.size > 16 * 1024 * 1024) { error.value = `${file.name} 超过16MiB限制`; continue }
    try {
      const result = await uploadWorkspaceFile(ownerId, directory, file)
      if (ownerId !== props.agentId) break
      fileRefs.value[result.path] = result.file_ref
      // Uploading only writes to the workspace; attaching remains an explicit action.
    } catch (e) { error.value = String(e.message || e); break }
  }
  await refreshDirectorySilently()
}
function entryMetadata(entry) {
  return JSON.stringify([entry?.size ?? null, entry?.modified_at ?? entry?.modified ?? entry?.mod_time ?? null, entry?.etag ?? null])
}
function scheduleMissingConfirmation(request) {
  if (missingConfirmTimer) return
  missingConfirmTimer = window.setTimeout(() => {
    missingConfirmTimer = 0
    if (disposed || !props.active || document.visibilityState === 'hidden'
      || request.epoch !== requestEpoch || request.agentId !== props.agentId
      || request.scope !== props.workspaceScope || request.generation !== props.requestGeneration
      || request.path !== selectedPath.value) return
    void refreshResources('missing-confirm')
  }, 350)
}
async function refreshDirectorySilently({ confirmMissing = true } = {}) {
  const agentId = props.agentId, scope = props.workspaceScope, generation = props.requestGeneration, path = currentPath.value, epoch = requestEpoch
  if (!agentId) return false
  const previous = entries.value.find(entry => entry.path === selectedPath.value)
  const wasMissing = fileMissing.value
  let result
  try { result = await listWorkspace(agentId, path) }
  catch (e) {
    if (!disposed && epoch === requestEpoch && agentId === props.agentId && scope === props.workspaceScope && generation === props.requestGeneration && path === currentPath.value) refreshError.value = String(e?.message || e)
    throw e
  }
  if (disposed || epoch !== requestEpoch || agentId !== props.agentId || scope !== props.workspaceScope || generation !== props.requestGeneration || path !== currentPath.value) return false
  const next = Array.isArray(result.entries) ? result.entries : []
  const selected = next.find(entry => entry.path === selectedPath.value)
  const metadataChanged = Boolean(previous && selected && (wasMissing || entryMetadata(previous) !== entryMetadata(selected)))
  entries.value = next
  lastDirectoryCheck = Date.now()
  refreshError.value = ''
  if (selectedPath.value && !selected) {
    if (confirmMissing && !wasMissing) {
      scheduleMissingConfirmation({ epoch: requestEpoch, agentId: props.agentId, scope: props.workspaceScope, generation: props.requestGeneration, path: selectedPath.value })
    } else fileMissing.value = true
  }
  else if (selected) fileMissing.value = false
  return metadataChanged
}
async function refreshSelectedFile() {
  const path = selectedPath.value, agentId = props.agentId, scope = props.workspaceScope
  if (!path || !agentId) return false
  const request = { epoch: requestEpoch, agentId, path, scope, generation: props.requestGeneration }
  const editedAtStart = contentEditVersion
  const revisionAtStart = revision.value
  const readSequence = ++fileReadSequence
  let result
  try { result = await readWorkspaceFile(agentId, path, { ifNoneMatch: revisionAtStart }) }
  catch (e) { if (request.epoch === requestEpoch && agentId === props.agentId && scope === props.workspaceScope && request.generation === props.requestGeneration && path === selectedPath.value) lastFileCheck = Date.now(); throw e }
  if (disposed || !isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value, generation: props.requestGeneration }) || scope !== props.workspaceScope) return false
  if (revision.value !== revisionAtStart || readSequence !== fileReadSequence) { lastFileCheck = 0; refreshQueued = true; return false }
  lastFileCheck = Date.now()
  fileMissing.value = false
  refreshError.value = ''
  if (result.notModified || result.etag && result.etag === revision.value) { externalUpdate.value = false; return true }
  if (saving.value) { lastFileCheck = 0; refreshAfterSave = true; return false }
  if (dirty.value || contentEditVersion !== editedAtStart) { externalUpdate.value = true; return true }
  const editor = editorRef.value
  const selection = editor ? [editor.selectionStart, editor.selectionEnd, editor.scrollTop] : null
  const isCurrent = () => !disposed
    && isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value, generation: props.requestGeneration })
    && request.scope === props.workspaceScope && revision.value === revisionAtStart
    && contentEditVersion === editedAtStart && readSequence === fileReadSequence && !dirty.value && !saving.value
  const snapshot = await buildFilePreview(path, result.contentType, result.bytes, null, isCurrent)
  if (!snapshot || !isCurrent()) {
    if (saving.value) refreshAfterSave = true
    else { lastFileCheck = 0; refreshQueued = true }
    return false
  }
  const keepMode = previewKind.value === snapshot.kind
  fileBytes.value = result.bytes
  revision.value = result.etag || revision.value
  applyFilePreview(snapshot, { keepMode })
  await nextTick()
  const currentEditor = editorRef.value
  if (currentEditor && selection && request.epoch === requestEpoch && request.path === selectedPath.value && request.scope === props.workspaceScope) {
    currentEditor.setSelectionRange(Math.min(selection[0], content.value.length), Math.min(selection[1], content.value.length))
    currentEditor.scrollTop = selection[2]
  }
  draftConflict.value = false
  externalUpdate.value = false
  saveDraft()
  return true
}
async function performRefresh(reason) {
  const now = Date.now()
  let metadataChanged = false
  const missingConfirmation = reason === 'missing-confirm'
  const force = ['tool', 'local-write', 'manual', 'return', 'missing-confirm'].includes(reason)
  if (reason !== 'file-only' && (force || now - lastDirectoryCheck >= 3000)) metadataChanged = await refreshDirectorySilently({ confirmMissing: !missingConfirmation })
  const interval = (fileBytes.value?.byteLength || 0) > 1024 * 1024 ? 30000 : 10000
  if (selectedPath.value && (!missingConfirmation || !fileMissing.value) && (force || metadataChanged || now - lastFileCheck >= interval)) {
    const request = { epoch: requestEpoch, agentId: props.agentId, scope: props.workspaceScope, generation: props.requestGeneration, path: selectedPath.value }
    try { await refreshSelectedFile() }
    catch (e) {
      if (e?.status === 404) {
        const expectedEpoch = request.epoch, expectedAgent = request.agentId, expectedScope = request.scope, expectedGeneration = request.generation, expectedPath = request.path
        if (missingConfirmation) lastFileCheck = Date.now()
        else scheduleMissingConfirmation({ epoch: expectedEpoch, agentId: expectedAgent, scope: expectedScope, generation: expectedGeneration, path: expectedPath })
      } else throw e
    }
  }
}
async function refreshResources(reason = 'manual') {
  if (!props.active || document.visibilityState === 'hidden' || !props.agentId || disposed) return
  if (reason === 'manual' && selectedPath.value && dirty.value) {
    if (!await confirmDesktopAction('刷新会丢弃当前本地草稿并重新读取工作区文件。继续？')) return
    await discardDraftAndReload({ confirm: false })
    return
  }
  if (refreshInFlight) {
    refreshQueued = true
    if (['tool', 'local-write', 'manual', 'return'].includes(reason)) refreshQueuedReason = reason
    return
  }
  refreshInFlight = true
  let refreshOwner = captureRefreshOwner()
  try {
    let nextReason = reason
    do {
      refreshQueued = false
      refreshQueuedReason = ''
      refreshOwner = captureRefreshOwner()
      await performRefresh(nextReason)
      nextReason = refreshQueuedReason || 'poll'
      if (isRefreshOwnerCurrent(refreshOwner)) refreshRetry = 0
    } while (refreshQueued && props.active && document.visibilityState !== 'hidden' && !disposed)
  } catch (e) {
    if (isRefreshOwnerCurrent(refreshOwner)) {
      refreshError.value = String(e?.message || e)
      refreshRetry = Math.min(refreshRetry + 1, 3)
    }
  } finally {
    refreshInFlight = false
    if (props.active && document.visibilityState !== 'hidden' && !disposed) {
      if (!isRefreshOwnerCurrent(refreshOwner)) {
        void refreshResources('return')
      } else {
        const interval = (fileBytes.value?.byteLength || 0) > 1024 * 1024 ? 30000 : 10000
        const now = Date.now()
        const regularDelay = Math.max(250, Math.min(3000 - (now - lastDirectoryCheck), selectedPath.value ? interval - (now - lastFileCheck) : 3000))
        scheduleRefresh(refreshRetry ? [5000, 10000, 30000][refreshRetry - 1] : regularDelay)
      }
    }
  }
}
function scheduleRefresh(delay = 1000) {
  if (refreshTimer) window.clearTimeout(refreshTimer)
  refreshTimer = 0
  if (!props.active || document.visibilityState === 'hidden' || disposed) return
  refreshTimer = window.setTimeout(() => { refreshTimer = 0; void refreshResources(refreshRetry ? 'retry' : 'poll') }, delay)
}
function captureRefreshOwner() { return { epoch: requestEpoch, agentId: props.agentId, scope: props.workspaceScope, generation: props.requestGeneration } }
function isRefreshOwnerCurrent(owner) {
  return Boolean(owner && !disposed && owner.epoch === requestEpoch && owner.agentId === props.agentId
    && owner.scope === props.workspaceScope && owner.generation === props.requestGeneration)
}
function onVisibilityChange() { if (document.visibilityState === 'hidden') { if (refreshTimer) window.clearTimeout(refreshTimer); refreshTimer = 0 } else if (props.active) void refreshResources('return') }
function downloadFile() {
  const bytes = dirty.value && canEditText.value ? new TextEncoder().encode(content.value) : fileBytes.value
  if (bytes) downloadBytes(bytes, filename.value, fileContentType.value || 'text/plain')
}
function goUp() {
  const parts = currentPath.value.split('/').filter(Boolean)
  parts.pop()
  void loadDirectory(parts.join('/'))
}
function attachSelectedFile() {
  const fileRef = fileRefs.value[selectedPath.value]
  if (fileRef) emit('attach-file', { path: fileRef, name: filename.value })
}
function closeToolbarMenu() { if (toolbarMenu.value) toolbarMenu.value.open = false }
function runToolbarAction(action) {
  closeToolbarMenu()
  action()
}
function onToolbarPointerDown(event) {
  if (toolbarMenu.value && !toolbarMenu.value.contains(event.target)) closeToolbarMenu()
  if (referenceMenu.value && !referenceMenuElement.value?.contains(event.target)) referenceMenu.value = null
}
function onToolbarKeydown(event) {
  if (event.key !== 'Escape') return
  if (referenceMenu.value) { referenceMenu.value = null; return }
  if (toolbarMenu.value?.open) {
    closeToolbarMenu()
    toolbarMenu.value.querySelector('summary')?.focus()
  }
}
function resourceMenuActions() {
  const canRecoverDraft = Boolean(selectedPath.value && !loading.value && !saving.value && (dirty.value || draftConflict.value || fileMissing.value || externalUpdate.value))
  return { canRecoverDraft, recoveryLabel: fileMissing.value ? '丢弃草稿并重试' : '丢弃草稿并重载' }
}
watch(selectedPath, (path, previousPath) => { if (previousPath && path !== previousPath) clearAnnotations({ notify: true }) })
defineExpose({ openPath, refreshResources, discardDraftAndReload, resourceMenuActions, clearAnnotations, removeAnnotation })
watch(() => [content.value, dirty.value, selectedPath.value, props.agentId], saveDraft)
watch(() => [content.value, filename.value, previewKind.value, previewMode.value], () => { void refreshHighlightedContent() }, { immediate: true })
watch(() => [props.agentId, props.workspaceScope], () => { requestEpoch++; saveSequence++; saving.value = false; refreshAfterSave = false; if (missingConfirmTimer) window.clearTimeout(missingConfirmTimer); missingConfirmTimer = 0; currentPath.value = ''; selectedPath.value = ''; draftOwnerId.value = ''; draftOwnerScope.value = ''; dirty.value = false; fileRefs.value = {}; revision.value = ''; fileMissing.value = false; externalUpdate.value = false; lastDirectoryCheck = 0; lastFileCheck = 0; void loadDirectory('') }, { immediate: true })
watch(() => props.requestGeneration, () => { requestEpoch++; saveSequence++; saving.value = false; refreshAfterSave = false; if (missingConfirmTimer) window.clearTimeout(missingConfirmTimer); missingConfirmTimer = 0; lastDirectoryCheck = 0; lastFileCheck = 0; if (refreshTimer) window.clearTimeout(refreshTimer); refreshTimer = 0; if (props.active) void refreshResources('return') })
watch(() => props.active, active => { if (active) { lastDirectoryCheck = 0; lastFileCheck = 0; void refreshResources('return') } else if (refreshTimer) { window.clearTimeout(refreshTimer); refreshTimer = 0 } }, { immediate: true })
watch(() => props.dockCollapsed, async collapsed => {
  if (!collapsed) return
  await nextTick()
  dockRestoreButton.value?.focus()
})
onMounted(() => { document.addEventListener('visibilitychange', onVisibilityChange); document.addEventListener('pointerdown', onToolbarPointerDown); document.addEventListener('pointerup', stopSpreadsheetSelection); document.addEventListener('keydown', onToolbarKeydown); if (props.active) void refreshResources('return') })
onUnmounted(() => { disposed = true; requestEpoch++; if (refreshTimer) window.clearTimeout(refreshTimer); if (missingConfirmTimer) window.clearTimeout(missingConfirmTimer); document.removeEventListener('visibilitychange', onVisibilityChange); document.removeEventListener('pointerdown', onToolbarPointerDown); document.removeEventListener('pointerup', stopSpreadsheetSelection); document.removeEventListener('keydown', onToolbarKeydown); revokePreview() })
</script>

<template>
  <section class="workspace-files" aria-label="Agent 工作区文件">
    <header class="workspace-files__toolbar">
      <button type="button" class="desktop-icon-button" :disabled="!currentPath || loading" title="上级目录" aria-label="上级目录" @click="goUp"><UiIcon name="arrow-up" :size="16" /></button>
      <div class="workspace-files__breadcrumbs"><button type="button" @click="loadDirectory('')">工作区</button><template v-for="(part,index) in breadcrumbs" :key="index"><span>/</span><button type="button" @click="loadDirectory(breadcrumbs.slice(0,index+1).join('/'))">{{ part }}</button></template><span v-if="filename">/</span><b v-if="filename" :title="filename">{{ filename }}</b></div>
      <span v-if="previewKind === 'docx' && documentWarnings.length && !documentPreviewError" class="workspace-files__format-warning" tabindex="0" role="status" aria-label="Word 预览格式提示" :title="documentWarnings.join('\n')"><UiIcon name="alert" :size="15" /></span>
      <span v-if="noticeSummary" class="workspace-files__notice" tabindex="0" role="status" aria-live="polite" :aria-label="noticeSummary" aria-describedby="workspace-files-notice-detail"><UiIcon name="alert" :size="16" /><span class="workspace-files__sr-only">{{ noticeSummary }}</span><span id="workspace-files-notice-detail" class="workspace-files__notice-detail" role="tooltip"><span v-for="(detail,index) in noticeDetails" :key="index">{{ detail }}</span></span></span>
      <input ref="uploadInput" class="workspace-files__hidden" type="file" multiple @change="onUpload">
      <div class="workspace-files__toolbar-actions">
      <button v-if="dockCollapsed" ref="dockRestoreButton" type="button" class="desktop-icon-button workspace-files__dock-toggle" :title="`${dockAgentName} · ${dockStatusLabel}；展开快速消息栏`" :aria-label="`${dockAgentName}：展开快速消息栏，当前${dockStatusLabel}`" @click="emit('expand-agent-dock')"><UiIcon name="message-square" :size="16" /><i :class="`is-${dockStatus}`" aria-hidden="true"></i></button>
      <details ref="toolbarMenu" class="workspace-files__actions-menu">
        <summary class="desktop-icon-button" title="更多文件操作" aria-label="更多文件操作"><UiIcon name="more-horizontal" :size="16" /></summary>
        <div class="workspace-files__actions-popover" aria-label="更多文件操作">
          <button type="button" :disabled="loading" @click="runToolbarAction(createFile)"><UiIcon name="file-plus" :size="15" /><span>新建文件</span></button>
          <button type="button" :disabled="loading" @click="runToolbarAction(() => uploadInput?.click())"><UiIcon name="upload" :size="15" /><span>上传到当前目录</span></button>
          <button type="button" :disabled="!selectedPath" @click="runToolbarAction(downloadFile)"><UiIcon name="arrow-down" :size="15" /><span>{{ dirty && canEditText ? '下载本地草稿' : '下载文件' }}</span></button>
          <button type="button" :disabled="!fileRefs[selectedPath]" @click="runToolbarAction(attachSelectedFile)"><UiIcon name="paperclip" :size="15" /><span>添加到对话</span></button>
          <button v-if="canTogglePreview" type="button" @click="runToolbarAction(togglePreviewMode)"><UiIcon :name="previewMode === 'preview' ? 'code-2' : 'image'" :size="15" /><span>{{ previewMode === 'preview' ? '查看源代码' : '查看预览' }}</span></button>
        </div>
      </details>
      <button type="button" class="desktop-icon-button" title="刷新" aria-label="刷新" :disabled="loading" @click="refreshResources('manual')"><UiIcon name="refresh-cw" :size="16" /></button>
      <button type="button" class="desktop-icon-button" :title="saving ? '正在保存' : draftConflict || externalUpdate ? '工作区已有更新，请选择覆盖保存或丢弃草稿' : fileMissing ? '文件已移除，无法保存' : '保存文件'" aria-label="保存文件" :disabled="!dirty || saving || !canEditText || draftConflict || externalUpdate || fileMissing" @click="saveFile"><UiIcon name="save" :size="16" /></button>
      </div>
    </header>
    <div class="workspace-files__body">
      <nav class="workspace-files__list" aria-label="文件列表">
        <button v-for="entry in entries" :key="entry.path" type="button" :class="{ selected: selectedPath === entry.path }" :title="entry.path" @click="openEntry(entry)"><span v-if="entry.is_dir" class="workspace-files__folder-icon" :style="folderMaskStyle" aria-hidden="true"></span><UiIcon v-else name="file" :size="16" /><span>{{ entry.name }}</span></button>
      </nav>
      <main class="workspace-files__editor" @contextmenu="openReferenceMenu">
        <div v-if="dirty" class="workspace-files__draft-bar" :class="{ 'is-conflict': draftConflict || externalUpdate || fileMissing }" role="status" aria-live="polite">
          <span>{{ draftStatusText }}</span>
          <div class="workspace-files__draft-actions">
            <button v-if="canOverwriteDraft" type="button" :disabled="saving || loading" @click="saveFile({ overwrite: true })">覆盖保存</button>
          </div>
        </div>
        <div class="workspace-files__editor-content">
          <div v-if="loading" class="workspace-files__empty">正在读取工作区…</div>
          <template v-else-if="selectedPath">
            <div v-if="documentPreviewError" class="workspace-files__empty workspace-files__preview-error"><span>{{ documentPreviewError }}</span><button type="button" @click="downloadFile">下载原文件</button></div>
            <div v-else-if="previewKind === 'image'" class="workspace-files__image-stage"><div class="workspace-files__image-target" @pointerdown="startImageSelection" @pointermove="moveImageSelection" @pointerup="stopImageSelection" @pointercancel="stopImageSelection"><img class="workspace-files__image" :src="previewUrl" :alt="filename" draggable="false"><div v-if="imageSelection" class="workspace-files__image-selection" :style="imageSelectionStyle()"></div></div></div>
            <PdfPreview v-else-if="previewKind === 'pdf'" :bytes="fileBytes || new Uint8Array()" @selection="capturePdfSelection" @reference-contextmenu="openReferenceMenu" />
            <iframe v-else-if="['html', 'docx'].includes(previewKind) && previewMode === 'preview'" ref="documentFrame" class="workspace-files__document-frame" :srcdoc="safeDocumentPreview" :title="`${filename} 文档预览`" sandbox="allow-same-origin" @load="onDocumentFrameLoad"></iframe>
            <div v-else-if="previewKind === 'svg' && previewMode === 'preview'" class="workspace-files__image-stage"><div class="workspace-files__image-target" @pointerdown="startImageSelection" @pointermove="moveImageSelection" @pointerup="stopImageSelection" @pointercancel="stopImageSelection"><img class="workspace-files__image" :src="svgPreviewUrl" :alt="filename" draggable="false"><div v-if="imageSelection" class="workspace-files__image-selection" :style="imageSelectionStyle()"></div></div></div>
            <section v-else-if="previewKind === 'xlsx'" class="workspace-files__sheet-preview" :aria-label="`${filename} Excel 预览`">
              <div v-if="workbookSheets.length" class="workspace-files__sheet-toolbar">
                <select v-model="workbookSheetName" aria-label="选择工作表"><option v-for="sheet in workbookSheets" :key="sheet.name" :value="sheet.name">{{ sheet.name }}</option></select>
                <span v-if="workbookPreview?.truncated">预览上限：{{ workspacePreviewLimits.rows }} 行、{{ workspacePreviewLimits.columns }} 列</span>
              </div>
              <div v-if="workbookPreview?.rows.length" class="workspace-files__sheet-scroll"><table><thead><tr><th v-for="index in workbookColumnCount" :key="index">{{ xlsxColumnLabel(index) }}</th></tr></thead><tbody><tr v-for="(row,rowIndex) in workbookGrid" :key="rowIndex"><template v-for="cell in row" :key="cell.address"><td v-if="!cell.covered" :rowspan="cell.rowspan" :colspan="cell.colspan" :class="{ 'is-selected': cell.selected }" :data-cell-address="cell.address" @pointerdown="startSpreadsheetSelection($event, cell)" @pointerenter="continueSpreadsheetSelection(cell)">{{ cell.value }}</td></template></tr></tbody></table></div>
              <div v-else class="workspace-files__empty">{{ documentPreviewError || '此工作簿没有可显示的数据。' }}</div>
            </section>
            <section v-else-if="previewKind === 'csv' && previewMode === 'preview'" class="workspace-files__sheet-preview" :aria-label="`${filename} CSV 预览`">
              <div class="workspace-files__sheet-toolbar"><span>CSV 表格预览</span><span v-if="workbookPreview?.truncated">预览上限：{{ workspacePreviewLimits.rows }} 行、{{ workspacePreviewLimits.columns }} 列</span></div>
              <div v-if="workbookPreview?.rows.length" class="workspace-files__sheet-scroll"><table><thead><tr><th v-for="index in workbookColumnCount" :key="index">{{ xlsxColumnLabel(index) }}</th></tr></thead><tbody><tr v-for="(row,rowIndex) in workbookGrid" :key="rowIndex"><td v-for="cell in row" :key="cell.address" :class="{ 'is-selected': cell.selected }" @pointerdown="startSpreadsheetSelection($event, cell)" @pointerenter="continueSpreadsheetSelection(cell)">{{ cell.value }}</td></tr></tbody></table></div>
              <div v-else class="workspace-files__empty">CSV 文件没有可显示的数据。</div>
            </section>
            <div v-else-if="previewKind === 'code' && previewMode === 'preview'" class="workspace-files__code-preview" @mouseup="captureCodeSelection"><pre><code class="hljs" v-html="highlightedContent"></code></pre></div>
            <textarea v-else-if="canEditText" ref="editorRef" v-model="content" spellcheck="false" :aria-label="`编辑 ${filename}`" @input="updateContent($event.target.value)" @select="captureTextSelection" />
            <div v-else-if="unsupportedPreviewNotice" class="workspace-files__empty workspace-files__unsupported"><span>{{ unsupportedPreviewNotice }}</span><button type="button" @click="downloadFile">下载原文件</button></div>
            <div v-else class="workspace-files__empty">无法读取此文件。<button type="button" @click="downloadFile">下载原文件</button></div>
          </template>
          <div v-else class="workspace-files__empty">{{ entries.length ? '选择工作区文件以预览或编辑。' : '此目录为空，可新建文件或上传文件开始使用。' }}</div>
        </div>
      </main>
    </div>
    <Teleport to="body">
      <div v-if="referenceMenu" ref="referenceMenuElement" class="workspace-files__reference-menu" role="menu" :style="{ left: `${referenceMenu.x}px`, top: `${referenceMenu.y}px` }" @pointerdown.stop @contextmenu.prevent>
        <div class="workspace-files__reference-menu-context"><strong>{{ referenceMenu.anchor.label }}</strong><span v-if="referenceMenu.anchor.quote">{{ referenceMenu.anchor.quote }}</span><span v-else>{{ selectedPath }}</span></div>
        <button type="button" role="menuitem" :disabled="attachmentLimitReached" :title="attachmentLimitReached ? '一条消息最多添加 8 个附件或注解' : '在此位置添加注解'" @click="addAnnotation"><UiIcon name="pencil" :size="15" /><span>添加注解</span></button>
      </div>
    </Teleport>
    <Teleport to="body">
      <section v-for="annotation in annotations" :key="annotation.id" class="workspace-files__annotation-popover" role="dialog" aria-label="资源注解" :style="annotationStyle(annotation)" @pointerdown.stop>
        <header><div><strong>资源注解</strong><span :title="`${annotation.reference.path} · ${annotation.reference.anchor.label}`">{{ annotation.reference.name }} · {{ annotation.reference.anchor.label }}</span></div><button type="button" aria-label="取消注解" title="取消注解" @click="removeAnnotation(annotation.id, { notify: true })"><UiIcon name="close" :size="15" /></button></header>
        <textarea :ref="element => setAnnotationInput(annotation.id, element)" :value="annotation.comment" maxlength="2000" rows="3" placeholder="写下希望模型关注的内容…" aria-label="注解内容" @input="updateAnnotation(annotation, $event)" @keydown.esc.stop.prevent="removeAnnotation(annotation.id, { notify: true })"></textarea>
        <p>注解会随资源一起发送；关闭或切换页面会移除。</p>
      </section>
    </Teleport>
  </section>
  <Teleport to="body">
    <div v-if="createNameDialogOpen" class="workspace-files__create-scrim" @click.self="cancelCreateFile">
      <form class="workspace-files__create-dialog" role="dialog" aria-modal="true" aria-labelledby="workspace-create-file-title" @submit.prevent="submitCreateFile" @keydown.esc.stop.prevent="cancelCreateFile">
        <header><div><h2 id="workspace-create-file-title">新建文件</h2><p>将在 {{ currentPath || '工作区根目录' }} 中创建一个空文件。</p></div><button type="button" aria-label="关闭" @click="cancelCreateFile"><UiIcon name="close" :size="16" /></button></header>
        <label for="workspace-create-file-name">文件名</label>
        <input id="workspace-create-file-name" ref="newFileNameInput" v-model="newFileName" type="text" maxlength="255" autocomplete="off" placeholder="例如：notes.md" :aria-invalid="Boolean(newFileNameError)" :aria-describedby="newFileNameError ? 'workspace-create-file-error' : undefined" @input="newFileNameError = ''">
        <p v-if="newFileNameError" id="workspace-create-file-error" class="workspace-files__create-error" role="alert">{{ newFileNameError }}</p>
        <footer><button type="button" @click="cancelCreateFile">取消</button><button type="submit" class="is-primary">创建</button></footer>
      </form>
    </div>
  </Teleport>
</template>

<style scoped>
.workspace-files{position:relative}
.workspace-files__image-stage{display:flex;flex:1;min-width:0;min-height:0;align-items:center;justify-content:center;overflow:auto;padding:12px}
.workspace-files__image-target{position:relative;flex:none;max-width:100%;max-height:100%;cursor:crosshair;touch-action:none;user-select:none}
.workspace-files__image-target .workspace-files__image{display:block;max-width:100%;max-height:calc(100vh - 190px);margin:0;object-fit:contain;pointer-events:none}
.workspace-files__image-selection{position:absolute;border:2px solid #536c76;background:#78909c38;pointer-events:none}
.workspace-files__sheet-preview td{user-select:none;cursor:cell}
.workspace-files__sheet-preview td.is-selected{background:#dfe8eb;box-shadow:inset 0 0 0 1px #788f98}
.workspace-files__reference-menu{position:fixed;z-index:1000;box-sizing:border-box;width:216px;padding:5px;border:1px solid #d6dddf;border-radius:7px;background:#fff;color:#30383c;box-shadow:0 8px 26px #17212630}
.workspace-files__reference-menu-context{display:grid;gap:4px;padding:7px 8px 8px;border-bottom:1px solid #edf0f1}
.workspace-files__reference-menu-context strong{overflow:hidden;color:#59656a;font-size:11px;font-weight:600;text-overflow:ellipsis;white-space:nowrap}
.workspace-files__reference-menu-context span{display:-webkit-box;max-height:40px;overflow:hidden;color:#778186;font-size:10px;line-height:1.4;overflow-wrap:anywhere;-webkit-box-orient:vertical;-webkit-line-clamp:2}
.workspace-files__reference-menu>button{display:flex;align-items:center;gap:8px;width:100%;height:32px;margin-top:4px;padding:0 8px;border:0;border-radius:5px;background:transparent;color:#3d474b;font:inherit;font-size:12px;text-align:left;cursor:pointer}
.workspace-files__reference-menu>button:hover,.workspace-files__reference-menu>button:focus-visible{outline:0;background:#eef1f2}
.workspace-files__reference-menu>button:disabled{color:#929b9f;cursor:default}
.workspace-files__annotation-popover{position:fixed;z-index:1001;box-sizing:border-box;width:min(320px,calc(100vw - 16px));padding:10px;border:1px solid #d6dddf;border-radius:9px;background:#fff;color:#30383c;box-shadow:0 10px 30px #17212636}
.workspace-files__annotation-popover>header{display:flex;align-items:center;gap:8px;margin-bottom:8px}
.workspace-files__annotation-popover>header>div{display:grid;flex:1;min-width:0;gap:3px}
.workspace-files__annotation-popover>header strong{font-size:12px;font-weight:600}
.workspace-files__annotation-popover>header span{overflow:hidden;color:#667176;font-size:10px;text-overflow:ellipsis;white-space:nowrap}
.workspace-files__annotation-popover>header button{display:grid;flex:none;width:28px;height:28px;padding:0;place-items:center;border:0;border-radius:5px;background:transparent;color:#596368;cursor:pointer}
.workspace-files__annotation-popover>header button:hover{background:#eceeef}
.workspace-files__annotation-popover textarea{display:block;box-sizing:border-box;width:100%;min-height:72px;max-height:180px;padding:8px 9px;border:1px solid #d9e0e2;border-radius:6px;outline:0;background:#fff;color:#30383c;font-family:inherit;font-size:12px;line-height:1.5;resize:vertical}
.workspace-files__annotation-popover textarea:focus{border-color:#89999f;box-shadow:0 0 0 2px #89999f22}
.workspace-files__annotation-popover textarea::placeholder{color:#828c90}
.workspace-files__annotation-popover p{margin:6px 1px 0;color:#727d81;font-size:10px;line-height:1.4}
.workspace-files{display:flex;flex:1;min-width:0;min-height:0;box-sizing:border-box;flex-direction:column;background:#fff;color:#30383c}.workspace-files__toolbar{display:flex;align-items:center;gap:6px;min-height:42px;padding:0 12px;border-bottom:1px solid #e1e4e6}.workspace-files__toolbar button{display:grid;place-items:center;min-width:30px;height:30px;padding:0 7px;border:1px solid transparent;border-radius:5px;background:transparent;color:#4c5559;font-size:11px;cursor:pointer}.workspace-files__toolbar button:hover:not(:disabled){background:#eceeef}.workspace-files__toolbar button:disabled{opacity:.45}.workspace-files__breadcrumbs{display:flex;flex:1;align-items:center;gap:6px;min-width:0;overflow:hidden;color:#777f83;font-size:12px}.workspace-files__breadcrumbs button{overflow:hidden;max-width:160px;padding:3px 1px;border:0;background:transparent;color:#566065;text-overflow:ellipsis;white-space:nowrap;cursor:pointer}.workspace-files__breadcrumbs b{overflow:hidden;color:#30383c;text-overflow:ellipsis;white-space:nowrap}.workspace-files__body{display:flex;flex:1;min-height:0}.workspace-files__list{flex:0 0 245px;min-width:150px;overflow:auto;scroll-padding-bottom:82px;padding:8px;border-right:1px solid #d4dce0;background:#fafbfc}.workspace-files__list button{display:flex;align-items:center;gap:8px;width:100%;height:34px;padding:0 8px;border:0;border-radius:5px;background:transparent;color:#4a5357;text-align:left;font-size:12px;cursor:pointer}.workspace-files__list button.selected,.workspace-files__list button:hover{background:#eceeef}.workspace-files__list button span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.workspace-files__list button .workspace-files__folder-icon{flex:none;width:16px;height:16px;background:#586166;mask-repeat:no-repeat;mask-position:center;mask-size:contain}.workspace-files__list button small{color:#777f83;font-size:10px}.workspace-files__editor{display:flex;background:#fff;flex:1;min-width:0;min-height:0;align-items:stretch;justify-content:stretch}.workspace-files__editor textarea{flex:1;min-width:0;min-height:0;scroll-padding-bottom:82px;padding:16px;border:0;outline:0;background:#fff;color:#30383c;font:12px/1.6 Consolas,monospace;resize:none}.workspace-files__image{max-width:100%;max-height:100%;margin:auto;object-fit:contain}.workspace-files__empty{display:grid;flex:1;place-items:center;padding:24px;color:#777f83;font-size:12px;text-align:center}.workspace-files__hidden{display:none}.workspace-files__notice{position:relative;display:grid;flex:0 0 30px;place-items:center;width:30px;height:30px;border-radius:5px;color:#a53c38;cursor:help}.workspace-files__notice:hover,.workspace-files__notice:focus-visible{background:#fff1ef;outline:2px solid #b74a43;outline-offset:1px}.workspace-files__notice-detail{position:absolute;z-index:50;top:calc(100% + 5px);right:0;display:none;min-width:220px;max-width:min(420px,70vw);padding:9px 11px;border:1px solid #e2c7c8;border-radius:6px;background:#fff7f6;box-shadow:0 5px 16px #26374024;color:#7f302d;font-size:11px;line-height:1.45;white-space:normal}.workspace-files__notice-detail>span{display:block}.workspace-files__notice-detail>span+span{margin-top:5px}.workspace-files__notice:hover .workspace-files__notice-detail,.workspace-files__notice:focus .workspace-files__notice-detail{display:block}.workspace-files__sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
.workspace-files__toolbar-actions{position:relative;display:flex;flex:none;align-items:center;gap:4px;margin-left:auto}.workspace-files__actions-menu{position:relative;flex:none}.workspace-files__actions-menu>summary{display:grid;place-items:center;list-style:none;width:30px;height:30px;padding:0;border:1px solid transparent;border-radius:5px;color:#4c5559;cursor:pointer}.workspace-files__actions-menu>summary::-webkit-details-marker{display:none}.workspace-files__actions-menu>summary:hover,.workspace-files__actions-menu[open]>summary{background:#eceeef}.workspace-files__actions-popover{position:absolute;z-index:40;top:calc(100% + 5px);right:0;display:grid;min-width:190px;padding:4px;border:1px solid #d9e0e2;border-radius:7px;background:#fff;box-shadow:0 8px 24px #26374020}.workspace-files__actions-popover button{display:flex!important;align-items:center;justify-content:flex-start;gap:9px;width:100%;min-width:0!important;height:34px;padding:0 9px!important;border:0!important;border-radius:5px!important;background:transparent!important;color:#3d474b!important;text-align:left!important;font:inherit!important;cursor:pointer}.workspace-files__actions-popover button:hover:not(:disabled){background:#f0f2f2!important}.workspace-files__actions-popover button:disabled{color:#9aa3a6!important;cursor:default}.workspace-files__actions-popover button .ui-icon{flex:none}.workspace-files__dock-toggle{position:relative;color:#3f484c}.workspace-files__dock-toggle i{position:absolute;right:3px;top:3px;width:7px;height:7px;border:1px solid #fff;border-radius:50%;background:#7a8589}.workspace-files__dock-toggle i.is-pending{background:#c18526}.workspace-files__dock-toggle i.is-generating{background:#657780}.workspace-files__dock-toggle i.is-offline{background:#b7534c}.workspace-files__dock-toggle:focus-visible,.workspace-files__actions-menu>summary:focus-visible{outline:2px solid #65777f;outline-offset:1px}
.workspace-files__mode-toggle { flex: 0 0 30px; width:30px; min-width:30px; padding:0; }
.workspace-files__document-frame { display:block; flex:1; width:100%; min-width:0; min-height:0; border:0; background:#fff; }
.workspace-files__pdf-frame { height:100%; }
.workspace-files__code-preview { flex:1; min-width:0; min-height:0; overflow:auto; background:#fbfcfc; }
.workspace-files__code-preview pre { box-sizing:border-box; min-width:max-content; min-height:100%; margin:0; padding:16px 20px; color:#364147; font:12px/1.65 Consolas,Monaco,monospace; tab-size:2; }
.workspace-files__code-preview code { font:inherit; }
.workspace-files__code-preview .hljs-comment,.workspace-files__code-preview .hljs-quote { color:#7c8588; font-style:italic; }
.workspace-files__code-preview .hljs-keyword,.workspace-files__code-preview .hljs-selector-tag,.workspace-files__code-preview .hljs-literal,.workspace-files__code-preview .hljs-section { color:#805a94; }
.workspace-files__code-preview .hljs-string,.workspace-files__code-preview .hljs-attr,.workspace-files__code-preview .hljs-template-tag,.workspace-files__code-preview .hljs-template-variable { color:#367660; }
.workspace-files__code-preview .hljs-number,.workspace-files__code-preview .hljs-variable,.workspace-files__code-preview .hljs-symbol,.workspace-files__code-preview .hljs-bullet { color:#93632f; }
.workspace-files__code-preview .hljs-title,.workspace-files__code-preview .hljs-title.function_,.workspace-files__code-preview .hljs-built_in { color:#315e7a; }
.workspace-files__code-preview .hljs-type,.workspace-files__code-preview .hljs-params { color:#9a5146; }
.workspace-files__sheet-preview { display:flex; flex:1; min-width:0; min-height:0; flex-direction:column; overflow:hidden; }
.workspace-files__sheet-toolbar { display:flex; flex:none; align-items:center; justify-content:space-between; gap:12px; min-height:40px; padding:0 12px; border-bottom:1px solid #e4e8e9; color:#788185; font-size:11px; }
.workspace-files__sheet-toolbar select { max-width:min(260px,60%); padding:5px 24px 5px 8px; border:1px solid #d9e0e2; border-radius:5px; background:#fff; color:#30383c; }
.workspace-files__sheet-scroll { flex:1; min-height:0; overflow:auto; }
.workspace-files__sheet-scroll table { border-collapse:separate; border-spacing:0; color:#3c4549; font-size:12px; }
.workspace-files__sheet-scroll th,.workspace-files__sheet-scroll td { min-width:90px; max-width:360px; padding:6px 9px; border-right:1px solid #e6e9ea; border-bottom:1px solid #e6e9ea; text-align:left; vertical-align:top; overflow-wrap:anywhere; }
.workspace-files__sheet-scroll th { position:sticky; top:0; z-index:1; background:#f5f7f7; color:#606a6e; font-weight:600; }
.workspace-files__sheet-scroll td { white-space:pre-wrap; }
.workspace-files__format-warning { display:grid; flex:0 0 30px; width:30px; height:30px; place-items:center; border-radius:5px; color:#a77d32; cursor:help; }
.workspace-files__format-warning:hover,.workspace-files__format-warning:focus-visible { background:#fff7e6; outline:2px solid #d5bf8c; outline-offset:1px; }
.workspace-files__preview-error,.workspace-files__unsupported { align-content:center; gap:10px; }
.workspace-files__preview-error button,.workspace-files__unsupported button,.workspace-files__empty button { padding:5px 10px; border:1px solid #d2dadd; border-radius:5px; background:#fff; color:#3e484c; cursor:pointer; }
.workspace-files__editor { flex-direction:column; }
.workspace-files__editor-content { display:flex; flex:1; min-width:0; min-height:0; align-items:stretch; justify-content:stretch; overflow:hidden; }
.workspace-files__editor-content > * { min-width:0; }
.workspace-files__draft-bar { display:flex; flex:none; align-items:center; justify-content:space-between; gap:12px; min-height:40px; padding:5px 12px; border-bottom:1px solid #e6e9ea; background:#f5f7f7; color:#586469; font-size:12px; }
.workspace-files__draft-bar.is-conflict { border-bottom-color:#ead7bb; background:#fff9ef; color:#795a2b; }
.workspace-files__draft-actions { display:flex; flex:none; align-items:center; gap:6px; }
.workspace-files__draft-actions button { min-height:28px; padding:4px 9px; border:1px solid #d2dadd; border-radius:5px; background:#fff; color:#3e484c; font:inherit; cursor:pointer; }
.workspace-files__draft-actions button:hover:not(:disabled) { background:#edf0f1; }
.workspace-files__draft-actions button:disabled { opacity:.5; cursor:default; }
.workspace-files__create-scrim { position:fixed; z-index:1000; inset:0; display:grid; place-items:center; padding:24px; background:#17212655; }
.workspace-files__create-dialog { box-sizing:border-box; width:min(420px,100%); padding:20px; border:1px solid #d8dfe1; border-radius:10px; background:#fff; color:#30383c; box-shadow:0 18px 54px #18292d30; font:inherit; }
.workspace-files__create-dialog header { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:20px; }
.workspace-files__create-dialog h2 { margin:0; color:#30383c; font-size:17px; font-weight:600; }
.workspace-files__create-dialog header p { margin:5px 0 0; color:#6b7579; font-size:12px; line-height:1.5; }
.workspace-files__create-dialog header button { display:grid; flex:none; width:30px; height:30px; place-items:center; padding:0; border:0; border-radius:5px; background:transparent; color:#596469; cursor:pointer; }
.workspace-files__create-dialog header button:hover { background:#eef0f1; }
.workspace-files__create-dialog label { display:block; margin-bottom:6px; color:#475257; font-size:12px; font-weight:600; }
.workspace-files__create-dialog input { box-sizing:border-box; width:100%; height:36px; padding:0 10px; border:1px solid #cbd3d6; border-radius:6px; outline:0; background:#fff; color:#30383c; font:inherit; font-size:13px; line-height:1.4; }
.workspace-files__create-dialog input:focus-visible { border-color:#727e83; box-shadow:0 0 0 2px #707d831c; }
.workspace-files__create-dialog footer { display:flex; justify-content:flex-end; gap:8px; margin-top:22px; }
.workspace-files__create-dialog footer button { min-width:72px; height:32px; padding:0 12px; border:1px solid #cbd3d6; border-radius:6px; background:#fff; color:#3f494e; font:inherit; font-size:12px; cursor:pointer; }
.workspace-files__create-dialog footer button:hover { background:#f1f3f4; }
.workspace-files__create-dialog footer button.is-primary { border-color:#343c40; background:#343c40; color:#fff; }
.workspace-files__create-dialog footer button.is-primary:hover { background:#242b2f; }
.workspace-files__create-error { margin:7px 0 0; color:#a13c37; font-size:12px; }
@media (max-width: 760px) {
  .workspace-files__draft-bar { align-items:flex-start; flex-direction:column; gap:7px; }
  .workspace-files__draft-actions { align-self:flex-end; }
}
</style>
