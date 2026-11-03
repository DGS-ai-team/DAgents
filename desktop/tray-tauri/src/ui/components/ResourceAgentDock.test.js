// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { h, nextTick } from 'vue'
import ResourceAgentDock from './ResourceAgentDock.vue'

describe('ResourceAgentDock', () => {
  it('renders one compact input row with status, expansion, and a single send action', () => {
    const wrapper = mount(ResourceAgentDock, {
      props: { modelValue: 'hello', status: 'pending', canSend: true },
    })
    expect(wrapper.find('.dock-input-surface').exists()).toBe(true)
    expect(wrapper.find('.dock-status-icon').classes()).toContain('icon-mask')
    expect(wrapper.find('.dock-status-icon').attributes('aria-label')).toContain('有待处理的审批')
    expect(wrapper.find('.dock-status-icon').attributes('title')).toContain('待处理')
    expect(wrapper.findAll('.dock-action')).toHaveLength(1)
    expect(wrapper.find('.dock-action').attributes('aria-label')).toBe('发送消息')
    expect(wrapper.find('.dock-details-panel').exists()).toBe(false)
    expect(wrapper.find('.dock-heading').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows terminal turn errors in the dock and exposes them when the toolbar is collapsed', async () => {
    const wrapper = mount(ResourceAgentDock, {
      props: { turnError: '模型服务暂时不可用，已重试2次。请稍后重试或切换模型。' },
    })
    expect(wrapper.find('[role="alert"]').text()).toContain('已重试2次')
    await wrapper.setProps({ collapsed: true, collapsedInToolbar: true })
    expect(wrapper.find('.dock-error-compact').text()).toBe('本轮失败')
    expect(wrapper.find('.dock-collapsed-placeholder').attributes('aria-label')).toContain('模型服务暂时不可用')
    wrapper.unmount()
  })

  it('emits input, send, stop, and failed-draft restore actions without duplicate action buttons', async () => {
    const wrapper = mount(ResourceAgentDock, {
      props: {
        modelValue: '', status: 'idle', canSend: true, failedDraft: true,
      },
    })
    await wrapper.find('.dock-message-input').setValue('send this')
    expect(wrapper.emitted('update:modelValue').at(-1)).toEqual(['send this'])
    await wrapper.setProps({ modelValue: 'send this' })
    await wrapper.find('.dock-restore').trigger('click')
    expect(wrapper.emitted('restore')).toHaveLength(1)
    await wrapper.find('.dock-input-surface').trigger('submit')
    expect(wrapper.emitted('send')).toHaveLength(1)

    const actionElement = wrapper.find('.dock-action').element
    await wrapper.setProps({ status: 'generating', canStop: true, cancelling: false })
    expect(wrapper.findAll('.dock-action')).toHaveLength(1)
    expect(wrapper.find('.dock-action').element).toBe(actionElement)
    expect(wrapper.find('.dock-action').attributes('aria-label')).toBe('停止本轮')
    await wrapper.find('.dock-input-surface').trigger('submit')
    expect(wrapper.emitted('send')).toHaveLength(1)
    expect(wrapper.emitted('stop')).toBeUndefined()
    await wrapper.find('.dock-action').trigger('click')
    expect(wrapper.emitted('stop')).toHaveLength(1)
    await wrapper.setProps({ cancelling: true })
    await wrapper.find('.dock-action').trigger('click')
    expect(wrapper.emitted('stop')).toHaveLength(1)
    await wrapper.setProps({ cancelling: false, canStop: false })
    await wrapper.find('.dock-action').trigger('click')
    expect(wrapper.emitted('stop')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps a labeled workspace reference removable and sendable without leaving the resource dock', async () => {
    const wrapper = mount(ResourceAgentDock, {
      props: {
        agentName: 'Researcher', canSend: true,
        references: [{ id: 'ref-1', name: 'guide.md', path: 'docs/guide.md', anchor: { label: '整个文件' } }],
      },
    })
    expect(wrapper.find('.dock-references').attributes('aria-label')).toBe('Researcher 的已引用资源')
    expect(wrapper.find('.dock-reference').text()).toContain('guide.md')
    expect(wrapper.find('.dock-action').attributes('disabled')).toBeUndefined()
    await wrapper.find('.dock-reference button').trigger('click')
    expect(wrapper.emitted('remove-reference')).toEqual([['ref-1']])
    await wrapper.find('.dock-input-surface').trigger('submit')
    expect(wrapper.emitted('send')).toHaveLength(1)
  })

  it('keeps the same input and draft while the panel expands, retains approval slot actions, and returns focus on collapse', async () => {
    const approve = vi.fn()
    const wrapper = mount(ResourceAgentDock, {
      attachTo: document.body,
      props: { modelValue: '', expanded: false, status: 'pending', agentName: 'Writer' },
      slots: {
        details: () => h('button', { class: 'approval-action', onClick: approve }, '批准'),
      },
    })
    const input = wrapper.find('.dock-message-input')
    const inputElement = input.element
    await input.setValue('keep this draft')
    await wrapper.setProps({ modelValue: 'keep this draft', expanded: true })
    expect(wrapper.find('.dock-message-input').element).toBe(inputElement)
    expect(wrapper.find('.dock-message-input').element.value).toBe('keep this draft')
    expect(wrapper.find('.dock-details-panel').exists()).toBe(true)
    expect(wrapper.find('.approval-action').exists()).toBe(true)
    await wrapper.find('.approval-action').trigger('click')
    expect(approve).toHaveBeenCalledTimes(1)
    const backButton = wrapper.find('.dock-back-to-chat')
    expect(backButton.attributes('title')).toContain('Writer')
    await backButton.trigger('click')
    expect(wrapper.emitted('back-to-chat')).toHaveLength(1)

    await wrapper.find('.approval-action').element.focus()
    await wrapper.setProps({ expanded: false })
    await nextTick()
    expect(document.activeElement).toBe(wrapper.find('.dock-expand-toggle').element)
    expect(wrapper.find('.dock-details-panel').exists()).toBe(false)
    expect(wrapper.find('.dock-message-input').element).toBe(inputElement)
    expect(wrapper.find('.dock-message-input').element.value).toBe('keep this draft')
    wrapper.unmount()
  })
})
