<script setup>
import { chromeStore } from "../stores/chrome.js";
import brandIcon from "../assets/brand-icon.png";

defineProps({
  subtitle: { type: String, default: "Node Web UI" },
});
</script>

<template>
  <header class="app__header">
    <div class="app__brand">
      <img class="app__brand-mark" :src="brandIcon" width="24" height="24" alt="" aria-hidden="true" />
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
