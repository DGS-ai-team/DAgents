<script setup>
import ContextConfigPanel from "../../components/ContextConfigPanel.vue";
import ContextPanel from "../../components/ContextPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">上下文</h1>
    <p class="settings-page__hint">上方为进程级压缩与钩子；下方为当前智能体消息的只读视图。</p>
    <ContextConfigPanel />

    <section class="settings-section settings-section--standalone settings-section--debug">
      <h2 class="settings-section__title">当前对话消息</h2>
      <p class="settings-section__desc">已发给模型的消息预览，一般无需日常调整。</p>
      <ContextPanel embedded />
    </section>
  </div>
</template>
