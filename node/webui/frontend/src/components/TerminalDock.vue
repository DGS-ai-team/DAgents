<script setup>
import TerminalPanel from "./TerminalPanel.vue";

defineProps({
  agentId: { type: String, required: true },
  selectedTerminalId: { type: String, required: true },
  terminalMeta: { type: Object, default: null },
});

const emit = defineEmits(["close"]);
</script>

<template>
  <section class="terminal-dock">
    <TerminalPanel
      :agent-id="agentId"
      :terminal-id="selectedTerminalId"
      :terminal-meta="terminalMeta"
      auto-connect
      preserve-session
      embedded
      read-only
      show-close
      @close="emit('close')"
    />
  </section>
</template>

<style scoped>
.terminal-dock {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  height: 100%;
  padding: 0;
  overflow: hidden;
  background: var(--color-surface, #fff);
}

.terminal-dock :deep(.terminal-panel) {
  margin-top: 0;
}
</style>
