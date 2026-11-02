// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../api/node.js', () => ({ getAgentBrowserView: vi.fn(), controlAgentBrowser: vi.fn() }))
import { controlAgentBrowser, getAgentBrowserView } from '../api/node.js'
import BrowserViewer from './BrowserViewer.vue'

const snapshot = (pageId, image = 'cGFnZQ==') => ({
  page_id: pageId,
  title: `Page ${pageId}`,
  url: `https://${pageId}.test`,
  image_base64: image,
  mime: 'image/png',
  pages: [
    { page_id: 'page-1', title: 'First', url: 'https://one.test' },
    { page_id: 'page-2', title: 'Second', url: 'https://two.test' },
  ],
})

let wrapper
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.resetAllMocks() })

describe('Desktop browser resource refresh', () => {
  it('opens a user URL from a friendly empty state without an Agent chat turn', async () => {
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('not started'), { data: { error: { code: 'browser_not_started' } } }))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a' } })
    await flushPromises()
    expect(wrapper.text()).toContain('打开一个网页')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('请先')
    controlAgentBrowser.mockResolvedValue({ page_id: 'page-1' })
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    await wrapper.find('input').setValue('example.com')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(controlAgentBrowser).toHaveBeenCalledWith('agent-a', 'navigate', '', 'https://example.com/')
    expect(wrapper.find('img').exists()).toBe(true)
    expect(wrapper.find('input').element.value).toBe('https://page-1.test')
  })

  it('preserves an edited address through polling and rejects unsafe URL schemes', async () => {
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a' } })
    await flushPromises()
    await wrapper.find('input').setValue('javascript:alert(1)')
    await wrapper.vm.refreshResources()
    expect(wrapper.find('input').element.value).toBe('javascript:alert(1)')
    await wrapper.find('form').trigger('submit')
    expect(controlAgentBrowser).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').text()).toContain('仅支持')
  })

  it('waits for a pending screenshot, sends navigation once, and never replays a busy mutation', async () => {
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a' } })
    await flushPromises()
    let finishRead
    getAgentBrowserView.mockImplementationOnce(() => new Promise(resolve => { finishRead = resolve }))
    const refresh = wrapper.vm.refreshResources()
    await wrapper.find('input').setValue('example.com')
    await wrapper.find('form').trigger('submit')
    expect(controlAgentBrowser).not.toHaveBeenCalled()
    controlAgentBrowser.mockRejectedValueOnce(Object.assign(new Error('busy'), { status: 423 }))
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    finishRead(snapshot('page-1'))
    await refresh; await flushPromises()
    expect(controlAgentBrowser).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[role="alert"]').text()).toContain('请稍后再试')
    await wrapper.vm.refreshResources()
    expect(controlAgentBrowser).toHaveBeenCalledTimes(1)
  })

  it('does not apply navigation results to another Agent', async () => {
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a' } })
    await flushPromises()
    let finishAction
    controlAgentBrowser.mockImplementationOnce(() => new Promise(resolve => { finishAction = resolve }))
    await wrapper.find('input').setValue('example.com')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    getAgentBrowserView.mockResolvedValue(snapshot('page-2'))
    await wrapper.setProps({ agentId: 'agent-b' }); await flushPromises()
    finishAction({ page_id: 'old-page' }); await flushPromises()
    expect(wrapper.find('input').element.value).toBe('https://page-2.test')
    expect(getAgentBrowserView).toHaveBeenLastCalledWith('agent-b', '')
  })

  it('uses server history and supports creating and closing a page', async () => {
    getAgentBrowserView.mockResolvedValue({ ...snapshot('page-1'), can_go_back: true, can_go_forward: false })
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a' } })
    await flushPromises()
    expect(wrapper.find('[aria-label="前进"]').attributes('disabled')).toBeDefined()
    controlAgentBrowser.mockResolvedValue({ page_id: 'page-1' })
    await wrapper.find('[aria-label="后退"]').trigger('click'); await flushPromises()
    expect(controlAgentBrowser).toHaveBeenLastCalledWith('agent-a', 'back', 'page-1', '')
    controlAgentBrowser.mockResolvedValue({ page_id: 'blank' })
    getAgentBrowserView.mockResolvedValue({ page_id: 'blank', url: 'about:blank', pages: [{ page_id: 'blank', url: 'about:blank' }] })
    await wrapper.find('[aria-label="新建页面"]').trigger('click'); await flushPromises()
    expect(controlAgentBrowser).toHaveBeenLastCalledWith('agent-a', 'new', '', '')
    expect(wrapper.find('input').element.value).toBe('')
    expect(wrapper.text()).toContain('打开一个网页')
    controlAgentBrowser.mockResolvedValue({ page_id: '' })
    getAgentBrowserView.mockResolvedValue({ pages: [], page_id: '' })
    await wrapper.find('[aria-label="关闭页面"]').trigger('click'); await flushPromises()
    expect(controlAgentBrowser).toHaveBeenLastCalledWith('agent-a', 'close', 'blank', '')
    expect(wrapper.find('.browser-viewer__tabs').exists()).toBe(false)
  })

  it('keeps the selected page through hiding and re-entry', async () => {
    getAgentBrowserView.mockImplementation(async (_agentId, pageId) => snapshot(pageId || 'page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    await wrapper.findAll('.browser-viewer__tab > button:first-child')[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('.browser-viewer__tab.selected > button:first-child').text()).toBe('Second')

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(getAgentBrowserView).toHaveBeenLastCalledWith('agent-a', 'page-2')
    expect(wrapper.find('.browser-viewer__tab.selected > button:first-child').text()).toBe('Second')
  })

  it('ignores a late old-page response when a newer page is selected, and retains the last image after an error', async () => {
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    let resolveOld
    getAgentBrowserView.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const oldLoad = wrapper.vm.refreshResources('tool')
    await flushPromises()
    await wrapper.findAll('.browser-viewer__tab > button:first-child')[1].trigger('click')
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('temporary offline'), { status: 502 }))
    resolveOld(snapshot('page-1', 'b2xk'))
    await flushPromises()
    await oldLoad

    expect(wrapper.find('.browser-viewer__tab.selected > button:first-child').text()).toBe('Second')
    expect(wrapper.find('.browser-viewer__canvas img').attributes('src')).toContain('cGFnZQ==')
  })

  it('clears only a closed selected page and recovers to the remaining browser page', async () => {
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('browser page was closed'), { status: 404, data: { error: { code: 'browser_page_not_found' } } }))
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1', 'bmV3'))

    await wrapper.findAll('.browser-viewer__tab > button:first-child')[1].trigger('click')
    await flushPromises()

    expect(getAgentBrowserView).toHaveBeenLastCalledWith('agent-a', '')
    expect(wrapper.find('.browser-viewer__tab.selected > button:first-child').text()).toBe('First')
    expect(wrapper.find('.browser-viewer__canvas img').attributes('src')).toContain('bmV3')
  })
})
