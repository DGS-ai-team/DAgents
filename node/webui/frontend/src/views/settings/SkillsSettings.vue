<script setup>
import SkillsPanel from "../../components/SkillsPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">技能</h1>
    <p class="settings-page__hint">为当前会话加载或卸载 Agent 技能。</p>
    <SkillsPanel @close="() => {}" />
  </div>
</template>
