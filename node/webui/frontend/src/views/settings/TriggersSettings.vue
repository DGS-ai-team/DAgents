<script setup>
import TriggersPanel from "../../components/TriggersPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">定时任务</h1>
    <p class="settings-page__hint">查看与管理自动触发的任务（编辑与启停将在后续版本完善）。</p>
    <TriggersPanel @close="() => {}" />
  </div>
</template>
