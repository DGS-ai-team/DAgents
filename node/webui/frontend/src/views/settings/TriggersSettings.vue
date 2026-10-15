<script setup>
import TriggersPanel from "../../components/TriggersPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="定时任务"
      description="按固定间隔向指定智能体投递任务；轮询间隔在能力 › 运行配额中统一设置。"
    />
    <TriggersPanel embedded @close="() => {}" />
  </div>
</template>
