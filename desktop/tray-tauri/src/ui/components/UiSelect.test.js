// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import UiSelect from './UiSelect.vue'

let wrapper
afterEach(() => { wrapper?.unmount(); wrapper = null; document.body.innerHTML = '' })

describe('UiSelect', () => {
  it('preserves the default non-searchable selector with a portalled menu', async () => {
    wrapper = mount(UiSelect, { attachTo: document.body, props: { modelValue: 1, options: [1, { value: 2, label: 'Second' }] } })
    expect(wrapper.get('.ui-select__value').text()).toBe('1')
    await wrapper.get('.ui-select__trigger').trigger('click')
    await nextTick()
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(document.querySelector('[role="listbox"]')).not.toBeNull()
    expect(document.querySelector('[role="combobox"]')).toBeNull()
    document.querySelectorAll('[role="option"]')[1].click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([['2']])
    expect(wrapper.emitted('change')).toEqual([['2']])
    expect(document.querySelector('[role="listbox"]')).toBeNull()
    expect(document.activeElement).toBe(wrapper.get('.ui-select__trigger').element)
  })

  it('skips disabled search results and closes when the selector becomes disabled', async () => {
    wrapper = mount(UiSelect, {
      attachTo: document.body,
      props: { searchable: true, contained: true, options: [{ value: 'a', label: 'Disabled', disabled: true }, { value: 'b', label: 'Available' }] },
    })
    await wrapper.get('.ui-select__trigger').trigger('click')
    await wrapper.get('[role="combobox"]').trigger('keydown', { key: 'Home' })
    const active = document.getElementById(wrapper.get('[role="combobox"]').attributes('aria-activedescendant'))
    expect(active.textContent).toContain('Available')
    await wrapper.setProps({ disabled: true })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(wrapper.get('.ui-select__trigger').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('change')).toBeUndefined()
  })
})
