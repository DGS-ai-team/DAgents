<script setup>
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  active: { type: String, default: "messages" },
  terminalAvailable: { type: Boolean, default: true },
});

const emit = defineEmits(["change"]);

const items = [
  { id: "messages", label: "消息" },
  { id: "terminal", label: "终端" },
];

function available(id) {
  if (id === "terminal") return props.terminalAvailable;
  return true;
}

function select(id) {
  if (!available(id)) return;
  emit("change", id);
}
</script>

<template>
  <nav class="workspace-switcher" aria-label="工作区视图" role="tablist">
    <button
      v-for="item in items"
      :key="item.id"
      type="button"
      class="workspace-switcher__item"
      :class="{ 'workspace-switcher__item--active': props.active === item.id }"
      :disabled="!available(item.id)"
      :aria-selected="props.active === item.id"
      :aria-disabled="!available(item.id)"
      :aria-label="item.label"
      :title="available(item.id) ? item.label : `${item.label}暂不可用`"
      role="tab"
      @click="select(item.id)"
    >
      <UiIcon :name="item.id === 'messages' ? 'message-square' : 'terminal'" :size="15" />
    </button>
  </nav>
</template>

<style scoped>
.workspace-switcher {
  display: inline-flex;
  align-items: center;
  gap: 1px;
  padding: 2px;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--color-surface, #fff) 92%, #eef3f8);
}

.workspace-switcher__item {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 28px;
  padding: 4px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--color-text-subtle);
  cursor: pointer;
}

.workspace-switcher__item svg {
  width: 14px;
  height: 14px;
  flex: 0 0 auto;
}

.workspace-switcher__item:hover:not(:disabled) {
  color: var(--color-text);
  background: var(--color-surface-hover);
}

.workspace-switcher__item--active {
  color: var(--color-text);
  background: var(--color-surface, #fff);
  box-shadow: 0 1px 3px rgb(20 35 50 / 12%);
}

.workspace-switcher__item:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

</style>
