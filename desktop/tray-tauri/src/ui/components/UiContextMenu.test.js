// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UiContextMenu from './UiContextMenu.vue'
import { nodeIconNames, resolveNodeIcon } from '../../../../../shared/icons/ui/node-registry.js'

let wrappers = []
function menu(props) {
  const wrapper = mount(UiContextMenu, { props, attachTo: document.body })
  wrappers.push(wrapper)
  return wrapper
}
function contextMenu() { return document.body.querySelector('.ui-context-menu') }
afterEach(() => {
  wrappers.forEach(wrapper => wrapper.unmount())
  wrappers = []
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('UiContextMenu', () => {
  it('uses icon keys that resolve to real Node catalog assets', () => {
    const menuIconKeys = ['folder-open', 'file', 'pencil', 'trash', 'workgroup']
    for (const key of menuIconKeys) {
      expect(nodeIconNames).toContain(key)
      expect(resolveNodeIcon(key)).not.toBe(resolveNodeIcon('__missing-menu-icon__'))
    }
  })

  it('omits a repeated path when it matches the menu title', async () => {
    menu({ modelValue: { title: 'reports', context: { path: 'reports' }, items: [{ label: 'Show' }] } })
    await flushPromises()
    expect(contextMenu().querySelector('.ui-context-menu__context').children).toHaveLength(1)
  })

  it('places the measured panel within the viewport edges', async () => {
    vi.stubGlobal('innerWidth', 320)
    vi.stubGlobal('innerHeight', 220)
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 320 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 220 })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
      return this.classList?.contains('ui-context-menu') ? { width: 180, height: 100 } : { width: 0, height: 0 }
    })
    menu({ modelValue: { x: 300, y: 205, items: [{ label: 'Open' }] } })
    await flushPromises()
    const panel = contextMenu()
    expect(panel.style.left).toBe('132px')
    expect(panel.style.top).toBe('112px')
  })

  it('skips disabled items and restores trigger focus on Escape', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    const wrapper = menu({ modelValue: { x: 10, y: 10, trigger, items: [{ label: 'Disabled', disabled: true }, { label: 'First' }, { label: 'Last' }] } })
    await flushPromises()
    const items = contextMenu().querySelectorAll('[role="menuitem"]')
    expect(document.activeElement).toBe(items[1])
    items[1].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    expect(document.activeElement).toBe(items[2])
    items[2].dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(contextMenu()).toBeNull()
    expect(document.activeElement).toBe(trigger)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
  })

  it('lets Escape dismiss a menu whose every action is disabled', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    const wrapper = menu({ modelValue: { x: 10, y: 10, trigger, items: [{ label: 'Unavailable', disabled: true }] } })
    await flushPromises()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(contextMenu()).toBeNull()
    expect(document.activeElement).toBe(trigger)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
  })

  it('closes on outside pointer and hands off rapidly to another menu', async () => {
    const first = menu({ modelValue: { x: 10, y: 10, items: [{ label: 'One' }] } })
    await flushPromises()
    expect(contextMenu()?.textContent).toContain('One')
    const second = menu({ modelValue: { x: 20, y: 20, items: [{ label: 'Two' }] } })
    await flushPromises()
    expect(document.body.querySelectorAll('.ui-context-menu')).toHaveLength(1)
    expect(contextMenu()?.textContent).toContain('Two')
    expect(first.emitted('update:modelValue')?.at(-1)).toEqual([null])
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(contextMenu()).toBeNull()
    expect(second.emitted('update:modelValue')?.at(-1)).toEqual([null])
  })

  it('keeps the panel open while its own long list scrolls', async () => {
    menu({ modelValue: { x: 10, y: 10, items: Array.from({ length: 30 }, (_, index) => ({ label: `Item ${index}` })) } })
    await flushPromises()
    const panel = contextMenu()
    panel.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(contextMenu()).toBe(panel)
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(contextMenu()).toBeNull()
  })

  it('does not let a late menu opening steal focus or a Tab dismissal close the next menu', async () => {
    const triggerA = document.createElement('button')
    const triggerB = document.createElement('button')
    document.body.append(triggerA, triggerB)
    menu({ modelValue: { x: 10, y: 10, trigger: triggerA, items: [{ label: 'A' }] } })
    menu({ modelValue: { x: 20, y: 20, trigger: triggerB, items: [{ label: 'B' }] } })
    await flushPromises()
    const itemB = contextMenu().querySelector('[role="menuitem"]')
    expect(itemB.textContent).toContain('B')
    expect(document.activeElement).toBe(itemB)

    const next = menu({ modelValue: null })
    triggerB.addEventListener('focus', () => {
      void next.setProps({ modelValue: { x: 30, y: 30, items: [{ label: 'Next' }] } })
    }, { once: true })
    itemB.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }))
    await flushPromises()
    expect(contextMenu()?.textContent).toContain('Next')
  })

  it('closes before emitting the captured menu item and snapshot', async () => {
    const snapshot = { x: 10, y: 10, context: { path: 'reports/a.md' }, items: [{ label: 'Show' }] }
    const sequence = []
    const wrapper = menu({ modelValue: snapshot, 'onUpdate:modelValue': value => sequence.push(['close', value]), onSelect: (item, menuSnapshot) => sequence.push(['select', item, menuSnapshot, Boolean(contextMenu())]) })
    await flushPromises()
    contextMenu().querySelector('button').click()
    await flushPromises()
    expect(wrapper.emitted('select')).toEqual([[snapshot.items[0], snapshot]])
    expect(sequence.map(([type]) => type)).toEqual(['close', 'select'])
    expect(sequence[1][3]).toBe(false)
    expect(snapshot.context.path).toBe('reports/a.md')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
  })
})
