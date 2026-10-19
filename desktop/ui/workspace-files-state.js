const textExtensions = new Set(['md', 'txt', 'json', 'js', 'mjs', 'ts', 'css', 'html', 'xml', 'yaml', 'yml', 'csv', 'py', 'go', 'rs', 'toml', 'sh', 'ps1'])

export function workspaceUploadPath(directory, name) {
  return [String(directory || '').replace(/^\/+|\/+$/g, ''), String(name || '').replace(/^\/+/, '')].filter(Boolean).join('/')
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

export function workspaceContentType(name, suppliedType, bytes) {
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  const explicit = String(suppliedType || '').split(';')[0].trim().toLowerCase()
  const detectedImage = ['image/png','image/jpeg','image/gif','image/webp'].find(type => hasImageSignature(type, data))
  if (detectedImage) return detectedImage
  if (explicit.startsWith('image/')) return 'application/octet-stream'
  const extension = String(name || '').split('.').pop()?.toLowerCase()
  if (textExtensions.has(extension) && isUtf8Text(data)) return 'text/plain'
  if (explicit === 'application/pdf' && data[0] === 0x25 && data[1] === 0x50 && data[2] === 0x44 && data[3] === 0x46) return explicit
  if (explicit.startsWith('text/') || explicit === 'application/json' || explicit === 'application/xml' || explicit === 'application/javascript') return isUtf8Text(data) ? 'text/plain' : 'application/octet-stream'
  if (isUtf8Text(data)) return 'text/plain'
  return 'application/octet-stream'
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
  return false
}
