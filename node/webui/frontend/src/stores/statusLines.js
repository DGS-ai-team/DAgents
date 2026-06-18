import { reactive } from "vue";

const PHASE_LABELS = {
  prefilling: "prefilling",
  thinking: "thinking",
  compression_blocking: "compressing context (blocking)",
};

export const statusPhaseOrder = ["prefilling", "thinking", "compression_blocking"];

export const statusStore = reactive({
  phases: {},
  tick: Date.now(),
});

let tickTimer = null;

function ensureTick() {
  if (tickTimer) return;
  tickTimer = setInterval(() => {
    statusStore.tick = Date.now();
  }, 500);
}

function stopTickIfIdle() {
  if (Object.keys(statusStore.phases).length === 0 && tickTimer) {
    clearInterval(tickTimer);
    tickTimer = null;
  }
}

export function startStatus(phase) {
  if (hasStatus(phase)) return;
  statusStore.phases[phase] = { startedAt: Date.now(), done: false };
  statusStore.tick = Date.now();
  ensureTick();
}

export function finishStatus(phase) {
  const state = statusStore.phases[phase];
  if (!state || state.done) return;
  state.done = true;
  statusStore.tick = Date.now();
  setTimeout(() => {
    if (statusStore.phases[phase]?.done) {
      delete statusStore.phases[phase];
      stopTickIfIdle();
    }
  }, 600);
}

export function finishWaitingStatuses({ beforeReasoning = false } = {}) {
  if (hasStatus("prefilling")) finishStatus("prefilling");
  if (!beforeReasoning && hasStatus("thinking")) finishStatus("thinking");
}

export function hasStatus(phase) {
  return !!statusStore.phases[phase] && !statusStore.phases[phase].done;
}

export function resetStatusLines() {
  statusStore.phases = {};
  stopTickIfIdle();
}

export function formatStatusText(phase, state, now = statusStore.tick) {
  const label = PHASE_LABELS[phase] || phase;
  const elapsed = Math.max(0, Math.floor((now - state.startedAt) / 1000));
  if (state.done) return `${label}... ${elapsed}s done`;
  const frame = Math.floor((now - state.startedAt) / 500) % 3;
  const dots = ".".repeat(frame + 1).padEnd(3, " ");
  return `${label}${dots} ${elapsed}s`;
}
