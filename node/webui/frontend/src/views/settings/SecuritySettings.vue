<script setup>
import SecurityConfigPanel from "../../components/SecurityConfigPanel.vue";
import PolicyPanel from "../../components/PolicyPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">安全</h1>
    <SecurityConfigPanel />
    <PolicyPanel embedded @close="() => {}" />
  </div>
</template>
