<script setup>
import CompressionConfigPanel from "../../components/CompressionConfigPanel.vue";
import HooksConfigPanel from "../../components/HooksConfigPanel.vue";
import ContextPanel from "../../components/ContextPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">上下文</h1>
    <p class="settings-page__hint">压缩阈值、内置 Hook，以及当前会话 LLM 消息列表。</p>
    <CompressionConfigPanel />
    <HooksConfigPanel />
    <ContextPanel embedded />
  </div>
</template>
