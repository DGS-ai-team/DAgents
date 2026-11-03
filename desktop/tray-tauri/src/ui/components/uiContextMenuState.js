let menuOpenSequence = 0

export function nextContextMenuSequence() {
  menuOpenSequence += 1
  return menuOpenSequence
}

export function isCurrentContextMenuSequence(sequence) {
  return sequence === menuOpenSequence
}
