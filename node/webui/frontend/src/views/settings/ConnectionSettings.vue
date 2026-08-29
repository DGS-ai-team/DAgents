<script setup>
import SetupConfigPanel from "../../components/SetupConfigPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="模型与连接"
      eyebrow="工作区设置"
      description="管理模型配置、默认模型和 Manage 连接。"
    />
    <SetupConfigPanel />
  </div>
</template>
