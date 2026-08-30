<script setup>
import GeneralConfigPanel from "../../components/GeneralConfigPanel.vue";
import StatusPanel from "../../components/StatusPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="通用"
      eyebrow="本机设置"
      description="管理本机节点的显示信息、日志和运行状态。"
    />
    <GeneralConfigPanel />
    <StatusPanel embedded @close="() => {}" />
  </div>
</template>
