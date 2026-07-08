<script setup>
import PolicyPanel from "../../components/PolicyPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">安全</h1>
    <p class="settings-page__hint">控制工具与 Shell 命令的审批策略。</p>
    <PolicyPanel @close="() => {}" />
  </div>
</template>
