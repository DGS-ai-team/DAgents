import { reactive } from "vue";

const PHASE_LABELS = {
  queued: "等待执行",
  model_generating: "模型生成中",
  prefilling: "准备回复",
  thinking: "思考中",
  assistant_generating: "回复生成中",
  tool_executing: "工具执行中",
  tool_waiting: "等待工具审批",
  waiting_user: "等待你的输入",
  compression: "正在压缩上下文",
};

/** 对话流气泡中展示的相位（不含压缩；压缩只在下方状态栏）。 */
export const statusPhaseOrder = [
  "queued",
  "model_generating",
  "prefilling",
  "thinking",
  "assistant_generating",
  "tool_executing",
  "tool_waiting",
  "waiting_user",
];

const TURN_STATUS_PHASES = new Set(statusPhaseOrder);

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
  for (const phase of TURN_STATUS_PHASES) {
    if (phase === "thinking" && beforeReasoning) continue;
    finishStatus(phase);
  }
}

/** 将权威 Turn phase 和内容输出通道映射为一个可展示的状态气泡。 */
export function syncTurnStatus({ phase = "idle", outputChannel = "none" } = {}) {
  let displayPhase = "";
  if (phase === "queued") displayPhase = "queued";
  if (phase === "model_generating") {
    if (outputChannel === "reasoning") displayPhase = "thinking";
    else if (outputChannel === "assistant") displayPhase = "assistant_generating";
    else displayPhase = "model_generating";
  }
  if (phase === "tool_executing") displayPhase = "tool_executing";
  // HITL cards and the user-information composer already communicate these
  // states. Keeping them out of the tail bubble avoids duplicate messages.
  for (const currentPhase of TURN_STATUS_PHASES) {
    if (currentPhase !== displayPhase) finishStatus(currentPhase);
  }
  if (displayPhase) startStatus(displayPhase);
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
