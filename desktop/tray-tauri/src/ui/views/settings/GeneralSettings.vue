<script setup>
import GeneralConfigPanel from "../../components/GeneralConfigPanel.vue";
import StatusPanel from "../../components/StatusPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <GeneralConfigPanel />
    <StatusPanel embedded @close="() => {}" />
  </div>
</template>
