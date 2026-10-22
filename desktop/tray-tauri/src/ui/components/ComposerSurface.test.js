// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import ComposerSurface from './ComposerSurface.vue'
import { COMPOSER_MORPH_DURATION_MS, composerPageSettlementMatches, composerPageVisibility } from '../composer-surface-motion.js'

function rect(left, top, width, height) {
  return { left, top, width, height, right: left + width, bottom: top + height, x: left, y: top, toJSON() {} }
}

function setupGeometry() {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
    if (this.matches?.('.composer')) return rect(100, 700, 600, 56)
    if (this.matches?.('.dock-input-surface')) return rect(800, 650, 300, 56)
    const left = Number.parseFloat(this.style.left) || 0
    const top = Number.parseFloat(this.style.top) || 0
    const width = Number.parseFloat(this.style.width) || 0
    const height = Number.parseFloat(this.style.height) || 0
    return rect(left, top, width, height)
  })
  Object.defineProperty(HTMLElement.prototype, 'animate', {
    configurable: true,
    value: vi.fn(() => ({ finished: Promise.resolve(), cancel: vi.fn() })),
  })
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', { configurable: true, value: vi.fn(() => []) })
  window.matchMedia = vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })
}

function mountSurface(events = []) {
  const mode = ref('chat')
  const draft = ref('chat draft')
  const wrapper = mount(defineComponent({
    setup() {
      return () => h(ComposerSurface, {
        mode: mode.value,
        onTransitionStart: event => events.push({ type: 'start', ...event }),
        onTransitionSettled: event => events.push({ type: 'settled', ...event }),
      }, {
        chat: () => h('footer', { class: 'composer-wrap' }, [h('div', { class: 'composer' }, [h('textarea', { value: draft.value, onInput: event => { draft.value = event.target.value } })])]),
        dock: () => h('section', { class: 'terminal-agent-dock' }, [h('div', { class: 'dock-input-surface' }, [h('input', { class: 'dock-composer-input' })])]),
      })
    },
  }), { attachTo: document.body })
  return { mode, wrapper }
}

describe('ComposerSurface', () => {
  it('keeps the requested page hidden until the current morph emits settled', async () => {
    setupGeometry()
    const animations = []
    HTMLElement.prototype.animate.mockImplementation(() => {
      let resolve
      let reject
      const finished = new Promise((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise })
      const animation = { finished, finish: resolve, cancel: vi.fn(() => reject(new Error('cancelled'))) }
      animations.push(animation)
      return animation
    })
    const events = []
    const mode = ref('chat')
    const displayedPage = ref('chat')
    const pending = ref(null)
    const wrapper = mount(defineComponent({
      setup() {
        return () => h('div', [
          h(ComposerSurface, {
            mode: mode.value,
            onTransitionStart: event => {
              events.push({ type: 'start', ...event })
              if (event.mode === 'dock') pending.value = { id: event.id, mode: event.mode }
            },
            onTransitionSettled: event => {
              events.push({ type: 'settled', ...event })
              if (pending.value && composerPageSettlementMatches({
                eventId: event.id,
                eventMode: event.mode,
                pendingId: pending.value.id,
                pendingMode: pending.value.mode,
                currentMode: mode.value,
              })) displayedPage.value = 'terminal'
            },
          }, {
            chat: () => h('footer', { class: 'composer-wrap' }, [h('div', { class: 'composer' })]),
            dock: () => h('section', { class: 'terminal-agent-dock' }, [h('div', { class: 'dock-input-surface' })]),
          }),
          h('main', { class: 'requested-page', style: { display: displayedPage.value === 'terminal' ? '' : 'none' } }, 'terminal page'),
        ])
      },
    }), { attachTo: document.body })
    const modeForTab = tab => tab === 'chat' ? 'chat' : 'dock'
    const visibility = composerPageVisibility({ requestedTab: 'terminal', displayedTab: 'chat', modeForTab })
    expect(visibility).toEqual({ kind: 'wait', tab: 'chat', mode: 'dock' })
    expect(wrapper.find('.requested-page').element.style.display).toBe('none')

    mode.value = 'dock'
    await nextTick()
    await flushPromises()
    const start = events.find(event => event.type === 'start' && event.mode === 'dock')
    expect(start).toBeTruthy()
    expect(wrapper.find('.composer-surface-dock').attributes('aria-hidden')).toBe('true')
    expect(wrapper.find('.requested-page').element.style.display).toBe('none')

    for (let phase = 0; phase < 3; phase += 1) {
      expect(animations[phase]).toBeTruthy()
      animations[phase].finish()
      await flushPromises()
      await nextTick()
      if (phase < 2) expect(wrapper.find('.requested-page').element.style.display).toBe('none')
    }

    const settled = events.find(event => event.type === 'settled' && event.id === start.id)
    expect(settled).toMatchObject({ mode: 'dock', animated: true })
    expect(wrapper.find('.requested-page').element.style.display).toBe('')
    expect(wrapper.find('.composer-surface-dock').attributes('aria-hidden')).toBe('false')
    wrapper.unmount()
  })

  it('keeps the page locked through a quick reverse navigation and ignores the cancelled settle', async () => {
    setupGeometry()
    const animations = []
    HTMLElement.prototype.animate.mockImplementation(() => {
      let resolve
      let reject
      const finished = new Promise((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise })
      const animation = { finished, finish: resolve, cancel: vi.fn(() => reject(new Error('superseded'))) }
      animations.push(animation)
      return animation
    })
    const mode = ref('dock')
    const displayedPage = ref('file')
    const requestedPage = ref('file')
    const pending = ref(null)
    const events = []
    const modeForTab = tab => tab === 'chat' ? 'chat' : ['terminal', 'file', 'browser'].includes(tab) ? 'dock' : 'hidden'
    const onTransitionStart = event => {
      events.push({ type: 'start', ...event })
      if (pending.value?.mode === event.mode) pending.value = { ...pending.value, id: event.id }
    }
    const onTransitionSettled = event => {
      events.push({ type: 'settled', ...event })
      if (pending.value && composerPageSettlementMatches({
        eventId: event.id,
        eventMode: event.mode,
        pendingId: pending.value.id,
        pendingMode: pending.value.mode,
        currentMode: mode.value,
      })) {
        displayedPage.value = requestedPage.value
        pending.value = null
      }
    }
    function requestPage(tab) {
      requestedPage.value = tab
      const decision = composerPageVisibility({
        requestedTab: tab,
        displayedTab: displayedPage.value,
        modeForTab,
        pendingTransition: Boolean(pending.value),
      })
      if (decision.kind === 'show') {
        displayedPage.value = tab
        pending.value = null
      } else {
        const current = pending.value
        pending.value = { mode: decision.mode, id: current?.mode === decision.mode ? current.id : null }
        mode.value = modeForTab(tab)
      }
    }
    const wrapper = mount(defineComponent({
      setup() {
        return () => h('div', [
          h(ComposerSurface, { mode: mode.value, onTransitionStart, onTransitionSettled }, {
            chat: () => h('footer', { class: 'composer-wrap' }, [h('div', { class: 'composer' })]),
            dock: () => h('section', { class: 'terminal-agent-dock' }, [h('div', { class: 'dock-input-surface' })]),
          }),
          h('main', { class: 'requested-page', style: { display: displayedPage.value === 'terminal' ? '' : 'none' } }, 'terminal page'),
        ])
      },
    }), { attachTo: document.body })

    requestPage('chat')
    await nextTick()
    await flushPromises()
    const firstStart = events.find(event => event.type === 'start' && event.mode === 'chat')
    expect(firstStart).toBeTruthy()
    expect(displayedPage.value).toBe('file')
    requestPage('terminal')
    await nextTick()
    await flushPromises()
    const latestStart = events.filter(event => event.type === 'start').at(-1)
    expect(latestStart).toMatchObject({ mode: 'dock' })
    expect(animations[0].cancel).toHaveBeenCalled()
    expect(pending.value).toMatchObject({ id: latestStart.id, mode: 'dock' })
    expect(displayedPage.value).toBe('file')
    expect(wrapper.find('.requested-page').element.style.display).toBe('none')

    onTransitionSettled({ id: firstStart.id, mode: 'chat', animated: true })
    expect(displayedPage.value).toBe('file')
    expect(pending.value).toMatchObject({ id: latestStart.id, mode: 'dock' })

    for (let phase = 1; phase <= 3; phase += 1) {
      animations[phase].finish()
      await flushPromises()
      await nextTick()
      if (phase < 3) expect(wrapper.find('.requested-page').element.style.display).toBe('none')
    }
    expect(displayedPage.value).toBe('terminal')
    expect(wrapper.find('.requested-page').element.style.display).toBe('')
    expect(events.filter(event => event.type === 'settled').at(-1)).toMatchObject({ id: latestStart.id, mode: 'dock' })
    wrapper.unmount()
  })

  it('keeps both slots mounted, animates through the three shape phases, and activates the dock after completion', async () => {
    setupGeometry()
    const { mode, wrapper } = mountSurface()
    const chat = wrapper.find('.composer-surface-chat')
    const dock = wrapper.find('.composer-surface-dock')
    expect(chat.exists()).toBe(true)
    expect(dock.exists()).toBe(true)
    expect(chat.attributes('aria-hidden')).toBe('false')
    expect(dock.attributes('aria-hidden')).toBe('true')
    const originalInput = wrapper.find('.composer textarea').element
    originalInput.value = 'unsent draft'
    originalInput.dispatchEvent(new Event('input', { bubbles: true }))

    mode.value = 'dock'
    await nextTick()
    await flushPromises()

    expect(wrapper.find('.composer-surface-root').attributes('data-mode')).toBe('dock')
    expect(wrapper.find('.composer-surface-chat').attributes('aria-hidden')).toBe('true')
    expect(wrapper.find('.composer-surface-dock').attributes('aria-hidden')).toBe('false')
    expect(HTMLElement.prototype.animate).toHaveBeenCalledTimes(3)
    expect(HTMLElement.prototype.animate.mock.calls.map(([, options]) => options.duration)).toEqual(Array(3).fill(COMPOSER_MORPH_DURATION_MS / 3))
    expect(wrapper.find('.composer').exists()).toBe(true)
    expect(wrapper.find('.composer textarea').element).toBe(originalInput)
    expect(wrapper.find('.composer textarea').element.value).toBe('unsent draft')
    expect(wrapper.find('.terminal-agent-dock').exists()).toBe(true)
    wrapper.unmount()
  })

  it('switches immediately with reduced motion and creates no transient overlay', async () => {
    setupGeometry()
    window.matchMedia.mockReturnValue({ matches: true })
    const { mode, wrapper } = mountSurface()
    mode.value = 'dock'
    await nextTick()
    await flushPromises()
    expect(wrapper.find('.composer-surface-root').attributes('data-mode')).toBe('dock')
    expect(wrapper.find('.composer-surface-motion').exists()).toBe(false)
    expect(HTMLElement.prototype.animate).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('cancels an in-flight target when the mode changes quickly and finishes on the latest mode', async () => {
    setupGeometry()
    const events = []
    const animations = []
    HTMLElement.prototype.animate.mockImplementation(() => {
      let resolve
      let reject
      const finished = new Promise((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise })
      const animation = {
        finished,
        finish: resolve,
        cancel: vi.fn(() => reject(new Error('superseded'))),
      }
      animations.push(animation)
      return animation
    })
    const { mode, wrapper } = mountSurface(events)
    mode.value = 'dock'
    await nextTick()
    await flushPromises()
    expect(animations).toHaveLength(1)

    mode.value = 'chat'
    await nextTick()
    await flushPromises()
    expect(animations[0].cancel).toHaveBeenCalled()
    for (let index = 1; index <= 3; index += 1) {
      await flushPromises()
      animations[index]?.finish()
    }
    await flushPromises()
    expect(wrapper.find('.composer-surface-root').attributes('data-mode')).toBe('chat')
    expect(wrapper.find('.composer-surface-chat').attributes('aria-hidden')).toBe('false')
    const starts = events.filter(event => event.type === 'start')
    const settled = events.filter(event => event.type === 'settled')
    expect(starts.at(-1)?.mode).toBe('chat')
    expect(settled.some(event => event.id === starts[0]?.id && event.mode === 'dock')).toBe(false)
    expect(settled.at(-1)).toMatchObject({ id: starts.at(-1)?.id, mode: 'chat' })
    wrapper.unmount()
  })

  it('cancels the active morph when unmounted', async () => {
    setupGeometry()
    const pending = []
    HTMLElement.prototype.animate.mockImplementation(() => {
      let reject
      const finished = new Promise((_, rejectPromise) => { reject = rejectPromise })
      const animation = { finished, cancel: vi.fn(() => reject(new Error('cancelled'))) }
      pending.push(animation)
      return animation
    })
    const { mode, wrapper } = mountSurface()
    mode.value = 'dock'
    await nextTick()
    await flushPromises()
    wrapper.unmount()
    expect(pending[0]?.cancel).toHaveBeenCalled()
  })
})
