<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { terminalStatusLabel, terminalTargetLabel } from "../utils/terminalWorkbench.js";

const props = defineProps({
  terminals: { type: Array, default: () => [] },
  activeTerminalId: { type: String, default: "" },
  activeTerminalStatus: { type: String, default: "idle" },
  loading: { type: Boolean, default: false },
});

const emit = defineEmits(["terminal-select"]);
const open = ref(false);
const rootRef = ref(null);
const summary = computed(() => `${props.terminals.length}`);
const countBadge = computed(() => (props.terminals.length > 9 ? "9+" : summary.value));

function toggle() {
  open.value = !open.value;
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
        <span v-if="props.loading" class="terminal-session-indicator__loading">读取中…</span>
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
.terminal-session-indicator { position: relative; display: inline-flex; flex: 0 0 auto; min-width: 26px; }
.terminal-session-indicator__trigger { position: relative; display: inline-flex; width: 26px; min-width: 26px; height: 26px; flex: 0 0 26px; align-items: center; justify-content: center; padding: 0; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--color-text-muted); cursor: pointer; }
.terminal-session-indicator__trigger:hover, .terminal-session-indicator__trigger:focus-visible { border-color: var(--color-border); background: var(--color-surface-alt, #f5f7f9); }
.terminal-session-indicator__icon { width: 18px; height: 18px; }
.terminal-session-indicator__count { position: absolute; right: -3px; bottom: -3px; display: inline-flex; min-width: 12px; height: 12px; align-items: center; justify-content: center; padding: 0 2px; border: 1px solid var(--color-surface, #fff); border-radius: 999px; background: var(--color-accent, #64748b); color: #fff; font-size: 8px; font-weight: 700; line-height: 10px; }
.terminal-session-indicator__popover { position: absolute; left: 0; bottom: calc(100% + 8px); z-index: 30; width: min(320px, calc(100vw - 24px)); padding: 10px; border: 1px solid var(--color-border); border-radius: 9px; background: var(--color-surface, #fff); box-shadow: 0 10px 28px rgb(20 35 50 / 16%); }
.terminal-session-indicator__head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.terminal-session-indicator__head strong { color: var(--color-text); font-size: 12px; }
.terminal-session-indicator__loading { color: var(--color-text-subtle); font-size: 10px; }
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
