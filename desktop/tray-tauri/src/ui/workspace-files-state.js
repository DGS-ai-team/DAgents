const textExtensions = new Set(['md', 'txt', 'json', 'js', 'mjs', 'ts', 'css', 'html', 'htm', 'xml', 'yaml', 'yml', 'csv', 'py', 'go', 'rs', 'toml', 'sh', 'ps1', 'sql', 'java', 'cs', 'cpp', 'c', 'h', 'rb', 'php'])
const codeExtensions = new Set(['json', 'js', 'mjs', 'ts', 'css', 'html', 'htm', 'xml', 'yaml', 'yml', 'py', 'go', 'rs', 'toml', 'sh', 'ps1', 'sql', 'java', 'cs', 'cpp', 'c', 'h', 'rb', 'php'])
export const workspacePreviewLimits = Object.freeze({ bytes: 16 * 1024 * 1024, textChunkBytes: 256 * 1024, rows: 500, columns: 60 })

// Textarea values normalize line endings, so retain the original file's format
// separately and restore it when encoding an edited buffer.
export function workspaceTextFormat(bytes) {
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  const hasBom = data.length >= 3 && data[0] === 0xef && data[1] === 0xbb && data[2] === 0xbf
  const source = new TextDecoder('utf-8').decode(hasBom ? data.subarray(3) : data)
  const crlf = (source.match(/\r\n/g) || []).length
  const lf = (source.match(/(?<!\r)\n/g) || []).length
  const cr = (source.match(/\r(?!\n)/g) || []).length
  const newline = crlf >= lf && crlf >= cr && crlf ? '\r\n' : cr > lf && cr ? '\r' : '\n'
  return { hasBom, newline }
}

export function decodeWorkspaceText(bytes) {
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  const hasBom = data.length >= 3 && data[0] === 0xef && data[1] === 0xbb && data[2] === 0xbf
  return new TextDecoder('utf-8').decode(hasBom ? data.subarray(3) : data).replace(/\r\n|\r/g, '\n')
}

export function encodeWorkspaceText(content, format = {}) {
  const newline = format.newline || '\n'
  const normalized = String(content ?? '').replace(/\r\n|\r|\n/g, '\n').replace(/\n/g, newline)
  const encoded = new TextEncoder().encode(`${format.hasBom ? '\uFEFF' : ''}${normalized}`)
  return encoded
}

export function createWorkspaceCsvPreviewParser({ rows: maxRows = workspacePreviewLimits.rows, columns: maxColumns = workspacePreviewLimits.columns, maxCellChars = 64 * 1024 } = {}) {
  const rows = []
  let row = []
  let field = ''
  let quoted = false
  let pendingQuote = false
  let pendingCR = false
  let hasRecordContent = false
  let finished = false
  let truncated = false
  const pushField = () => {
    if (row.length < maxColumns) row.push(field)
    else truncated = true
    field = ''
  }
  const pushRow = () => {
    pushField()
    if (rows.length < maxRows) rows.push(row)
    else truncated = true
    row = []
  }
  const appendField = character => {
    if (field.length < maxCellChars) field += character
    else truncated = true
  }

  function append(source, { final = false } = {}) {
    if (finished) return snapshot()
    let text = String(source || '')
    if (rows.length === 0 && row.length === 0 && field === '' && !hasRecordContent) text = text.replace(/^\uFEFF/, '')
    for (let index = 0; index < text.length; index += 1) {
      const character = text[index]
      if (pendingCR) {
        pendingCR = false
        if (character === '\n') continue
      }
      if (pendingQuote) {
        pendingQuote = false
        if (character === '"') { appendField('"'); hasRecordContent = true; continue }
        quoted = false
      }
      if (quoted) {
        if (character === '"') {
          if (index + 1 < text.length) {
            if (text[index + 1] === '"') { appendField('"'); index += 1 }
            else quoted = false
          } else pendingQuote = true
        } else appendField(character)
        hasRecordContent = true
      } else if (character === '"' && field === '') {
        quoted = true
        hasRecordContent = true
      } else if (character === ',') {
        pushField()
        hasRecordContent = true
      } else if (character === '\n' || character === '\r') {
        pushRow()
        hasRecordContent = false
        if (character === '\r' && index + 1 < text.length && text[index + 1] === '\n') index += 1
        else if (character === '\r' && index + 1 === text.length) pendingCR = true
      } else {
        appendField(character)
        hasRecordContent = true
      }
    }
    if (final) {
      if (pendingQuote) { pendingQuote = false; quoted = false }
      if (field || row.length || hasRecordContent) pushRow()
      finished = true
    }
    return snapshot()
  }
  function snapshot() { return { rows: rows.map(cells => [...cells]), truncated } }
  return { append, snapshot }
}

export function parseWorkspaceCsvPreview(source, options = {}) {
  const parser = createWorkspaceCsvPreviewParser(options)
  const result = parser.append(source, { final: true })
  const text = String(source || '').replace(/^\uFEFF/, '')
  if (!text) return { rows: [], truncated: false }
  return result
}

export function normalizeWorkspacePath(value) {
  return String(value || '').replaceAll('\\', '/').replace(/^\.\//, '').replace(/^\.$/, '').replace(/\/+$/, '')
}

export function workspaceFileLocation(value) {
  const path = normalizeWorkspacePath(value)
  const parts = path.split('/').filter(Boolean)
  const name = parts.pop() || ''
  return { path, name, parent: parts.join('/') }
}

export function shouldClearWorkspaceUploadError(currentError, failedUploadError, batchHadFailure = false) {
  return !batchHadFailure && Boolean(failedUploadError) && currentError === failedUploadError
}

export function toNodeFileReference(file) {
  return { path: String(file?.path || ''), name: String(file?.name || '') }
}

export function workspaceDraftKey(agentId, path, scope = 'default') {
  return `dagents.desktop.workspace-draft:${encodeURIComponent(scope)}:${encodeURIComponent(agentId)}:${encodeURIComponent(path)}`
}

export function serializeWorkspaceDraft(content, revision) {
  return JSON.stringify({ content: String(content || ''), revision: String(revision || '') })
}

export function deserializeWorkspaceDraft(raw, serverRevision) {
  if (raw == null) return null
  try {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && typeof parsed.content === 'string') {
      return { content: parsed.content, revision: String(parsed.revision || ''), conflict: Boolean(parsed.revision && serverRevision && parsed.revision !== serverRevision) }
    }
  } catch { /* Older drafts were stored as plain text. */ }
  return { content: String(raw), revision: '', conflict: false }
}

export function agentMediaId(source, expectedAgentId) {
  try {
    const url = new URL(String(source || ''), 'http://127.0.0.1')
    if (!['http:', 'https:'].includes(url.protocol) || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)) return ''
    const match = url.pathname.match(/^\/v1\/agents\/([^/]+)\/media\/([^/]+)$/)
    if (!match || decodeURIComponent(match[1]) !== expectedAgentId) return ''
    const mediaId = decodeURIComponent(match[2])
    return /^[A-Za-z0-9_-]{1,128}$/.test(mediaId) ? mediaId : ''
  } catch { return '' }
}

export function isWorkspaceRequestCurrent(request, current) {
  return request.epoch === current.epoch && request.agentId === current.agentId
    && (request.generation === undefined || request.generation === current.generation)
    && (!request.path || request.path === current.path)
}

export function isWorkspaceWriteRequestCurrent(request, current) {
  return Boolean(request && current && request.requestId === current.requestId
    && request.agentId === current.agentId && request.path === current.path
    && request.scope === current.scope && request.generation === current.generation
    && request.epoch === current.epoch)
}

export function workspaceWriteProgressLabel(phase, bytes = 0, total = 0) {
  if (phase === 'committing') return '正在提交到工作区；此阶段取消后结果可能未知。'
  if (total > 0) return `正在导入 ${Math.min(100, Math.floor(bytes * 100 / total))}%`
  return phase === 'preparing' ? '正在准备暂存文件…' : '正在复制文件到暂存区…'
}

export function createWorkspaceAttachmentBatch(ownerId) {
  return { ownerId, controller: new AbortController(), cancelled: false, currentRequestId: '' }
}

export function cancelWorkspaceAttachmentBatch(batch) {
  if (!batch || batch.cancelled) return
  batch.cancelled = true
  batch.controller.abort()
}

export function isWorkspaceAttachmentBatchCurrent(batch, activeBatch, activeAgentId) {
  return Boolean(batch && batch === activeBatch && !batch.cancelled && batch.ownerId === activeAgentId)
}

export function workspaceContentType(name, suppliedType, bytes) {
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  const explicit = String(suppliedType || '').split(';')[0].trim().toLowerCase()
  const detectedImage = ['image/png','image/jpeg','image/gif','image/webp','image/bmp','image/x-icon'].find(type => hasImageSignature(type, data))
  if (detectedImage) return detectedImage
  if (explicit.startsWith('image/')) return 'application/octet-stream'
  const extension = String(name || '').split('.').pop()?.toLowerCase()
  if (extension === 'pdf' && hasPdfSignature(data)) return 'application/pdf'
  if (extension === 'docx' && hasZipSignature(data)) return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
  if (extension === 'xlsx' && hasZipSignature(data)) return 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
  if (extension === 'html' || extension === 'htm' || explicit === 'text/html') return isUtf8Text(data) ? 'text/html' : 'application/octet-stream'
  if (textExtensions.has(extension) && isUtf8Text(data)) return 'text/plain'
  if (explicit === 'application/pdf' && hasPdfSignature(data)) return explicit
  if (explicit.startsWith('text/') || explicit === 'application/json' || explicit === 'application/xml' || explicit === 'application/javascript') return isUtf8Text(data) ? explicit : 'application/octet-stream'
  if (isUtf8Text(data)) return 'text/plain'
  return 'application/octet-stream'
}

export function workspacePreviewKind(name, suppliedType, bytes) {
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  const extension = String(name || '').split('.').pop()?.toLowerCase() || ''
  const type = workspaceContentType(name, suppliedType, data)
  if (extension === 'doc' || extension === 'xls') return 'unsupported-legacy-office'
  if (extension === 'md' && isUtf8Text(data)) return 'markdown'
  if (extension === 'pdf') return hasPdfSignature(data) ? 'pdf' : 'unsupported-invalid'
  if (extension === 'docx') return hasZipSignature(data) ? 'docx' : 'unsupported-invalid'
  if (extension === 'xlsx') return hasZipSignature(data) ? 'xlsx' : 'unsupported-invalid'
  if (type.startsWith('image/')) return 'image'
  if (extension === 'svg' && hasSvgMarkup(data)) return 'svg'
  if ((extension === 'html' || extension === 'htm' || type === 'text/html') && isUtf8Text(data)) return 'html'
  if (extension === 'csv' && isUtf8Text(data)) return 'csv'
  if (codeExtensions.has(extension) && isUtf8Text(data)) return 'code'
  if (type.startsWith('text/') || ['application/json', 'application/xml', 'application/javascript'].includes(type) || isUtf8Text(data)) return 'text'
  return 'unsupported'
}

export function workspaceCodeLanguage(name) {
  const extension = String(name || '').split('.').pop()?.toLowerCase() || ''
  return ({
    c: 'c', cpp: 'cpp', cs: 'csharp', css: 'css', go: 'go', h: 'c', html: 'xml', htm: 'xml',
    java: 'java', js: 'javascript', json: 'json', mjs: 'javascript', php: 'php', py: 'python',
    rb: 'ruby', rs: 'rust', sh: 'bash', sql: 'sql', toml: 'ini', ts: 'typescript', xml: 'xml',
    yaml: 'yaml', yml: 'yaml', ps1: 'powershell',
  })[extension] || ''
}

export function workspaceFilePreviewNotice(name, kind) {
  const extension = String(name || '').split('.').pop()?.toLowerCase() || 'unknown'
  if (kind === 'unsupported-invalid') return `无法预览此 .${extension} 文件，格式无效或内容不完整。可下载原文件检查。`
  if (kind === 'unsupported-legacy-office') {
    if (extension === 'doc') return '暂不支持预览旧版 Word .doc 文件，可下载查看；建议另存为 .docx。'
    return '暂不支持预览旧版 Excel .xls 文件，可下载查看；建议另存为 .xlsx。'
  }
  return `暂不支持预览 .${extension} 格式，可下载原文件查看。`
}

export function workspaceRasterBlob(path, result) {
  const bytes = result?.bytes instanceof Uint8Array ? result.bytes : new Uint8Array(result?.bytes || [])
  const contentType = workspaceContentType(path, result?.contentType, bytes)
  if (!['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(contentType)) {
    throw new Error('此工作区文件不是受支持的 PNG、JPEG、GIF 或 WebP 图片。')
  }
  return new Blob([bytes], { type: contentType })
}

export function workspaceImageBlob(path, result) {
  const extension = String(path || '').split('.').at(-1)?.toLowerCase()
  if (extension === 'svg') {
    const bytes = result?.bytes instanceof Uint8Array ? result.bytes : new Uint8Array(result?.bytes || [])
    if (bytes.length > 1024 * 1024 || bytes.length === 0) throw new Error('SVG 图片为空或超过 1 MiB 限制。')
    try { new TextDecoder('utf-8', { fatal: true }).decode(bytes) } catch { throw new Error('SVG 图片不是有效的 UTF-8 文本。') }
    const supplied = String(result?.contentType || '').split(';')[0].trim().toLowerCase()
    if (supplied && !['image/svg+xml', 'application/octet-stream', 'text/plain'].includes(supplied)) throw new Error('SVG 图片响应类型无效。')
    return new Blob([bytes], { type: 'image/svg+xml' })
  }
  return workspaceRasterBlob(path, result)
}

function isUtf8Text(bytes) {
  if (bytes.includes(0)) return false
  try { new TextDecoder('utf-8', { fatal: true }).decode(bytes); return true } catch { return false }
}

function hasImageSignature(type, bytes) {
  if (type === 'image/png') return bytes.length >= 8 && [137,80,78,71,13,10,26,10].every((value, index) => bytes[index] === value)
  if (type === 'image/jpeg') return bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff
  if (type === 'image/gif') return bytes.length >= 6 && ['GIF87a','GIF89a'].includes(new TextDecoder().decode(bytes.subarray(0,6)))
  if (type === 'image/webp') return bytes.length >= 12 && new TextDecoder().decode(bytes.subarray(0,4)) === 'RIFF' && new TextDecoder().decode(bytes.subarray(8,12)) === 'WEBP'
  if (type === 'image/bmp') return bytes.length >= 14 && bytes[0] === 0x42 && bytes[1] === 0x4d
  if (type === 'image/x-icon') return bytes.length >= 4 && bytes[0] === 0 && bytes[1] === 0 && bytes[2] === 1 && bytes[3] === 0
  return false
}

function hasPdfSignature(bytes) {
  return bytes.length >= 5 && bytes[0] === 0x25 && bytes[1] === 0x50 && bytes[2] === 0x44 && bytes[3] === 0x46 && bytes[4] === 0x2d
}

function hasZipSignature(bytes) {
  return bytes.length >= 4 && bytes[0] === 0x50 && bytes[1] === 0x4b && [0x03, 0x05, 0x07].includes(bytes[2]) && [0x04, 0x06, 0x08].includes(bytes[3])
}

function hasSvgMarkup(bytes) {
  if (!isUtf8Text(bytes)) return false
  return /^\s*(?:<\?xml\s+[^>]*>\s*)?<svg\b/i.test(new TextDecoder().decode(bytes))
}
