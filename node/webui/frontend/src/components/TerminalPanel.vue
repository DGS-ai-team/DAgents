<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { TerminalSession } from "../terminal/terminalSession.js";
import * as api from "../api/node.js";

const props = defineProps({
  agentId: { type: String, required: true },
  terminalId: { type: String, default: "" },
  terminalMeta: { type: Object, default: null },
  autoConnect: { type: Boolean, default: false },
  preserveSession: { type: Boolean, default: false },
  embedded: { type: Boolean, default: false },
});

const outputRef = ref(null);
const status = ref("idle");
const error = ref("");
const replayGap = ref(false);
const autoScroll = ref(true);
const selectedShell = ref("powershell");
const defaultLocalShell = typeof navigator !== "undefined" && /Windows/i.test(navigator.userAgent) ? "powershell" : "bash";
const selectedTarget = ref(`local:${defaultLocalShell}`);
const remoteChannels = ref([]);
const targetLoading = ref(false);
let session = null;
let terminal = null;
let fitAddon = null;
let terminalDataDisposable = null;
let terminalScrollDisposable = null;
let resizeObserver = null;

const statusText = computed(
  () =>
    ({
      idle: "未连接",
      connecting: "连接中",
      connected: "已连接",
      disconnected: "连接已断开",
      reconnecting: "重连中",
      exited: "进程已退出",
      closed: "已关闭",
      error: "连接错误",
    })[status.value] || status.value,
);

function appendOutput(event) {
  if (!terminal) return;
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
    theme: {
      background: "#17212b",
      foreground: "#e8f0f7",
      cursor: "#8cc8ff",
      selectionBackground: "rgba(140, 200, 255, 0.28)",
    },
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
      status.value = nextStatus;
      if (nextStatus !== "error") error.value = "";
    },
    onOutput: appendOutput,
    onReplayGap: () => {
      replayGap.value = true;
    },
    onError: (nextError) => {
      error.value = nextError?.message || String(nextError || "终端连接失败");
    },
  });
  if (props.terminalId) {
    session.connect({ sessionId: props.terminalId, rows: 24, cols: 80 });
  } else {
    const [targetKind, targetId = ""] = String(selectedTarget.value || `local:${defaultLocalShell}`).split(":");
    const localShell = targetKind === "local" && targetId ? targetId : selectedShell.value;
    session.connect({
      targetKind: targetKind === "linux_channel" ? "linux_channel" : "local",
      targetId: targetKind === "linux_channel" ? targetId : undefined,
      shell: targetKind === "linux_channel" ? "bash" : localShell,
      rows: 24,
      cols: 80,
    });
  }
}

async function loadRemoteChannels() {
  if (!props.agentId || props.terminalId) return;
  targetLoading.value = true;
  try {
    const [channelResult, bindingResult] = await Promise.all([
      api.listLinuxChannels(),
      api.getAgentLinuxChannels(props.agentId),
    ]);
    const enabled = new Set(
      (bindingResult?.bindings || [])
        .filter((item) => item.enabled !== false)
        .map((item) => String(item.channel_id || "").trim())
        .filter(Boolean),
    );
    remoteChannels.value = (Array.isArray(channelResult?.channels) ? channelResult.channels : [])
      .filter((item) => item.enabled !== false && enabled.has(String(item.channel_id || "").trim()));
  } catch {
    remoteChannels.value = [];
  } finally {
    targetLoading.value = false;
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
  session?.terminate();
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

watch(
  () => props.terminalMeta?.shell,
  (shell) => {
    if (shell) selectedShell.value = String(shell);
  },
  { immediate: true },
);

watch(
  () => props.agentId,
  () => void loadRemoteChannels(),
  { immediate: true },
);

onMounted(() => {
  ensureTerminal();
  if (typeof ResizeObserver === "function" && outputRef.value) {
    resizeObserver = new ResizeObserver(() => resizeTerminal());
    resizeObserver.observe(outputRef.value);
  }
  if (props.autoConnect) connectTerminal();
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  terminalDataDisposable?.dispose();
  terminalScrollDisposable?.dispose();
  terminal?.dispose();
  if (props.preserveSession) session?.detach();
  else session?.close();
});
</script>

<template>
  <section class="terminal-panel">
    <div class="terminal-panel__head">
      <div>
        <div class="terminal-panel__title">终端</div>
        <div class="terminal-panel__subtitle">
          <span class="terminal-panel__dot" :class="`terminal-panel__dot--${status}`"></span>
          {{ statusText }}
        </div>
      </div>
      <div class="terminal-panel__actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="status === 'connecting'" @click="reconnectTerminal">
          {{ status === "idle" || status === "closed" || status === "exited" ? "连接" : status === "reconnecting" ? "重连中…" : "重连" }}
        </button>
        <button
          type="button"
          class="btn btn--ghost btn--sm"
          :disabled="status !== 'connected'"
          @click="terminateTerminal"
        >
          终止
        </button>
        <button type="button" class="btn btn--ghost btn--sm" @click="clearOutput">清空</button>
      </div>
    </div>

    <div v-if="!props.terminalId" class="terminal-panel__options">
      <label class="terminal-panel__field">
        <span>连接目标</span>
        <select v-model="selectedTarget" class="terminal-panel__select" :disabled="status === 'connected' || status === 'connecting' || targetLoading">
          <option value="local:powershell">本机 · PowerShell</option>
          <option value="local:bash">本机 · Bash</option>
          <option value="local:wsl">本机 · WSL（默认发行版）</option>
          <option value="local:cmd">本机 · CMD</option>
          <option v-for="channel in remoteChannels" :key="channel.channel_id" :value="`linux_channel:${channel.channel_id}`">
            {{ channel.display_name || channel.channel_id }} · Linux
          </option>
        </select>
      </label>
      <span class="terminal-panel__hint">WSL 使用本机 wsl.exe；Linux 目标来自当前 Agent 已绑定的通道。</span>
    </div>

    <div v-if="replayGap" class="terminal-panel__notice">
      重连期间的部分终端输出已超过服务端回放范围，当前内容可能不完整。
    </div>
    <div ref="outputRef" class="terminal-panel__output"></div>
    <p class="terminal-panel__input-hint">连接后可直接在终端区域输入命令，支持方向键、Ctrl+C、粘贴和多行交互。</p>
    <p v-if="error" class="terminal-panel__error">{{ error }}</p>
  </section>
</template>

<style scoped>
.terminal-panel {
  margin-top: v-bind('embedded ? "0" : "20px"');
  padding: 16px;
  border: 1px solid color-mix(in srgb, var(--color-border) 85%, transparent);
  border-radius: 12px;
  background: color-mix(in srgb, var(--color-surface, #fff) 96%, #eef5ff);
}

.terminal-panel__head,
.terminal-panel__input-row {
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

.terminal-panel__options {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
}

.terminal-panel__field {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--color-text-subtle);
  font-size: 12px;
}

.terminal-panel__select {
  min-width: 150px;
  padding: 6px 8px;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  background: var(--color-surface, #fff);
  color: var(--color-text);
  font-size: 12px;
}

.terminal-panel__hint {
  color: var(--color-text-subtle);
  font-size: 11px;
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
  background: #17212b;
}

.terminal-panel__output :deep(.xterm) {
  height: 100%;
  padding: 12px;
}

.terminal-panel__output :deep(.xterm-viewport) {
  border-radius: 8px;
}

.terminal-panel__input-row {
  align-items: flex-end;
}

.terminal-panel__input {
  flex: 1;
  min-width: 0;
  resize: vertical;
  padding: 9px 10px;
  border: 1px solid var(--color-border);
  border-radius: 7px;
  background: var(--color-surface, #fff);
  color: var(--color-text);
  font: 12px/1.45 var(--font-mono, ui-monospace, monospace);
}

.terminal-panel__input-hint {
  margin: 0 0 10px;
  color: var(--color-text-subtle);
  font-size: 11px;
}

.terminal-panel__error {
  margin: 8px 0 0;
  color: var(--color-danger);
  font-size: 12px;
}

@media (max-width: 640px) {
  .terminal-panel__head,
  .terminal-panel__input-row {
    align-items: stretch;
    flex-direction: column;
  }

  .terminal-panel__actions {
    justify-content: flex-start;
  }
}
</style>
