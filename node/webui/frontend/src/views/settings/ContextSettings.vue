<script setup>
import ContextConfigPanel from "../../components/ContextConfigPanel.vue";
import ContextPanel from "../../components/ContextPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">上下文</h1>
    <ContextConfigPanel />

    <section class="settings-section settings-section--standalone settings-section--debug">
      <h2 class="settings-section__title">当前对话消息</h2>
      <p class="settings-section__desc">用于查看本 Agent 已发给模型的消息，一般无需日常调整。</p>
      <ContextPanel embedded />
    </section>
  </div>
</template>
