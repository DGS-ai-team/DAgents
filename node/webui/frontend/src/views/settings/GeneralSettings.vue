<script setup>
import RuntimeConfigPanel from "../../components/RuntimeConfigPanel.vue";
import ChildAgentsLimitsPanel from "../../components/ChildAgentsLimitsPanel.vue";
import ChildAgentsSection from "../../components/settings/ChildAgentsSection.vue";
import StatusPanel from "../../components/StatusPanel.vue";
import { computed } from "vue";
import { transcriptStore, setShowReasoning } from "../../stores/transcript.js";

const showReasoning = computed({
  get: () => transcriptStore.showReasoning,
  set: (value) => setShowReasoning(value),
});
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">通用</h1>
    <p class="settings-page__hint">运行时路径、Agent 身份与运行状态。Node 监听地址须改 config.yaml。</p>

    <RuntimeConfigPanel />
    <ChildAgentsLimitsPanel />
    <ChildAgentsSection />

    <section class="settings-section settings-embedded-panel panel">
      <h2 class="settings-section__title">界面</h2>
      <label class="settings-toggle">
        <input v-model="showReasoning" type="checkbox" />
        <span>显示思考过程</span>
      </label>
      <p class="settings-section__desc">
        开启后在对话中展示模型的 reasoning / thinking 流；是否启用思考模式请在输入框旁切换。
      </p>
    </section>

    <StatusPanel @close="() => {}" />
  </div>
</template>
