<script setup>
import SecurityConfigPanel from "../../components/SecurityConfigPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="输出防护"
      eyebrow="系统设置"
      description="控制编码、命令输出长度与重复调用提醒；工具 / Shell 审批在智能体详情中单独配置。"
    />
    <SecurityConfigPanel />
  </div>
</template>
