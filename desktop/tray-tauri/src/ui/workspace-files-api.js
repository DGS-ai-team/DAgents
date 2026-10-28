import { workspaceUploadPath } from './workspace-files-state.js'

async function desktopInvoke(...args) {
  if (globalThis.__DAGENTS_DESKTOP_INVOKE__) return globalThis.__DAGENTS_DESKTOP_INVOKE__(...args)
  const { invoke } = await import('@tauri-apps/api/core')
  return invoke(...args)
}

const encodePath = value => encodeURIComponent(String(value || '').replaceAll('\\', '/'))
const agentPath = agentId => `/v1/agents/${encodeURIComponent(agentId)}/workspace`

function decodeBase64(value) {
  const raw = atob(value || '')
  return Uint8Array.from(raw, char => char.charCodeAt(0))
}

function encodeBase64(bytes) {
  const array = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes)
  let binary = ''
  const chunkSize = 0x8000
  for (let offset = 0; offset < array.length; offset += chunkSize) {
    binary += String.fromCharCode(...array.subarray(offset, Math.min(offset + chunkSize, array.length)))
  }
  return btoa(binary)
}

function assertResponse(response) {
  if (response.status >= 200 && response.status < 300) return
  let code = ''
  try {
    const body = JSON.parse(new TextDecoder().decode(decodeBase64(response.body_base64)))
    code = body?.error?.code || ''
  } catch { /* non-JSON error bodies keep the status-based fallback */ }
  const message = code === 'workspace_file_exists'
    ? '同名文件已存在，请换名后重新上传。'
    : code === 'workspace_file_too_large'
      ? '单次完整读取、上传或保存上限为 16 MiB。大文本可分段只读预览；可使用“另存为原文件”保存任意大小的工作区文件。'
    : response.status === 412
      ? '文件已被其他操作修改，请重新载入后再保存。'
      : response.status === 428
        ? '保存时缺少版本信息，请重新载入文件。'
        : `工作区请求失败（${response.status}）`
  const error = new Error(message)
  error.status = response.status
  throw error
}

export async function listWorkspace(agentId, path = '') {
  const query = `?path=${encodePath(path)}`
  return globalThis.__DAGENTS_API_REQUEST__({ method: 'GET', path: `${agentPath(agentId)}${query}`, body: null })
}

async function binaryRequest(agentId, endpoint, method, path, { bytes, ifMatch, ifNoneMatch } = {}) {
  const query = `?path=${encodePath(path)}`
  const response = await desktopInvoke('desktop_workspace_binary_request', {
    method,
    path: `${agentPath(agentId)}/${endpoint}${query}`,
    bodyBase64: bytes ? encodeBase64(bytes) : null,
    ifMatch: ifMatch || null,
    ifNoneMatch: ifNoneMatch || null,
  })
  assertResponse(response)
  return { notModified: false, bytes: decodeBase64(response.body_base64), contentType: response.content_type, etag: response.etag, fileSize: response.file_size, nextOffset: response.next_offset, status: response.status }
}

export async function readWorkspaceFile(agentId, path, { ifNoneMatch } = {}) {
  const response = await desktopInvoke('desktop_workspace_read_file', {
    agentId,
    path: String(path || '').replaceAll('\\', '/'),
    ifNoneMatch: ifNoneMatch || null,
  })
  if (!(response.status === 304)) assertResponse(response)
  if (response.status === 304) return { notModified: true, bytes: null, contentType: response.content_type, etag: response.etag, status: response.status }
  return { notModified: false, bytes: decodeBase64(response.body_base64), contentType: response.content_type, etag: response.etag, fileSize: response.file_size, nextOffset: response.next_offset, status: response.status }
}

async function readWorkspaceChunk(agentId, path, offset = 0, limit = 256 * 1024, revision = '', text = false) {
  const response = await desktopInvoke('desktop_workspace_read_chunk', {
    agentId,
    path: String(path || '').replaceAll('\\', '/'),
    offset,
    limit,
    revision: revision || null,
    text,
  })
  assertResponse(response)
  return { notModified: false, bytes: decodeBase64(response.body_base64), contentType: response.content_type, etag: response.etag, fileSize: response.file_size, nextOffset: response.next_offset, status: response.status }
}

export async function readWorkspaceTextChunk(agentId, path, offset = 0, limit = 256 * 1024, revision = '') {
  return readWorkspaceChunk(agentId, path, offset, limit, revision, true)
}

export async function saveWorkspaceFileAs(agentId, path, draftContent = null) {
  return desktopInvoke('desktop_workspace_save_as', { agentId, path: String(path || '').replaceAll('\\', '/'), draftContent })
}

export async function revealWorkspaceFile(agentId, path) {
  return desktopInvoke('desktop_workspace_reveal', { agentId, path: String(path || '').replaceAll('\\', '/') })
}

export async function saveWorkspaceFile(agentId, path, bytes, etag) {
  return binaryRequest(agentId, 'file', 'PUT', path, { bytes, ifMatch: etag })
}

export async function createWorkspaceFile(agentId, path, bytes) {
  return binaryRequest(agentId, 'file', 'PUT', path, { bytes, ifNoneMatch: '*' })
}

export async function uploadWorkspaceFile(agentId, directory, file) {
  const bytes = new Uint8Array(await file.arrayBuffer())
  const response = await binaryRequest(agentId, 'upload', 'POST', workspaceUploadPath(directory, file.name), { bytes })
  return JSON.parse(new TextDecoder().decode(response.bytes))
}

export async function readAgentMedia(agentId, mediaId) {
  const response = await desktopInvoke('desktop_media_binary_request', { agentId, mediaId })
  if (response.status < 200 || response.status >= 300) throw new Error(`媒体读取失败（${response.status}）`)
  return new Blob([decodeBase64(response.body_base64)], { type: response.content_type || 'application/octet-stream' })
}
