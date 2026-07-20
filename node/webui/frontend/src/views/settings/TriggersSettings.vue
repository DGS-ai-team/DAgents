<script setup>
import TriggersPanel from "../../components/TriggersPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">定时任务</h1>
    <TriggersPanel embedded @close="() => {}" />
  </div>
</template>
