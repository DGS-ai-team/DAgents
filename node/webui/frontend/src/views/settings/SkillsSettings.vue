<script setup>
import SkillsPanel from "../../components/SkillsPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="技能"
      eyebrow="全局工具目录"
      description="管理 Node 可发现的技能元数据；具体 Agent 的可见范围在智能体设置中绑定。"
    />
    <SkillsPanel embedded />
  </div>
</template>
