<script setup>
import CapabilitiesConfigPanel from "../../components/CapabilitiesConfigPanel.vue";
import ChildAgentsSection from "../../components/settings/ChildAgentsSection.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">能力</h1>
    <p class="settings-page__lead">Node 进程总闸与共享服务；Web UI 固定可用。Agent 工具组请在「Agents」中配置（随浏览器/企微进程开关显隐）。</p>
    <CapabilitiesConfigPanel />
    <ChildAgentsSection />
  </div>
</template>

<style scoped>
.settings-page__lead {
  margin: 0 0 12px;
  font-size: 13px;
  color: var(--color-text-muted);
  line-height: 1.45;
}
</style>
