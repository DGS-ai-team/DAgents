const textExtensions = new Set(['md', 'txt', 'json', 'js', 'mjs', 'ts', 'css', 'html', 'xml', 'yaml', 'yml', 'csv', 'py', 'go', 'rs', 'toml', 'sh', 'ps1'])

export function workspaceUploadPath(directory, name) {
  return [String(directory || '').replace(/^\/+|\/+$/g, ''), String(name || '').replace(/^\/+/, '')].filter(Boolean).join('/')
}

export function shouldClearWorkspaceUploadError(currentError, failedUploadError) {
  return Boolean(failedUploadError) && currentError === failedUploadError
}

export function toNodeFileReference(file) {
  return { path: String(file?.path || ''), name: String(file?.name || '') }
}

export function workspaceDraftKey(agentId, path) {
  return `dagents.desktop.workspace-draft:${encodeURIComponent(agentId)}:${encodeURIComponent(path)}`
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
