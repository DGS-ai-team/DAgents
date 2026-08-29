<script setup>
import CapabilitiesConfigPanel from "../../components/CapabilitiesConfigPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="能力"
      eyebrow="运行时设置"
      description="管理共享服务、运行配额与子 Agent 限制。"
    />
    <CapabilitiesConfigPanel />
  </div>
</template>
