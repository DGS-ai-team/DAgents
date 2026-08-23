<script setup>
const props = defineProps({
  active: { type: String, default: "messages" },
  terminalAvailable: { type: Boolean, default: true },
  activityAvailable: { type: Boolean, default: true },
  terminalBadge: { type: [String, Number], default: "" },
  activityBadge: { type: [String, Number], default: "" },
});

const emit = defineEmits(["change"]);

const items = [
  { id: "messages", label: "消息" },
  { id: "terminal", label: "终端" },
  { id: "activity", label: "活动" },
];

function available(id) {
  if (id === "terminal") return props.terminalAvailable;
  if (id === "activity") return props.activityAvailable;
  return true;
}

function badge(id) {
  if (id === "terminal") return props.terminalBadge;
  if (id === "activity") return props.activityBadge;
  return "";
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
      :title="available(item.id) ? item.label : `${item.label}暂不可用`"
      role="tab"
      @click="select(item.id)"
    >
      <svg v-if="item.id === 'messages'" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <path d="M4 4.25h12a1.75 1.75 0 0 1 1.75 1.75v6A1.75 1.75 0 0 1 16 13.75H9l-3.5 2v-2H4A1.75 1.75 0 0 1 2.25 12V6A1.75 1.75 0 0 1 4 4.25Z" stroke="currentColor" stroke-width="1.35" stroke-linejoin="round" />
      </svg>
      <svg v-else-if="item.id === 'terminal'" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <rect x="2.75" y="3.25" width="14.5" height="13.5" rx="2" stroke="currentColor" stroke-width="1.35" />
        <path d="m5.75 7 2.5 2.25-2.5 2.25M10.5 12h3.25" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
      <svg v-else viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <path d="M4 5h12M4 10h12M4 15h12" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" />
        <circle cx="7" cy="5" r="1.35" fill="currentColor" />
        <circle cx="12" cy="10" r="1.35" fill="currentColor" />
        <circle cx="9" cy="15" r="1.35" fill="currentColor" />
      </svg>
      <span class="workspace-switcher__label">{{ item.label }}</span>
      <span v-if="badge(item.id)" class="workspace-switcher__badge">{{ badge(item.id) }}</span>
    </button>
  </nav>
</template>

<style scoped>
.workspace-switcher {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--color-surface, #fff) 92%, #eef3f8);
}

.workspace-switcher__item {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  min-height: 26px;
  padding: 4px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--color-text-subtle);
  cursor: pointer;
  font-size: 11px;
  white-space: nowrap;
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

.workspace-switcher__badge {
  min-width: 14px;
  padding: 0 3px;
  border-radius: 999px;
  background: var(--color-primary, #3689d6);
  color: #fff;
  font-size: 9px;
  line-height: 14px;
  text-align: center;
}

@media (max-width: 640px) {
  .workspace-switcher__label { display: none; }
  .workspace-switcher__item { width: 30px; justify-content: center; padding-inline: 4px; }
}
</style>
