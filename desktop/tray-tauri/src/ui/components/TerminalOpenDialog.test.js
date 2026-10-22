// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TerminalOpenDialog from './TerminalOpenDialog.vue'

const choices = [
  { kind: 'local', label: 'Node 本机 PowerShell', hint: '由当前 Node 主机启动' },
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
    const remote = wrapper.findAll('input[type="radio"]').find(input => input.element.value === 'channel-stage')
    await remote.setValue()
    await wrapper.setProps({ choices: [...choices, { kind: 'linux_channel', targetId: 'channel-prod', label: 'Production', hint: 'deploy@prod.example' }] })
    expect(wrapper.findAll('input[type="radio"]').find(input => input.element.value === 'channel-stage').element.checked).toBe(true)
    await wrapper.get('.terminal-open-create').trigger('click')
    expect(wrapper.emitted('create')?.[0]?.[0].targetId).toBe('channel-stage')
  })

  it('does not create while loading or on error, and offers Linux settings when no host is configured', async () => {
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices: [choices[0]], channelsLoading: true } })
    await flushPromises()
    expect(wrapper.get('.terminal-open-create').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ channelsLoading: false, channelsError: 'Linux 通道不可用' })
    expect(wrapper.get('.terminal-open-create').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('.terminal-open-settings').text()).toBe('打开 Linux 设置')
    await wrapper.get('.terminal-open-settings').trigger('click')
    expect(wrapper.emitted('open-settings')).toHaveLength(1)
    expect(wrapper.emitted('create')).toBeUndefined()
  })

  it('offers existing running sessions independently and emits resume without creating', async () => {
    const session = { terminal_id: 'term-12345678abcdef12', status: 'running', display_name: 'Build shell', shell: '/bin/bash', target_kind: 'linux_channel', target_id: 'staging', cwd: '/workspace/project' }
    wrapper = mount(TerminalOpenDialog, { attachTo: document.body, props: { open: true, choices, sessions: [session], channelsError: 'SSH list failed' } })
    await flushPromises()
    expect(wrapper.get('.terminal-open-session').text()).toContain('SSH staging · /workspace/project · #abcdef12')
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
})
