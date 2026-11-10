<script setup>
import ContextConfigPanel from "../../components/ContextConfigPanel.vue";
import ContextPanel from "../../components/ContextPanel.vue";
import SettingsDisclosure from "../../components/SettingsDisclosure.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <ContextConfigPanel />

    <section class="settings-section settings-section--standalone settings-section--debug">
      <SettingsDisclosure title="当前对话诊断">
        <p class="settings-section__desc">查看当前智能体的消息、Token、工具循环与回合状态。</p>
        <ContextPanel embedded />
      </SettingsDisclosure>
    </section>
  </div>
</template>
