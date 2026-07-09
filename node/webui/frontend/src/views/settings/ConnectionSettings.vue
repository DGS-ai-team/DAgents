<script setup>
import SetupConfigPanel from "../../components/SetupConfigPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">连接</h1>
    <p class="settings-page__hint">配置 LLM、Manage 与功能开关；保存后写入 config.yaml。</p>
    <SetupConfigPanel />
  </div>
</template>
