<script setup>
import SetupConfigPanel from "../../components/SetupConfigPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">连接</h1>
    <p class="settings-page__hint">配置 LLM、Manage、功能开关与 Browser；保存后写入 config.yaml。</p>
    <SetupConfigPanel />
  </div>
</template>
