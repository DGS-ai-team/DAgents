import { workspacePreviewLimits } from './workspace-files-state.js'

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
      ? '单次完整读取和文本保存上限为 16 MiB。大文本可分段只读预览；通过本机文件选择器导入上限为 1 GiB，拖放或粘贴导入上限为 16 MiB；可使用“另存为原文件”导出完整文件。'
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

export async function saveWorkspaceFile(agentId, path, bytes, etag, { requestId = newWorkspaceWriteRequestId() } = {}) {
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  if (data.byteLength > workspacePreviewLimits.bytes) throw new Error('文本保存上限为 16 MiB。')
  const content = new TextDecoder('utf-8', { ignoreBOM: true }).decode(data)
  const result = await desktopInvoke('desktop_workspace_save_text', { agentId, path: String(path || '').replaceAll('\\', '/'), content, expectedRevision: etag, requestId })
  return assertWriteResult(result)
}

function assertWriteResult(result) {
  if (result?.status === 'saved') {
    const revision = result.revision ?? result.etag ?? ''
    const etag = revision && !(revision.startsWith('"') && revision.endsWith('"')) ? `"${revision}"` : revision
    return { ...result, file_ref: result.fileRef ?? result.file_ref, etag }
  }
  const message = result?.message || (result?.errorCode === 'workspace_file_exists' ? '同名文件已存在，请换名后重新导入。' : { conflict: '文件已被其他操作修改，请重新载入后再保存。', unknown: '写入结果未知。请重新读取文件并保留当前草稿。', cancelled: '操作已取消。' }[result?.status] || '工作区写入失败。')
  const error = new Error(message)
  error.status = result?.httpStatus || (result?.status === 'conflict' ? 412 : 0)
  error.writeStatus = result?.status || 'error'
  throw error
}
export function newWorkspaceWriteRequestId() {
  return globalThis.crypto?.randomUUID?.() || `workspace-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export async function createWorkspaceFile(agentId, path, bytes, { requestId = newWorkspaceWriteRequestId() } = {}) {
  const normalized = String(path || '').replaceAll('\\', '/')
  const slash = normalized.lastIndexOf('/')
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || [])
  if (data.byteLength > workspacePreviewLimits.bytes) throw new Error('文本保存上限为 16 MiB。')
  const content = new TextDecoder('utf-8', { ignoreBOM: true }).decode(data)
  const result = await desktopInvoke('desktop_workspace_create_text', { agentId, directory: slash < 0 ? '' : normalized.slice(0, slash), fileName: normalized.slice(slash + 1), content, requestId })
  return assertWriteResult(result)
}

export async function uploadWorkspaceFile(agentId, directory, file, { requestId = newWorkspaceWriteRequestId(), signal } = {}) {
  if (file.size > workspacePreviewLimits.bytes) throw new Error('拖放或剪贴板导入上限为 16 MiB；使用“导入本机文件”选择大文件。')
  if (signal?.aborted) throw Object.assign(new Error('操作已取消。'), { writeStatus: 'cancelled' })
  const buffer = await file.arrayBuffer()
  if (signal?.aborted) throw Object.assign(new Error('操作已取消。'), { writeStatus: 'cancelled' })
  const bytes = new Uint8Array(buffer)
  const result = await desktopInvoke('desktop_workspace_import_bytes', { agentId, directory: String(directory || '').replaceAll('\\', '/'), fileName: file.name, bytes: Array.from(bytes), requestId })
  return assertWriteResult(result)
}

export async function pickWorkspaceImport(agentId, directory, { requestId = newWorkspaceWriteRequestId(), maxFiles = 8 } = {}) {
  const result = await desktopInvoke('desktop_workspace_pick_import', { agentId, directory: String(directory || '').replaceAll('\\', '/'), requestId, maxFiles })
  if (Array.isArray(result?.results)) return { ...result, results: result.results.map(item => item.status === 'saved' ? assertWriteResult(item) : item) }
  return assertWriteResult(result)
}

export async function cancelWorkspaceWrite(requestId) {
  if (!requestId) return
  return desktopInvoke('desktop_workspace_cancel_write', { requestId })
}

export async function readAgentMedia(agentId, mediaId) {
  const response = await desktopInvoke('desktop_media_binary_request', { agentId, mediaId })
  if (response.status < 200 || response.status >= 300) throw new Error(`媒体读取失败（${response.status}）`)
  return new Blob([decodeBase64(response.body_base64)], { type: response.content_type || 'application/octet-stream' })
}
