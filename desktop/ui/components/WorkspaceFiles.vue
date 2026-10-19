<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import UiIcon from '../../../node/webui/frontend/src/components/UiIcon.vue'
import { validateWorkspacePath } from '../../../node/webui/frontend/src/utils/resourceContext.js'
import folderOpenIcon from '@shared-icons/desktop/folder-open.svg?url'
import { createWorkspaceFile, downloadBytes, listWorkspace, readWorkspaceFile, saveWorkspaceFile, uploadWorkspaceFile } from '../workspace-files-api.js'
import { deserializeWorkspaceDraft, isWorkspaceRequestCurrent, normalizeWorkspacePath, serializeWorkspaceDraft, workspaceContentType, workspaceDraftKey, workspaceFileLocation } from '../workspace-files-state.js'

const props = defineProps({ agentId: { type: String, required: true }, active: { type: Boolean, default: false }, workspaceScope: { type: String, default: 'default' }, requestGeneration: { type: Number, default: 0 } })
const emit = defineEmits(['resource-name', 'attach-file'])
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
const previewUrl = ref('')
const fileRefs = ref({})
const editorRef = ref(null)
const draftOwnerId = ref('')
const draftOwnerScope = ref('')
const folderMaskStyle = { maskImage: `url("${folderOpenIcon}")`, WebkitMaskImage: `url("${folderOpenIcon}")` }
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
const filename = computed(() => selectedPath.value.split('/').filter(Boolean).at(-1) || '')
const canEditText = computed(() => Boolean(selectedPath.value) && !fileContentType.value.startsWith('image/') && !fileContentType.value.startsWith('application/octet-stream'))
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
function revokePreview() { if (previewUrl.value) URL.revokeObjectURL(previewUrl.value); previewUrl.value = '' }
function saveDraft() {
  if (!selectedPath.value || !draftOwnerId.value || !draftOwnerScope.value) return
  const key = workspaceDraftKey(draftOwnerId.value, selectedPath.value, draftOwnerScope.value)
  try {
    if (dirty.value) localStorage.setItem(key, serializeWorkspaceDraft(content.value, revision.value))
    else localStorage.removeItem(key)
  } catch { /* Workspace edits remain in memory when local storage is unavailable. */ }
}
function confirmDiscard() { return !dirty.value || window.confirm('当前文件有未保存修改。继续操作会保留本地草稿并离开编辑器，是否继续？') }
async function loadDirectory(path = currentPath.value) {
  if (!props.agentId) return { ok: false, canceled: false }
  if (!confirmDiscard()) return { ok: false, canceled: true }
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
  if (!discard && !confirmDiscard()) return false
  const request = { epoch: ++requestEpoch, agentId: props.agentId, path: entry.path, scope: props.workspaceScope, generation: props.requestGeneration }
  loading.value = true; error.value = ''; revokePreview()
  try {
    const result = await readWorkspaceFile(request.agentId, request.path)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: request.path, generation: props.requestGeneration }) || request.scope !== props.workspaceScope) return false
    selectedPath.value = entry.path
    draftOwnerId.value = request.agentId
    draftOwnerScope.value = request.scope
    contentEditVersion++
    fileBytes.value = result.bytes
    const draft = deserializeWorkspaceDraft(localStorage.getItem(workspaceDraftKey(request.agentId, request.path, request.scope)), result.etag)
    revision.value = draft?.conflict ? draft.revision : result.etag
    draftConflict.value = Boolean(draft?.conflict)
    fileContentType.value = workspaceContentType(entry.name, result.contentType, result.bytes)
    if (fileContentType.value.startsWith('image/')) {
      previewUrl.value = URL.createObjectURL(new Blob([result.bytes], { type: fileContentType.value }))
      content.value = ''
    } else if (fileContentType.value.startsWith('text/') || ['application/json', 'application/xml', 'application/javascript'].includes(fileContentType.value)) {
      content.value = draft?.content ?? new TextDecoder().decode(result.bytes)
    } else {
      content.value = ''
    }
    dirty.value = draft !== null && draft.content !== new TextDecoder().decode(result.bytes)
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
function updateContent(value) { contentEditVersion++; content.value = value; dirty.value = true; saveDraft() }
function discardDraftAndReload() {
  if (loading.value || saving.value || !selectedPath.value || !(draftConflict.value || fileMissing.value || externalUpdate.value)) return
  if (!window.confirm('丢弃当前本地草稿并重新读取工作区文件？')) return
  const path = selectedPath.value
  try { localStorage.removeItem(workspaceDraftKey(draftOwnerId.value, path, draftOwnerScope.value)) } catch { /* ignore unavailable storage */ }
  dirty.value = false
  draftConflict.value = false
  externalUpdate.value = false
  const entry = entries.value.find(item => item.path === path)
  if (entry) void openEntry(entry, true)
  else void refreshResources('manual')
}
async function saveFile() {
  if (!selectedPath.value || !canEditText.value || !dirty.value || saving.value) return
  const request = { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value, scope: props.workspaceScope, generation: props.requestGeneration, saveId: ++saveSequence }
  const text = content.value
  saving.value = true; error.value = ''
  try {
    const bytes = new TextEncoder().encode(text)
    const response = await saveWorkspaceFile(request.agentId, request.path, bytes, revision.value)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value, generation: props.requestGeneration }) || request.scope !== props.workspaceScope || request.saveId !== saveSequence) return
    revision.value = response.etag || revision.value
    fileBytes.value = bytes
    dirty.value = content.value !== text
    if (!dirty.value) { draftConflict.value = false; externalUpdate.value = false; fileMissing.value = false; saveDraft() }
    lastFileCheck = Date.now()
    lastDirectoryCheck = 0
    void refreshResources('local-write')
  } catch (e) {
    if (request.epoch === requestEpoch && request.agentId === props.agentId && request.scope === props.workspaceScope && request.generation === props.requestGeneration && request.saveId === saveSequence) {
      if (e?.status === 412) draftConflict.value = true
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
  if (!confirmDiscard()) return
  const ownerId = props.agentId
  const name = window.prompt('新文件名（相对于当前目录）')
  if (!name?.trim()) return
  if (name.includes('/') || name.includes('\\') || name === '.' || name === '..') { error.value = '请输入单个文件名'; return }
  try {
    const path = workspacePath(currentPath.value, name.trim())
    await createWorkspaceFile(ownerId, path, new TextEncoder().encode(''))
    if (ownerId !== props.agentId) return
    await loadDirectory(currentPath.value)
    await openEntry({ name: name.trim(), path, is_dir: false })
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
  const nextType = workspaceContentType(path, result.contentType, result.bytes)
  if (nextType.startsWith('image/')) {
    const nextUrl = URL.createObjectURL(new Blob([result.bytes], { type: nextType }))
    try {
      const image = new Image()
      image.src = nextUrl
      if (typeof image.decode === 'function') await image.decode()
      if (disposed || request.epoch !== requestEpoch || request.agentId !== props.agentId || request.scope !== props.workspaceScope || request.generation !== props.requestGeneration || request.path !== selectedPath.value || revision.value !== revisionAtStart || contentEditVersion !== editedAtStart || readSequence !== fileReadSequence || saving.value) { URL.revokeObjectURL(nextUrl); lastFileCheck = 0; if (saving.value) refreshAfterSave = true; else refreshQueued = true; return false }
      const oldUrl = previewUrl.value
      fileBytes.value = result.bytes
      revision.value = result.etag || revision.value
      fileContentType.value = nextType
      previewUrl.value = nextUrl
      if (oldUrl) URL.revokeObjectURL(oldUrl)
    } catch (e) { URL.revokeObjectURL(nextUrl); throw e }
    content.value = ''
  } else {
    if (revision.value !== revisionAtStart || contentEditVersion !== editedAtStart || readSequence !== fileReadSequence || dirty.value || saving.value) { lastFileCheck = 0; if (saving.value) refreshAfterSave = true; else refreshQueued = true; return false }
    const editor = editorRef.value
    const selection = editor ? [editor.selectionStart, editor.selectionEnd, editor.scrollTop] : null
    const nextContent = nextType.startsWith('text/') || ['application/json', 'application/xml', 'application/javascript'].includes(nextType) ? new TextDecoder().decode(result.bytes) : ''
    content.value = nextContent
    fileBytes.value = result.bytes
    revision.value = result.etag || revision.value
    fileContentType.value = nextType
    await nextTick()
    const currentEditor = editorRef.value
    if (currentEditor && selection && request.epoch === requestEpoch && request.path === selectedPath.value && request.scope === props.workspaceScope) {
      currentEditor.setSelectionRange(Math.min(selection[0], nextContent.length), Math.min(selection[1], nextContent.length))
      currentEditor.scrollTop = selection[2]
    }
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
    if (!props.active || document.visibilityState === 'hidden' || disposed) return
    if (!isRefreshOwnerCurrent(refreshOwner)) { void refreshResources('return'); return }
    const interval = (fileBytes.value?.byteLength || 0) > 1024 * 1024 ? 30000 : 10000
    const now = Date.now()
    const regularDelay = Math.max(250, Math.min(3000 - (now - lastDirectoryCheck), selectedPath.value ? interval - (now - lastFileCheck) : 3000))
    scheduleRefresh(refreshRetry ? [5000, 10000, 30000][refreshRetry - 1] : regularDelay)
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
function resourceMenuActions() {
  const canRecoverDraft = Boolean(selectedPath.value && !loading.value && !saving.value && (draftConflict.value || fileMissing.value || externalUpdate.value))
  return { canRecoverDraft, recoveryLabel: fileMissing.value ? '丢弃草稿并重试' : '丢弃草稿并重载' }
}
defineExpose({ openPath, refreshResources, discardDraftAndReload, resourceMenuActions })
watch(() => [content.value, dirty.value, selectedPath.value, props.agentId], saveDraft)
watch(() => [props.agentId, props.workspaceScope], () => { requestEpoch++; saveSequence++; saving.value = false; refreshAfterSave = false; if (missingConfirmTimer) window.clearTimeout(missingConfirmTimer); missingConfirmTimer = 0; currentPath.value = ''; selectedPath.value = ''; draftOwnerId.value = ''; draftOwnerScope.value = ''; dirty.value = false; fileRefs.value = {}; revision.value = ''; fileMissing.value = false; externalUpdate.value = false; lastDirectoryCheck = 0; lastFileCheck = 0; void loadDirectory('') }, { immediate: true })
watch(() => props.requestGeneration, () => { requestEpoch++; saveSequence++; saving.value = false; refreshAfterSave = false; if (missingConfirmTimer) window.clearTimeout(missingConfirmTimer); missingConfirmTimer = 0; lastDirectoryCheck = 0; lastFileCheck = 0; if (refreshTimer) window.clearTimeout(refreshTimer); refreshTimer = 0; if (props.active) void refreshResources('return') })
watch(() => props.active, active => { if (active) { lastDirectoryCheck = 0; lastFileCheck = 0; void refreshResources('return') } else if (refreshTimer) { window.clearTimeout(refreshTimer); refreshTimer = 0 } }, { immediate: true })
onMounted(() => { document.addEventListener('visibilitychange', onVisibilityChange); if (props.active) void refreshResources('return') })
onUnmounted(() => { disposed = true; requestEpoch++; if (refreshTimer) window.clearTimeout(refreshTimer); if (missingConfirmTimer) window.clearTimeout(missingConfirmTimer); document.removeEventListener('visibilitychange', onVisibilityChange); revokePreview() })
</script>

<template>
  <section class="workspace-files" aria-label="Agent 工作区文件">
    <header class="workspace-files__toolbar">
      <button type="button" :disabled="!currentPath || loading" title="上级目录" aria-label="上级目录" @click="goUp"><UiIcon name="arrow-up" :size="16" /></button>
      <div class="workspace-files__breadcrumbs"><button type="button" @click="loadDirectory('')">工作区</button><template v-for="(part,index) in breadcrumbs" :key="index"><span>/</span><button type="button" @click="loadDirectory(breadcrumbs.slice(0,index+1).join('/'))">{{ part }}</button></template><span v-if="filename">/</span><b v-if="filename" :title="filename">{{ filename }}</b></div>
      <button type="button" title="刷新" aria-label="刷新" :disabled="loading" @click="refreshResources('manual')"><UiIcon name="refresh-cw" :size="16" /></button>
      <button type="button" title="新建文件" aria-label="新建文件" :disabled="loading" @click="createFile"><UiIcon name="file-plus" :size="16" /></button>
      <button type="button" title="上传到当前目录" aria-label="上传文件" :disabled="loading" @click="uploadInput?.click()"><UiIcon name="upload" :size="16" /></button>
      <input ref="uploadInput" class="workspace-files__hidden" type="file" multiple @change="onUpload">
      <button type="button" :title="dirty && canEditText ? '下载当前本地草稿副本' : '下载文件'" aria-label="下载文件" :disabled="!selectedPath" @click="downloadFile"><UiIcon name="arrow-down" :size="16" /></button>
      <button type="button" title="添加到对话" aria-label="添加到对话" :disabled="!fileRefs[selectedPath]" @click="attachSelectedFile"><UiIcon name="paperclip" :size="16" /></button>
      <button type="button" :title="saving ? '正在保存' : draftConflict ? '文件版本冲突，无法保存' : fileMissing ? '文件已移除，无法保存' : '保存文件（使用读取时的ETag校验）'" aria-label="保存文件" :disabled="!dirty || saving || !canEditText || draftConflict || fileMissing" @click="saveFile"><UiIcon name="save" :size="16" /></button>
      <span v-if="noticeSummary" class="workspace-files__notice" tabindex="0" role="status" aria-live="polite" :aria-label="noticeSummary" aria-describedby="workspace-files-notice-detail"><UiIcon name="alert" :size="16" /><span class="workspace-files__sr-only">{{ noticeSummary }}</span><span id="workspace-files-notice-detail" class="workspace-files__notice-detail" role="tooltip"><span v-for="(detail,index) in noticeDetails" :key="index">{{ detail }}</span></span></span>
    </header>
    <div class="workspace-files__body">
      <nav class="workspace-files__list" aria-label="文件列表">
        <button v-for="entry in entries" :key="entry.path" type="button" :class="{ selected: selectedPath === entry.path }" :title="entry.path" @click="openEntry(entry)"><span v-if="entry.is_dir" class="workspace-files__folder-icon" :style="folderMaskStyle" aria-hidden="true"></span><UiIcon v-else name="file" :size="16" /><span>{{ entry.name }}</span><small v-if="!entry.is_dir">{{ entry.size }}</small></button>
      </nav>
      <main class="workspace-files__editor">
        <div v-if="loading" class="workspace-files__empty">正在读取工作区…</div>
        <template v-else-if="selectedPath">
          <img v-if="fileContentType.startsWith('image/')" class="workspace-files__image" :src="previewUrl" :alt="filename">
          <textarea ref="editorRef" v-else-if="canEditText" v-model="content" spellcheck="false" :aria-label="`编辑 ${filename}`" @input="updateContent($event.target.value)" />
          <div v-else class="workspace-files__empty">已读取此二进制文件，可下载到本机查看。</div>
        </template>
        <div v-else class="workspace-files__empty">{{ entries.length ? '选择工作区文件以预览或编辑。' : '此目录为空，可新建文件或上传文件开始使用。' }}</div>
      </main>
    </div>
  </section>
</template>

<style scoped>
.workspace-files{display:flex;flex:1;min-width:0;min-height:0;box-sizing:border-box;flex-direction:column;background:#fff;color:#30383c}.workspace-files__toolbar{display:flex;align-items:center;gap:6px;min-height:42px;padding:0 12px;border-bottom:1px solid #e1e4e6}.workspace-files__toolbar button{display:grid;place-items:center;min-width:30px;height:30px;padding:0 7px;border:1px solid transparent;border-radius:5px;background:transparent;color:#4c5559;font-size:11px;cursor:pointer}.workspace-files__toolbar button:hover:not(:disabled){background:#eceeef}.workspace-files__toolbar button:disabled{opacity:.45}.workspace-files__breadcrumbs{display:flex;flex:1;align-items:center;gap:6px;min-width:0;overflow:hidden;color:#777f83;font-size:12px}.workspace-files__breadcrumbs button{overflow:hidden;max-width:160px;padding:3px 1px;border:0;background:transparent;color:#566065;text-overflow:ellipsis;white-space:nowrap;cursor:pointer}.workspace-files__breadcrumbs b{overflow:hidden;color:#30383c;text-overflow:ellipsis;white-space:nowrap}.workspace-files__body{display:flex;flex:1;min-height:0}.workspace-files__list{flex:0 0 245px;min-width:150px;overflow:auto;scroll-padding-bottom:82px;padding:8px;border-right:1px solid #d4dce0;background:#fafbfc}.workspace-files__list button{display:flex;align-items:center;gap:8px;width:100%;height:34px;padding:0 8px;border:0;border-radius:5px;background:transparent;color:#4a5357;text-align:left;font-size:12px;cursor:pointer}.workspace-files__list button.selected,.workspace-files__list button:hover{background:#eceeef}.workspace-files__list button span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.workspace-files__list button .workspace-files__folder-icon{flex:none;width:16px;height:16px;background:#586166;mask-repeat:no-repeat;mask-position:center;mask-size:contain}.workspace-files__list button small{color:#777f83;font-size:10px}.workspace-files__editor{display:flex;background:#fff;flex:1;min-width:0;min-height:0;align-items:stretch;justify-content:stretch}.workspace-files__editor textarea{flex:1;min-width:0;min-height:0;scroll-padding-bottom:82px;padding:16px;border:0;outline:0;background:#fff;color:#30383c;font:12px/1.6 Consolas,monospace;resize:none}.workspace-files__image{max-width:100%;max-height:100%;margin:auto;object-fit:contain}.workspace-files__empty{display:grid;flex:1;place-items:center;padding:24px;color:#777f83;font-size:12px;text-align:center}.workspace-files__hidden{display:none}.workspace-files__notice{position:relative;display:grid;flex:0 0 30px;place-items:center;width:30px;height:30px;border-radius:5px;color:#a53c38;cursor:help}.workspace-files__notice:hover,.workspace-files__notice:focus-visible{background:#fff1ef;outline:2px solid #b74a43;outline-offset:1px}.workspace-files__notice-detail{position:absolute;z-index:50;top:calc(100% + 5px);right:0;display:none;min-width:220px;max-width:min(420px,70vw);padding:9px 11px;border:1px solid #e2c7c8;border-radius:6px;background:#fff7f6;box-shadow:0 5px 16px #26374024;color:#7f302d;font-size:11px;line-height:1.45;white-space:normal}.workspace-files__notice-detail>span{display:block}.workspace-files__notice-detail>span+span{margin-top:5px}.workspace-files__notice:hover .workspace-files__notice-detail,.workspace-files__notice:focus .workspace-files__notice-detail{display:block}.workspace-files__sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
</style>
