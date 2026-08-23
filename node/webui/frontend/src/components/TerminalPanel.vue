<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { TerminalSession } from "../terminal/terminalSession.js";
import { themeStore } from "../stores/theme.js";

const props = defineProps({
  agentId: { type: String, required: true },
  terminalId: { type: String, default: "" },
  terminalMeta: { type: Object, default: null },
  target: { type: Object, default: () => ({ kind: "local", shell: "powershell" }) },
  autoConnect: { type: Boolean, default: false },
  preserveSession: { type: Boolean, default: false },
  embedded: { type: Boolean, default: false },
  showActions: { type: Boolean, default: true },
});

const emit = defineEmits(["started", "status-changed", "exited", "error", "terminating"]);

const outputRef = ref(null);
const status = ref("idle");
const error = ref("");
const replayGap = ref(false);
const autoScroll = ref(true);
let session = null;
let terminal = null;
let fitAddon = null;
let terminalDataDisposable = null;
let terminalScrollDisposable = null;
let resizeObserver = null;
let mounted = false;

const statusText = computed(
  () =>
    ({
      idle: "未连接",
      connecting: "连接中",
      connected: "已连接",
      terminating: "终止中",
      disconnected: "连接已断开",
      reconnecting: "重连中",
      exited: "进程已退出",
      closed: "已关闭",
      error: "连接错误",
    })[status.value] || status.value,
);

function appendOutput(event) {
  if (!mounted || !terminal) return;
  const shouldFollow = autoScroll.value;
  terminal.write(event.text, () => {
    if (shouldFollow) terminal.scrollToBottom();
  });
}

function ensureTerminal() {
  if (terminal || !outputRef.value) return;
  terminal = new Terminal({
    convertEol: false,
    cursorBlink: true,
    scrollback: 5000,
    fontSize: 12,
    lineHeight: 1.35,
    theme: terminalTheme(),
  });
  fitAddon = new FitAddon();
  terminal.loadAddon(fitAddon);
  terminal.open(outputRef.value);
  terminalDataDisposable = terminal.onData((data) => {
    if (status.value === "connected") session?.sendInput(data);
  });
  terminalScrollDisposable = terminal.onScroll(() => {
    autoScroll.value = terminal.buffer.active.viewportY >= terminal.buffer.active.baseY;
  });
  void nextTick(resizeTerminal);
}

function connectTerminal() {
  if (!props.agentId) return;
  ensureTerminal();
  if (session) {
    if (props.preserveSession) session.detach();
    else session.close();
  }
  error.value = "";
  replayGap.value = false;
  session = new TerminalSession(props.agentId, {
    onStatus: (nextStatus) => {
      if (!mounted) return;
      status.value = nextStatus;
      if (nextStatus !== "error") error.value = "";
      emit("status-changed", nextStatus);
    },
    onOutput: appendOutput,
    onReplayGap: () => {
      if (!mounted) return;
      replayGap.value = true;
    },
    onError: (nextError) => {
      if (!mounted) return;
      error.value = nextError?.message || String(nextError || "终端连接失败");
      emit("error", nextError);
    },
    onEvent: (event) => {
      if (!mounted) return;
      if (event?.type === "started") emit("started", event);
      if (["exited", "terminated", "closed"].includes(event?.type)) emit("exited", event);
    },
  });
  if (props.terminalId) {
    session.connect({ sessionId: props.terminalId, rows: 24, cols: 80 });
  } else {
    const target = props.target || {};
    const targetKind = String(target.kind || "local");
    session.connect({
      targetKind,
      targetId: target.id || undefined,
      shell: target.shell || (targetKind === "linux_channel" ? "bash" : "powershell"),
      rows: 24,
      cols: 80,
    });
  }
}

function reconnectTerminal() {
  if (!session || status.value === "closed" || status.value === "exited") {
    connectTerminal();
    return;
  }
  error.value = "";
  session.reconnectNow();
}

function terminateTerminal() {
  if (session?.terminate()) emit("terminating");
}

function clearOutput() {
  terminal?.reset();
}

function resizeTerminal() {
  if (!terminal || !fitAddon) return;
  try {
    fitAddon.fit();
    if (status.value === "connected") session?.resize(terminal.rows, terminal.cols);
  } catch {
    // The panel may be hidden while its settings route is changing.
  }
}

watch(
  () => props.agentId,
  () => {
    if (status.value !== "idle") connectTerminal();
  },
);

function themeColor(name, fallback) {
  if (typeof window === "undefined") return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}

function terminalTheme() {
  return {
    background: themeColor("--color-editor", "#202020"),
    foreground: themeColor("--color-text", "#e6e6e6"),
    cursor: themeColor("--color-primary", "#60cdff"),
    selectionBackground: themeColor("--color-selection", "rgba(96, 205, 255, 0.28)"),
  };
}

function applyTerminalTheme() {
  if (terminal) terminal.options.theme = terminalTheme();
}

watch(
  () => props.autoConnect,
  (next, previous) => {
    if (next && !previous && status.value === "idle") connectTerminal();
  },
);

watch(
  () => themeStore.resolved,
  () => nextTick(applyTerminalTheme),
);

watch(
  () => props.terminalId,
  (next, previous) => {
    if (next === previous) return;
    if (!next) {
      if (props.preserveSession) session?.detach();
      else session?.close();
      return;
    }
    connectTerminal();
  },
);

onMounted(() => {
  mounted = true;
  ensureTerminal();
  if (typeof ResizeObserver === "function" && outputRef.value) {
    resizeObserver = new ResizeObserver(() => resizeTerminal());
    resizeObserver.observe(outputRef.value);
  }
  if (props.autoConnect) connectTerminal();
});

onBeforeUnmount(() => {
  mounted = false;
  resizeObserver?.disconnect();
  terminalDataDisposable?.dispose();
  terminalScrollDisposable?.dispose();
  terminal?.dispose();
  if (props.preserveSession) session?.detach();
  else session?.close();
});

defineExpose({
  getStatus: () => status.value,
  reconnect: reconnectTerminal,
  terminate: terminateTerminal,
  clearOutput,
});
</script>

<template>
  <section class="terminal-panel">
    <div class="terminal-panel__head">
      <div class="terminal-panel__identity">
        <div class="terminal-panel__title">终端</div>
        <div class="terminal-panel__subtitle">
          <span class="terminal-panel__dot" :class="`terminal-panel__dot--${status}`"></span>
          {{ statusText }}
        </div>
      </div>
      <div v-if="props.showActions" class="terminal-panel__actions">
        <button
          type="button"
          class="btn btn--ghost btn--sm terminal-panel__icon-btn"
          :disabled="status === 'connecting' || status === 'connected' || status === 'terminating'"
          :title="status === 'connected' ? '终端已连接' : status === 'terminating' ? '终止中' : status === 'idle' || status === 'closed' || status === 'exited' ? '连接终端' : status === 'reconnecting' ? '重连中' : '重连终端'"
          :aria-label="status === 'connected' ? '终端已连接' : status === 'terminating' ? '终止中' : status === 'idle' || status === 'closed' || status === 'exited' ? '连接终端' : status === 'reconnecting' ? '重连中' : '重连终端'"
          @click="reconnectTerminal"
        >
          <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <path d="M16 9a6 6 0 1 0 1 3" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
            <path d="M16 4.5v4h-4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <button
          type="button"
          class="btn btn--ghost btn--sm terminal-panel__icon-btn"
          :disabled="status !== 'connected'"
          title="终止终端"
          aria-label="终止终端"
          @click="terminateTerminal"
        >
          <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <rect x="5" y="5" width="10" height="10" rx="1.3" fill="currentColor" />
          </svg>
        </button>
        <button type="button" class="btn btn--ghost btn--sm terminal-panel__icon-btn" title="清空终端输出" aria-label="清空终端输出" @click="clearOutput">
          <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <path d="M4.5 6h11M8 3.5h4l.8 2.5H7.2L8 3.5ZM6 8v6.5a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V8M8.5 9.5v4M11.5 9.5v4" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>
    </div>

    <div v-if="replayGap" class="terminal-panel__notice">
      重连期间的部分终端输出已超过服务端回放范围，当前内容可能不完整。
    </div>
    <div ref="outputRef" class="terminal-panel__output"></div>
    <p v-if="error" class="terminal-panel__error">{{ error }}</p>
  </section>
</template>

<style scoped>
.terminal-panel {
  margin-top: v-bind('embedded ? "0" : "20px"');
  padding: 16px;
  border: 1px solid color-mix(in srgb, var(--color-border) 85%, transparent);
  border-radius: 12px;
  background: var(--color-editor, #202020);
}

.terminal-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.terminal-panel__title {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-text);
}

.terminal-panel__subtitle {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  font-size: 12px;
  color: var(--color-text-subtle);
}

.terminal-panel__dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-text-subtle);
}

.terminal-panel__dot--connected {
  background: var(--color-success, #3d9a5f);
}

.terminal-panel__dot--connecting,
.terminal-panel__dot--reconnecting {
  background: var(--color-warning, #c28a24);
}

.terminal-panel__dot--error,
.terminal-panel__dot--disconnected {
  background: var(--color-danger, #c45757);
}

.terminal-panel__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.terminal-panel__icon-btn {
  width: 30px;
  height: 30px;
  padding: 0;
}

.terminal-panel__icon-btn svg {
  width: 15px;
  height: 15px;
}

.terminal-panel__notice {
  margin-top: 12px;
  padding: 8px 10px;
  border-radius: 7px;
  background: #fff7e6;
  color: #8a621b;
  font-size: 12px;
  line-height: 1.45;
}

.terminal-panel__output {
  height: 260px;
  margin: 14px 0 10px;
  overflow: hidden;
  border-radius: 8px;
  background: var(--color-editor, #202020);
}

.terminal-panel__output :deep(.xterm) {
  height: 100%;
  padding: 12px;
}

.terminal-panel__output :deep(.xterm-viewport) {
  border-radius: 8px;
  background: var(--color-editor, #202020) !important;
}

.terminal-panel__output :deep(.xterm-screen) {
  background: var(--color-editor, #202020);
}

.terminal-panel__error {
  margin: 8px 0 0;
  color: var(--color-danger);
  font-size: 12px;
}

@media (max-width: 640px) {
  .terminal-panel__head {
    align-items: stretch;
    flex-direction: column;
  }

  .terminal-panel__actions {
    justify-content: flex-start;
  }
}
</style>
