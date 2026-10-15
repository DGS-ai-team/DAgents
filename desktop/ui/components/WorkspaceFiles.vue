<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import UiIcon from '../../../node/webui/frontend/src/components/UiIcon.vue'
import folderOpenIcon from '@shared-icons/desktop/folder-open.svg?url'
import { createWorkspaceFile, downloadBytes, listWorkspace, readWorkspaceFile, saveWorkspaceFile, uploadWorkspaceFile } from '../workspace-files-api.js'
import { deserializeWorkspaceDraft, isWorkspaceRequestCurrent, serializeWorkspaceDraft, workspaceContentType, workspaceDraftKey } from '../workspace-files-state.js'

const props = defineProps({ agentId: { type: String, required: true } })
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
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const uploadInput = ref(null)
const previewUrl = ref('')
const fileRefs = ref({})
const folderMaskStyle = { maskImage: `url("${folderOpenIcon}")`, WebkitMaskImage: `url("${folderOpenIcon}")` }
let requestEpoch = 0
const filename = computed(() => selectedPath.value.split('/').filter(Boolean).at(-1) || '')
const canEditText = computed(() => Boolean(selectedPath.value) && !fileContentType.value.startsWith('image/') && !fileContentType.value.startsWith('application/octet-stream'))
const breadcrumbs = computed(() => currentPath.value ? currentPath.value.split('/').filter(Boolean) : [])

function workspacePath(parent, child) { return [parent, child].filter(Boolean).join('/') }
function normalizeWorkspacePath(value) { return String(value || '').replaceAll('\\', '/').replace(/^\.\/?/, '').replace(/\/+$/, '') }
function revokePreview() { if (previewUrl.value) URL.revokeObjectURL(previewUrl.value); previewUrl.value = '' }
function saveDraft() {
  if (!selectedPath.value || !props.agentId) return
  const key = workspaceDraftKey(props.agentId, selectedPath.value)
  try {
    if (dirty.value) localStorage.setItem(key, serializeWorkspaceDraft(content.value, revision.value))
    else localStorage.removeItem(key)
  } catch { /* Workspace edits remain in memory when local storage is unavailable. */ }
}
function confirmDiscard() { return !dirty.value || window.confirm('当前文件有未保存修改。继续操作会保留本地草稿并离开编辑器，是否继续？') }
async function loadDirectory(path = currentPath.value) {
  if (!props.agentId) return
  if (!confirmDiscard()) return
  const request = { epoch: ++requestEpoch, agentId: props.agentId, path }
  loading.value = true; error.value = ''
  try {
    const result = await listWorkspace(request.agentId, request.path)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: request.path })) return
    currentPath.value = normalizeWorkspacePath(result.path || path)
    entries.value = Array.isArray(result.entries) ? result.entries : []
    selectedPath.value = ''
    content.value = ''
    fileBytes.value = null
    dirty.value = false
    revokePreview()
    emit('resource-name', currentPath.value.split('/').filter(Boolean).at(-1) || '工作区')
  } catch (e) {
    if (request.epoch === requestEpoch && request.agentId === props.agentId) {
      if (e?.status === 412) draftConflict.value = true
      error.value = String(e.message || e)
    }
  }
  finally { if (request.epoch === requestEpoch && request.agentId === props.agentId) loading.value = false }
}
async function openEntry(entry, discard = false) {
  if (entry.is_dir) return loadDirectory(entry.path)
  if (!discard && !confirmDiscard()) return
  const request = { epoch: ++requestEpoch, agentId: props.agentId, path: entry.path }
  loading.value = true; error.value = ''; revokePreview()
  try {
    const result = await readWorkspaceFile(request.agentId, request.path)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: request.path })) return
    selectedPath.value = entry.path
    fileBytes.value = result.bytes
    const draft = deserializeWorkspaceDraft(localStorage.getItem(workspaceDraftKey(request.agentId, request.path)), result.etag)
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
    error.value = draftConflict.value ? '本地草稿基于较旧文件版本；已保留旧版本标记，保存会被拒绝。可丢弃草稿并重新载入。' : ''
    if (entry.file_ref) fileRefs.value[entry.path] = entry.file_ref
    emit('resource-name', entry.name)
  } catch (e) { if (request.epoch === requestEpoch && request.agentId === props.agentId) error.value = String(e.message || e) }
  finally { if (request.epoch === requestEpoch && request.agentId === props.agentId) loading.value = false }
}
function updateContent(value) { content.value = value; dirty.value = true; saveDraft() }
function discardDraftAndReload() {
  const path = selectedPath.value
  try { localStorage.removeItem(workspaceDraftKey(props.agentId, path)) } catch { /* ignore unavailable storage */ }
  dirty.value = false
  draftConflict.value = false
  const entry = entries.value.find(item => item.path === path)
  if (entry) void openEntry(entry, true)
}
async function saveFile() {
  if (!selectedPath.value || !canEditText.value || !dirty.value || saving.value) return
  const request = { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value }
  const text = content.value
  saving.value = true; error.value = ''
  try {
    const bytes = new TextEncoder().encode(text)
    const response = await saveWorkspaceFile(request.agentId, request.path, bytes, revision.value)
    if (!isWorkspaceRequestCurrent(request, { epoch: requestEpoch, agentId: props.agentId, path: selectedPath.value })) return
    revision.value = response.etag || revision.value
    fileBytes.value = bytes
    dirty.value = content.value !== text
    if (!dirty.value) { draftConflict.value = false; saveDraft() }
  } catch (e) {
    if (request.epoch === requestEpoch && request.agentId === props.agentId) {
      if (e?.status === 412) draftConflict.value = true
      error.value = String(e.message || e)
    }
  }
  finally { if (request.epoch === requestEpoch && request.agentId === props.agentId) saving.value = false }
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
  const uploadError = error.value
  await loadDirectory(directory)
  if (uploadError) error.value = uploadError
}
function downloadFile() {
  if (fileBytes.value) downloadBytes(fileBytes.value, filename.value, fileContentType.value || 'application/octet-stream')
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
watch(() => [content.value, dirty.value, selectedPath.value, props.agentId], saveDraft)
watch(() => props.agentId, () => { requestEpoch++; currentPath.value = ''; selectedPath.value = ''; dirty.value = false; fileRefs.value = {}; void loadDirectory('') }, { immediate: true })
onUnmounted(() => { requestEpoch++; revokePreview() })
</script>

<template>
  <section class="workspace-files" aria-label="Agent 工作区文件">
    <header class="workspace-files__toolbar">
      <button type="button" :disabled="!currentPath || loading" title="上级目录" aria-label="上级目录" @click="goUp"><UiIcon name="arrow-up" :size="16" /></button>
      <div class="workspace-files__breadcrumbs"><button type="button" @click="loadDirectory('')">工作区</button><template v-for="(part,index) in breadcrumbs" :key="index"><span>/</span><button type="button" @click="loadDirectory(breadcrumbs.slice(0,index+1).join('/'))">{{ part }}</button></template><span v-if="filename">/</span><b v-if="filename" :title="filename">{{ filename }}</b></div>
      <button type="button" title="刷新" aria-label="刷新" :disabled="loading" @click="loadDirectory()"><UiIcon name="refresh-cw" :size="16" /></button>
      <button type="button" title="新建文件" aria-label="新建文件" :disabled="loading" @click="createFile"><UiIcon name="file-plus" :size="16" /></button>
      <button type="button" title="上传到当前目录" aria-label="上传文件" :disabled="loading" @click="uploadInput?.click()"><UiIcon name="paperclip" :size="16" /></button>
      <input ref="uploadInput" class="workspace-files__hidden" type="file" multiple @change="onUpload">
      <button type="button" title="下载文件" aria-label="下载文件" :disabled="!selectedPath" @click="downloadFile"><UiIcon name="arrow-down" :size="16" /></button>
      <button type="button" title="添加到对话" aria-label="添加到对话" :disabled="!fileRefs[selectedPath]" @click="attachSelectedFile">添加到对话</button>
      <button type="button" title="保存文件（使用读取时的ETag校验）" aria-label="保存文件" :disabled="!dirty || saving || !canEditText || draftConflict" @click="saveFile">{{ saving ? '保存中' : draftConflict ? '版本冲突' : '保存' }}</button>
      <button v-if="draftConflict" type="button" title="丢弃旧版本草稿并重新载入服务器文件" @click="discardDraftAndReload">丢弃草稿并重载</button>
    </header>
    <p v-if="error" class="workspace-files__error" role="alert">{{ error }}</p>
    <div class="workspace-files__body">
      <nav class="workspace-files__list" aria-label="文件列表">
        <button v-for="entry in entries" :key="entry.path" type="button" :class="{ selected: selectedPath === entry.path }" :title="entry.path" @click="openEntry(entry)"><span v-if="entry.is_dir" class="workspace-files__folder-icon" :style="folderMaskStyle" aria-hidden="true"></span><UiIcon v-else name="file" :size="16" /><span>{{ entry.name }}</span><small v-if="!entry.is_dir">{{ entry.size }}</small></button>
      </nav>
      <main class="workspace-files__editor">
        <div v-if="loading" class="workspace-files__empty">正在读取工作区…</div>
        <template v-else-if="selectedPath">
          <img v-if="fileContentType.startsWith('image/')" class="workspace-files__image" :src="previewUrl" :alt="filename">
          <textarea v-else-if="canEditText" v-model="content" spellcheck="false" :aria-label="`编辑 ${filename}`" @input="updateContent($event.target.value)" />
          <div v-else class="workspace-files__empty">已读取此二进制文件，可下载到本机查看。</div>
        </template>
        <div v-else class="workspace-files__empty">{{ entries.length ? '选择工作区文件以预览或编辑。' : '此目录为空，可新建文件或上传文件开始使用。' }}</div>
      </main>
    </div>
  </section>
</template>

<style scoped>
.workspace-files{display:flex;flex:1;min-width:0;min-height:0;box-sizing:border-box;flex-direction:column;background:#fff;color:#30383c}.workspace-files__toolbar{display:flex;align-items:center;gap:6px;min-height:42px;padding:0 12px;border-bottom:1px solid #e1e4e6}.workspace-files__toolbar button{display:grid;place-items:center;min-width:30px;height:30px;padding:0 7px;border:1px solid transparent;border-radius:5px;background:transparent;color:#4c5559;font-size:11px;cursor:pointer}.workspace-files__toolbar button:hover:not(:disabled){background:#eceeef}.workspace-files__toolbar button:disabled{opacity:.45}.workspace-files__breadcrumbs{display:flex;flex:1;align-items:center;gap:6px;min-width:0;overflow:hidden;color:#777f83;font-size:12px}.workspace-files__breadcrumbs button{overflow:hidden;max-width:160px;padding:3px 1px;border:0;background:transparent;color:#566065;text-overflow:ellipsis;white-space:nowrap;cursor:pointer}.workspace-files__breadcrumbs b{overflow:hidden;color:#30383c;text-overflow:ellipsis;white-space:nowrap}.workspace-files__body{display:flex;flex:1;min-height:0}.workspace-files__list{flex:0 0 245px;min-width:150px;overflow:auto;scroll-padding-bottom:82px;padding:8px;border-right:1px solid #d4dce0;background:#fafbfc}.workspace-files__list button{display:flex;align-items:center;gap:8px;width:100%;height:34px;padding:0 8px;border:0;border-radius:5px;background:transparent;color:#4a5357;text-align:left;font-size:12px;cursor:pointer}.workspace-files__list button.selected,.workspace-files__list button:hover{background:#eceeef}.workspace-files__list button span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.workspace-files__list button .workspace-files__folder-icon{flex:none;width:16px;height:16px;background:#586166;mask-repeat:no-repeat;mask-position:center;mask-size:contain}.workspace-files__list button small{color:#777f83;font-size:10px}.workspace-files__editor{display:flex;background:#fff;flex:1;min-width:0;min-height:0;align-items:stretch;justify-content:stretch}.workspace-files__editor textarea{flex:1;min-width:0;min-height:0;scroll-padding-bottom:82px;padding:16px;border:0;outline:0;background:#fff;color:#30383c;font:12px/1.6 Consolas,monospace;resize:none}.workspace-files__image{max-width:100%;max-height:100%;margin:auto;object-fit:contain}.workspace-files__empty{display:grid;flex:1;place-items:center;padding:24px;color:#777f83;font-size:12px;text-align:center}.workspace-files__error{margin:0;padding:6px 12px;background:#fbf1f0;color:#a53c38;font-size:11px}.workspace-files__hidden{display:none}
</style>
