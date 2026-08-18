<script setup>
import GeneralConfigPanel from "../../components/GeneralConfigPanel.vue";
import StatusPanel from "../../components/StatusPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <header class="settings-page__header">
      <div class="settings-page__header-main">
        <h1 class="settings-page__title">通用</h1>
      </div>
    </header>
    <GeneralConfigPanel />
    <StatusPanel embedded @close="() => {}" />
  </div>
</template>
