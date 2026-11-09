// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import WorkspaceFiles from './WorkspaceFiles.vue'
import WorkspaceReferenceAttachments from './WorkspaceReferenceAttachments.vue'
import { dispatchWorkspaceMessage, hasWorkspaceMessageContent, settleWorkspaceReferences } from '../workspace-references.js'
import * as api from '../workspace-files-api.js'
import { clearWorkspaceViewsForTests } from '../workspace-view-state.js'

vi.mock('../workspace-files-api.js', () => ({
  createWorkspaceFile: vi.fn(),
  cancelWorkspaceWrite: vi.fn(),
  listWorkspace: vi.fn(),
  newWorkspaceWriteRequestId: vi.fn(() => 'write-request-1'),
  pickWorkspaceImport: vi.fn(),
  readWorkspaceFile: vi.fn(),
  readWorkspaceTextChunk: vi.fn(),
  revealWorkspacePath: vi.fn(),
  saveWorkspaceFile: vi.fn(),
  saveWorkspaceFileAs: vi.fn(),
  uploadWorkspaceFile: vi.fn(),
}))

const bytes = text => new TextEncoder().encode(text)
const directories = {
  '': [
    { name: '.gitignore', path: '.gitignore', is_dir: false, size: 20 },
    { name: 'reports', path: 'reports', is_dir: true, size: 0 },
    { name: 'a.txt', path: 'a.txt', is_dir: false, size: 10 },
    { name: 'b.txt', path: 'b.txt', is_dir: false, size: 10 },
    { name: 'records.csv', path: 'records.csv', is_dir: false, size: 32 },
    { name: 'large.bin', path: 'large.bin', is_dir: false, size: 17 * 1024 * 1024 },
    { name: 'too-large.bin', path: 'too-large.bin', is_dir: false, size: 257 * 1024 * 1024 },
    { name: 'large.txt', path: 'large.txt', is_dir: false, size: 17 * 1024 * 1024 },
    { name: 'large.md', path: 'large.md', is_dir: false, size: 17 * 1024 * 1024 },
    { name: 'large.go', path: 'large.go', is_dir: false, size: 17 * 1024 * 1024 },
    { name: 'large.csv', path: 'large.csv', is_dir: false, size: 17 * 1024 * 1024 },
  ],
  reports: [{ name: 'summary.txt', path: 'reports/summary.txt', is_dir: false, size: 20 }],
}
let wrapper

function result(text, etag = 'etag-1') {
  return { bytes: bytes(text), contentType: 'application/octet-stream', etag }
}

beforeEach(() => {
  vi.clearAllMocks()
  clearWorkspaceViewsForTests()
  localStorage.clear()
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: directories[path] || [] }))
  api.readWorkspaceFile.mockImplementation(async (_agentId, path) => {
    if (path === 'large.bin' || path === 'too-large.bin') throw Object.assign(new Error('File exceeds the full read limit'), { status: 413 })
    return result(path)
  })
  api.readWorkspaceTextChunk.mockImplementation(async (_agentId, _path, offset = 0) => offset === 0
    ? { ...result('# First section\n'), fileSize: 300000, nextOffset: 262144 }
    : { ...result('\n## Next section\n'), fileSize: 300000, nextOffset: 300000 })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Desktop workspace resource navigation', () => {
  it('opens DOCX files in the rendered document preview by default', async () => {
    const docx = new Uint8Array(await readFile(resolve(process.cwd(), 'node_modules/mammoth/test/test-data/single-paragraph.docx')))
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({
      path,
      entries: path ? [] : [{ name: 'guide.docx', path: 'guide.docx', is_dir: false, size: docx.byteLength }],
    }))
    api.readWorkspaceFile.mockResolvedValueOnce({ bytes: docx, contentType: 'application/octet-stream', etag: 'docx-etag' })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect(await wrapper.vm.openPath('guide.docx')).toEqual({ ok: true, canceled: false })
    await flushPromises()
    expect(wrapper.find('.workspace-files__document-frame').exists()).toBe(true)
    expect(wrapper.find('.workspace-files__document-frame').attributes('srcdoc')).toContain('Walking on imported air')
    expect(wrapper.text()).not.toContain('无法读取此文件')
  })

  it('shows Python syntax highlighting and a visible preview/source switch', async () => {
    const source = 'def answer():\n    return 42\n'
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({
      path,
      entries: path ? [] : [{ name: 'answer.py', path: 'answer.py', is_dir: false, size: source.length }],
    }))
    api.readWorkspaceFile.mockResolvedValueOnce({ bytes: bytes(source), contentType: 'application/octet-stream', etag: 'python-etag' })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect(await wrapper.vm.openPath('answer.py')).toEqual({ ok: true, canceled: false })
    await flushPromises()
    await vi.waitFor(() => expect(wrapper.find('.workspace-files__code-preview .hljs-keyword').text()).toBe('def'))
    expect(wrapper.find('.workspace-files__code-preview .hljs-number').text()).toBe('42')
    expect(wrapper.find('.workspace-files__preview-toggle').exists()).toBe(true)
    await wrapper.findAll('.workspace-files__preview-toggle button').find(button => button.text() === '源代码').trigger('click')
    expect(wrapper.find('textarea').element.value).toBe(source)
  })

  it('offers an inline Markdown preview/source switch in the toolbar', async () => {
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({
      path,
      entries: path ? [] : [{ name: 'guide.md', path: 'guide.md', is_dir: false, size: 8 }, { name: 'notes.md', path: 'notes.md', is_dir: false, size: 9 }],
    }))
    api.readWorkspaceFile.mockImplementation(async (_agentId, path) => result(path === 'guide.md' ? '# Guide' : '# Notes'))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect(await wrapper.vm.openPath('guide.md')).toEqual({ ok: true, canceled: false })
    const controls = wrapper.findAll('.workspace-files__preview-toggle button')
    expect(controls).toHaveLength(2)
    expect(controls[0].attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('.workspace-files__markdown-preview h1').text()).toBe('Guide')
    await controls[1].trigger('click')
    expect(wrapper.find('textarea').element.value).toBe('# Guide')
    await wrapper.vm.openPath('notes.md')
    await flushPromises()
    expect(wrapper.find('.workspace-files__markdown-preview h1').text()).toBe('Notes')
    await wrapper.vm.openPath('guide.md')
    await flushPromises()
    expect(wrapper.find('textarea').element.value).toBe('# Guide')
    await controls[1].trigger('click')
    expect(wrapper.find('textarea').element.value).toBe('# Guide')
  })

  it('adds a whole-file reference from the visible toolbar action without changing pages', async () => {
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path ? [] : [{ name: 'guide.md', path: 'guide.md', is_dir: false, size: 10 }] }))
    api.readWorkspaceFile.mockResolvedValueOnce(result('# Guide'))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'reference-action-agent', attachmentLimitReached: false } })
    await flushPromises()
    await wrapper.vm.openPath('guide.md')
    const referenceButton = wrapper.find('[aria-label="引用文件到对话"]')
    expect(referenceButton.attributes('title')).toContain('引用 guide.md')
    await referenceButton.trigger('click')
    const reference = wrapper.emitted('annotation-update')?.[0]?.[0]
    expect(reference).toMatchObject({ agentId: 'reference-action-agent', path: 'guide.md', anchor: { type: 'file', label: '整个文件' } })
    expect(wrapper.find('.workspace-files__markdown-preview').exists()).toBe(true)
    await wrapper.setProps({ attachmentLimitReached: true })
    const limitedReferenceButton = wrapper.find('[aria-label="无法引用：一条消息最多添加 8 个附件或注解"]')
    expect(limitedReferenceButton.element.disabled).toBe(true)
    expect(limitedReferenceButton.attributes('title')).toContain('最多添加 8 个附件或注解')
  })

  it('carries a selected whole-file reference into chat, sends its file_ref, and retains it after failure', async () => {
    const submit = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(undefined)
    const agentId = 'reference-chat-agent'
    const ChatHarness = defineComponent({
      setup() {
        const inChat = ref(false)
        const references = ref([])
        const captureReference = reference => {
          if (reference.agentId === agentId) references.value = [...references.value, reference]
        }
        const send = async () => {
          const sentReferences = references.value.slice()
          try {
            await dispatchWorkspaceMessage({ agentId, references: sentReferences }, submit)
            references.value = settleWorkspaceReferences({ [agentId]: references.value }, agentId, sentReferences, true)[agentId]
          } catch { /* A failed submit leaves the selected reference queued for retry. */ }
        }
        return () => h('div', [
          h('button', { type: 'button', onClick: () => { inChat.value = !inChat.value } }, '切换到聊天'),
          inChat.value
            ? h('section', [
              h(WorkspaceReferenceAttachments, { references: references.value }),
              h('button', { type: 'button', disabled: !hasWorkspaceMessageContent({ references: references.value }), onClick: send }, '发送'),
            ])
            : h(WorkspaceFiles, { agentId, onAnnotationUpdate: captureReference }),
        ])
      },
    })
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({
      path,
      entries: path ? [] : [{ name: 'guide.md', path: 'guide.md', file_ref: 'C:/private/agent/guide.md', is_dir: false, size: 8 }],
    }))
    api.readWorkspaceFile.mockResolvedValueOnce(result('# Guide'))
    wrapper = mount(ChatHarness)
    await flushPromises()
    const guide = wrapper.findAll('.workspace-files__list button').find(button => button.text().includes('guide.md'))
    await guide.trigger('click')
    await flushPromises()
    await wrapper.find('[aria-label="引用文件到对话"]').trigger('click')
    expect(wrapper.findComponent(WorkspaceFiles).emitted('annotation-update')?.[0]?.[0]).toMatchObject({ agentId, path: 'guide.md' })
    await wrapper.findAll('button').find(button => button.text() === '切换到聊天').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('guide.md · 整个文件')
    const sendButton = wrapper.findAll('section button').find(button => button.text() === '发送')
    expect(sendButton.element.disabled).toBe(false)
    await sendButton.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('guide.md · 整个文件')
    expect(submit).toHaveBeenCalledTimes(1)
    await sendButton.trigger('click')
    await flushPromises()
    expect(submit).toHaveBeenCalledTimes(2)
    expect(submit.mock.calls[1]).toMatchObject([
      agentId,
      '请结合我引用的工作区内容处理。',
      [],
      [{ id: expect.any(String), path: 'C:/private/agent/guide.md', name: 'guide.md', revision: 'etag-1', draft: false, annotation: '', anchor: { type: 'file', label: '整个文件', quote: '', location: {} } }],
    ])
    expect(wrapper.text()).not.toContain('guide.md · 整个文件')
  })

  it('offers the same whole-file reference action in the file context menu', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'context-reference-agent' } })
    await flushPromises()
    const file = wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'a.txt')
    await file.trigger('contextmenu', { clientX: 40, clientY: 50 })
    const item = [...document.querySelectorAll('[role="menuitem"]')].find(button => button.textContent.includes('引用到对话'))
    expect(item).toBeTruthy()
    item.click()
    await flushPromises()
    expect(wrapper.emitted('annotation-update')?.[0]?.[0]).toMatchObject({ path: 'a.txt', anchor: { label: '整个文件' } })
  })

  it('restores a bounded workspace view for the same Agent and scope', async () => {
    const props = { agentId: 'view-restore-agent', workspaceScope: 'node-view-a' }
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    await wrapper.vm.openPath('reports/summary.txt')
    wrapper.unmount()
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    expect(wrapper.find('.workspace-files__breadcrumbs').text()).toContain('reports')
    expect(wrapper.find('.workspace-files__breadcrumbs').text()).toContain('summary.txt')
  })

  it('keeps workspace view snapshots isolated by Agent and Node scope', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'scoped-view-agent-a', workspaceScope: 'node-view-a' } })
    await flushPromises()
    await wrapper.vm.openPath('reports/summary.txt')
    wrapper.unmount()

    wrapper = mount(WorkspaceFiles, { props: { agentId: 'scoped-view-agent-b', workspaceScope: 'node-view-a' } })
    await flushPromises()
    expect(wrapper.find('.workspace-files__breadcrumbs').text()).toBe('工作区')
    wrapper.unmount()

    wrapper = mount(WorkspaceFiles, { props: { agentId: 'scoped-view-agent-a', workspaceScope: 'node-view-b' } })
    await flushPromises()
    expect(wrapper.find('.workspace-files__breadcrumbs').text()).toBe('工作区')
  })

  it('does not restore a view after the configured workspace location changes', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'location-view-agent', workspaceScope: 'node-view', workspaceLocation: 'D:/workspace-a' } })
    await flushPromises()
    await wrapper.vm.openPath('reports/summary.txt')
    wrapper.unmount()

    wrapper = mount(WorkspaceFiles, { props: { agentId: 'location-view-agent', workspaceScope: 'node-view', workspaceLocation: 'D:/workspace-b' } })
    await flushPromises()
    expect(wrapper.find('.workspace-files__breadcrumbs').text()).toBe('工作区')
  })

  it('falls back to the nearest existing directory when the remembered file was deleted', async () => {
    const props = { agentId: 'deleted-view-agent', workspaceScope: 'node-deleted' }
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    await wrapper.vm.openPath('reports/summary.txt')
    wrapper.unmount()

    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path === '' ? directories[''] : [] }))
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    expect(wrapper.find('.workspace-files__breadcrumbs').text()).toContain('reports')
    expect(wrapper.find('.workspace-files__breadcrumbs b').exists()).toBe(false)
    wrapper.unmount()

    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path === '' ? directories[''].filter(entry => entry.path !== 'reports') : [] }))
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    expect(wrapper.find('.workspace-files__breadcrumbs').text()).toBe('工作区')
  })

  it('restores the Markdown mode and scroll position on remount', async () => {
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path ? [] : [{ name: 'guide.md', path: 'guide.md', is_dir: false, size: 10 }] }))
    api.readWorkspaceFile.mockImplementation(async () => result('# Guide'))
    const props = { agentId: 'scroll-view-agent', workspaceScope: 'node-scroll', active: true }
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    await wrapper.vm.openPath('guide.md')
    await wrapper.findAll('.workspace-files__preview-toggle button').find(button => button.text() === '源内容').trigger('click')
    const firstEditor = wrapper.find('textarea').element
    Object.defineProperty(firstEditor, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(firstEditor, 'scrollHeight', { configurable: true, value: 500 })
    firstEditor.scrollTop = 73
    wrapper.unmount()

    wrapper = mount(WorkspaceFiles, { props: { ...props, active: false } })
    await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(true)
    wrapper.unmount()

    wrapper = mount(WorkspaceFiles, { props: { ...props, active: false } })
    await flushPromises()
    const remountedEditor = wrapper.find('textarea').element
    Object.defineProperty(remountedEditor, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(remountedEditor, 'scrollHeight', { configurable: true, value: 500 })
    expect(remountedEditor.scrollTop).toBe(0)
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(wrapper.find('textarea').element.scrollTop).toBe(73)
    expect(wrapper.findAll('.workspace-files__preview-toggle button').find(button => button.text() === '源内容').attributes('aria-pressed')).toBe('true')

    wrapper.unmount()
    wrapper = mount(WorkspaceFiles, { props: { ...props, active: false } })
    await flushPromises()
    const userEditor = wrapper.find('textarea').element
    Object.defineProperty(userEditor, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(userEditor, 'scrollHeight', { configurable: true, value: 500 })
    await wrapper.find('.workspace-files__editor-content').trigger('wheel')
    userEditor.scrollTop = 19
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(userEditor.scrollTop).toBe(19)
  })

  it('clamps a remembered scroll when the restored file is now shorter', async () => {
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path ? [] : [{ name: 'guide.md', path: 'guide.md', is_dir: false, size: 10 }] }))
    api.readWorkspaceFile.mockResolvedValue(result('# Guide'))
    const props = { agentId: 'short-scroll-agent', workspaceScope: 'node-short-scroll', active: true }
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    await wrapper.vm.openPath('guide.md')
    const editor = wrapper.find('.workspace-files__markdown-preview').element
    Object.defineProperty(editor, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(editor, 'scrollHeight', { configurable: true, value: 500 })
    editor.scrollTop = 73
    wrapper.unmount()

    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    const shortEditor = wrapper.find('.workspace-files__markdown-preview').element
    Object.defineProperty(shortEditor, 'clientHeight', { configurable: true, value: 100 })
    Object.defineProperty(shortEditor, 'scrollHeight', { configurable: true, value: 100 })
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(shortEditor.scrollTop).toBe(0)

    Object.defineProperty(shortEditor, 'scrollHeight', { configurable: true, value: 500 })
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(shortEditor.scrollTop).toBe(0)
  })

  it('does not apply a late restored-file scroll after the user opens another file', async () => {
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path ? [] : [{ name: 'guide.md', path: 'guide.md', is_dir: false, size: 10 }, { name: 'notes.md', path: 'notes.md', is_dir: false, size: 10 }] }))
    api.readWorkspaceFile.mockImplementation(async (_agentId, path) => result(path === 'guide.md' ? '# Guide' : '# Notes'))
    const props = { agentId: 'late-restore-agent', workspaceScope: 'node-late' }
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    await wrapper.vm.openPath('guide.md')
    wrapper.find('.workspace-files__markdown-preview').element.scrollTop = 73
    wrapper.unmount()

    let finishRestoredRead
    api.readWorkspaceFile.mockImplementation((_agentId, path) => path === 'guide.md'
      ? new Promise(resolve => { finishRestoredRead = resolve })
      : Promise.resolve(result('# Notes')))
    wrapper = mount(WorkspaceFiles, { props })
    await flushPromises()
    await vi.waitFor(() => expect(finishRestoredRead).toBeTypeOf('function'))
    await wrapper.vm.openPath('notes.md')
    const notesPreview = wrapper.find('.workspace-files__markdown-preview').element
    notesPreview.scrollTop = 7
    finishRestoredRead(result('# Guide'))
    await flushPromises()
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('notes.md')
    expect(wrapper.find('.workspace-files__markdown-preview').element.scrollTop).toBe(7)
  })

  it('renders Markdown and lazily appends bounded chunks for large text files', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect(await wrapper.vm.openPath('large.md')).toEqual({ ok: true, canceled: false })
    expect(api.readWorkspaceFile).not.toHaveBeenCalledWith('agent-a', 'large.md')
    expect(api.readWorkspaceTextChunk).toHaveBeenCalledWith('agent-a', 'large.md', 0, 256 * 1024)
    expect(wrapper.find('.workspace-files__markdown-preview h1').text()).toBe('First section')
    expect(wrapper.text()).toContain('剩余 37,856 字节')

    await wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()
    expect(api.readWorkspaceTextChunk).toHaveBeenLastCalledWith('agent-a', 'large.md', 262144, 256 * 1024, 'etag-1')
    expect(wrapper.find('.workspace-files__markdown-preview h2').text()).toBe('Next section')
    expect(wrapper.find('.workspace-files__markdown-preview').text()).toContain('First section')
    expect(wrapper.find('[aria-label="保存文件"]').attributes('disabled')).toBeDefined()
  })

  it('shows large chunked source as plain read-only text and labels the preview partial', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('large.go')

    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('# First section')
    expect(wrapper.find('.workspace-files__code-preview').exists()).toBe(false)
    expect(wrapper.find('.workspace-files__chunk-toolbar').text()).toContain('大文件部分内容')
    expect(wrapper.find('.workspace-files__chunk-toolbar').text()).toContain('剩余')
  })

  it('treats a complete first chunk as a full file when directory size metadata is stale', async () => {
    const strongRevision = `"${'a'.repeat(64)}"`
    api.readWorkspaceTextChunk.mockResolvedValueOnce({ ...result('now small'), fileSize: 8, nextOffset: 8, etag: 'W/"8-123"' })
    api.readWorkspaceFile.mockResolvedValueOnce({ bytes: bytes('now small'), contentType: 'text/plain', etag: strongRevision })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')

    expect(wrapper.find('.workspace-files__chunk-toolbar').exists()).toBe(false)
    expect(wrapper.find('textarea').element.value).toBe('now small')
    await wrapper.find('textarea').setValue('edited small file')
    api.saveWorkspaceFile.mockResolvedValueOnce({ status: 'saved', etag: strongRevision })
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await flushPromises()
    expect(api.saveWorkspaceFile.mock.calls.at(-1)[3]).toBe(strongRevision)
  })

  it('keeps a complete chunk read-only if the file grows before its strong full read', async () => {
    api.readWorkspaceTextChunk
      .mockResolvedValueOnce({ ...result('old complete content'), fileSize: 20, nextOffset: 20, etag: 'W/"20-123"' })
      .mockResolvedValueOnce({ ...result('# New large prefix\n'), fileSize: 17 * 1024 * 1024, nextOffset: 256 * 1024, etag: 'W/"17825792-456"' })
    api.readWorkspaceFile.mockRejectedValueOnce(Object.assign(new Error('File exceeds full read limit'), { status: 413 }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')

    expect(api.readWorkspaceFile).toHaveBeenCalledTimes(1)
    expect(api.readWorkspaceTextChunk).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('New large prefix')
    expect(wrapper.find('.workspace-files__chunk-toolbar').exists()).toBe(true)
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.find('[aria-label="保存文件"]').attributes('disabled')).toBeDefined()
  })

  it('offers reload instead of a broken download when a chunk revision changes', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')
    api.readWorkspaceTextChunk.mockRejectedValueOnce(Object.assign(new Error('revision changed'), { status: 409 }))
    await wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('重新读取文件')
    expect(wrapper.text()).not.toContain('下载原文件')
  })

  it('sends the current dirty text to native Save As and reports completion', async () => {
    let finishSaveAs
    api.saveWorkspaceFileAs.mockImplementationOnce(() => new Promise(resolve => { finishSaveAs = resolve }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('current draft text')
    await wrapper.find('.workspace-files__actions-menu summary').trigger('click')
    await wrapper.findAll('.workspace-files__actions-popover button').find(button => button.text().includes('另存为')).trigger('click')
    await flushPromises()

    expect(api.saveWorkspaceFileAs).toHaveBeenCalledWith('agent-a', 'a.txt', 'current draft text')
    expect(wrapper.find('.workspace-files__action-status').text()).toBe('正在选择保存位置或另存文件…')
    finishSaveAs({ status: 'saved', bytesWritten: 18 })
    await flushPromises()
    expect(wrapper.find('.workspace-files__action-status').text()).toContain('已另存为')
    expect(wrapper.find('.workspace-files__action-status').text()).toContain('18 字节')
  })

  it('reports native Save As cancellation and supports files beyond the former download cap', async () => {
    api.saveWorkspaceFileAs.mockResolvedValueOnce({ status: 'cancelled', bytesWritten: 0 })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('too-large.bin')
    expect(wrapper.text()).toContain('File exceeds the full read limit')
    const saveOriginal = wrapper.find('.workspace-files__preview-error button:last-child')
    expect(saveOriginal.text()).toBe('另存为原文件')
    await saveOriginal.trigger('click')
    await flushPromises()

    expect(api.saveWorkspaceFileAs).toHaveBeenCalledWith('agent-a', 'too-large.bin', null)
    expect(wrapper.find('.workspace-files__action-status').text()).toBe('已取消另存')
    expect(wrapper.text()).not.toContain('256 MiB')
  })

  it('reveals the selected file through the native command and reports failures', async () => {
    api.revealWorkspacePath.mockRejectedValueOnce(new Error('定位失败'))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('.workspace-files__actions-menu summary').trigger('click')
    await wrapper.findAll('.workspace-files__actions-popover button').find(button => button.text().includes('在文件管理器中定位')).trigger('click')
    await flushPromises()

    expect(api.revealWorkspacePath).toHaveBeenCalledWith('agent-a', 'a.txt')
    expect(wrapper.text()).toContain('定位失败')
  })

  it('sorts folders before files and exposes native path actions from the file-list context menu', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    const names = wrapper.findAll('.workspace-files__list button').map(button => button.text())
    expect(names[0]).toBe('reports')
    expect(names.slice(1)).toEqual([...names.slice(1)].sort((a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' }) || a.localeCompare(b)))
    expect(wrapper.find('.workspace-files__folder-icon').exists()).toBe(true)
    expect(wrapper.find('.workspace-files__file-icon').exists()).toBe(true)

    await wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'reports').trigger('contextmenu', { clientX: 30, clientY: 40 })
    await flushPromises()
    const entryMenu = document.body.querySelector('.ui-context-menu')
    expect(entryMenu?.textContent).toContain('在文件管理器中打开文件夹')
    entryMenu.querySelector('button').click()
    await flushPromises()
    expect(api.revealWorkspacePath).toHaveBeenCalledWith('agent-a', 'reports')

    await wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'a.txt').trigger('contextmenu', { clientX: 40, clientY: 50 })
    await flushPromises()
    const fileMenu = document.body.querySelector('.ui-context-menu')
    expect(fileMenu?.textContent).toContain('在文件管理器中显示')
    fileMenu.querySelector('button').click()
    await flushPromises()
    expect(api.revealWorkspacePath).toHaveBeenLastCalledWith('agent-a', 'a.txt')

    await wrapper.find('.workspace-files__list').trigger('contextmenu', { clientX: 30, clientY: 40 })
    document.body.querySelector('.ui-context-menu button').click()
    await flushPromises()
    expect(api.revealWorkspacePath).toHaveBeenLastCalledWith('agent-a', '')
  })

  it('keeps reveal success quiet while preserving failures and supports keyboard context menus', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    wrapper.vm.actionStatus = 'stale success notice'
    await wrapper.vm.revealSelectedFile()
    expect(wrapper.find('.workspace-files__action-status').exists()).toBe(false)
    expect(wrapper.vm.actionStatus).toBe('')

    await wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'reports').trigger('contextmenu', { clientX: 30, clientY: 40 })
    await flushPromises()
    document.body.querySelector('.ui-context-menu button').click()
    await flushPromises()
    expect(wrapper.find('.workspace-files__action-status').exists()).toBe(false)

    api.revealWorkspacePath.mockRejectedValueOnce(new Error('文件管理器不可用'))
    await wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'a.txt').trigger('contextmenu', { clientX: 30, clientY: 40 })
    await flushPromises()
    document.body.querySelector('.ui-context-menu button').click()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('文件管理器不可用')

    const entry = wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'reports')
    await entry.trigger('keydown', { key: 'ContextMenu' })
    await flushPromises()
    expect(document.body.querySelector('.ui-context-menu')?.textContent).toContain('在文件管理器中打开文件夹')
    expect(document.activeElement).toBe(document.body.querySelector('.ui-context-menu button'))
  })

  it('closes floating context menus when workspace ownership or selection changes', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'reports').trigger('contextmenu', { clientX: 30, clientY: 40 })
    await flushPromises()
    expect(document.body.querySelector('.ui-context-menu')).not.toBeNull()
    await wrapper.setProps({ active: false })
    await flushPromises()
    expect(document.body.querySelector('.ui-context-menu')).toBeNull()

    await wrapper.setProps({ active: true, agentId: 'agent-b' })
    await flushPromises()
    await wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'reports').trigger('contextmenu', { clientX: 30, clientY: 40 })
    await flushPromises()
    await wrapper.setProps({ agentId: 'agent-c' })
    await flushPromises()
    expect(document.body.querySelector('.ui-context-menu')).toBeNull()

    await wrapper.findAll('.workspace-files__list button').find(button => button.text() === 'reports').trigger('contextmenu', { clientX: 30, clientY: 40 })
    await flushPromises()
    wrapper.vm.selectedPath = 'reports'
    await flushPromises()
    expect(document.body.querySelector('.ui-context-menu')).toBeNull()
  })

  it('opens reference actions from the workspace keyboard context-menu key', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' }, attachTo: document.body })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('.workspace-files__editor').trigger('keydown', { key: 'F10', shiftKey: true })
    await flushPromises()
    expect(document.body.querySelector('.ui-context-menu')?.textContent).toContain('添加注解')
    document.body.querySelector('.ui-context-menu').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.find('.workspace-files__editor').element)
    await wrapper.find('.workspace-files__editor').trigger('keydown', { key: 'F10', shiftKey: true })
    await flushPromises()
    expect(document.body.querySelector('.ui-context-menu')?.textContent).toContain('添加注解')
  })

  it('keeps large CSV table preview capped at 500 rows and exposes remaining source text', async () => {
    const source = Array.from({ length: 501 }, (_, index) => `row${index},value`).join('\n')
    api.readWorkspaceTextChunk.mockResolvedValueOnce({ ...result(source), fileSize: 17 * 1024 * 1024, nextOffset: 256 * 1024 })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('large.csv')
    expect(wrapper.findAll('.workspace-files__sheet-scroll tbody tr')).toHaveLength(500)
    expect(wrapper.findAll('.workspace-files__sheet-scroll thead th')).toHaveLength(3)
    expect(wrapper.find('.workspace-files__row-index').text()).toBe('1')
    expect(wrapper.text()).toContain('切换到源代码可继续分段阅读')
    await wrapper.findAll('.workspace-files__preview-toggle button').find(button => button.text() === '原始数据').trigger('click')
    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('row500')
  })

  it('right-aligns CSV numeric cells and keeps identifier strings intact', async () => {
    api.readWorkspaceFile.mockImplementation(async (_agentId, path) => path === 'records.csv'
      ? { bytes: bytes('name,amount,code\nAlpha,1200,00012345678901234567890'), contentType: 'text/csv', etag: 'csv-numeric' }
      : result(path))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('records.csv')
    const cells = wrapper.findAll('.workspace-files__sheet-scroll tbody td')
    expect(cells.map(cell => cell.text())).toEqual(['name', 'amount', 'code', 'Alpha', '1200', '00012345678901234567890'])
    expect(cells[3].classes()).not.toContain('is-numeric')
    expect(cells[4].classes()).toContain('is-numeric')
    expect(cells[5].classes()).toContain('is-numeric')
    const workspaceSource = await readFile(resolve(process.cwd(), 'src/ui/components/WorkspaceFiles.vue'), 'utf8')
    expect(workspaceSource).toMatch(/\.workspace-files__sheet-scroll td \{ overflow-wrap:normal; word-break:normal; \}/)
    expect(workspaceSource).toContain('.workspace-files__image-stage{display:flex;flex:1;min-width:0;min-height:0;align-items:center;justify-content:center;overflow:auto;scroll-padding-bottom:var(--workspace-dock-bottom-space, 12px);padding:12px 12px calc(12px + var(--workspace-dock-bottom-space, 12px))}')
  })

  it('offers immediate create/import actions for an empty workspace and can collapse the file list', async () => {
    api.listWorkspace.mockResolvedValue({ path: '', entries: [] })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'empty-agent' } })
    await flushPromises()
    expect(wrapper.get('.workspace-files').attributes('style')).toContain('26px')
    expect(wrapper.text()).toContain('此目录为空')
    const emptyGroup = wrapper.get('.workspace-files__empty-workspace .workspace-files__empty-group')
    expect(emptyGroup.element.children).toHaveLength(2)
    expect(emptyGroup.element.children[0].textContent).toContain('此目录为空')
    expect(emptyGroup.element.children[1].classList.contains('workspace-files__empty-actions')).toBe(true)
    expect(wrapper.find('[aria-label="新建文件"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="导入本机文件"]').exists()).toBe(true)
    const toggle = wrapper.get('.workspace-files__list-toggle')
    await toggle.trigger('click')
    expect(wrapper.find('.workspace-files__body').classes()).toContain('is-list-collapsed')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await wrapper.find('[aria-label="新建文件"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('.workspace-files__create-dialog')).not.toBeNull()
  })

  it('keeps refresh and save in the toolbar and moves secondary actions into the overflow menu', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect(wrapper.find('[aria-label="刷新"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="保存文件"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="更多文件操作"]').exists()).toBe(true)
    expect(wrapper.findAll('.workspace-files__actions-popover button')).toHaveLength(4)
    expect(wrapper.find('.workspace-files__toolbar-actions > [aria-label="上传文件"]').exists()).toBe(false)
    expect(wrapper.find('.workspace-files__toolbar-actions > [aria-label="下载文件"]').exists()).toBe(false)
  })

  it('restores the floating message bar from its file toolbar icon', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', dockCollapsed: true, dockStatus: 'generating', dockAgentName: '测试 Agent' } })
    await flushPromises()
    expect(wrapper.get('.workspace-files').attributes('style')).toContain('12px')

    const restore = wrapper.find('.workspace-files__dock-toggle')
    expect(restore.attributes('title')).toContain('测试 Agent · 生成中')
    await restore.trigger('click')
    expect(wrapper.emitted('expand-agent-dock')).toHaveLength(1)
  })

  it('preserves dotfiles and opens a nested resource by listing its parent directory', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect((await wrapper.vm.openPath('.gitignore')).ok).toBe(true)
    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', '.gitignore')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('.gitignore')

    expect((await wrapper.vm.openPath('reports/summary.txt')).ok).toBe(true)
    expect(api.listWorkspace).toHaveBeenLastCalledWith('agent-a', 'reports')
    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'reports/summary.txt')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('summary.txt')
  })

  it('opens a persistent annotation for a CSV cell and emits it with the resource reference', async () => {
    api.readWorkspaceFile.mockImplementation(async (_agentId, path) => path === 'records.csv'
      ? { bytes: bytes('name,amount\nAlpha,12\nBeta,34'), contentType: 'text/csv', etag: 'csv-revision-1' }
      : result(path))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect((await wrapper.vm.openPath('records.csv')).ok).toBe(true)
    const cells = wrapper.findAll('.workspace-files__sheet-preview tbody td')
    expect(cells.map(cell => cell.text())).toEqual(['name', 'amount', 'Alpha', '12', 'Beta', '34'])

    await cells[2].trigger('pointerdown', { button: 0 })
    await cells[2].trigger('contextmenu', { clientX: 120, clientY: 80 })
    expect(document.body.querySelector('.ui-context-menu__context')?.textContent).toContain('records.csv!A2')
    document.body.querySelector('.ui-context-menu button').click()
    await flushPromises()

    const textarea = document.body.querySelector('[aria-label="注解内容"]')
    expect(textarea).not.toBeNull()
    textarea.value = '请核对这个单元格'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))

    const annotations = wrapper.emitted('annotation-update')
    const [payload] = annotations.at(-1)
    expect(payload).toMatchObject({ agentId: 'agent-a', path: 'records.csv', revision: 'csv-revision-1', annotation: '请核对这个单元格' })
    expect(payload.anchor).toMatchObject({ type: 'sheet-range', label: 'records.csv!A2', quote: 'Alpha' })
  })

  it('keeps the current editor and its draft when confirmation to leave is canceled', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('reports/summary.txt')
    const editor = wrapper.find('textarea')
    await editor.setValue('unsaved local edit')
    window.confirm.mockReturnValue(false)

    const outcome = await wrapper.vm.openPath('a.txt')
    expect(outcome).toEqual({ ok: false, canceled: true })
    expect(wrapper.find('textarea').element.value).toBe('unsaved local edit')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('summary.txt')
    expect(api.readWorkspaceFile).toHaveBeenCalledTimes(1)
  })

  it('ignores an older file read after a newer resource link is opened', async () => {
    let resolveA
    let resolveB
    api.readWorkspaceFile.mockImplementation((_agentId, path) => new Promise(resolve => {
      if (path === 'a.txt') resolveA = resolve
      else resolveB = resolve
    }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    const openingA = wrapper.vm.openPath('a.txt')
    await flushPromises()
    const openingB = wrapper.vm.openPath('b.txt')
    await flushPromises()
    resolveB(result('newer file', 'etag-b'))
    await flushPromises()
    resolveA(result('stale file', 'etag-a'))
    await flushPromises()

    expect((await openingB).ok).toBe(true)
    expect((await openingA).ok).toBe(false)
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('b.txt')
    expect(wrapper.find('textarea').element.value).toBe('newer file')
  })

  it('restores a saved draft when reopening its file after navigating away', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('A draft to keep')

    await wrapper.vm.openPath('b.txt')
    expect(localStorage.getItem('dagents.desktop.workspace-draft:node-a:agent-a:a.txt')).toContain('A draft to keep')
    await wrapper.vm.openPath('a.txt')

    expect(wrapper.find('textarea').element.value).toBe('A draft to keep')
  })

  it('keeps a file draft in storage when reopening that file fails to read', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('draft survives failed read')
    await wrapper.vm.openPath('b.txt')
    const draftKey = 'dagents.desktop.workspace-draft:node-a:agent-a:a.txt'
    const storedDraft = localStorage.getItem(draftKey)
    api.readWorkspaceFile.mockRejectedValueOnce(new Error('temporary read failure'))

    expect((await wrapper.vm.openPath('a.txt')).ok).toBe(false)
    expect(localStorage.getItem(draftKey)).toBe(storedDraft)
  })

  it('retains and lets users save as a draft when the source grows into a chunked preview', async () => {
    const draftKey = 'dagents.desktop.workspace-draft:node-a:agent-a:a.txt'
    api.readWorkspaceFile.mockResolvedValueOnce({
      bytes: new TextEncoder().encode('\uFEFForiginal\r\nsource\r\n'),
      contentType: 'text/plain',
      etag: 'original-format-etag',
    })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('original local draft\nsecond line')
    const storedDraft = localStorage.getItem(draftKey)
    await wrapper.vm.openPath('b.txt')

    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({
      path,
      entries: path === '' ? directories[''].map(entry => entry.path === 'a.txt' ? { ...entry, size: 17 * 1024 * 1024 } : entry) : directories[path] || [],
    }))
    api.readWorkspaceTextChunk.mockResolvedValueOnce({ ...result('# New large source chunk\n'), fileSize: 17 * 1024 * 1024, nextOffset: 256 * 1024, etag: 'large-etag' })
    await wrapper.vm.openPath('a.txt')

    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('New large source chunk')
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.find('.workspace-files__draft-bar.is-retained').text()).toContain('只读分段预览')
    expect(localStorage.getItem(draftKey)).toBe(storedDraft)

    api.saveWorkspaceFileAs.mockResolvedValueOnce({ status: 'saved', bytesWritten: 20 })
    await wrapper.find('.workspace-files__draft-bar.is-retained button').trigger('click')
    await flushPromises()
    expect(api.saveWorkspaceFileAs).toHaveBeenCalledWith('agent-a', 'a.txt', '\uFEFForiginal local draft\r\nsecond line')
    expect(localStorage.getItem(draftKey)).toBe(storedDraft)

    await wrapper.vm.discardDraftAndReload({ confirm: false })
    expect(localStorage.getItem(draftKey)).toBeNull()
    expect(wrapper.find('.workspace-files__draft-bar.is-retained').exists()).toBe(false)
  })

  it('ignores a delayed segmented read after switching Agents', async () => {
    let resolveChunk
    api.readWorkspaceTextChunk.mockImplementationOnce(() => new Promise(resolve => { resolveChunk = resolve }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    const opening = wrapper.vm.openPath('large.txt')
    await flushPromises()
    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()
    resolveChunk({ ...result('stale Agent content'), fileSize: 300000, nextOffset: 20 })
    await flushPromises()

    expect((await opening).ok).toBe(false)
    expect(wrapper.find('.workspace-files__chunked-text').exists()).toBe(false)
    expect(wrapper.find('.workspace-files__chunk-toolbar').exists()).toBe(false)
  })

  it('does not start a 413 chunk fallback after the selected path changes', async () => {
    let rejectA
    api.readWorkspaceFile.mockImplementation((_agentId, path) => path === 'a.txt'
      ? new Promise((_resolve, reject) => { rejectA = reject })
      : Promise.resolve(result(`contents for ${path}`)))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    const openingA = wrapper.vm.openPath('a.txt')
    await flushPromises()
    const openingB = wrapper.vm.openPath('b.txt')
    await flushPromises()
    expect((await openingB).ok).toBe(true)
    rejectA(Object.assign(new Error('full read limit'), { status: 413 }))
    await flushPromises()

    expect((await openingA).ok).toBe(false)
    expect(api.readWorkspaceTextChunk).not.toHaveBeenCalledWith('agent-a', 'a.txt', 0, 256 * 1024)
    expect(wrapper.find('textarea').element.value).toBe('contents for b.txt')
  })

  it('does not let an old chunk response clear the busy state of the newly opened file', async () => {
    const pendingChunks = []
    api.readWorkspaceTextChunk.mockImplementation((_agentId, path, offset = 0) => offset === 0
      ? Promise.resolve({ ...result(`first ${path}`), fileSize: 300000, nextOffset: 100 })
      : new Promise(resolve => { pendingChunks.push({ path, resolve }) }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')
    const oldLoad = wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()

    await wrapper.vm.openPath('large.go')
    const currentLoad = wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()
    pendingChunks[0].resolve({ ...result('stale tail'), fileSize: 300000, nextOffset: 200 })
    await flushPromises()
    expect(wrapper.find('.workspace-files__chunk-toolbar button').attributes('disabled')).toBeDefined()

    pendingChunks[1].resolve({ ...result('current tail'), fileSize: 300000, nextOffset: 300000 })
    await Promise.all([oldLoad, currentLoad])
    await flushPromises()
    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('current tail')
  })

  it('keeps the editor usable when a text save exceeds the native limit', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('still editable after the save error')
    api.saveWorkspaceFile.mockRejectedValueOnce(new Error('文本保存上限为 16 MiB。'))

    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('textarea').element.value).toBe('still editable after the save error')
    expect(wrapper.find('.workspace-files__preview-error').exists()).toBe(false)
    expect(wrapper.find('[role="alert"]').text()).toContain('16 MiB')
    await wrapper.find('textarea').setValue('shortened draft')
    expect(wrapper.find('textarea').element.value).toBe('shortened draft')
  })

  it('opens a root file link while the initial directory listing is still pending', async () => {
    let resolveInitialList
    let listCalls = 0
    api.listWorkspace.mockImplementation((_agentId, path = '') => {
      listCalls += 1
      if (listCalls === 1) return new Promise(resolve => { resolveInitialList = resolve })
      return Promise.resolve({ path, entries: [...directories[''], { name: 'README.txt', path: 'README.txt', is_dir: false, size: 16 }] })
    })
    api.readWorkspaceFile.mockImplementation(async (_agentId, path) => result(`contents for ${path}`))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    const opening = wrapper.vm.openPath('README.txt')
    await flushPromises()
    expect(listCalls).toBe(2)
    expect((await opening).ok).toBe(true)
    resolveInitialList({ path: '', entries: directories[''] })
    await flushPromises()

    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'README.txt')
    expect(wrapper.find('textarea').element.value).toBe('contents for README.txt')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('README.txt')
  })

  it('refreshes an already-open directory so a newly created file link can open', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({
      path,
      entries: path === '' ? [...directories[''], { name: 'report.md', path: 'report.md', is_dir: false, size: 14 }] : directories[path] || [],
    }))

    expect((await wrapper.vm.openPath('report.md')).ok).toBe(true)
    expect(api.listWorkspace).toHaveBeenLastCalledWith('agent-a', '')
    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'report.md')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('report.md')
  })

  it('keeps the selected editor on a 304 conditional refresh', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    const oldText = wrapper.find('textarea').element.value
    api.readWorkspaceFile.mockResolvedValueOnce({ notModified: true, bytes: null, etag: 'etag-1' })

    await wrapper.vm.refreshResources('manual')

    expect(wrapper.find('textarea').element.value).toBe(oldText)
    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'a.txt', { ifNoneMatch: 'etag-1' })
  })

  it('uses the refreshed BOM and dominant newline format for the next save', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    api.readWorkspaceFile.mockResolvedValueOnce({ bytes: bytes('\uFEFFfirst\r\nsecond\r\n'), contentType: 'text/plain', etag: 'etag-format' })
    await wrapper.vm.refreshResources('tool')

    expect(wrapper.find('textarea').element.value).toBe('first\nsecond\n')
    await wrapper.find('textarea').setValue('edited\nagain\n')
    api.saveWorkspaceFile.mockResolvedValueOnce({ etag: 'etag-saved' })
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await flushPromises()

    const savedBytes = api.saveWorkspaceFile.mock.calls.at(-1)[2]
    expect(Array.from(savedBytes.subarray(0, 3))).toEqual([0xef, 0xbb, 0xbf])
    expect(new TextDecoder('utf-8', { ignoreBOM: true }).decode(savedBytes)).toBe('\uFEFFedited\r\nagain\r\n')
  })

  it('does not install a clean-looking refresh over a revision-conflicted draft', async () => {
    localStorage.setItem('dagents.desktop.workspace-draft:node-a:agent-a:a.txt', JSON.stringify({ content: 'a.txt', revision: 'older-revision' }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    api.readWorkspaceFile.mockResolvedValueOnce(result('external replacement', 'newer-revision'))
    await wrapper.vm.refreshResources('tool')

    expect(wrapper.find('textarea').element.value).toBe('a.txt')
    expect(wrapper.vm.$.setupState.draftConflict).toBe(true)
  })

  it('clears chunked read-only state when a refreshed large file now fits a full read', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')
    api.readWorkspaceFile.mockResolvedValueOnce(result('now small and editable', 'etag-small'))
    await wrapper.vm.refreshResources('tool')

    expect(wrapper.find('.workspace-files__chunk-toolbar').exists()).toBe(false)
    expect(wrapper.find('textarea').element.value).toBe('now small and editable')
    expect(wrapper.find('[aria-label="保存文件"]').attributes('disabled')).toBeDefined()
    await wrapper.find('textarea').setValue('editable after shrink')
    expect(wrapper.find('[aria-label="保存文件"]').attributes('disabled')).toBeUndefined()
  })

  it('refreshes changed large content from an unpinned first chunk and keeps later chunks revision-guarded', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')
    api.readWorkspaceFile.mockRejectedValueOnce(Object.assign(new Error('full read limit'), { status: 413 }))
    api.readWorkspaceTextChunk.mockResolvedValueOnce({ ...result('# External update\n'), fileSize: 320000, nextOffset: 262144, etag: 'etag-external' })

    await wrapper.vm.refreshResources('tool')

    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'large.txt', { ifNoneMatch: 'etag-1' })
    expect(api.readWorkspaceTextChunk).toHaveBeenLastCalledWith('agent-a', 'large.txt', 0, 256 * 1024)
    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('External update')
    expect(wrapper.find('.workspace-files__chunk-toolbar').text()).toContain('320,000')
    await wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()
    expect(api.readWorkspaceTextChunk).toHaveBeenLastCalledWith('agent-a', 'large.txt', 262144, 256 * 1024, 'etag-external')
  })

  it('discards a pending continuation from the previous same-path refresh version', async () => {
    const pending = []
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')
    api.readWorkspaceTextChunk.mockImplementation((_agentId, _path, offset = 0) => offset === 0
      ? Promise.resolve({ ...result('# refreshed version\n'), fileSize: 300000, nextOffset: 262144, etag: 'etag-refreshed' })
      : new Promise(resolve => { pending.push(resolve) }))
    const oldContinuation = wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()

    api.readWorkspaceFile.mockRejectedValueOnce(Object.assign(new Error('full read limit'), { status: 413 }))
    await wrapper.vm.refreshResources('tool')
    const currentContinuation = wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()
    pending[0]({ ...result('stale continuation'), fileSize: 300000, nextOffset: 300000, etag: 'etag-1' })
    await flushPromises()

    expect(wrapper.find('.workspace-files__chunk-toolbar button').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.workspace-files__chunked-text').text()).not.toContain('stale continuation')
    pending[1]({ ...result('current continuation'), fileSize: 300000, nextOffset: 300000, etag: 'etag-refreshed' })
    await Promise.all([oldContinuation, currentContinuation])
    await flushPromises()
    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('current continuation')
    expect(wrapper.find('.workspace-files__chunked-text').text()).not.toContain('stale continuation')
  })

  it('releases the busy state after a middle chunk so a third chunk can be read', async () => {
    api.readWorkspaceTextChunk.mockImplementation(async (_agentId, _path, offset = 0) => {
      if (offset === 0) return { ...result('first '), fileSize: 300, nextOffset: 100, etag: 'etag-three' }
      if (offset === 100) return { ...result('second '), fileSize: 300, nextOffset: 200, etag: 'etag-three' }
      return { ...result('third'), fileSize: 300, nextOffset: 300, etag: 'etag-three' }
    })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('large.txt')

    await wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()
    expect(wrapper.find('.workspace-files__chunk-toolbar button').exists()).toBe(true)
    expect(wrapper.find('.workspace-files__chunk-toolbar button').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('second')

    await wrapper.find('.workspace-files__chunk-toolbar button').trigger('click')
    await flushPromises()
    expect(api.readWorkspaceTextChunk).toHaveBeenLastCalledWith('agent-a', 'large.txt', 200, 256 * 1024, 'etag-three')
    expect(wrapper.find('.workspace-files__chunked-text').text()).toContain('third')
    expect(wrapper.find('.workspace-files__chunk-toolbar').text()).toContain('已到文件末尾')
  })

  it('preserves BOM and CRLF in the draft passed to native Save As', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    api.readWorkspaceFile.mockResolvedValueOnce({ bytes: bytes('\uFEFFfirst\r\nsecond\r\n'), contentType: 'text/plain', etag: 'etag-format' })
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('draft\nline\n')
    await wrapper.find('.workspace-files__actions-menu summary').trigger('click')
    await wrapper.findAll('.workspace-files__actions-popover button').find(button => button.text().includes('另存为')).trigger('click')
    await flushPromises()

    expect(api.saveWorkspaceFileAs).toHaveBeenCalledWith('agent-a', 'a.txt', '\uFEFFdraft\r\nline\r\n')
  })

  it('confirms deletion without a 404 polling storm', async () => {
    vi.useFakeTimers()
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path === '' ? directories[''].filter(entry => entry.path !== 'a.txt') : directories[path] || [] }))
    api.readWorkspaceFile.mockRejectedValue(Object.assign(new Error('missing'), { status: 404 }))

    const listsBefore = api.listWorkspace.mock.calls.length
    const readsBefore = api.readWorkspaceFile.mock.calls.length
    await wrapper.vm.refreshResources('manual')
    const readsAfterFirstCheck = api.readWorkspaceFile.mock.calls.length
    expect(readsAfterFirstCheck - readsBefore).toBe(1)
    await vi.advanceTimersByTimeAsync(5000)

    expect(wrapper.find('.workspace-files__notice-detail').text()).toContain('已从工作区移除')
    expect(api.readWorkspaceFile.mock.calls.length - readsBefore).toBe(1)
    expect(api.listWorkspace.mock.calls.length - listsBefore).toBeLessThanOrEqual(4)
  })

  it('preserves drafts and the Node-scoped draft key across reconnect request generations', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', requestGeneration: 1, active: false } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('typed during disconnect')
    const key = 'dagents.desktop.workspace-draft:node-a:agent-a:a.txt'
    const draftBefore = localStorage.getItem(key)

    await wrapper.setProps({ requestGeneration: 2 })
    await flushPromises()

    expect(wrapper.find('textarea').element.value).toBe('typed during disconnect')
    expect(localStorage.getItem(key)).toBe(draftBefore)
    expect(wrapper.vm.$.setupState.selectedPath).toBe('a.txt')
  })

  it('never writes the previous Agent draft under the newly selected Agent id', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: false } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('agent a draft')
    const originalKey = 'dagents.desktop.workspace-draft:node-a:agent-a:a.txt'
    const originalDraft = localStorage.getItem(originalKey)

    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()

    expect(localStorage.getItem(originalKey)).toBe(originalDraft)
    expect(localStorage.getItem('dagents.desktop.workspace-draft:node-a:agent-b:a.txt')).toBeNull()
  })

  it('does not replace an editor when the user types while a conditional read is in flight', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    let resolveRefresh
    api.readWorkspaceFile.mockImplementationOnce(() => new Promise(resolve => { resolveRefresh = resolve }))
    const refreshing = wrapper.vm.refreshResources('manual')
    await flushPromises()
    await wrapper.find('textarea').setValue('new local typing')
    resolveRefresh(result('external version', 'etag-2'))
    await refreshing

    expect(wrapper.find('textarea').element.value).toBe('new local typing')
    expect(wrapper.find('.workspace-files__notice-detail').text()).toContain('当前编辑内容和版本基线已保留')
  })

  it('offers one external-update recovery action and keeps the draft when confirmation is canceled', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('keep this local draft')
    api.readWorkspaceFile.mockResolvedValueOnce(result('external version', 'etag-2'))
    await wrapper.vm.refreshResources('tool')

    expect(wrapper.vm.resourceMenuActions()).toEqual({ canRecoverDraft: true, recoveryLabel: '丢弃草稿并重载' })
    window.confirm.mockReturnValue(false)
    await wrapper.vm.discardDraftAndReload()

    expect(wrapper.find('textarea').element.value).toBe('keep this local draft')
    expect(localStorage.getItem('dagents.desktop.workspace-draft:node-a:agent-a:a.txt')).toContain('keep this local draft')
    expect(wrapper.vm.resourceMenuActions().canRecoverDraft).toBe(true)
  })

  it('uses an in-app file-name dialog instead of a browser prompt', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    const prompt = vi.spyOn(window, 'prompt')
    await wrapper.find('.workspace-files__actions-menu summary').trigger('click')
    await wrapper.get('.workspace-files__actions-popover button').trigger('click')
    await flushPromises()

    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog?.textContent).toContain('新建文件')
    expect(dialog?.querySelector('input')).not.toBeNull()
    expect(prompt).not.toHaveBeenCalled()
  })

  it('shows only one recovery action when a draft conflict and missing file overlap', async () => {
    vi.useFakeTimers()
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('conflicted local draft')
    api.saveWorkspaceFile.mockRejectedValueOnce(Object.assign(new Error('revision conflict'), { status: 412 }))
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await flushPromises()

    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: directories[path]?.filter(entry => entry.path !== 'a.txt') || [] }))
    api.readWorkspaceFile.mockRejectedValue(Object.assign(new Error('missing'), { status: 404 }))
    await wrapper.vm.refreshResources('tool')
    await vi.advanceTimersByTimeAsync(400)

    expect(wrapper.find('.workspace-files__notice-detail').text()).toContain('已从工作区移除')
    const noticeText = wrapper.find('.workspace-files__notice-detail').text()
    expect(noticeText.match(/本地草稿基于较旧文件版本/g)).toHaveLength(1)
    expect(noticeText).toContain('revision conflict')
    expect(wrapper.vm.resourceMenuActions()).toEqual({ canRecoverDraft: true, recoveryLabel: '丢弃草稿并重试' })
    expect(wrapper.findAll('button').some(button => button.text().includes('丢弃草稿'))).toBe(false)
  })

  it('ignores a refresh GET that returns after a successful save', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('saved local version')

    let finishRead
    api.readWorkspaceFile.mockImplementationOnce(() => new Promise(resolve => { finishRead = resolve }))
    api.readWorkspaceFile.mockResolvedValueOnce({ notModified: true, bytes: null, etag: 'etag-saved' })
    const refreshing = wrapper.vm.refreshResources('tool')
    await flushPromises()

    api.saveWorkspaceFile.mockResolvedValueOnce({ etag: 'etag-saved' })
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await flushPromises()
    finishRead(result('stale server response', 'etag-old'))
    await refreshing
    await flushPromises()

    expect(wrapper.find('textarea').element.value).toBe('saved local version')
    expect(wrapper.find('.workspace-files__notice').exists()).toBe(false)
    expect(api.readWorkspaceFile).toHaveBeenCalledTimes(3)
  })

  it('keeps the quoted commit ETag as baseline while later edits survive conditional refresh', async () => {
    const revision = 'b'.repeat(64)
    api.readWorkspaceFile.mockImplementation(async (_agentId, _path, { ifNoneMatch } = {}) => {
      if (ifNoneMatch) return { notModified: true, bytes: null, etag: ifNoneMatch, status: 304 }
      return { ...result('original content', '"initial"'), contentType: 'text/plain' }
    })
    api.saveWorkspaceFile.mockResolvedValueOnce({ status: 'saved', revision, etag: `"${revision}"` })
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('saved baseline')
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await flushPromises()
    expect(api.saveWorkspaceFile.mock.calls[0][3]).toBe('"initial"')
    expect(localStorage.getItem('dagents.desktop.workspace-draft:default:agent-a:a.txt')).toBeNull()
    expect(wrapper.findAll('.workspace-files__action-status').map(status => status.text())).toContain('已保存')
    expect(wrapper.text()).not.toContain('此阶段取消后结果可能未知')

    await wrapper.find('textarea').setValue('later local edits')
    await wrapper.vm.refreshSelectedFile()
    await flushPromises()
    expect(api.readWorkspaceFile).toHaveBeenCalledWith('agent-a', 'a.txt', { ifNoneMatch: `"${revision}"` })
    expect(wrapper.find('textarea').element.value).toBe('later local edits')
    expect(wrapper.find('.workspace-files__draft-bar').text()).not.toContain('较旧文件版本')
    expect(wrapper.find('.workspace-files__draft-bar').classes()).not.toContain('is-conflict')
  })

  it('does not let an old Agent 404 mark the new Agent file missing', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')

    let rejectRead
    api.readWorkspaceFile.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRead = reject }))
    const oldRefresh = wrapper.vm.refreshResources('manual')
    await flushPromises()

    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    rejectRead(Object.assign(new Error('missing from old Agent'), { status: 404 }))
    await oldRefresh
    await flushPromises()

    expect(wrapper.find('.workspace-files__notice').exists()).toBe(false)
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('a.txt')
    expect(wrapper.find('textarea').element.value).toBe('a.txt')
  })

  it('does not expose a late old Agent refresh error in the new Agent view', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')

    let rejectRead
    api.readWorkspaceFile.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRead = reject }))
    const oldRefresh = wrapper.vm.refreshResources('manual')
    await flushPromises()

    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    rejectRead(Object.assign(new Error('old Agent gateway failure'), { status: 502 }))
    await oldRefresh
    await flushPromises()

    expect(wrapper.find('.workspace-files__notice').exists()).toBe(false)
    expect(wrapper.find('textarea').element.value).toBe('a.txt')
  })

  it('keeps the draft and releases the save control when the connection changes during save', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', requestGeneration: 1, active: false } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('draft survives reconnect')
    let finishSave
    api.saveWorkspaceFile.mockImplementationOnce(() => new Promise(resolve => { finishSave = resolve }))
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    expect(wrapper.find('[aria-label="保存文件"]').attributes('disabled')).toBeDefined()

    await wrapper.setProps({ requestGeneration: 2 })
    await flushPromises()
    expect(wrapper.find('[aria-label="保存文件"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('textarea').element.value).toBe('draft survives reconnect')

    finishSave({ etag: 'stale-save-etag' })
    await flushPromises()
    expect(wrapper.find('textarea').element.value).toBe('draft survives reconnect')
    expect(wrapper.find('[aria-label="保存文件"]').attributes('disabled')).toBeUndefined()
  })

  it('keeps edits made while save is pending in the local draft', async () => {
    const draftKey = 'dagents.desktop.workspace-draft:default:agent-a:a.txt'
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('a.txt')
    await wrapper.find('textarea').setValue('submitted text')
    let finishSave
    api.saveWorkspaceFile.mockImplementationOnce(() => new Promise(resolve => { finishSave = resolve }))
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await wrapper.find('textarea').setValue('newer text while saving')
    expect(localStorage.getItem(draftKey)).toContain('newer text while saving')

    finishSave({ status: 'saved', etag: 'saved-etag' })
    await flushPromises()
    expect(wrapper.find('textarea').element.value).toBe('newer text while saving')
    expect(localStorage.getItem(draftKey)).toContain('newer text while saving')
  })

  it('serializes create and import writes and keeps a newer request when an old create settles', async () => {
    let finishCreate
    let finishImport
    api.newWorkspaceWriteRequestId.mockReturnValueOnce('create-a').mockReturnValueOnce('import-b')
    api.createWorkspaceFile.mockImplementationOnce(() => new Promise(resolve => { finishCreate = resolve }))
    api.pickWorkspaceImport.mockImplementationOnce(() => new Promise(resolve => { finishImport = resolve }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.createFile()
    const input = document.body.querySelector('#workspace-create-file-name')
    input.value = 'draft.md'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    const creating = wrapper.vm.submitCreateFile()
    await flushPromises()
    expect(api.createWorkspaceFile).toHaveBeenCalledTimes(1)
    await wrapper.vm.submitCreateFile()
    await wrapper.vm.onUpload()
    expect(api.createWorkspaceFile).toHaveBeenCalledTimes(1)
    expect(api.pickWorkspaceImport).not.toHaveBeenCalled()

    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()
    await wrapper.vm.loadDirectory('')
    const importing = wrapper.vm.onUpload()
    await flushPromises()
    expect(api.pickWorkspaceImport).toHaveBeenCalledTimes(1)

    finishCreate({ status: 'saved' })
    await creating
    await flushPromises()
    expect(wrapper.find('.workspace-files__action-status').text()).toContain('正在复制文件到暂存区')
    finishImport({ status: 'complete', results: [] })
    await importing
    await flushPromises()
    expect(wrapper.findAll('.workspace-files__action-status').map(status => status.text())).toContain('没有选择文件。')
    expect(wrapper.text()).not.toContain('此阶段取消后结果可能未知')
    expect(wrapper.findAll('.workspace-files__action-status').some(status => status.find('button').exists())).toBe(false)
  })

  it('ignores an import rejection after Agent and request scope change', async () => {
    let rejectImport
    api.newWorkspaceWriteRequestId.mockReturnValueOnce('stale-import')
    api.pickWorkspaceImport.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectImport = reject }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.loadDirectory('')
    const importing = wrapper.vm.onUpload()
    await flushPromises()
    expect(api.pickWorkspaceImport).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()
    rejectImport(new Error('stale import failure'))
    await importing
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
