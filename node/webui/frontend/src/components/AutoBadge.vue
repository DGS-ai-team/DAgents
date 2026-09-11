<template>
  <span v-if="isAuto" class="auto-badge" title="Auto 自主智能体" aria-label="Auto 自主智能体">
    <svg class="auto-badge__icon" viewBox="0 0 20 20" aria-hidden="true">
      <rect x="2.5" y="6" width="11" height="8" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.4" />
      <path d="M8 3.5V6M5.5 10h.01M10.5 10h.01M5.5 12.5h5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
      <path d="m16.25 2.5.55 1.7a1 1 0 0 0 .65.65l1.7.55-1.7.55a1 1 0 0 0-.65.65l-.55 1.7-.55-1.7a1 1 0 0 0-.65-.65l-1.7-.55 1.7-.55a1 1 0 0 0 .65-.65Z" fill="none" stroke="currentColor" stroke-width="1.1" stroke-linejoin="round" />
    </svg>
    <span>Auto</span>
  </span>
</template>

<script setup>
import { computed } from "vue";
const props = defineProps({ agent: { type: Object, default: null } });
const isAuto = computed(() => String(props.agent?.agent_type || props.agent?.AgentType || "").toLowerCase() === "auto");
</script>

<style scoped>
.auto-badge { display: inline-flex; align-items: center; gap: 3px; color: var(--color-text-subtle); font-size: 11px; font-weight: 500; letter-spacing: .01em; white-space: nowrap; }
.auto-badge__icon { display: block; width: 14px; height: 14px; flex: 0 0 auto; color: var(--color-auto, var(--color-primary)); }
</style>
