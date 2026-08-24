<script setup>
import { ref } from "vue";

const props = defineProps({
  targets: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  error: { type: String, default: "" },
});

const emit = defineEmits(["select", "refresh"]);
const open = ref(false);

function openMenu() {
  open.value = true;
  emit("refresh");
}

function toggle() {
  if (open.value) {
    open.value = false;
    return;
  }
  openMenu();
}

function choose(target) {
  open.value = false;
  emit("select", target);
}

defineExpose({ open: openMenu, close: () => { open.value = false; } });
</script>

<template>
  <div class="terminal-target-menu">
    <button
      type="button"
      class="btn btn--ghost btn--sm terminal-target-menu__trigger"
      :aria-expanded="open"
      aria-label="新建终端"
      title="新建终端"
      @click="toggle"
    >
      <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <path d="M10 4v12M4 10h12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
    </button>

    <div v-if="open" class="terminal-target-menu__popover" role="dialog" aria-label="可连接的终端配置">
      <div class="terminal-target-menu__head">
        <strong>新建终端</strong>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="props.loading" @click="emit('refresh')">
          {{ props.loading ? "读取中…" : "刷新" }}
        </button>
      </div>
      <p v-if="props.error" class="terminal-target-menu__error" role="alert">{{ props.error }}</p>
      <p v-else-if="props.loading && !props.targets.length" class="terminal-target-menu__muted">读取可连接配置中…</p>
      <p v-else-if="!props.targets.length" class="terminal-target-menu__muted">暂无可连接的终端配置。</p>
      <ul v-else class="terminal-target-menu__list">
        <li v-for="target in props.targets" :key="`${target.kind}:${target.id || target.shell}`">
          <button type="button" class="terminal-target-menu__item" @click="choose(target)">
            <span class="terminal-target-menu__icon" aria-hidden="true">&gt;_</span>
            <span class="terminal-target-menu__item-text">
              <strong>{{ target.label }}</strong>
              <small>{{ target.description }}</small>
            </span>
          </button>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.terminal-target-menu { position: relative; display: inline-flex; }
.terminal-target-menu__trigger { width: 30px; height: 30px; padding: 0; }
.terminal-target-menu__trigger svg { width: 15px; height: 15px; }
.terminal-target-menu__popover {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  z-index: 30;
  width: min(300px, calc(100vw - 24px));
  padding: 10px;
  border: 1px solid var(--color-border);
  border-radius: 9px;
  background: var(--color-surface, #fff);
  box-shadow: 0 10px 28px rgb(20 35 50 / 16%);
}
.terminal-target-menu__head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.terminal-target-menu__head strong { color: var(--color-text); font-size: 12px; }
.terminal-target-menu__list { display: grid; gap: 5px; max-height: 300px; margin: 9px 0 0; padding: 0; overflow: auto; list-style: none; }
.terminal-target-menu__item { display: flex; width: 100%; align-items: center; gap: 9px; padding: 8px; border: 1px solid transparent; border-radius: 7px; background: transparent; color: var(--color-text); cursor: pointer; text-align: left; }
.terminal-target-menu__item:hover, .terminal-target-menu__item:focus-visible { border-color: var(--color-border); background: var(--color-surface-alt, #f5f7f9); }
.terminal-target-menu__icon { width: 28px; flex: 0 0 auto; color: var(--color-primary, #3689d6); font: 600 11px/1 var(--font-mono, ui-monospace, monospace); text-align: center; }
.terminal-target-menu__item-text { display: grid; min-width: 0; gap: 3px; }
.terminal-target-menu__item-text strong { overflow: hidden; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.terminal-target-menu__item-text small, .terminal-target-menu__muted, .terminal-target-menu__error { color: var(--color-text-subtle); font-size: 10px; }
.terminal-target-menu__muted, .terminal-target-menu__error { margin: 12px 2px 2px; text-align: center; }
.terminal-target-menu__error { color: var(--color-danger, #c45757); }
</style>
