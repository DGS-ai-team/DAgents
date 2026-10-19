// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkspaceFiles from '../../../../../desktop/ui/components/WorkspaceFiles.vue'
import * as api from '../../../../../desktop/ui/workspace-files-api.js'

vi.mock('../../../../../desktop/ui/workspace-files-api.js', () => ({
  createWorkspaceFile: vi.fn(),
  downloadBytes: vi.fn(),
  listWorkspace: vi.fn(),
  readWorkspaceFile: vi.fn(),
  saveWorkspaceFile: vi.fn(),
  uploadWorkspaceFile: vi.fn(),
}))

const bytes = text => new TextEncoder().encode(text)
const directories = {
  '': [
    { name: '.gitignore', path: '.gitignore', is_dir: false, size: 20 },
    { name: 'reports', path: 'reports', is_dir: true, size: 0 },
    { name: 'a.md', path: 'a.md', is_dir: false, size: 10 },
    { name: 'b.md', path: 'b.md', is_dir: false, size: 10 },
  ],
  reports: [{ name: 'summary.md', path: 'reports/summary.md', is_dir: false, size: 20 }],
}
let wrapper

function result(text, etag = 'etag-1') {
  return { bytes: bytes(text), contentType: 'application/octet-stream', etag }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: directories[path] || [] }))
  api.readWorkspaceFile.mockImplementation(async (_agentId, path) => result(path))
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Desktop workspace resource navigation', () => {
  it('preserves dotfiles and opens a nested resource by listing its parent directory', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    expect((await wrapper.vm.openPath('.gitignore')).ok).toBe(true)
    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', '.gitignore')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('.gitignore')

    expect((await wrapper.vm.openPath('reports/summary.md')).ok).toBe(true)
    expect(api.listWorkspace).toHaveBeenLastCalledWith('agent-a', 'reports')
    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'reports/summary.md')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('summary.md')
  })

  it('keeps the current editor and its draft when confirmation to leave is canceled', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.vm.openPath('reports/summary.md')
    const editor = wrapper.find('textarea')
    await editor.setValue('unsaved local edit')
    window.confirm.mockReturnValue(false)

    const outcome = await wrapper.vm.openPath('a.md')
    expect(outcome).toEqual({ ok: false, canceled: true })
    expect(wrapper.find('textarea').element.value).toBe('unsaved local edit')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('summary.md')
    expect(api.readWorkspaceFile).toHaveBeenCalledTimes(1)
  })

  it('ignores an older file read after a newer resource link is opened', async () => {
    let resolveA
    let resolveB
    api.readWorkspaceFile.mockImplementation((_agentId, path) => new Promise(resolve => {
      if (path === 'a.md') resolveA = resolve
      else resolveB = resolve
    }))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()
    const openingA = wrapper.vm.openPath('a.md')
    await flushPromises()
    const openingB = wrapper.vm.openPath('b.md')
    await flushPromises()
    resolveB(result('newer file', 'etag-b'))
    await flushPromises()
    resolveA(result('stale file', 'etag-a'))
    await flushPromises()

    expect((await openingB).ok).toBe(true)
    expect((await openingA).ok).toBe(false)
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('b.md')
    expect(wrapper.find('textarea').element.value).toBe('newer file')
  })

  it('opens a root file link while the initial directory listing is still pending', async () => {
    let resolveInitialList
    let listCalls = 0
    api.listWorkspace.mockImplementation((_agentId, path = '') => {
      listCalls += 1
      if (listCalls === 1) return new Promise(resolve => { resolveInitialList = resolve })
      return Promise.resolve({ path, entries: [...directories[''], { name: 'README.md', path: 'README.md', is_dir: false, size: 16 }] })
    })
    api.readWorkspaceFile.mockImplementation(async (_agentId, path) => result(`contents for ${path}`))
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a' } })
    await flushPromises()

    const opening = wrapper.vm.openPath('README.md')
    await flushPromises()
    expect(listCalls).toBe(2)
    expect((await opening).ok).toBe(true)
    resolveInitialList({ path: '', entries: directories[''] })
    await flushPromises()

    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'README.md')
    expect(wrapper.find('textarea').element.value).toBe('contents for README.md')
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('README.md')
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
    await wrapper.vm.openPath('a.md')
    const oldText = wrapper.find('textarea').element.value
    api.readWorkspaceFile.mockResolvedValueOnce({ notModified: true, bytes: null, etag: 'etag-1' })

    await wrapper.vm.refreshResources('manual')

    expect(wrapper.find('textarea').element.value).toBe(oldText)
    expect(api.readWorkspaceFile).toHaveBeenLastCalledWith('agent-a', 'a.md', { ifNoneMatch: 'etag-1' })
  })

  it('confirms deletion without a 404 polling storm', async () => {
    vi.useFakeTimers()
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.md')
    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: path === '' ? directories[''].filter(entry => entry.path !== 'a.md') : directories[path] || [] }))
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
    await wrapper.vm.openPath('a.md')
    await wrapper.find('textarea').setValue('typed during disconnect')
    const key = 'dagents.desktop.workspace-draft:node-a:agent-a:a.md'
    const draftBefore = localStorage.getItem(key)

    await wrapper.setProps({ requestGeneration: 2 })
    await flushPromises()

    expect(wrapper.find('textarea').element.value).toBe('typed during disconnect')
    expect(localStorage.getItem(key)).toBe(draftBefore)
    expect(wrapper.vm.$.setupState.selectedPath).toBe('a.md')
  })

  it('never writes the previous Agent draft under the newly selected Agent id', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: false } })
    await flushPromises()
    await wrapper.vm.openPath('a.md')
    await wrapper.find('textarea').setValue('agent a draft')
    const originalKey = 'dagents.desktop.workspace-draft:node-a:agent-a:a.md'
    const originalDraft = localStorage.getItem(originalKey)

    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()

    expect(localStorage.getItem(originalKey)).toBe(originalDraft)
    expect(localStorage.getItem('dagents.desktop.workspace-draft:node-a:agent-b:a.md')).toBeNull()
  })

  it('does not replace an editor when the user types while a conditional read is in flight', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.md')
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
    await wrapper.vm.openPath('a.md')
    await wrapper.find('textarea').setValue('keep this local draft')
    api.readWorkspaceFile.mockResolvedValueOnce(result('external version', 'etag-2'))
    await wrapper.vm.refreshResources('manual')

    expect(wrapper.vm.resourceMenuActions()).toEqual({ canRecoverDraft: true, recoveryLabel: '丢弃草稿并重载' })
    window.confirm.mockReturnValue(false)
    wrapper.vm.discardDraftAndReload()

    expect(wrapper.find('textarea').element.value).toBe('keep this local draft')
    expect(localStorage.getItem('dagents.desktop.workspace-draft:node-a:agent-a:a.md')).toContain('keep this local draft')
    expect(wrapper.vm.resourceMenuActions().canRecoverDraft).toBe(true)
  })

  it('shows only one recovery action when a draft conflict and missing file overlap', async () => {
    vi.useFakeTimers()
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.md')
    await wrapper.find('textarea').setValue('conflicted local draft')
    api.saveWorkspaceFile.mockRejectedValueOnce(Object.assign(new Error('revision conflict'), { status: 412 }))
    await wrapper.find('[aria-label="保存文件"]').trigger('click')
    await flushPromises()

    api.listWorkspace.mockImplementation(async (_agentId, path = '') => ({ path, entries: directories[path]?.filter(entry => entry.path !== 'a.md') || [] }))
    api.readWorkspaceFile.mockRejectedValue(Object.assign(new Error('missing'), { status: 404 }))
    await wrapper.vm.refreshResources('manual')
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
    await wrapper.vm.openPath('a.md')
    await wrapper.find('textarea').setValue('saved local version')

    let finishRead
    api.readWorkspaceFile.mockImplementationOnce(() => new Promise(resolve => { finishRead = resolve }))
    api.readWorkspaceFile.mockResolvedValueOnce({ notModified: true, bytes: null, etag: 'etag-saved' })
    const refreshing = wrapper.vm.refreshResources('manual')
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

  it('does not let an old Agent 404 mark the new Agent file missing', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.md')

    let rejectRead
    api.readWorkspaceFile.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRead = reject }))
    const oldRefresh = wrapper.vm.refreshResources('manual')
    await flushPromises()

    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()
    await wrapper.vm.openPath('a.md')
    rejectRead(Object.assign(new Error('missing from old Agent'), { status: 404 }))
    await oldRefresh
    await flushPromises()

    expect(wrapper.find('.workspace-files__notice').exists()).toBe(false)
    expect(wrapper.find('.workspace-files__breadcrumbs b').text()).toBe('a.md')
    expect(wrapper.find('textarea').element.value).toBe('a.md')
  })

  it('does not expose a late old Agent refresh error in the new Agent view', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.vm.openPath('a.md')

    let rejectRead
    api.readWorkspaceFile.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRead = reject }))
    const oldRefresh = wrapper.vm.refreshResources('manual')
    await flushPromises()

    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()
    await wrapper.vm.openPath('a.md')
    rejectRead(Object.assign(new Error('old Agent gateway failure'), { status: 502 }))
    await oldRefresh
    await flushPromises()

    expect(wrapper.find('.workspace-files__notice').exists()).toBe(false)
    expect(wrapper.find('textarea').element.value).toBe('a.md')
  })

  it('keeps the draft and releases the save control when the connection changes during save', async () => {
    wrapper = mount(WorkspaceFiles, { props: { agentId: 'agent-a', workspaceScope: 'node-a', requestGeneration: 1, active: false } })
    await flushPromises()
    await wrapper.vm.openPath('a.md')
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
})
