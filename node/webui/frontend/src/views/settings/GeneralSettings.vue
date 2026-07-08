<script setup>
import StatusPanel from "../../components/StatusPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">通用</h1>
    <p class="settings-page__hint">模型与助手运行信息。思考模式可在对话输入框旁切换。</p>
    <StatusPanel @close="() => {}" />
  </div>
</template>
