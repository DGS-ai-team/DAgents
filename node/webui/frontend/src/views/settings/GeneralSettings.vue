<script setup>
import GeneralConfigPanel from "../../components/GeneralConfigPanel.vue";
import StatusPanel from "../../components/StatusPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">通用</h1>
    <GeneralConfigPanel />
    <StatusPanel embedded @close="() => {}" />
  </div>
</template>
