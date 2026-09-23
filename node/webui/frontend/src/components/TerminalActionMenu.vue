<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  status: { type: String, default: "idle" },
});

const emit = defineEmits(["action"]);
const open = ref(false);
const rootRef = ref(null);

const reconnectDisabled = computed(() => ["connecting", "connected", "terminating", "reconnecting"].includes(String(props.status || "")));
const terminateDisabled = computed(() => String(props.status || "") !== "connected");

function toggle() {
  open.value = !open.value;
}

function run(action) {
  if (action === "reconnect" && reconnectDisabled.value) return;
  if (action === "terminate" && terminateDisabled.value) return;
  emit("action", action);
  open.value = false;
}

function onDocumentPointerDown(event) {
  if (!rootRef.value?.contains(event.target)) open.value = false;
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
  <div
    ref="rootRef"
    class="terminal-action-menu"
  >
    <button
      type="button"
      class="terminal-action-menu__trigger"
      :aria-expanded="open"
      aria-label="终端操作"
      title="终端操作"
      @click="toggle"
    >
      <UiIcon name="more-horizontal" :size="16" />
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
        <UiIcon name="refresh-cw" :size="16" />
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
        <UiIcon name="square" :size="15" />
      </button>
      <button
        type="button"
        class="terminal-action-menu__action"
        role="menuitem"
        aria-label="清空终端输出"
        title="清空终端输出"
        @click="run('clear')"
      >
        <UiIcon name="trash" :size="15" />
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
