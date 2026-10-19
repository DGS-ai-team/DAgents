const phases = ['collapse', 'travel', 'expand']
export const COMPOSER_MORPH_DURATION_MS = 840

export function composerPageHandoffNeedsFade(fromTab, toTab, modeForTab, reducedMotion = false) {
  if (reducedMotion) return false
  const fromMode = modeForTab(fromTab)
  const toMode = modeForTab(toTab)
  return (fromMode === 'chat' && toMode === 'dock') || (fromMode === 'dock' && toMode === 'chat')
}

export function composerPageHandoffReady(pending) {
  return Boolean(pending?.composerSettled && pending?.fadeOutDone)
}

export function composerPageFadeOutCompletion(pending, key) {
  return pending?.key === key ? { ...pending, fadeOutDone: true } : pending
}

export function composerPageHandoffContinues(pending, nextMode) {
  return Boolean(pending && pending.mode === nextMode)
}

export function composerPageHandoffKeepsOutgoingFade(pending, displayedTab) {
  return Boolean(pending?.fadeOut && pending.fromTab === displayedTab)
}

export function createComposerPageHandoff({ current, mode, id = null, displayedTab, fadeOut, sequence }) {
  const sameMode = composerPageHandoffContinues(current, mode)
  const keepOutgoing = composerPageHandoffKeepsOutgoingFade(current, displayedTab)
  const continueFade = sameMode || keepOutgoing
  return {
    mode,
    id: sameMode ? current.id : id,
    key: continueFade ? current.key : sequence,
    fromTab: continueFade ? current.fromTab : displayedTab,
    fadeOut: continueFade ? current.fadeOut : fadeOut,
    fadeOutDone: continueFade ? current.fadeOutDone : !fadeOut,
    composerSettled: sameMode ? current.composerSettled : false,
  }
}

export function composerPageVisibility({ requestedTab, displayedTab, modeForTab, composerMounted = true, pendingTransition = false }) {
  if (!composerMounted || requestedTab === 'settings') return { kind: 'show', tab: requestedTab }
  const mode = modeForTab(requestedTab)
  if (pendingTransition) return { kind: 'wait', tab: displayedTab, mode }
  if (mode === modeForTab(displayedTab)) return { kind: 'show', tab: requestedTab }
  return { kind: 'wait', tab: displayedTab, mode }
}

export function composerPageSettlementMatches({ eventId, eventMode, pendingId, pendingMode, currentMode }) {
  return eventId === pendingId && eventMode === pendingMode && eventMode === currentMode
}

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
