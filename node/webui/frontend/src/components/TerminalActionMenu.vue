<script setup>
import { computed, onBeforeUnmount, ref } from "vue";

const props = defineProps({
  status: { type: String, default: "idle" },
});

const emit = defineEmits(["action"]);
const open = ref(false);
let closeTimer = null;

const reconnectDisabled = computed(() => ["connecting", "connected", "terminating", "reconnecting"].includes(String(props.status || "")));
const terminateDisabled = computed(() => String(props.status || "") !== "connected");

function clearCloseTimer() {
  if (closeTimer) window.clearTimeout(closeTimer);
  closeTimer = null;
}

function show() {
  clearCloseTimer();
  open.value = true;
}

function scheduleClose() {
  clearCloseTimer();
  closeTimer = window.setTimeout(() => {
    open.value = false;
    closeTimer = null;
  }, 140);
}

function toggle() {
  if (open.value) scheduleClose();
  else show();
}

function run(action) {
  if (action === "reconnect" && reconnectDisabled.value) return;
  if (action === "terminate" && terminateDisabled.value) return;
  emit("action", action);
  scheduleClose();
}

onBeforeUnmount(clearCloseTimer);
</script>

<template>
  <div
    class="terminal-action-menu"
    @mouseenter="show"
    @mouseleave="scheduleClose"
    @focusin="show"
    @focusout="scheduleClose"
  >
    <button
      type="button"
      class="terminal-action-menu__trigger"
      :aria-expanded="open"
      aria-label="终端操作"
      title="终端操作"
      @click="toggle"
    >
      <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <circle cx="5" cy="10" r="1.2" fill="currentColor" />
        <circle cx="10" cy="10" r="1.2" fill="currentColor" />
        <circle cx="15" cy="10" r="1.2" fill="currentColor" />
      </svg>
    </button>

    <div v-if="open" class="terminal-action-menu__popover" role="menu" aria-label="终端操作">
      <button
        type="button"
        class="terminal-action-menu__action"
        :disabled="reconnectDisabled"
        role="menuitem"
        aria-label="重连终端"
        title="重连终端"
        @click="run('reconnect')"
      >
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M16 9a6 6 0 1 0 1 3" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
          <path d="M16 4.5v4h-4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
      <button
        type="button"
        class="terminal-action-menu__action terminal-action-menu__action--danger"
        :disabled="terminateDisabled"
        role="menuitem"
        aria-label="终止终端"
        title="终止终端"
        @click="run('terminate')"
      >
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <rect x="5" y="5" width="10" height="10" rx="1.3" fill="currentColor" />
        </svg>
      </button>
      <button
        type="button"
        class="terminal-action-menu__action"
        role="menuitem"
        aria-label="清空终端输出"
        title="清空终端输出"
        @click="run('clear')"
      >
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M4.5 6h11M8 3.5h4l.8 2.5H7.2L8 3.5ZM6 8v6.5a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V8M8.5 9.5v4M11.5 9.5v4" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.terminal-action-menu { position: relative; display: inline-flex; flex: 0 0 auto; }
.terminal-action-menu__trigger,
.terminal-action-menu__action {
  display: inline-flex;
  width: 28px;
  height: 28px;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--color-text-muted);
  cursor: pointer;
}
.terminal-action-menu__trigger:hover,
.terminal-action-menu__trigger:focus-visible,
.terminal-action-menu__action:hover:not(:disabled),
.terminal-action-menu__action:focus-visible:not(:disabled) {
  border-color: var(--color-border);
  background: var(--color-surface-alt, #f5f7f9);
  color: var(--color-text);
}
.terminal-action-menu__trigger svg { width: 16px; height: 16px; }
.terminal-action-menu__popover {
  position: absolute;
  top: calc(100% + 5px);
  right: 0;
  z-index: 30;
  display: flex;
  gap: 2px;
  padding: 4px;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface, #fff);
  box-shadow: 0 8px 24px rgb(20 35 50 / 16%);
}
.terminal-action-menu__action svg { width: 15px; height: 15px; }
.terminal-action-menu__action--danger:hover:not(:disabled),
.terminal-action-menu__action--danger:focus-visible:not(:disabled) { color: var(--color-danger, #c45757); }
.terminal-action-menu__action:disabled { cursor: not-allowed; opacity: 0.35; }
</style>
