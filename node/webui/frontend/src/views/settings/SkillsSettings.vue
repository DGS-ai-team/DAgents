<script setup>
import SkillsPanel from "../../components/SkillsPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">技能</h1>
    <SkillsPanel embedded @close="() => {}" />
  </div>
</template>
