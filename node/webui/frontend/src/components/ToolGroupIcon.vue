<script setup>
import { computed } from "vue";
import UiIcon from "./UiIcon.vue";

/** Shared Lucide-style icon mapping for tools, settings and tool bubbles. */
const props = defineProps({
  name: { type: String, required: true },
});

const iconName = computed(() => {
  const aliases = {
    bash: "square-terminal",
    shell: "square-terminal",
    terminal: "terminal",
    browser: "globe",
    child_agents: "network",
    child: "network",
    computer: "monitor",
    mcp: "network",
    linux: "square-terminal",
    fs: "file",
    hitl: "message-circle",
    memory: "bookmark",
    skills: "sparkles",
    triggers: "clock",
    wecom: "message-circle",
    wrench: "wrench",
  };
  return aliases[props.name] || props.name || "info";
});
</script>

<template>
  <UiIcon class="tool-group-icon" :name="iconName" :size="20" />
</template>

<style scoped>
.tool-group-icon {
  display: block;
  flex-shrink: 0;
}
</style>
