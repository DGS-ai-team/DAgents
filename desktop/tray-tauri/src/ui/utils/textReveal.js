const BASE_CHARACTERS_PER_SECOND = 48;
// Keep backlog recovery to roughly 3–4 characters per 32 ms frame.
const MAX_CHARACTERS_PER_SECOND = 108;
const BUFFER_BEFORE_CATCH_UP = 96;
// Increase pace gradually so a burst does not turn into a visible text jump.
const BACKLOG_CATCH_UP_RATE = 0.35;
const FRAME_INTERVAL_MS = 32;

/** Advance a buffered assistant string by a time-based, Unicode-safe amount. */
export function advanceTextReveal({ text = "", visibleLength = 0, elapsedMs = 0, remainder = 0 } = {}) {
  const target = String(text);
  let position = Math.max(0, Math.min(target.length, Number(visibleLength) || 0));
  const pendingUnits = target.length - position;
  if (pendingUnits <= 0) return { visibleLength: target.length, remainder: 0, done: true };

  const elapsed = Math.max(0, Math.min(100, Number(elapsedMs) || 0));
  const bufferedUnits = Math.max(0, pendingUnits - BUFFER_BEFORE_CATCH_UP);
  const rate = Math.min(MAX_CHARACTERS_PER_SECOND, BASE_CHARACTERS_PER_SECOND + bufferedUnits * BACKLOG_CATCH_UP_RATE);
  const accumulated = Math.max(0, Number(remainder) || 0) + elapsed * rate / 1000;
  let characters = Math.floor(accumulated);
  const nextRemainder = accumulated - characters;

  while (characters > 0 && position < target.length) {
    const codePoint = target.codePointAt(position);
    position += codePoint > 0xffff ? 2 : 1;
    characters -= 1;
  }

  const done = position >= target.length;
  return {
    visibleLength: position,
    remainder: done ? 0 : nextRemainder,
    done,
  };
}

/** Keep a corrected stream prefix without splitting UTF-16 surrogate pairs. */
export function commonTextPrefixLength(left = "", right = "") {
  const a = String(left);
  const b = String(right);
  let position = 0;
  while (position < a.length && position < b.length) {
    const aPoint = a.codePointAt(position);
    const bPoint = b.codePointAt(position);
    if (aPoint !== bPoint) break;
    position += aPoint > 0xffff ? 2 : 1;
  }
  return position;
}

export const TEXT_REVEAL_FRAME_INTERVAL_MS = FRAME_INTERVAL_MS;
export const TEXT_REVEAL_INITIAL_BACKLOG_LIMIT = 120;
