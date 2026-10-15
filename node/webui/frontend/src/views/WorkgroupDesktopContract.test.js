// @vitest-environment jsdom
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkgroupWorkspace from '../../../../../desktop/ui/components/WorkgroupWorkspace.vue'
import AutoOverviewPanel from '../../../../../desktop/ui/components/AutoOverviewPanel.vue'
import BrowserViewer from '../../../../../desktop/ui/components/BrowserViewer.vue'
import * as api from '../api/node.js'

vi.mock('../api/node.js', () => ({
  getAutoOverview: vi.fn(),
  getAgentInfo: vi.fn(),
  getAgentBrowserView: vi.fn(),
  listWorkgroups: vi.fn(), getWorkgroup: vi.fn(), listWorkgroupMembers: vi.fn(),
  getWorkgroupTimeline: vi.fn(), listWorkgroupHITL: vi.fn(), listWorkgroupAgents: vi.fn(),
  postWorkgroupMessage: vi.fn(), cancelWorkgroupTurn: vi.fn(), resolveWorkgroupHITL: vi.fn(),
  createWorkgroup: vi.fn(), createWorkgroupMember: vi.fn(), subscribeWorkgroup: vi.fn(),
  unsubscribeWorkgroup: vi.fn(), publishWorkgroup: vi.fn(), listWorkgroupLLMConfigs: vi.fn(), patchWorkgroup: vi.fn(),
  fetchWorkgroupHumanQueue: vi.fn(), patchWorkgroupHumanQueueItem: vi.fn(), cancelWorkgroupHumanQueueItem: vi.fn(), sendWorkgroupHumanQueueItemNow: vi.fn(),
}))

const timelineFixture = [
  { type: 'human_message', event_id: 'human-1', seq: 1, actor_id: 'node-1', text: '请检查成员任务' },
  { type: 'assign_started', event_id: 'assign-started', seq: 2, assign_id: 'as-1', actor_id: 'member-1', direct_member_id: 'member-1', text: '@Worker\n检查项目' },
  { type: 'tool_started', event_id: 'tool-start-1', seq: 3, assign_id: 'as-1', actor_id: 'member-1', direct_member_id: 'member-1', tool_name: 'read_file', tool_call_id: 'call-1', text: 'read_file' },
  { type: 'tool_started', event_id: 'tool-start-2', seq: 4, assign_id: 'as-1', actor_id: 'member-1', direct_member_id: 'member-1', tool_name: 'bash_run', tool_call_id: 'call-2', text: 'bash_run' },
  { type: 'assistant_content', event_id: 'assistant-preview', seq: 5, assign_id: 'as-1', actor_id: 'member-1', direct_member_id: 'member-1', text: '预览不得重复' },
  { type: 'actor_final_text', event_id: 'assistant-final', seq: 6, assign_id: 'as-1', actor_id: 'member-1', direct_member_id: 'member-1', text: '最终结论只显示一次' },
]
const approvalFixture = [{ hitl_id: 'hitl-1', created_at: '2026-10-08T00:00:00Z', metadata: { source: 'agent_ref', assign_id: 'as-1', member_id: 'member-1', items: [{ id: 'call-1', name: 'read_file', hitl_type: 'execute_tool' }, { id: 'call-2', name: 'bash_run', hitl_type: 'execute_tool' }] } }]
let wrapper
const desktopAppSource = readFileSync(resolve(process.cwd(), '../../../desktop/ui/DesktopApp.vue'), 'utf8')

function mockDetail({ events = timelineFixture, hitl = approvalFixture } = {}) {
  api.getWorkgroup.mockResolvedValue({ workgroup_id: 'wg-a', display_name: '调研组', status: 'active' })
  api.listWorkgroupMembers.mockResolvedValue({ members: [{ member_id: 'member-1', agent_id: 'agent-1', display_name: 'Worker' }] })
  api.getWorkgroupTimeline.mockResolvedValue({ events })
  api.listWorkgroupHITL.mockResolvedValue({ hitl })
  api.listWorkgroupAgents.mockResolvedValue({ agents: [{ agent_id: 'agent-2', name: 'Planner', node_id: 'node-2' }] })
}

beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks(); mockDetail(); api.getAgentInfo.mockResolvedValue({ node_id: 'node-1' }); api.fetchWorkgroupHumanQueue.mockResolvedValue({ items: [], busy: false }); api.postWorkgroupMessage.mockResolvedValue({ loop: { status: 'succeeded' } }); api.getAutoOverview.mockResolvedValue({ items: [], total: 0, counts: {} }) })
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.useRealTimers() })

describe('Desktop Auto overview API contract', () => {
  it('shows real state, todo, wake, and dreaming values and emits navigation targets', async () => {
    api.getAutoOverview.mockResolvedValue({ items: [{ agent_id: 'auto-1', display_name: '巡检员', state: 'needs_attention', next_at: '2026-10-08T10:00:00Z', todo_counts: { total: 3, completed: 1 }, todo_summary: ['检查日志'], dreaming: { state: 'waiting', next_at: '2026-10-09T00:00:00Z' } }], counts: { total: 1, needs_attention: 1 }, total: 1, page: 1, page_size: 20 })
    wrapper = mount(AutoOverviewPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('需处理')
    expect(wrapper.text()).toContain('2 项未完成')
    expect(wrapper.text()).toContain('等待执行')
    await wrapper.findAll('button').find(button => button.text() === '打开聊天').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === '设置').trigger('click')
    expect(wrapper.emitted('open-chat')).toEqual([['auto-1']])
    expect(wrapper.emitted('open-config')).toEqual([['auto-1']])
  })
})

describe('Desktop browser viewer API contract', () => {
  it('renders returned page metadata and base64 screenshot, and requests selected tabs', async () => {
    api.getAgentBrowserView.mockResolvedValue({ page_id: 'page-1', title: '第一页', url: 'https://example.test/one', mime: 'image/png', image_base64: 'cG5n', pages: [{ page_id: 'page-1', title: '第一页', url: 'https://example.test/one' }, { page_id: 'page-2', title: '第二页', url: 'https://example.test/two' }] })
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-1' } })
    await flushPromises()
    expect(wrapper.find('h1').text()).toBe('第一页')
    expect(wrapper.find('img').attributes('src')).toBe('data:image/png;base64,cG5n')
    await wrapper.findAll('button').find(button => button.text() === '第二页').trigger('click')
    await flushPromises()
    expect(api.getAgentBrowserView).toHaveBeenLastCalledWith('agent-1', 'page-2')
  })

  it('ignores an old agent response after switching Agent', async () => {
    let finishOld
    api.getAgentBrowserView.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve })).mockResolvedValueOnce({ page_id: 'new-page', title: '新 Agent 页面', url: 'https://new.test', mime: 'image/png', image_base64: 'bmV3', pages: [{ page_id: 'new-page', title: '新 Agent 页面', url: 'https://new.test' }] })
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-old' } })
    await flushPromises()
    await wrapper.setProps({ agentId: 'agent-new' })
    await flushPromises()
    finishOld({ page_id: 'old-page', title: '旧 Agent 页面', image_base64: 'b2xk', pages: [] })
    await flushPromises()
    expect(wrapper.find('h1').text()).toBe('新 Agent 页面')
    expect(wrapper.text()).not.toContain('旧 Agent 页面')
  })

  it('offers connection settings for an unconfigured browser and explains a stopped session', async () => {
    api.getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('浏览器能力未配置或未启用'), { status: 503, data: { error: { code: 'browser_disabled' } } }))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-1' } })
    await flushPromises()
    expect(wrapper.find('[role="status"]').text()).toContain('尚未启用浏览器')
    expect(wrapper.find('[role="status"]').classes()).toContain('browser-viewer__error--neutral')
    await wrapper.findAll('button').find(button => button.text() === '前往浏览器设置').trigger('click')
    expect(wrapper.emitted('open-connection-settings')).toHaveLength(1)
    wrapper.unmount(); wrapper = null
    api.getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('browser session is not started'), { status: 409, data: { error: { code: 'browser_not_started' } } }))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-2' } })
    await flushPromises()
    expect(wrapper.find('[role="status"]').text()).toContain('尚未启动')
    expect(wrapper.find('[role="status"]').classes()).toContain('browser-viewer__error--neutral')
    expect(wrapper.find('[role="status"] button').exists()).toBe(false)
  })

  it('keeps the last screenshot on browser_busy, offers a light notice, and stops automatic polling', async () => {
    api.getAgentBrowserView.mockResolvedValueOnce({ page_id: 'page-1', title: '最近画面', url: 'https://example.test', mime: 'image/png', image_base64: 'b2xk', pages: [{ page_id: 'page-1', title: '最近画面', url: 'https://example.test' }] })
    api.getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('Agent browser is currently in use'), { status: 423, data: { error: { code: 'browser_busy' } } }))
    api.getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('Agent browser is currently in use'), { status: 423, data: { error: { code: 'browser_busy' } } }))
    api.getAgentBrowserView.mockRejectedValueOnce(Object.assign(new Error('Agent browser is currently in use'), { status: 423, data: { error: { code: 'browser_busy' } } }))
    api.getAgentBrowserView.mockResolvedValueOnce({ page_id: 'page-1', title: '新画面', url: 'https://example.test/new', mime: 'image/png', image_base64: 'bmV3', pages: [{ page_id: 'page-1', title: '新画面', url: 'https://example.test/new' }] })
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-1' } })
    await flushPromises()
    const previousImage = wrapper.find('img').attributes('src')
    await wrapper.find('header button').trigger('click')
    await flushPromises()
    expect(wrapper.find('img').attributes('src')).toBe(previousImage)
    expect(wrapper.find('[role="status"]').text()).toContain('稍后将自动重试')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(api.getAgentBrowserView).toHaveBeenCalledTimes(4)
    expect(wrapper.find('img').attributes('src')).toBe(previousImage)
    await wrapper.find('header button').trigger('click')
    await flushPromises()
    expect(wrapper.find('h1').text()).toBe('新画面')
  })

  it('does not keep polling when the browser session has not started', async () => {
    api.getAgentBrowserView.mockRejectedValue(Object.assign(new Error('browser session is not started'), { status: 409, data: { error: { code: 'browser_not_started' } } }))
    wrapper = mount(BrowserViewer, { props: { agentId: 'agent-1' } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(api.getAgentBrowserView).toHaveBeenCalledTimes(1)
  })
})

describe('Desktop workgroup API contract', () => {
  it('keeps browser capability setup separate from workgroup connection setup', () => {
    expect(desktopAppSource).toMatch(/function openWorkgroupConnectionSettings\(\)\s*\{\s*void router\.push\('\/settings\/connection'\)\s*\}/)
    expect(desktopAppSource).toMatch(/function openBrowserCapabilitySettings\(\)\s*\{\s*void router\.push\('\/settings\/capabilities'\)\s*\}/)
  })

  it('binds each resource setup action to its matching settings destination', () => {
    expect(desktopAppSource).toMatch(/<BrowserViewer\b[^>]*@open-connection-settings="openBrowserCapabilitySettings"/)
    expect(desktopAppSource).toMatch(/<WorkgroupWorkspace\b[^>]*@open-connection-settings="openWorkgroupConnectionSettings"/)
  })

  it('uses canonical timeline events and does not render streaming previews as durable messages', async () => {
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-a' } })
    await flushPromises()
    expect(wrapper.text()).toContain('请检查成员任务')
    expect(wrapper.text()).toContain('最终结论只显示一次')
    expect(wrapper.text()).not.toContain('预览不得重复')
    expect(wrapper.text()).toContain('Worker')
    expect(wrapper.text()).toContain('你')
    expect(wrapper.text()).not.toContain('node-1')
    expect(api.getAgentInfo).toHaveBeenCalledOnce()
  })

  it('resolves one approval item while leaving other items pending', async () => {
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-a' } })
    await flushPromises()
    const approve = wrapper.findAll('button').find(button => button.text().includes('批准'))
    expect(approve).toBeTruthy()
    await approve.trigger('click')
    await flushPromises()
    expect(api.resolveWorkgroupHITL).toHaveBeenCalledWith('wg-a', 'hitl-1', { type: 'selection', approved: ['call-1'], rejected: [] })
  })

  it('polls the real timeline during a pending post and exits when the post settles', async () => {
    let finishPost
    api.postWorkgroupMessage.mockImplementation(() => new Promise(resolve => { finishPost = resolve }))
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-a' } })
    await flushPromises()
    const composer = wrapper.find('textarea')
    await composer.setValue('请开始')
    await wrapper.find('form.wg-composer').trigger('submit')
    await flushPromises()
    const baseline = api.getWorkgroupTimeline.mock.calls.length
    await vi.advanceTimersByTimeAsync(1600)
    await flushPromises()
    expect(api.getWorkgroupTimeline.mock.calls.length).toBeGreaterThan(baseline)
    finishPost({ loop: { status: 'succeeded' } })
    await flushPromises()
    expect(wrapper.text()).toContain('本轮已完成')
    expect(wrapper.text()).not.toContain('停止')
  })

  it('discards completion state from a group after switching selection', async () => {
    let finishPost
    api.postWorkgroupMessage.mockImplementation(() => new Promise(resolve => { finishPost = resolve }))
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-a' } })
    await flushPromises()
    await wrapper.find('textarea').setValue('旧组消息')
    await wrapper.find('form.wg-composer').trigger('submit')
    await flushPromises()
    await wrapper.setProps({ workgroupId: 'wg-b' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('旧组消息')
    expect(wrapper.text()).not.toContain('停止')
    finishPost({ loop: { status: 'succeeded' } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('本轮已完成')
  })

  it('preserves queued responses as actionable pending work instead of claiming completion', async () => {
    api.postWorkgroupMessage.mockResolvedValue({ queued: true, queue_id: 'q-1', position: 2, text: '排队任务' })
    api.fetchWorkgroupHumanQueue.mockResolvedValue({ items: [{ queue_id: 'q-1', position: 2, text: '排队任务' }], busy: true })
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-a' } })
    await flushPromises()
    await wrapper.find('textarea').setValue('排队任务')
    await wrapper.find('form.wg-composer').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('消息已排队 · 第 2 位')
    expect(wrapper.text()).toContain('排队任务')
    expect(wrapper.text()).not.toContain('本轮已完成')
    expect(wrapper.text()).toContain('取消排队')
  })

  it('reuses the client message id when the same failed submission is retried', async () => {
    api.postWorkgroupMessage.mockRejectedValueOnce(new Error('temporary network failure')).mockResolvedValueOnce({ loop: { status: 'succeeded' } })
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-a' } })
    await flushPromises()
    await wrapper.find('textarea').setValue('same request')
    await wrapper.find('form.wg-composer').trigger('submit')
    await flushPromises()
    await wrapper.find('form.wg-composer').trigger('submit')
    await flushPromises()
    expect(api.postWorkgroupMessage.mock.calls[0][2]).toBeTruthy()
    expect(api.postWorkgroupMessage.mock.calls[1][2]).toBe(api.postWorkgroupMessage.mock.calls[0][2])
  })

  it('refreshes and emits the sidebar list after a subscription succeeds', async () => {
    api.listWorkgroups.mockResolvedValue([{ workgroup_id: 'wg-a', display_name: '调研组', status: 'active' }])
    api.subscribeWorkgroup.mockResolvedValue({ subscribed: true })
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-a' } })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '订阅').trigger('click')
    await flushPromises()
    expect(api.listWorkgroups).toHaveBeenCalledWith({ scope: 'all' })
    expect(wrapper.emitted('list').at(-1)).toEqual([[{ workgroup_id: 'wg-a', display_name: '调研组', status: 'active' }]])
  })

  it('shows a Manage failure and the connection-settings action instead of an empty list', async () => {
    api.listWorkgroups.mockResolvedValueOnce([{ workgroup_id: 'wg-stale', display_name: '旧工作组', status: 'active' }]).mockRejectedValueOnce(new Error('manage is not enabled'))
    wrapper = mount(WorkgroupWorkspace)
    await flushPromises()
    expect(wrapper.text()).toContain('旧工作组')
    await wrapper.findAll('button').find(button => button.text() === '刷新').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="status"]').text()).toContain('Manage 尚未启用')
    expect(wrapper.findAll('button').find(button => button.text() === '新建工作组').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('旧工作组')
    expect(wrapper.emitted('list').at(-1)).toEqual([[]])
    expect(wrapper.text()).not.toContain('暂无可访问的工作组')
    await wrapper.findAll('button').find(button => button.text() === '配置连接').trigger('click')
    expect(wrapper.emitted('open-connection-settings')).toHaveLength(1)
  })

  it('allows choosing a Supervisor profile before publishing a new group', async () => {
    api.createWorkgroup.mockResolvedValue({ workgroup: { workgroup_id: 'wg-new' } })
    api.getWorkgroup.mockResolvedValue({ workgroup_id: 'wg-new', display_name: '新组', status: 'configuring', llm_profile_id: 'default', llm_profile_revision: '1' })
    api.listWorkgroupLLMConfigs.mockResolvedValue([{ id: 'default', revision: '1', name: '默认模型', is_default: true }, { id: 'deepseek', revision: '3', name: 'DeepSeek' }])
    api.patchWorkgroup.mockResolvedValue({ workgroup: { workgroup_id: 'wg-new', status: 'configuring', llm_profile_id: 'deepseek', llm_profile_revision: '3' } })
    wrapper = mount(WorkgroupWorkspace)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '新建工作组').trigger('click')
    await wrapper.find('form.wg-create input').setValue('新组')
    await wrapper.find('form.wg-create').trigger('submit')
    await flushPromises()
    expect(api.createWorkgroup).toHaveBeenCalledWith('新组')
    expect(wrapper.emitted('select')).toEqual([['wg-new']])
    await wrapper.setProps({ workgroupId: 'wg-new' })
    await flushPromises()
    await wrapper.find('#wg-supervisor-model').setValue('deepseek')
    await flushPromises()
    expect(api.patchWorkgroup).toHaveBeenCalledWith('wg-new', { llm_profile_id: 'deepseek', llm_profile_revision: '3' })
  })

  it('blocks publishing and points to Manage when no allowed model profile is available', async () => {
    api.getWorkgroup.mockResolvedValue({ workgroup_id: 'wg-empty', display_name: '待配置组', status: 'configuring', llm_profile_id: 'default' })
    api.listWorkgroupLLMConfigs.mockResolvedValue([])
    wrapper = mount(WorkgroupWorkspace, { props: { workgroupId: 'wg-empty' } })
    await flushPromises()
    const publish = wrapper.findAll('button').find(button => button.text() === '发布')
    expect(publish.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('Manage 尚无当前节点可用的 LLM 配置')
    await wrapper.findAll('button').find(button => button.text() === '前往连接配置').trigger('click')
    expect(wrapper.emitted('open-connection-settings')).toHaveLength(1)
    expect(api.publishWorkgroup).not.toHaveBeenCalled()
  })
})
