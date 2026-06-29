<script setup>
import { ref } from "vue";
import LLMView from "./LLMView.vue";
import SkillsView from "./SkillsView.vue";
import ReleasesView from "./ReleasesView.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const tab = ref("llm");

const TAB_HINTS = {
  llm: "多 Node 共用的 LLM 端点与模型配置",
  skills: "Skill 包上传、草稿发布与目录分发",
  releases: "本地助手安装包版本发布与 latest 标记",
};
</script>

<template>
  <div class="node-admin">
    <nav class="subtabs" role="tablist" aria-label="Node 配置">
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
        :class="{ active: tab === 'skills' }"
        role="tab"
        :aria-selected="tab === 'skills'"
        @click="tab = 'skills'"
      >
        Skills
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
    </nav>
    <p class="node-admin-hint muted">{{ TAB_HINTS[tab] }}</p>

    <LLMView
      v-if="tab === 'llm'"
      :active="active && tab === 'llm'"
      @toast="emit('toast', $event)"
    />
    <SkillsView
      v-if="tab === 'skills'"
      :active="active && tab === 'skills'"
      @toast="emit('toast', $event)"
    />
    <ReleasesView
      v-if="tab === 'releases'"
      :active="active && tab === 'releases'"
      @toast="emit('toast', $event)"
    />
  </div>
</template>

<style scoped>
.node-admin-hint {
  margin: -8px 0 16px;
  font-size: 0.875rem;
}
</style>
