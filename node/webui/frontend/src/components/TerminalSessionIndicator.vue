<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { terminalStatusLabel, terminalTargetLabel } from "../utils/terminalWorkbench.js";

const props = defineProps({
  terminals: { type: Array, default: () => [] },
  activeTerminalId: { type: String, default: "" },
  activeTerminalStatus: { type: String, default: "idle" },
  loading: { type: Boolean, default: false },
});

const emit = defineEmits(["terminal-select", "terminal-action", "refresh"]);
const open = ref(false);
const rootRef = ref(null);
const summary = computed(() => `${props.terminals.length}`);
const countBadge = computed(() => (props.terminals.length > 9 ? "9+" : summary.value));
const activeTerminal = computed(() => props.terminals.find((item) => item.terminal_id === props.activeTerminalId) || null);
const activeStatus = computed(() => String(props.activeTerminalStatus || activeTerminal.value?.status || "idle"));
const reconnectDisabled = computed(() => ["connecting", "connected", "terminating"].includes(activeStatus.value));
const terminateDisabled = computed(() => activeStatus.value !== "connected");
const reconnectLabel = computed(() => {
  if (activeStatus.value === "connected") return "终端已连接";
  if (activeStatus.value === "terminating") return "终止中";
  if (["idle", "closed", "exited"].includes(activeStatus.value)) return "连接终端";
  if (activeStatus.value === "reconnecting") return "重连中";
  return "重连终端";
});

function toggle() {
  open.value = !open.value;
  if (open.value) emit("refresh");
}

function onDocumentPointerDown(event) {
  if (open.value && !rootRef.value?.contains(event.target)) open.value = false;
}

function onDocumentKeydown(event) {
  if (event.key === "Escape") open.value = false;
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointerDown);
  document.addEventListener("keydown", onDocumentKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onDocumentKeydown);
});
</script>

<template>
  <div ref="rootRef" class="terminal-session-indicator">
    <button
      type="button"
      class="terminal-session-indicator__trigger"
      :aria-expanded="open"
      :aria-label="`终端列表，${summary} 个终端，点击查看`"
      :title="`终端列表 · ${summary}`"
      @click="toggle"
    >
      <svg class="terminal-session-indicator__icon" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <rect x="2.75" y="3.25" width="14.5" height="13.5" rx="2" stroke="currentColor" stroke-width="1.35" />
        <path d="m5.75 7 2.5 2.25-2.5 2.25M10.5 12h3.25" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
      <span v-if="props.terminals.length" class="terminal-session-indicator__count" aria-hidden="true">{{ countBadge }}</span>
    </button>

    <div v-if="open" class="terminal-session-indicator__popover" role="dialog" aria-label="终端列表">
      <div class="terminal-session-indicator__head">
        <strong>终端列表</strong>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="props.loading" @click="emit('refresh')">
          {{ props.loading ? "读取中…" : "刷新" }}
        </button>
      </div>
      <div v-if="activeTerminal" class="terminal-session-indicator__active-actions">
        <div class="terminal-session-indicator__active-meta">
          <strong>当前终端</strong>
          <small>{{ terminalTargetLabel(activeTerminal) }} · {{ terminalStatusLabel(activeStatus) }}</small>
        </div>
        <div class="terminal-session-indicator__action-buttons">
          <button
            type="button"
            class="terminal-session-indicator__action"
            :disabled="reconnectDisabled"
            :title="reconnectLabel"
            :aria-label="reconnectLabel"
            @click="emit('terminal-action', 'reconnect')"
          >
            <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M16 9a6 6 0 1 0 1 3" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
              <path d="M16 4.5v4h-4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
          <button
            type="button"
            class="terminal-session-indicator__action terminal-session-indicator__action--danger"
            :disabled="terminateDisabled"
            title="终止终端"
            aria-label="终止终端"
            @click="emit('terminal-action', 'terminate')"
          >
            <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <rect x="5" y="5" width="10" height="10" rx="1.3" fill="currentColor" />
            </svg>
          </button>
          <button
            type="button"
            class="terminal-session-indicator__action"
            title="清空终端输出"
            aria-label="清空终端输出"
            @click="emit('terminal-action', 'clear')"
          >
            <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M4.5 6h11M8 3.5h4l.8 2.5H7.2L8 3.5ZM6 8v6.5a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V8M8.5 9.5v4M11.5 9.5v4" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>
      </div>
      <p v-if="props.loading && !props.terminals.length" class="terminal-session-indicator__muted">读取中…</p>
      <p v-else-if="!props.terminals.length" class="terminal-session-indicator__muted">当前没有打开的终端。</p>
      <ul v-else class="terminal-session-indicator__list">
        <li v-for="item in props.terminals" :key="item.terminal_id">
          <button
            type="button"
            class="terminal-session-indicator__item"
            :class="{ 'terminal-session-indicator__item--active': item.terminal_id === props.activeTerminalId }"
            @click="open = false; emit('terminal-select', item)"
          >
            <span class="terminal-session-indicator__dot" :class="`terminal-session-indicator__dot--${item.status}`" aria-hidden="true"></span>
            <span class="terminal-session-indicator__item-text">
              <strong>{{ terminalTargetLabel(item) }}</strong>
              <small>{{ terminalStatusLabel(item.status) }}</small>
            </span>
          </button>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.terminal-session-indicator { position: relative; display: inline-flex; }
.terminal-session-indicator__trigger { position: relative; display: inline-flex; width: 26px; height: 26px; align-items: center; justify-content: center; padding: 0; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--color-text-muted); cursor: pointer; }
.terminal-session-indicator__trigger:hover, .terminal-session-indicator__trigger:focus-visible { border-color: var(--color-border); background: var(--color-surface-alt, #f5f7f9); }
.terminal-session-indicator__icon { width: 18px; height: 18px; }
.terminal-session-indicator__count { position: absolute; right: -3px; bottom: -3px; display: inline-flex; min-width: 12px; height: 12px; align-items: center; justify-content: center; padding: 0 2px; border: 1px solid var(--color-surface, #fff); border-radius: 999px; background: var(--color-accent, #64748b); color: #fff; font-size: 8px; font-weight: 700; line-height: 10px; }
.terminal-session-indicator__popover { position: absolute; left: 0; bottom: calc(100% + 8px); z-index: 30; width: min(320px, calc(100vw - 24px)); padding: 10px; border: 1px solid var(--color-border); border-radius: 9px; background: var(--color-surface, #fff); box-shadow: 0 10px 28px rgb(20 35 50 / 16%); }
.terminal-session-indicator__head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.terminal-session-indicator__head strong { color: var(--color-text); font-size: 12px; }
.terminal-session-indicator__active-actions { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-top: 10px; padding: 7px; border: 1px solid var(--color-border); border-radius: 7px; background: var(--color-surface-alt, #f5f7f9); }
.terminal-session-indicator__active-meta { display: grid; min-width: 0; gap: 3px; }
.terminal-session-indicator__active-meta strong { color: var(--color-text); font-size: 11px; }
.terminal-session-indicator__active-meta small { overflow: hidden; color: var(--color-text-subtle); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.terminal-session-indicator__action-buttons { display: inline-flex; flex: 0 0 auto; gap: 3px; }
.terminal-session-indicator__action { display: inline-flex; width: 26px; height: 26px; align-items: center; justify-content: center; padding: 0; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--color-text-muted); cursor: pointer; }
.terminal-session-indicator__action:hover:not(:disabled), .terminal-session-indicator__action:focus-visible:not(:disabled) { border-color: var(--color-border); background: var(--color-surface, #fff); color: var(--color-text); }
.terminal-session-indicator__action--danger:hover:not(:disabled), .terminal-session-indicator__action--danger:focus-visible:not(:disabled) { color: var(--color-danger, #c45757); }
.terminal-session-indicator__action:disabled { cursor: not-allowed; opacity: 0.35; }
.terminal-session-indicator__action svg { width: 15px; height: 15px; }
.terminal-session-indicator__list { display: grid; gap: 5px; max-height: 280px; margin: 9px 0 0; padding: 0; overflow: auto; list-style: none; }
.terminal-session-indicator__item { display: flex; width: 100%; align-items: center; gap: 8px; padding: 8px; border: 1px solid transparent; border-radius: 7px; background: transparent; color: var(--color-text); cursor: pointer; text-align: left; }
.terminal-session-indicator__item:hover, .terminal-session-indicator__item:focus-visible, .terminal-session-indicator__item--active { border-color: var(--color-border); background: var(--color-surface-alt, #f5f7f9); }
.terminal-session-indicator__dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--color-text-muted); }
.terminal-session-indicator__dot--running { background: var(--color-success, #3d9a5f); }
.terminal-session-indicator__item-text { display: grid; min-width: 0; gap: 3px; }
.terminal-session-indicator__item-text strong { overflow: hidden; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.terminal-session-indicator__item-text small, .terminal-session-indicator__muted { color: var(--color-text-subtle); font-size: 10px; }
.terminal-session-indicator__muted { margin: 12px 2px 2px; text-align: center; }
</style>
