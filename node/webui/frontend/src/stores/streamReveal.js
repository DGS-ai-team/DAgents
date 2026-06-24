/** 流式文本匀速揭示（chars/s）；SSE 突发写入 source，UI 按固定速率消费。 */
export const REVEAL_CHARS_PER_SECOND = 420;

const revealState = {
  assistant: { revealedLen: 0, streaming: false },
  reasoning: { revealedLen: 0, streaming: false },
};

let rafHandle = null;
let lastFrameAt = 0;
let sourceReader = () => "";
let revealSink = () => {};

export function configureStreamReveal({ getSourceText, onRevealText }) {
  sourceReader = getSourceText ?? (() => "");
  revealSink = onRevealText ?? (() => {});
}

function readSource(kind) {
  return String(sourceReader(kind) ?? "");
}

export function markRevealStreaming(kind, streaming = true) {
  if (!revealState[kind]) return;
  revealState[kind].streaming = streaming;
  if (streaming) scheduleReveal();
}

export function resetStreamReveal() {
  if (rafHandle != null) {
    cancelAnimationFrame(rafHandle);
    rafHandle = null;
  }
  lastFrameAt = 0;
  revealState.assistant = { revealedLen: 0, streaming: false };
  revealState.reasoning = { revealedLen: 0, streaming: false };
}

export function flushReveal(kind) {
  const source = readSource(kind);
  revealState[kind].revealedLen = source.length;
  revealState[kind].streaming = false;
  revealSink(kind, source, true);
  stopRevealLoopIfIdle();
  return source;
}

export function scheduleReveal() {
  if (rafHandle != null) return;
  lastFrameAt = performance.now();
  rafHandle = requestAnimationFrame(tickReveal);
}

function tickReveal(now) {
  const dt = Math.min(0.05, (now - lastFrameAt) / 1000);
  lastFrameAt = now;
  let budget = Math.max(1, Math.floor(REVEAL_CHARS_PER_SECOND * dt));
  let keepLoop = false;

  for (const kind of ["assistant", "reasoning"]) {
    const st = revealState[kind];
    const source = readSource(kind);
    const pending = source.length - st.revealedLen;
    if (pending > 0 && budget > 0) {
      const step = Math.min(pending, budget);
      st.revealedLen += step;
      budget -= step;
      revealSink(kind, source.slice(0, st.revealedLen), false);
    }
    if (st.streaming || st.revealedLen < source.length) keepLoop = true;
  }

  if (keepLoop) {
    rafHandle = requestAnimationFrame(tickReveal);
  } else {
    rafHandle = null;
  }
}

function stopRevealLoopIfIdle() {
  const busy = ["assistant", "reasoning"].some((kind) => {
    const st = revealState[kind];
    return st.streaming || st.revealedLen < readSource(kind).length;
  });
  if (!busy && rafHandle != null) {
    cancelAnimationFrame(rafHandle);
    rafHandle = null;
  }
}

export function revealedLength(kind) {
  return revealState[kind]?.revealedLen ?? 0;
}
