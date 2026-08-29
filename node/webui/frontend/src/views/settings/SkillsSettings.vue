<script setup>
import SkillsPanel from "../../components/SkillsPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="技能"
      eyebrow="运行时设置"
      description="查看当前会话已启用的技能与可发现的技能目录。模型会按需加载技能内容。"
    />
    <SkillsPanel embedded @close="() => {}" />
  </div>
</template>
