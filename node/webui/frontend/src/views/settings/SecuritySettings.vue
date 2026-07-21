<script setup>
import SecurityConfigPanel from "../../components/SecurityConfigPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">安全</h1>
    <p class="settings-page__hint">
      工具与 Shell 审批策略已按 Agent 区分，请到「设置 › Agents › 具体 Agent」中配置。
    </p>
    <SecurityConfigPanel />
  </div>
</template>

<style scoped>
.settings-page__hint {
  margin: 0 0 14px;
  font-size: 12.5px;
  line-height: 1.45;
  color: var(--color-text-subtle);
}
</style>
