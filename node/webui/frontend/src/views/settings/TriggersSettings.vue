<script setup>
import TriggersPanel from "../../components/TriggersPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <header class="settings-page__header">
      <div class="settings-page__header-main">
        <h1 class="settings-page__title">定时任务</h1>
        <p class="settings-page__hint">
          进程级定时投递；轮询间隔在
          <router-link to="/settings/capabilities">能力 › 运行配额</router-link>
          。
        </p>
      </div>
    </header>
    <TriggersPanel embedded @close="() => {}" />
  </div>
</template>
