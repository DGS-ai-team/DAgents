<script setup>
import ContextConfigPanel from "../../components/ContextConfigPanel.vue";
import ContextPanel from "../../components/ContextPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="上下文"
      eyebrow="系统设置"
      description="管理上下文压缩策略；当前智能体的消息与运行诊断默认收起，需要排查时再展开。"
    />
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
