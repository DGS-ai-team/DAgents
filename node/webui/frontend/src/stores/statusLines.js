import { reactive } from "vue";

const PHASE_LABELS = {
  prefilling: "准备回复",
  thinking: "思考中",
  compression: "正在压缩上下文",
};

/** 对话流气泡中展示的相位（不含压缩；压缩只在下方状态栏）。 */
export const statusPhaseOrder = ["prefilling", "thinking"];

export const statusStore = reactive({
  phases: {},
  tick: Date.now(),
});

let tickTimer = null;
let compressionWatchdog = null;

const COMPRESSION_STUCK_MS = 120_000;

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

function clearCompressionWatchdog() {
  if (compressionWatchdog) {
    clearTimeout(compressionWatchdog);
    compressionWatchdog = null;
  }
}

export function startStatus(phase) {
  if (hasStatus(phase)) return;
  statusStore.phases[phase] = { startedAt: Date.now() };
  statusStore.tick = Date.now();
  ensureTick();
  if (phase === "compression") {
    clearCompressionWatchdog();
    compressionWatchdog = setTimeout(() => {
      compressionWatchdog = null;
      finishStatus("compression");
    }, COMPRESSION_STUCK_MS);
  }
}

export function finishStatus(phase) {
  if (phase === "compression") clearCompressionWatchdog();
  if (!statusStore.phases[phase]) return;
  delete statusStore.phases[phase];
  statusStore.tick = Date.now();
  stopTickIfIdle();
}

export function finishWaitingStatuses({ beforeReasoning = false } = {}) {
  if (hasStatus("prefilling")) finishStatus("prefilling");
  if (!beforeReasoning && hasStatus("thinking")) finishStatus("thinking");
}

export function hasStatus(phase) {
  return !!statusStore.phases[phase];
}

export function resetStatusLines() {
  clearCompressionWatchdog();
  statusStore.phases = {};
  stopTickIfIdle();
}

export function statusPhaseLabel(phase) {
  return PHASE_LABELS[phase] || String(phase || "").trim() || "状态";
}

export function formatStatusText(phase, state, now = statusStore.tick) {
  const label = statusPhaseLabel(phase);
  const elapsed = Math.max(0, Math.floor((now - state.startedAt) / 1000));
  return `${label} · ${elapsed}s`;
}
