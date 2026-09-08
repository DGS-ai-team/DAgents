<template>
  <span v-if="isAuto" class="auto-badge" title="Auto 自主智能体" aria-label="Auto 自主智能体">
    <span class="auto-badge__icon" aria-hidden="true">✦</span>
    <span>Auto</span>
  </span>
</template>

<script setup>
import { computed } from "vue";
const props = defineProps({ agent: { type: Object, default: null } });
const isAuto = computed(() => String(props.agent?.agent_type || props.agent?.AgentType || "").toLowerCase() === "auto");
</script>

<style scoped>
.auto-badge { display: inline-flex; align-items: center; gap: 4px; color: var(--color-accent); font-size: 11px; font-weight: 700; letter-spacing: .02em; white-space: nowrap; }
.auto-badge__icon { display: inline-grid; place-items: center; width: 15px; height: 15px; border: 1px solid currentColor; border-radius: 50%; font-size: 9px; line-height: 1; }
</style>
