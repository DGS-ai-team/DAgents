// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../api/node.js', () => ({ getAgentBrowserView: vi.fn(), postAgentBrowserAction: vi.fn() }))
import { getAgentBrowserView, postAgentBrowserAction } from '../api/node.js'
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
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.clearAllMocks() })

describe('Desktop browser resource refresh', () => {
  it('keeps the selected page through hiding and re-entry', async () => {
    getAgentBrowserView.mockImplementation(async (_agentId, pageId) => snapshot(pageId || 'page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    await wrapper.findAll('.browser-tab')[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('.browser-tab.is-active').text()).toContain('Second')

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(getAgentBrowserView).toHaveBeenLastCalledWith('agent-a', 'page-2')
    expect(wrapper.find('.browser-tab.is-active').text()).toContain('Second')
  })

  it('ignores a late old-page response when a newer page is selected, and retains the last image after an error', async () => {
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    let resolveOld
    getAgentBrowserView.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const oldLoad = wrapper.vm.refreshResources('tool')
    await flushPromises()
    await wrapper.findAll('.browser-tab')[1].trigger('click')
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('temporary offline'), { status: 502 }))
    resolveOld(snapshot('page-1', 'b2xk'))
    await flushPromises()
    await oldLoad

    expect(wrapper.find('.browser-tab.is-active').text()).toContain('Second')
    expect(wrapper.find('.browser-canvas img').attributes('src')).toContain('cGFnZQ==')
  })

  it('clears only a closed selected page and recovers to the remaining browser page', async () => {
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('browser page was closed'), { status: 404, data: { error: { code: 'browser_page_not_found' } } }))
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1', 'bmV3'))

    await wrapper.findAll('.browser-tab')[1].trigger('click')
    await flushPromises()

    expect(getAgentBrowserView).toHaveBeenLastCalledWith('agent-a', '')
    expect(wrapper.find('.browser-tab.is-active').text()).toContain('First')
    expect(wrapper.find('.browser-canvas img').attributes('src')).toContain('bmV3')
  })

  it('shows each page title as a tab and navigates through the Agent browser session', async () => {
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    postAgentBrowserAction.mockResolvedValue({ ...snapshot('page-1'), title: '新网页', url: 'https://new.test' })
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()

    expect(wrapper.findAll('.browser-tab__title').map(tab => tab.text())).toEqual(['First', 'Second'])
    await wrapper.get('[aria-label="网址"]').setValue('example.com/path')
    await wrapper.get('.browser-address').trigger('submit')
    await flushPromises()

    expect(postAgentBrowserAction).toHaveBeenCalledWith('agent-a', { action: 'navigate', page_id: 'page-1', url: 'https://example.com/path' })
    expect(wrapper.get('[aria-label="网址"]').element.value).toBe('https://new.test')
  })

  it('submits the visible address when a refresh races with the Vue draft', async () => {
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    postAgentBrowserAction.mockResolvedValue({ ...snapshot('page-1'), title: 'Updated', url: 'https://visible.test/path' })
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()

    // Model a polling result updating component state while a focused input
    // still displays the user's newer, not-yet-blurred value.
    wrapper.get('[aria-label="网址"]').element.value = 'https://visible.test/path'
    await wrapper.get('[aria-label="网址"]').trigger('keydown.enter')
    await flushPromises()

    expect(postAgentBrowserAction).toHaveBeenCalledWith('agent-a', {
      action: 'navigate', page_id: 'page-1', url: 'https://visible.test/path',
    })
  })

  it('offers an explicit navigation button for a changed address', async () => {
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    postAgentBrowserAction.mockResolvedValue({ ...snapshot('page-1'), title: 'Updated', url: 'https://visible.test/path' })
    wrapper = mount(BrowserViewer, { attachTo: document.body, props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    const input = wrapper.get('[aria-label="网址"]')
    input.element.focus()
    await input.setValue('https://visible.test/path')
    getAgentBrowserView.mockResolvedValue(snapshot('page-1'))
    await wrapper.vm.refreshResources('manual')
    await flushPromises()
    expect(input.element.value).toBe('https://visible.test/path')
    await wrapper.get('[aria-label="访问网址"]').trigger('click')
    await flushPromises()

    expect(postAgentBrowserAction).toHaveBeenCalledWith('agent-a', {
      action: 'navigate', page_id: 'page-1', url: 'https://visible.test/path',
    })
  })

  it('starts the Agent browser session from the empty state and exposes tab creation', async () => {
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('browser session is not started'), { status: 409, data: { error: { code: 'browser_not_started' } } }))
    postAgentBrowserAction.mockResolvedValue(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    await wrapper.get('.browser-primary-action').trigger('click')
    await flushPromises()

    expect(postAgentBrowserAction).toHaveBeenCalledWith('agent-a', { action: 'start', page_id: undefined })
    expect(wrapper.get('[aria-label="新建标签页"]').exists()).toBe(true)
  })
})
