<script setup>
import { ref } from "vue";
import LLMView from "./LLMView.vue";
import ReleasesView from "./ReleasesView.vue";
import CasesView from "./CasesView.vue";

defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const tab = ref("llm");

const TAB_HINTS = {
  llm: "多 Node 共用的 LLM 端点与模型配置",
  releases: "本地助手安装包版本发布与 latest 标记",
  cases: "演示会话与案例资源（次级入口）",
};
</script>

<template>
  <div class="settings-view">
    <nav class="subtabs" role="tablist" aria-label="配置">
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'llm' }"
        role="tab"
        :aria-selected="tab === 'llm'"
        @click="tab = 'llm'"
      >
        LLM 配置
      </button>
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'releases' }"
        role="tab"
        :aria-selected="tab === 'releases'"
        @click="tab = 'releases'"
      >
        版本发布
      </button>
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'cases' }"
        role="tab"
        :aria-selected="tab === 'cases'"
        @click="tab = 'cases'"
      >
        案例库
      </button>
    </nav>
    <p class="settings-hint muted">{{ TAB_HINTS[tab] }}</p>

    <LLMView
      v-if="tab === 'llm'"
      :active="active && tab === 'llm'"
      @toast="emit('toast', $event)"
    />
    <ReleasesView
      v-if="tab === 'releases'"
      :active="active && tab === 'releases'"
      @toast="emit('toast', $event)"
    />
    <CasesView
      v-if="tab === 'cases'"
      :active="active && tab === 'cases'"
      @toast="emit('toast', $event)"
    />
  </div>
</template>

<style scoped>
.settings-hint {
  margin: -8px 0 16px;
  font-size: 0.875rem;
}
</style>
