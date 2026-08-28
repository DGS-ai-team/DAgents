<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import MainChatPanel from "./MainChatPanel.vue";
import TerminalPanel from "./TerminalPanel.vue";
import TerminalWorkbenchComposer from "./TerminalWorkbenchComposer.vue";
import TerminalActionMenu from "./TerminalActionMenu.vue";
import TerminalTargetMenu from "./TerminalTargetMenu.vue";
import WorkspaceSwitcher from "./WorkspaceSwitcher.vue";
import {
  terminalStatusLabel,
  terminalTargetLabel as formatTerminalTargetLabel,
} from "../utils/terminalWorkbench.js";

const props = defineProps({
  agentId: { type: String, required: true },
  selectedTerminalId: { type: String, default: "" },
  selectedTerminalMeta: { type: Object, default: null },
  refreshKey: { type: Number, default: 0 },
  entries: { type: Array, default: () => [] },
  hitlQueue: { type: Array, default: () => [] },
  toolVerbose: { type: Boolean, default: false },
  agentDisabled: { type: Boolean, default: false },
  agentInputDisabled: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  error: { type: String, default: "" },
  hitlBusy: { type: Boolean, default: false },
  hitlBusyIndex: { type: Number, default: -1 },
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
  agentTitle: { type: String, default: "" },
});

const emit = defineEmits([
  "close",
  "terminal-selected",
  "terminal-cleared",
  "send-agent",
  "cancel-agent",
  "toggle-thinking",
  "cycle-effort",
  "switch-profile",
  "approve-all",
  "reject-all",
  "approve-one",
  "reject-one",
  "user-info-submit",
  "user-info-selected",
  "memory-conflict-decide",
  "memory-conflict-cancel",
  "workspace-change",
]);

const terminals = ref([]);
const loading = ref(false);
const terminalError = ref("");
const activeTerminalId = ref(String(props.selectedTerminalId || ""));
// A terminal workspace without a selected session is an empty state. The
// new-session state starts only after the user explicitly chooses a target.
const newSession = ref(false);
// A newly opened session is authoritative as soon as the terminal transport
// emits `started`. Keep this optimistic record until the list API catches up;
// otherwise `hasTerminal` becomes false while the initial PTY replay is still
// being delivered and the TerminalPanel is unmounted mid-handshake.
const pendingTerminal = ref(null);
const attachedSessionId = ref("");
const terminalPanelKey = ref(String(props.selectedTerminalId || "").trim() || "new-terminal");
const autoConnectNewSession = ref(false);
const targetMenuLoading = ref(false);
const targetMenuError = ref("");
const remoteTargets = ref([]);
const selectedTarget = ref({ kind: "local", id: "", shell: "powershell", label: "本机 · PowerShell", description: "Windows PowerShell" });
const agentPanelCollapsed = ref(false);
const terminalPanelRef = ref(null);
const targetMenuRef = ref(null);
const terminalStatus = ref("idle");
const terminatingTerminalId = ref("");
let newTerminalSequence = 0;
let terminationPollTimer = null;
let terminationPollCount = 0;

const selectedTerminal = computed(
  () =>
    terminals.value.find((item) => String(item?.terminal_id || "") === activeTerminalId.value) ||
    (String(pendingTerminal.value?.terminal_id || "") === activeTerminalId.value ? pendingTerminal.value : null) ||
    (String(props.selectedTerminalMeta?.terminal_id || "") === activeTerminalId.value
      ? props.selectedTerminalMeta
      : null),
);

const hasTerminal = computed(() => Boolean(newSession.value || selectedTerminal.value));
const terminalHeader = computed(() => {
  const terminalRecord = selectedTerminal.value || {};
  const terminalKind = String(terminalRecord.target_kind || terminalRecord.kind || "").trim().toLowerCase();
  const terminalTargetId = String(terminalRecord.target_id || terminalRecord.config_id || "").trim();
  const configuredTarget = remoteTargets.value.find((candidate) => candidate.id === terminalTargetId) || {};
  const source = {
    ...(selectedTarget.value || {}),
    ...(terminalKind === "linux_channel" || terminalKind === "linux" ? configuredTarget : {}),
    ...terminalRecord,
  };
  const kind = String(source.target_kind || source.kind || "local").trim().toLowerCase();
  const shell = String(source.shell || (kind === "linux_channel" ? "bash" : "powershell")).trim().toLowerCase();
  const shellLabel = {
    powershell: "PowerShell",
    bash: "Bash",
    sh: "Shell",
    wsl: "WSL",
    cmd: "CMD",
  }[shell] || shell || "终端";
  const remote = kind === "linux_channel" || kind === "linux";
  const host = String(source.host || "").trim();
  const port = Number(source.port || 0);
  return {
    type: remote ? `远程 Linux · ${shellLabel}` : shellLabel,
    endpoint: remote ? (host ? `${host}${port ? `:${port}` : ""}` : "远程 Linux") : "本机",
    username: String(source.username || source.user || "").trim(),
    status: terminalStatusLabel(terminalStatus.value),
  };
});
const panelOwnsAttachedSession = computed(
  () => Boolean(attachedSessionId.value && attachedSessionId.value === activeTerminalId.value),
);
const terminalPanelId = computed(() => (panelOwnsAttachedSession.value || newSession.value ? "" : activeTerminalId.value));
const terminalPanelAutoConnect = computed(
  () => autoConnectNewSession.value || (!newSession.value && !panelOwnsAttachedSession.value),
);
const emptyState = computed(() => !loading.value && !terminalError.value && !hasTerminal.value);
const agentActionRequired = computed(() => props.hitlQueue.length > 0 || Boolean(String(props.error || "").trim()));

watch(agentActionRequired, (required, wasRequired) => {
  if (required && !wasRequired) agentPanelCollapsed.value = false;
});

const localTargets = [
  { kind: "local", id: "", shell: "powershell", label: "本机 · PowerShell", description: "Windows PowerShell" },
  { kind: "local", id: "", shell: "bash", label: "本机 · Bash", description: "本机 Bash" },
  { kind: "local", id: "", shell: "wsl", label: "本机 · WSL", description: "默认 WSL 发行版" },
  { kind: "local", id: "", shell: "cmd", label: "本机 · CMD", description: "Windows 命令提示符" },
];

const connectableTargets = computed(() => [...localTargets, ...remoteTargets.value]);

function targetFromTerminal(item) {
  const kind = String(item?.target_kind || "local").trim() || "local";
  const shell = String(item?.shell || (kind === "linux_channel" ? "bash" : "powershell"));
  if (kind === "linux_channel") {
    const targetId = String(item?.target_id || item?.config_id || "");
    const configured = remoteTargets.value.find((candidate) => candidate.id === targetId) || {};
    return {
      ...configured,
      kind,
      id: targetId,
      shell,
      label: formatTerminalTargetLabel(item),
      description: [item?.username && item?.host ? `${item.username}@${item.host}` : "远程 Linux", item?.port ? `端口 ${item.port}` : ""].filter(Boolean).join(" · "),
      username: item?.username || configured.username,
      host: item?.host || configured.host,
      port: item?.port || configured.port,
    };
  }
  const local = localTargets.find((candidate) => candidate.shell === shell) || localTargets[0];
  return { ...local };
}

async function loadConnectableTargets() {
  if (!props.agentId) return;
  targetMenuLoading.value = true;
  targetMenuError.value = "";
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
    remoteTargets.value = (Array.isArray(channelResult?.channels) ? channelResult.channels : [])
      .filter((item) => item.enabled !== false && enabled.has(String(item.channel_id || "").trim()))
      .map((item) => ({
        kind: "linux_channel",
        id: String(item.channel_id || ""),
        shell: item.remote_shell || "bash",
        label: item.display_name || item.channel_id,
        description: [item.username && item.host ? `${item.username}@${item.host}` : "远程 Linux", item.port ? `端口 ${item.port}` : ""].filter(Boolean).join(" · "),
        username: item.username,
        host: item.host,
        port: item.port,
      }));
  } catch (e) {
    remoteTargets.value = [];
    targetMenuError.value = e?.message || "可连接终端配置读取失败";
  } finally {
    targetMenuLoading.value = false;
  }
}

function selectTerminal(item) {
  const id = String(item?.terminal_id || "").trim();
  if (!id) return;
  if (id === activeTerminalId.value && panelOwnsAttachedSession.value) {
    emit("terminal-selected", item);
    return;
  }
  pendingTerminal.value = null;
  attachedSessionId.value = "";
  autoConnectNewSession.value = false;
  selectedTarget.value = targetFromTerminal(item);
  terminalPanelKey.value = id;
  newSession.value = false;
  activeTerminalId.value = id;
  terminalStatus.value = "idle";
  emit("terminal-selected", item);
}

function openNewTerminal(target = null) {
  // Only a terminal target selected from TerminalTargetMenu may start a
  // session. A bare click handler otherwise passes MouseEvent as the first
  // argument, which used to be treated as a target and auto-created the
  // default PowerShell terminal.
  const selected = target && typeof target === "object" && String(target.kind || "").trim()
    ? target
    : null;
  terminatingTerminalId.value = "";
  if (terminationPollTimer) clearTimeout(terminationPollTimer);
  terminationPollTimer = null;
  if (selected) selectedTarget.value = selected;
  autoConnectNewSession.value = Boolean(selected);
  pendingTerminal.value = null;
  attachedSessionId.value = "";
  newTerminalSequence += 1;
  terminalPanelKey.value = `new-terminal-${newTerminalSequence}`;
  newSession.value = true;
  activeTerminalId.value = "";
  terminalStatus.value = "idle";
  terminalError.value = "";
}

function clearTerminatingPoll() {
  if (terminationPollTimer) clearTimeout(terminationPollTimer);
  terminationPollTimer = null;
  terminationPollCount = 0;
}

function clearActiveTerminal(id) {
  const terminalId = String(id || "").trim();
  if (!terminalId || terminalId !== activeTerminalId.value) return;
  clearTerminatingPoll();
  terminatingTerminalId.value = "";
  pendingTerminal.value = null;
  attachedSessionId.value = "";
  terminals.value = terminals.value.filter(
    (item) => String(item?.terminal_id || "") !== terminalId,
  );
  activeTerminalId.value = "";
  terminalStatus.value = "idle";
  newSession.value = false;
  newTerminalSequence += 1;
  terminalPanelKey.value = `new-terminal-${newTerminalSequence}`;
  terminalError.value = "";
  emit("terminal-cleared", terminalId);
}

async function reconcileTerminatingTerminal(id) {
  const terminalId = String(id || "").trim();
  if (!terminalId || terminalId !== terminatingTerminalId.value) return;
  await load();
  if (terminalId !== terminatingTerminalId.value) return;
  const stillExists = terminals.value.some(
    (item) => String(item?.terminal_id || "") === terminalId,
  );
  if (!stillExists) {
    clearActiveTerminal(terminalId);
    return;
  }
  terminationPollCount += 1;
  if (terminationPollCount >= 20) {
    clearTerminatingPoll();
    terminalError.value = "终端终止请求尚未确认，请刷新终端列表。";
    return;
  }
  terminationPollTimer = setTimeout(() => void reconcileTerminatingTerminal(terminalId), 300);
}

function onTerminalTerminating() {
  const id = String(activeTerminalId.value || "").trim();
  if (!id) return;
  clearTerminatingPoll();
  terminatingTerminalId.value = id;
  pendingTerminal.value = null;
  attachedSessionId.value = "";
  terminalError.value = "";
  terminationPollTimer = setTimeout(() => void reconcileTerminatingTerminal(id), 250);
}

async function load() {
  if (!props.agentId) return;
  loading.value = true;
  terminalError.value = "";
  try {
    const result = await api.listAgentTerminals(props.agentId);
    const listed = Array.isArray(result?.terminals) ? result.terminals : [];
    const pendingId = String(pendingTerminal.value?.terminal_id || "").trim();
    if (pendingId) {
      const current = listed.find((item) => String(item?.terminal_id || "") === pendingId);
      const merged = current ? { ...pendingTerminal.value, ...current } : pendingTerminal.value;
      pendingTerminal.value = merged;
      terminals.value = [merged, ...listed.filter((item) => String(item?.terminal_id || "") !== pendingId)];
    } else {
      terminals.value = listed;
    }
    const current = terminals.value.find(
      (item) => String(item?.terminal_id || "") === activeTerminalId.value,
    );
    if (current) {
      newSession.value = false;
      if (String(props.selectedTerminalId || "") !== activeTerminalId.value) emit("terminal-selected", current);
    } else if (
      activeTerminalId.value &&
      !newSession.value &&
      !panelOwnsAttachedSession.value &&
      activeTerminalId.value !== terminatingTerminalId.value
    ) {
      clearActiveTerminal(activeTerminalId.value);
    }
  } catch (e) {
    terminalError.value = e.message || "加载终端列表失败";
  } finally {
    loading.value = false;
  }
}

async function onTerminalStarted(event) {
  const id = String(event?.terminal_id || event?.session_id || "").trim();
  if (!id) return;
  // Resuming an existing session already has an authoritative identity. Do
  // not treat its `started` event as a new-session event: clearing the
  // terminal_id here would make TerminalPanel detach immediately after it
  // successfully resumes the PTY.
  if (!newSession.value) return;
  const existing = terminals.value.find((candidate) => String(candidate?.terminal_id || "") === id);
  const selected = existing || {
    terminal_id: id,
    target_kind: selectedTarget.value.kind,
    target_id: selectedTarget.value.id || undefined,
    shell: selectedTarget.value.shell,
    display_name: selectedTarget.value.label,
    username: selectedTarget.value.username,
    host: selectedTarget.value.host,
    port: selectedTarget.value.port,
    status: "running",
  };
  pendingTerminal.value = { ...selected, status: "running" };
  terminals.value = [
    pendingTerminal.value,
    ...terminals.value.filter((candidate) => String(candidate?.terminal_id || "") !== id),
  ];
  activeTerminalId.value = id;
  attachedSessionId.value = id;
  autoConnectNewSession.value = false;
  newSession.value = false;
  // Synchronize the route while the original TerminalPanel remains mounted.
  // The list refresh below is reconciliation only and must not gate identity.
  emit("terminal-selected", selected);
  await load();
}

function onTerminalStatus(next) {
  terminalStatus.value = String(next || "idle");
  if (String(next || "") === "error") terminalError.value = "终端连接错误";
}

function onTerminalExited(event) {
  const id = String(event?.terminal_id || event?.session_id || "").trim();
  if (["terminated", "closed"].includes(String(event?.type || "")) && id === activeTerminalId.value) {
    clearActiveTerminal(id);
    return;
  }
  if (id === attachedSessionId.value) {
    attachedSessionId.value = "";
  }
  void load();
}

function onTerminalError(nextError) {
  const message = nextError?.message || String(nextError || "终端连接失败");
  if (
    activeTerminalId.value &&
    !newSession.value &&
    /terminal session (?:not found|is closed|is unavailable|does not belong to agent)/i.test(message)
  ) {
    clearActiveTerminal(activeTerminalId.value);
    return;
  }
  terminalError.value = message;
}

function onTerminalAction(action) {
  const command = String(action || "").trim();
  if (command === "reconnect") terminalPanelRef.value?.reconnect?.();
  else if (command === "terminate") terminalPanelRef.value?.terminate?.();
  else if (command === "clear") terminalPanelRef.value?.clearOutput?.();
}

function toggleAgentPanel() {
  if (agentActionRequired.value && !agentPanelCollapsed.value) return;
  agentPanelCollapsed.value = !agentPanelCollapsed.value;
}

function switchWorkspace(view) {
  const next = String(view || "terminal");
  if (next === "messages") {
    emit("close");
  }
}

watch(
  () => props.selectedTerminalId,
  (next) => {
    const id = String(next || "").trim();
    if (!id) {
      pendingTerminal.value = null;
      attachedSessionId.value = "";
      activeTerminalId.value = "";
      newSession.value = false;
      terminalError.value = "";
      return;
    }
    // The newly-created panel is already attached to this session. Do not
    // change its key when the parent route catches up, or the socket would be
    // closed while the server is replaying the initial PTY output.
    if (id === activeTerminalId.value && attachedSessionId.value === id) return;
    if (id === activeTerminalId.value) return;
    pendingTerminal.value = null;
    attachedSessionId.value = "";
    terminalPanelKey.value = id;
    activeTerminalId.value = id;
    newSession.value = false;
  },
);

watch(
  () => [props.agentId, props.refreshKey],
  () => void load(),
);

watch(
  () => props.agentId,
  () => void loadConnectableTargets(),
  { immediate: true },
);

onMounted(() => void load());

onBeforeUnmount(() => {
  clearTerminatingPoll();
});

defineExpose({ load, openNewTerminal });
</script>

<template>
  <section class="terminal-workbench">
    <header class="terminal-workbench__header">
      <div class="terminal-workbench__heading">
        <div class="terminal-workbench__title-block">
          <h1 class="terminal-workbench__title">终端工作台</h1>
        </div>
      </div>
      <div class="terminal-workbench__header-actions">
        <button
          type="button"
          class="terminal-workbench__agent-trigger"
          :class="{
            'terminal-workbench__agent-trigger--active': !agentPanelCollapsed,
            'terminal-workbench__agent-trigger--attention': agentActionRequired,
          }"
          :title="agentActionRequired ? '有待处理的 Agent 请求，点击查看' : (agentPanelCollapsed ? '展开 Agent 消息' : '收起 Agent 消息')"
          :aria-label="agentActionRequired ? '有待处理的 Agent 请求，点击查看' : (agentPanelCollapsed ? '展开 Agent 消息' : '收起 Agent 消息')"
          :aria-expanded="!agentPanelCollapsed"
          @click="toggleAgentPanel"
        >
          <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <path d="M4 4.25h12a1.75 1.75 0 0 1 1.75 1.75v6A1.75 1.75 0 0 1 16 13.75H9l-3.5 2v-2H4A1.75 1.75 0 0 1 2.25 12V6A1.75 1.75 0 0 1 4 4.25Z" stroke="currentColor" stroke-width="1.35" stroke-linejoin="round" />
          </svg>
          <span v-if="props.hitlQueue.length" class="terminal-workbench__agent-trigger-badge" aria-hidden="true">{{ props.hitlQueue.length > 9 ? "9+" : props.hitlQueue.length }}</span>
          <span v-else-if="props.error" class="terminal-workbench__agent-trigger-badge terminal-workbench__agent-trigger-badge--error" aria-hidden="true">!</span>
        </button>
        <TerminalTargetMenu
          ref="targetMenuRef"
          :targets="connectableTargets"
          :loading="targetMenuLoading"
          :error="targetMenuError"
          @refresh="loadConnectableTargets"
          @select="openNewTerminal"
        />
        <WorkspaceSwitcher active="terminal" @change="switchWorkspace" />
      </div>
    </header>

    <div class="terminal-workbench__body" :class="{ 'terminal-workbench__body--agent-open': !agentPanelCollapsed }">
      <section class="terminal-workbench__terminal-area" aria-label="终端区域">
        <p v-if="loading && !terminals.length" class="terminal-workbench__muted">加载终端列表中…</p>
        <p v-else-if="terminalError" class="terminal-workbench__error" role="alert">{{ terminalError }}</p>
        <div v-else-if="emptyState" class="terminal-workbench__empty">
          <strong>当前没有打开的终端</strong>
          <span>请点击右上角的添加按钮，选择要连接的终端。</span>
        </div>

        <div v-if="hasTerminal" class="terminal-workbench__terminal-bar" aria-label="当前终端">
          <div class="terminal-workbench__terminal-meta">
            <svg class="terminal-workbench__terminal-icon" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <rect x="2.75" y="3.25" width="14.5" height="13.5" rx="2" stroke="currentColor" stroke-width="1.35" />
              <path d="m5.75 7 2.5 2.25-2.5 2.25M10.5 12h3.25" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <strong>{{ terminalHeader.type }}</strong>
            <span class="terminal-workbench__terminal-separator" aria-hidden="true">·</span>
            <span>{{ terminalHeader.endpoint }}</span>
            <template v-if="terminalHeader.username">
              <span class="terminal-workbench__terminal-separator" aria-hidden="true">·</span>
              <span>用户 {{ terminalHeader.username }}</span>
            </template>
            <span
              class="terminal-workbench__terminal-status"
              :class="`terminal-workbench__terminal-status--${terminalStatus}`"
              :title="terminalHeader.status"
              :aria-label="terminalHeader.status"
            ></span>
          </div>
          <TerminalActionMenu :status="terminalStatus" @action="onTerminalAction" />
        </div>

        <TerminalPanel
          v-if="hasTerminal"
          :key="terminalPanelKey"
          ref="terminalPanelRef"
          :agent-id="props.agentId"
          :terminal-id="terminalPanelId"
          :terminal-meta="selectedTerminal"
          :target="selectedTarget"
          :auto-connect="terminalPanelAutoConnect"
          :preserve-session="true"
          :show-actions="false"
          embedded
          @started="onTerminalStarted"
          @status-changed="onTerminalStatus"
          @exited="onTerminalExited"
          @error="onTerminalError"
          @terminating="onTerminalTerminating"
        />
      </section>

      <aside v-if="!agentPanelCollapsed" class="terminal-workbench__agent-area" aria-label="Agent 消息">
        <div class="terminal-workbench__agent-head">
          <div class="terminal-workbench__agent-title"><strong>Agent 消息</strong></div>
          <div class="terminal-workbench__agent-actions">
            <button
              type="button"
              class="btn btn--ghost btn--sm terminal-workbench__icon-btn"
              :disabled="agentActionRequired"
              title="收起 Agent 消息"
              aria-label="收起 Agent 消息"
              @click="toggleAgentPanel"
            >
              <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
                <path d="m5.5 5.5 9 9M14.5 5.5l-9 9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
        <MainChatPanel
          v-if="!agentPanelCollapsed"
          :entries="props.entries"
          :hitl-queue="props.hitlQueue"
          :tool-verbose="props.toolVerbose"
          :disabled="props.agentDisabled"
          :sending="props.sending"
          :cancelling="props.cancelling"
          :error="props.error"
          :hitl-busy="props.hitlBusy"
          :hitl-busy-index="props.hitlBusyIndex"
          :thinking-supported="props.thinkingSupported"
          :llm-settings="props.llmSettings"
          :agent-title="props.agentTitle"
          hide-composer
          compact
          @approve-all="(idx) => emit('approve-all', idx)"
          @reject-all="(idx) => emit('reject-all', idx)"
          @approve-one="(payload) => emit('approve-one', payload)"
          @reject-one="(payload) => emit('reject-one', payload)"
          @user-info-submit="(idx) => emit('user-info-submit', idx)"
          @user-info-selected="(value) => emit('user-info-selected', value)"
          @memory-conflict-decide="(payload) => emit('memory-conflict-decide', payload)"
          @memory-conflict-cancel="(idx) => emit('memory-conflict-cancel', idx)"
        />
      </aside>
    </div>

    <TerminalWorkbenchComposer
      :agent-title="props.agentTitle"
      :agent-can-send="!props.agentDisabled && !props.sending && !props.cancelling"
      :agent-input-disabled="props.agentInputDisabled"
      :agent-sending="props.sending"
      :agent-cancelling="props.cancelling"
      :thinking-supported="props.thinkingSupported"
      :llm-settings="props.llmSettings"
      :terminals="terminals"
      :active-terminal-id="activeTerminalId"
      :active-terminal-status="terminalStatus"
      :terminal-loading="loading"
      @send-agent="(payload) => emit('send-agent', payload)"
      @cancel-agent="emit('cancel-agent')"
      @toggle-thinking="emit('toggle-thinking')"
      @cycle-effort="emit('cycle-effort')"
      @switch-profile="(id) => emit('switch-profile', id)"
      @select-terminal="selectTerminal"
      @refresh-terminals="load"
    />
  </section>
</template>

<style scoped>
.terminal-workbench {
  display: flex;
  min-height: 0;
  height: 100%;
  flex-direction: column;
  position: relative;
  background: var(--color-surface, #fff);
}

.terminal-workbench__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex: 0 0 auto;
  min-height: 42px;
  padding: 4px 12px;
  border-bottom: 1px solid var(--color-border);
}

.terminal-workbench__heading,
.terminal-workbench__header-actions,
.terminal-workbench__title-block {
  display: flex;
  align-items: center;
  gap: 8px;
}

.terminal-workbench__header-actions { margin-left: auto; }
.terminal-workbench__agent-trigger {
  position: relative;
  display: inline-flex;
  width: 30px;
  height: 30px;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 7px;
  background: transparent;
  color: var(--color-text-muted);
  cursor: pointer;
}
.terminal-workbench__agent-trigger:hover,
.terminal-workbench__agent-trigger:focus-visible,
.terminal-workbench__agent-trigger--active {
  border-color: var(--color-border);
  background: var(--color-surface-hover);
  color: var(--color-text);
}
.terminal-workbench__agent-trigger--attention { color: var(--color-warning, #c28a24); }
.terminal-workbench__agent-trigger svg { width: 16px; height: 16px; }
.terminal-workbench__agent-trigger-badge {
  position: absolute;
  top: -3px;
  right: -3px;
  display: inline-flex;
  min-width: 13px;
  height: 13px;
  align-items: center;
  justify-content: center;
  padding: 0 3px;
  border: 2px solid var(--color-surface, #fff);
  border-radius: 999px;
  background: var(--color-warning, #c28a24);
  color: var(--color-text-invert, #fff);
  font-size: 8px;
  font-weight: 700;
  line-height: 1;
}
.terminal-workbench__agent-trigger-badge--error { background: var(--color-danger, #c45757); }
.terminal-workbench__icon-btn { width: 30px; height: 30px; padding: 0; }
.terminal-workbench__icon-btn svg { width: 15px; height: 15px; }
.terminal-workbench__title { margin: 0; color: var(--color-text); font-size: 15px; }

.terminal-workbench__body {
  display: grid;
  position: relative;
  min-height: 0;
  flex: 1 1 auto;
  grid-template-columns: minmax(0, 1fr);
  gap: 6px;
  padding: 6px;
  overflow: hidden;
  background: color-mix(in srgb, var(--color-surface, #fff) 94%, #eef3f8);
}

.terminal-workbench__body--agent-open {
  grid-template-columns: minmax(0, 1fr) minmax(250px, 31%);
}

.terminal-workbench__terminal-area,
.terminal-workbench__agent-area {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: var(--color-surface, #fff);
}

.terminal-workbench__terminal-bar {
  display: flex;
  min-width: 0;
  min-height: 36px;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 3px 7px 3px 10px;
  border-bottom: 1px solid var(--color-border);
  background: var(--color-surface, #fff);
}

.terminal-workbench__terminal-meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  color: var(--color-text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.terminal-workbench__terminal-meta > span:not(.terminal-workbench__terminal-separator):not(.terminal-workbench__terminal-status) {
  overflow: hidden;
  text-overflow: ellipsis;
}

.terminal-workbench__terminal-meta strong {
  overflow: hidden;
  color: var(--color-text);
  font-size: 11px;
  font-weight: 600;
  text-overflow: ellipsis;
}

.terminal-workbench__terminal-icon { width: 15px; height: 15px; flex: 0 0 auto; color: var(--color-text-muted); }
.terminal-workbench__terminal-separator { color: var(--color-text-subtle); }
.terminal-workbench__terminal-status { width: 6px; height: 6px; flex: 0 0 auto; margin-left: 2px; border-radius: 50%; background: var(--color-text-subtle); }
.terminal-workbench__terminal-status--connected { background: var(--color-success, #3d9a5f); }
.terminal-workbench__terminal-status--running { background: var(--color-success, #3d9a5f); }
.terminal-workbench__terminal-status--error { background: var(--color-danger, #c45757); }
.terminal-workbench__terminal-status--terminating { background: var(--color-warning, #c28a24); }

.terminal-workbench__agent-area {
  position: relative;
  width: auto;
  box-shadow: none;
}

.terminal-workbench__terminal-area :deep(.terminal-panel) {
  display: flex;
  min-height: 0;
  height: 100%;
  margin: 0;
  padding: 0;
  flex-direction: column;
  border: 0;
  border-radius: 0;
  background: transparent;
}

.terminal-workbench__terminal-area :deep(.terminal-panel__head) { display: none; }

.terminal-workbench__terminal-area :deep(.terminal-panel__identity) {
  display: none;
}

.terminal-workbench__terminal-area :deep(.terminal-panel__output) {
  min-height: 180px;
  height: auto;
  flex: 1 1 auto;
  margin: 6px;
}

.terminal-workbench__agent-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 9px 10px;
  border-bottom: 1px solid var(--color-border);
}

.terminal-workbench__agent-head div { min-width: 0; }
.terminal-workbench__agent-title { display: block; }
.terminal-workbench__agent-head strong { color: var(--color-text); font-size: 12px; }
.terminal-workbench__agent-actions { display: inline-flex; flex: 0 0 auto; gap: 4px; }

.terminal-workbench__agent-area :deep(.chat--compact) {
  min-height: 0;
  height: 100%;
  border: 0;
  border-radius: 0;
}

.terminal-workbench__agent-area :deep(.chat--compact .chat__header) { display: none; }
.terminal-workbench__agent-area :deep(.chat--compact .chat__stream-wrap) { min-height: 0; }
.terminal-workbench__agent-area :deep(.chat--compact .chat__stream) { padding: 10px; }

.terminal-workbench__empty,
.terminal-workbench__error,
.terminal-workbench__muted {
  display: grid;
  justify-items: center;
  gap: 8px;
  margin: auto;
  padding: 24px;
  color: var(--color-text-subtle);
  font-size: 12px;
  text-align: center;
}

.terminal-workbench__empty strong { color: var(--color-text); font-size: 14px; }
.terminal-workbench__error { color: var(--color-danger, #c45757); }

@media (max-width: 900px) {
  .terminal-workbench__body--agent-open {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr) minmax(180px, 28%);
  }
}

@media (max-width: 640px) {
  .terminal-workbench__header { align-items: flex-start; flex-direction: column; }
  .terminal-workbench__header-actions { width: 100%; overflow-x: auto; }
  .terminal-workbench__body { padding: 6px; }
  .terminal-workbench__body--agent-open { grid-template-rows: minmax(0, 1fr) 190px; }
}
</style>
