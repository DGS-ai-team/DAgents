<script setup>
import ContextConfigPanel from "../../components/ContextConfigPanel.vue";
import ContextPanel from "../../components/ContextPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <ContextConfigPanel />

    <section class="settings-section settings-section--standalone settings-section--debug">
      <details class="context-settings__details">
        <summary class="context-settings__summary">
          <span>
            <strong>当前对话诊断</strong>
            <small>查看当前智能体的消息、Token、工具循环与回合状态</small>
          </span>
          <span class="context-settings__summary-action">展开</span>
        </summary>
        <div class="context-settings__content">
          <ContextPanel embedded />
        </div>
      </details>
    </section>
  </div>
</template>

<style scoped>
.context-settings__summary > span:first-child {
  min-width: 0;
}

.context-settings__summary-action,
.context-settings__summary::after {
  flex: 0 0 auto;
  white-space: nowrap;
}
</style>
