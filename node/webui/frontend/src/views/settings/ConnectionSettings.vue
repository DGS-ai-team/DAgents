<script setup>
import SetupConfigPanel from "../../components/SetupConfigPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">连接</h1>
    <p class="settings-page__hint">管理 LLM 配置、Manage 与功能开关。LLM Key 加密存入本地 SQLite。</p>
    <SetupConfigPanel />
  </div>
</template>
