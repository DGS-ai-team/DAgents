<script setup>
import { computed } from "vue";
import UiIcon from "./UiIcon.vue";

/** Shared Lucide-style icon mapping for tools, settings and tool bubbles. */
const props = defineProps({
  name: { type: String, required: true },
});

const iconName = computed(() => {
  const aliases = {
    bash: "code-2",
    shell: "code-2",
    terminal: "terminal",
    browser: "browser",
    child_agents: "bot",
    child: "bot",
    computer: "monitor",
    mcp: "network",
    linux: "square-terminal",
    fs: "file",
    hitl: "message-circle",
    memory: "brain",
    skills: "skills",
    triggers: "clock",
    wecom: "send",
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
