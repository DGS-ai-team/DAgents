<script setup>
import ContextPanel from "../../components/ContextPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">上下文</h1>
    <p class="settings-page__hint">查看当前会话在 Node 中的完整 LLM 消息列表（与对话页 transcript 不同，此为实际上下文）。</p>
    <ContextPanel embedded />
  </div>
</template>
