<script setup>
import CompressionConfigPanel from "../../components/CompressionConfigPanel.vue";
import ContextPanel from "../../components/ContextPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">上下文</h1>
    <p class="settings-page__hint">配置压缩阈值，并查看当前会话在 Node 中的完整 LLM 消息列表。</p>
    <CompressionConfigPanel />
    <ContextPanel embedded />
  </div>
</template>
