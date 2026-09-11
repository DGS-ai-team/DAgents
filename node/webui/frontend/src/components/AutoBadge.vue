<template>
  <span v-if="isAuto" class="auto-badge" title="Auto 自主智能体" aria-label="Auto 自主智能体">
    <span class="auto-badge__icon" aria-hidden="true">
      <UiIcon name="bot" :size="14" />
      <UiIcon class="auto-badge__spark" name="sparkles" :size="8" />
    </span>
    <span>Auto</span>
  </span>
</template>

<script setup>
import { computed } from "vue";
import UiIcon from "./UiIcon.vue";
const props = defineProps({ agent: { type: Object, default: null } });
const isAuto = computed(() => String(props.agent?.agent_type || props.agent?.AgentType || "").toLowerCase() === "auto");
</script>

<style scoped>
.auto-badge { display: inline-flex; align-items: center; gap: 3px; color: var(--color-text-subtle); font-size: 11px; font-weight: 500; letter-spacing: .01em; white-space: nowrap; }
.auto-badge__icon { position: relative; display: inline-flex; width: 14px; height: 14px; flex: 0 0 auto; color: var(--color-auto, var(--color-primary)); }
.auto-badge__spark { position: absolute; top: -2px; right: -3px; }
</style>
