// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../api/node.js', () => ({ getAgentBrowserView: vi.fn() }))
import { getAgentBrowserView } from '../api/node.js'
import BrowserViewer from '../../../../../desktop/ui/components/BrowserViewer.vue'

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
    await wrapper.findAll('.browser-viewer__tabs button')[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('.browser-viewer__tabs button.selected').text()).toBe('Second')

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(getAgentBrowserView).toHaveBeenLastCalledWith('agent-a', 'page-2')
    expect(wrapper.find('.browser-viewer__tabs button.selected').text()).toBe('Second')
  })

  it('ignores a late old-page response when a newer page is selected, and retains the last image after an error', async () => {
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', workspaceScope: 'node-a', active: true } })
    await flushPromises()
    let resolveOld
    getAgentBrowserView.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const oldLoad = wrapper.vm.refreshResources('tool')
    await flushPromises()
    await wrapper.findAll('.browser-viewer__tabs button')[1].trigger('click')
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('temporary offline'), { status: 502 }))
    resolveOld(snapshot('page-1', 'b2xk'))
    await flushPromises()
    await oldLoad

    expect(wrapper.find('.browser-viewer__tabs button.selected').text()).toBe('Second')
    expect(wrapper.find('.browser-viewer__canvas img').attributes('src')).toContain('cGFnZQ==')
  })

  it('clears only a closed selected page and recovers to the remaining browser page', async () => {
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1'))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-a', active: true } })
    await flushPromises()
    getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('browser page was closed'), { status: 404, data: { error: { code: 'browser_page_not_found' } } }))
    getAgentBrowserView.mockResolvedValueOnce(snapshot('page-1', 'bmV3'))

    await wrapper.findAll('.browser-viewer__tabs button')[1].trigger('click')
    await flushPromises()

    expect(getAgentBrowserView).toHaveBeenLastCalledWith('agent-a', '')
    expect(wrapper.find('.browser-viewer__tabs button.selected').text()).toBe('First')
    expect(wrapper.find('.browser-viewer__canvas img').attributes('src')).toContain('bmV3')
  })
})
