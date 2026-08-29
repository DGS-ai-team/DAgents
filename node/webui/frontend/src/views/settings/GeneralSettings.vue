<script setup>
import GeneralConfigPanel from "../../components/GeneralConfigPanel.vue";
import StatusPanel from "../../components/StatusPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="通用"
      eyebrow="工作区设置"
      description="管理 Node 的身份、日志与当前运行状态。"
    />
    <GeneralConfigPanel />
    <StatusPanel embedded @close="() => {}" />
  </div>
</template>
