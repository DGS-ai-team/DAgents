<script setup>
import GeneralConfigPanel from "../../components/GeneralConfigPanel.vue";
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
    <GeneralConfigPanel />

    <section class="settings-section settings-section--standalone">
      <h2 class="settings-section__title">界面</h2>
      <label class="settings-toggle">
        <input v-model="showReasoning" type="checkbox" />
        <span>显示思考过程</span>
      </label>
      <p class="settings-section__desc">在对话中展示模型的思考内容。</p>
    </section>

    <StatusPanel embedded @close="() => {}" />
  </div>
</template>
