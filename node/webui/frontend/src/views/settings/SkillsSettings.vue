<script setup>
import SkillsPanel from "../../components/SkillsPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <header class="settings-page__header">
      <div class="settings-page__header-main">
        <h1 class="settings-page__title">技能</h1>
        <p class="settings-page__hint">
          对应当前对话中的智能体；进程级注入上限在
          <router-link to="/settings/capabilities">能力 › 运行配额</router-link>
          。
        </p>
      </div>
    </header>
    <SkillsPanel embedded @close="() => {}" />
  </div>
</template>
