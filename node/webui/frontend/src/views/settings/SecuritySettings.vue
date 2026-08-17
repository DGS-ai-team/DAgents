<script setup>
import SecurityConfigPanel from "../../components/SecurityConfigPanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <header class="settings-page__header">
      <div class="settings-page__header-main">
        <h1 class="settings-page__title">输出防护</h1>
        <p class="settings-page__hint">
          编码、输出长度与重复调用提醒。工具 / Shell 审批请到
          <router-link to="/settings/agents">智能体</router-link>
          详情中配置。
        </p>
      </div>
    </header>
    <SecurityConfigPanel />
  </div>
</template>
