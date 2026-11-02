// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TerminalOpenDialog from './TerminalOpenDialog.vue'

const choices = [
  { kind: 'local', label: '本机 PowerShell' },
  { kind: 'linux_channel', targetId: 'channel-stage', label: 'Staging', hint: 'deploy@staging.example · :22' },
]
let wrapper
afterEach(() => { wrapper?.unmount(); wrapper = null; document.body.innerHTML = '' })

describe('TerminalOpenDialog', () => {
  it('cancel closes the chooser without requesting terminal creation', async () => {
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices } })
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.get('[role="dialog"]').element)
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('create')).toBeUndefined()
  })

  it('traps Tab focus inside the chooser', async () => {
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices } })
    await flushPromises()
    const dialog = wrapper.get('[role="dialog"]')
    await dialog.trigger('keydown', { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(wrapper.get('.terminal-open-create').element)
    await wrapper.get('.terminal-open-create').trigger('keydown', { key: 'Tab' })
    expect(document.activeElement).toBe(wrapper.get('button[aria-label="取消"]').element)
  })

  it('creates the selected configured host and preserves selection when channel props refresh', async () => {
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices } })
    await flushPromises()
    await wrapper.get('.ui-select__trigger').trigger('click')
    await wrapper.get('[role="combobox"]').setValue('staging')
    await wrapper.get('[role="option"]').trigger('click')
    await wrapper.setProps({ choices: [...choices, { kind: 'linux_channel', targetId: 'channel-prod', label: 'Production', hint: 'deploy@prod.example' }] })
    expect(wrapper.get('.ui-select__trigger').text()).toContain('Staging')
    await wrapper.get('.terminal-open-create').trigger('click')
    expect(wrapper.emitted('create')?.[0]?.[0].targetId).toBe('channel-stage')
  })

  it('keeps the local terminal available during Linux loading or failure, with a settings entry', async () => {
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices: [choices[0]], channelsLoading: true } })
    await flushPromises()
    expect(wrapper.get('.terminal-open-create').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ channelsLoading: false, channelsError: 'Linux 通道不可用' })
    expect(wrapper.get('.terminal-open-create').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('.terminal-open-settings').text()).toBe('Linux 设置')
    await wrapper.get('.terminal-open-settings').trigger('click')
    expect(wrapper.emitted('open-settings')).toHaveLength(1)
    expect(wrapper.emitted('create')).toBeUndefined()
  })

  it('offers existing running sessions independently and emits resume without creating', async () => {
    const session = { terminal_id: 'term-12345678abcdef12', status: 'running', display_name: 'Build shell', shell: '/bin/bash', target_kind: 'linux_channel', target_id: 'channel-stage', cwd: '/workspace/project' }
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices, sessions: [session], channelsError: 'SSH list failed' } })
    await flushPromises()
    expect(wrapper.get('.terminal-open-session').text()).toContain('Staging · /workspace/project')
    expect(wrapper.get('.terminal-open-session').text()).not.toContain('abcdef12')
    await wrapper.get('.terminal-open-session').trigger('click')
    expect(wrapper.emitted('resume')?.[0]?.[0]).toEqual(session)
    expect(wrapper.emitted('create')).toBeUndefined()
  })

  it('keeps list errors separate from SSH configuration and exposes independent retries', async () => {
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices, sessionsError: 'Node unavailable', channelsError: 'SSH list failed' } })
    await flushPromises()
    expect(wrapper.get('.terminal-open-create').attributes('disabled')).toBeUndefined()
    const retryButtons = wrapper.findAll('.terminal-open-error button')
    await retryButtons[0].trigger('click')
    await retryButtons[1].trigger('click')
    expect(wrapper.emitted('retry-sessions')).toHaveLength(1)
    expect(wrapper.emitted('retry-channels')).toHaveLength(1)
    expect(wrapper.emitted('create')).toBeUndefined()
  })

  it('keeps the empty dialog concise and searches many configurations by name or host without creating', async () => {
    const many = Array.from({ length: 120 }, (_, index) => ({ kind: 'linux_channel', targetId: `host-${index}`, label: `Linux ${index}`, hint: `deploy@10.0.0.${index}:22` }))
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices: [choices[0], ...many] } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('回收')
    expect(wrapper.text()).not.toContain('已有终端')
    expect(wrapper.findAll('input[type="radio"]')).toHaveLength(0)
    expect(wrapper.find('[role="combobox"]').exists()).toBe(false)
    await wrapper.get('.ui-select__trigger').trigger('click')
    const search = wrapper.get('[role="combobox"]')
    expect(document.activeElement).toBe(search.element)
    await search.setValue('10.0.0.119')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(1)
    expect(wrapper.get('[role="option"]').text()).toContain('Linux 119')
    await search.setValue('不存在的配置')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(0)
    expect(wrapper.get('.ui-select__empty').text()).toBe('无匹配项')
    await search.trigger('keydown', { key: 'Enter' })
    expect(wrapper.get('.ui-select__trigger').text()).toContain('本机 PowerShell')
    expect(wrapper.emitted('create')).toBeUndefined()
  })

  it('supports keyboard selection and closes only the selector on the first Escape', async () => {
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices } })
    await flushPromises()
    await wrapper.get('.ui-select__trigger').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const search = wrapper.get('[role="combobox"]')
    await search.trigger('keydown', { key: 'End' })
    const active = document.getElementById(search.attributes('aria-activedescendant'))
    expect(active.textContent).toContain('Staging')
    await search.trigger('keydown', { key: 'Enter' })
    expect(wrapper.get('.ui-select__trigger').text()).toContain('Staging')
    expect(document.activeElement).toBe(wrapper.get('.ui-select__trigger').element)
    expect(wrapper.emitted('create')).toBeUndefined()
    await wrapper.get('.ui-select__trigger').trigger('click')
    await wrapper.get('[role="combobox"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(wrapper.emitted('cancel')).toBeUndefined()
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})
