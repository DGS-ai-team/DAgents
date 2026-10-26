// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TerminalEndDialog from './TerminalEndDialog.vue'

let wrapper
afterEach(() => { wrapper?.unmount(); wrapper = null; document.body.innerHTML = '' })

describe('TerminalEndDialog', () => {
  it('focuses cancel, traps keyboard focus, and Escape cancels without confirming', async () => {
    wrapper = mount(TerminalEndDialog, { attachTo: document.body, props: { open: true, terminalName: 'Build' } })
    await flushPromises()
    const cancel = wrapper.get('.dialog-actions button').element
    const confirm = wrapper.get('.dialog-actions .danger').element
    expect(document.activeElement).toBe(cancel)
    await wrapper.get('[role="alertdialog"]').trigger('keydown', { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(confirm)
    await wrapper.get('[role="alertdialog"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })

  it('confirms only after the explicit destructive action is selected', async () => {
    wrapper = mount(TerminalEndDialog, { attachTo: document.body, props: { open: true, terminalName: 'Build' } })
    await flushPromises()
    expect(wrapper.text()).toContain('Agent 或模型也可能正在使用')
    expect(wrapper.emitted('confirm')).toBeUndefined()
    await wrapper.get('.dialog-actions .danger').trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    expect(wrapper.emitted('cancel')).toBeUndefined()
  })
})
