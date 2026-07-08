<script setup>
import { computed } from "vue";
import StatusPanel from "../../components/StatusPanel.vue";
import ChildAgentsSection from "../../components/settings/ChildAgentsSection.vue";
import { transcriptStore, setShowReasoning } from "../../stores/transcript.js";

const showReasoning = computed({
  get: () => transcriptStore.showReasoning,
  set: (value) => setShowReasoning(value),
});
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">通用</h1>
    <p class="settings-page__hint">模型与助手运行信息。思考模式可在对话输入框旁切换。</p>

    <ChildAgentsSection />

    <section class="settings-section">
      <h2 class="settings-section__title">高级</h2>
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
