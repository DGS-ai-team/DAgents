<script setup>
import { chromeStore } from "../stores/chrome.js";

defineProps({
  subtitle: { type: String, default: "Node Web UI" },
});
</script>

<template>
  <header class="app__header">
    <div class="app__brand">
      <div class="app__brand-mark" aria-hidden="true" />
      <div>
        <div class="app__title">DAgents</div>
        <div class="app__subtitle">{{ subtitle }}</div>
      </div>
    </div>
    <div class="app__meta">
      <span>{{ chromeStore.agentInfo?.agent_id || "—" }}</span>
      <span>·</span>
      <span>{{ chromeStore.llmSettings?.model || "—" }}</span>
    </div>
  </header>
</template>
