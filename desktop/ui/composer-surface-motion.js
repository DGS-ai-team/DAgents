const phases = ['collapse', 'travel', 'expand']
export const COMPOSER_MORPH_DURATION_MS = 840

function rectStyle(rect, radius) {
  return {
    left: `${rect.left}px`,
    top: `${rect.top}px`,
    width: `${rect.width}px`,
    height: `${rect.height}px`,
    borderRadius: `${radius}px`,
  }
}

function circleRect(rect) {
  const diameter = Math.max(1, Math.min(56, rect.width, rect.height))
  return {
    left: rect.left + (rect.width - diameter) / 2,
    top: rect.top + (rect.height - diameter) / 2,
    width: diameter,
    height: diameter,
  }
}

export function composerMorphStages(fromRect, toRect, fromRadius = 18, toRadius = 11) {
  const fromCircle = circleRect(fromRect)
  const toCircle = circleRect(toRect)
  return [
    { name: phases[0], from: rectStyle(fromRect, fromRadius), to: rectStyle(fromCircle, fromCircle.width / 2) },
    { name: phases[1], from: rectStyle(fromCircle, fromCircle.width / 2), to: rectStyle(toCircle, toCircle.width / 2) },
    { name: phases[2], from: rectStyle(toCircle, toCircle.width / 2), to: rectStyle(toRect, toRadius) },
  ]
}

export function runComposerMorph(element, fromRect, toRect, {
  duration = COMPOSER_MORPH_DURATION_MS,
  fromRadius = 18,
  toRadius = 11,
  fadeOut = false,
  reducedMotion = false,
  onPhase = () => {},
} = {}) {
  let cancelled = false
  let activeAnimation = null
  const stages = composerMorphStages(fromRect, toRect, fromRadius, toRadius)
  const cancel = () => {
    cancelled = true
    activeAnimation?.cancel()
  }

  const finished = (async () => {
    if (reducedMotion || typeof element?.animate !== 'function') return
    const stageDuration = duration / stages.length
    for (const stage of stages) {
      if (cancelled) return
      onPhase(stage.name)
      const keyframes = fadeOut && stage.name === 'expand'
        ? [{ ...stage.from, opacity: 1 }, { ...stage.to, opacity: 0 }]
        : [stage.from, stage.to]
      activeAnimation = element.animate(keyframes, {
        duration: stageDuration,
        easing: stage.name === 'travel' ? 'cubic-bezier(.2,.75,.25,1)' : 'ease-in-out',
        fill: 'forwards',
      })
      try {
        await activeAnimation.finished
      } catch {
        return
      }
    }
  })()

  return { finished, cancel }
}

export function createComposerMotionController() {
  let current = null
  let disposed = false
  return {
    start(element, fromRect, toRect, options) {
      if (disposed) return Promise.resolve()
      current?.cancel()
      const motion = runComposerMorph(element, fromRect, toRect, options)
      current = motion
      return motion.finished.finally(() => { if (current === motion) current = null })
    },
    cancel() {
      current?.cancel()
      current = null
    },
    dispose() {
      disposed = true
      this.cancel()
    },
    get disposed() { return disposed },
  }
}
