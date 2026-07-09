<script setup>
import PolicyPanel from "../../components/PolicyPanel.vue";
import ToolsConfigPanel from "../../components/ToolsConfigPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">安全</h1>
    <p class="settings-page__hint">工具组、编码与审批策略。</p>
    <ToolsConfigPanel />
    <PolicyPanel @close="() => {}" />
  </div>
</template>
