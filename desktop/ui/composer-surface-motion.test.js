import test from 'node:test'
import assert from 'node:assert/strict'
import { COMPOSER_MORPH_DURATION_MS, composerMorphStages, createComposerMotionController, runComposerMorph } from './composer-surface-motion.js'

const from = { left: 20, top: 100, width: 600, height: 56 }
const to = { left: 800, top: 650, width: 300, height: 84 }

test('composer morph collapses, travels as a circle, then expands in order', () => {
  const [collapse, travel, expand] = composerMorphStages(from, to, 28, 11)
  assert.deepEqual([collapse.name, travel.name, expand.name], ['collapse', 'travel', 'expand'])
  assert.equal(collapse.from.width, '600px')
  assert.equal(collapse.to.width, '56px')
  assert.equal(travel.from.width, '56px')
  assert.equal(travel.to.width, '56px')
  assert.equal(expand.to.width, '300px')
  assert.equal(travel.from.borderRadius, '28px')
  assert.equal(travel.to.borderRadius, '28px')
})

test('large expanded docks still travel as a compact input-sized circle', () => {
  const expandedDock = { left: 800, top: 300, width: 380, height: 420 }
  const [, travel] = composerMorphStages(from, expandedDock)
  assert.equal(travel.to.width, '56px')
  assert.equal(travel.to.height, '56px')
})

test('the shared morph duration is 840ms across the three ordered stages', async () => {
  const calls = []
  const phases = []
  await runComposerMorph({ animate(frames, options) { calls.push({ frames, options }); return { finished: Promise.resolve() } } }, from, to, {
    onPhase: phase => phases.push(phase),
  }).finished
  assert.equal(COMPOSER_MORPH_DURATION_MS, 840)
  assert.deepEqual(phases, ['collapse', 'travel', 'expand'])
  assert.deepEqual(calls.map(call => call.options.duration), [280, 280, 280])
})

test('rapid mode changes cancel the old animation before starting another', async () => {
  let rejectFirst
  const calls = []
  const element = {
    animate(frames, options) {
      calls.push({ frames, options })
      if (calls.length === 1) return { finished: new Promise((_, reject) => { rejectFirst = reject }), cancel() { rejectFirst(new Error('cancelled')) } }
      return { finished: Promise.resolve(), cancel() {} }
    },
  }
  const controller = createComposerMotionController()
  const first = controller.start(element, from, to)
  await Promise.resolve()
  const second = controller.start(element, to, from)
  await Promise.all([first, second])
  assert.equal(calls.length, 4)
  assert.deepEqual(calls.map(call => call.frames.length), [2, 2, 2, 2])
})

test('reduced motion skips all Web Animations', async () => {
  let calls = 0
  await runComposerMorph({ animate() { calls += 1 } }, from, to, { reducedMotion: true }).finished
  assert.equal(calls, 0)
})

test('disposing the surface cancels its active animation and rejects later starts', async () => {
  let cancelled = false
  let rejectAnimation
  let calls = 0
  const element = {
    animate() {
      calls += 1
      return {
        finished: new Promise((_, reject) => { rejectAnimation = reject }),
        cancel() { cancelled = true; rejectAnimation(new Error('disposed')) },
      }
    },
  }
  const controller = createComposerMotionController()
  const task = controller.start(element, from, to)
  await Promise.resolve()
  controller.dispose()
  await task
  await controller.start(element, from, to)
  assert.equal(cancelled, true)
  assert.equal(controller.disposed, true)
  assert.equal(calls, 1)
})
