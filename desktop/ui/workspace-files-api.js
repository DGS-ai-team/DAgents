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
  return { bytes: decodeBase64(response.body_base64), contentType: response.content_type, etag: response.etag }
}

export async function readWorkspaceFile(agentId, path) {
  return binaryRequest(agentId, 'file', 'GET', path)
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

export function downloadBytes(bytes, filename, contentType = 'application/octet-stream') {
  const blob = new Blob([bytes], { type: contentType })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
