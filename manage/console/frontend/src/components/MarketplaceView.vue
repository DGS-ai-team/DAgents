<script setup>
import { ref } from "vue";
import SkillsView from "./SkillsView.vue";
import PluginsView from "./PluginsView.vue";
import ExternalToolsView from "./ExternalToolsView.vue";

defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const tab = ref("skills");

const TAB_HINTS = {
  skills: "Skill 包上传、草稿发布与目录分发；Node 可同步安装",
  hooks: "Hook Plugin（.so）上传、发布与目录分发",
  tools: "外置 CLI / 二进制上传、发布与目录分发（.runtime/externaltools/）",
};
</script>

<template>
  <div class="marketplace-view">
    <nav class="subtabs" role="tablist" aria-label="能力类型">
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
        :class="{ active: tab === 'hooks' }"
        role="tab"
        :aria-selected="tab === 'hooks'"
        @click="tab = 'hooks'"
      >
        Hooks
      </button>
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'tools' }"
        role="tab"
        :aria-selected="tab === 'tools'"
        @click="tab = 'tools'"
      >
        External Tools
      </button>
    </nav>
    <p class="marketplace-hint muted">{{ TAB_HINTS[tab] }}</p>

    <SkillsView
      v-if="tab === 'skills'"
      :active="active && tab === 'skills'"
      @toast="emit('toast', $event)"
    />
    <PluginsView
      v-if="tab === 'hooks'"
      :active="active && tab === 'hooks'"
      @toast="emit('toast', $event)"
    />
    <ExternalToolsView
      v-if="tab === 'tools'"
      :active="active && tab === 'tools'"
      @toast="emit('toast', $event)"
    />
  </div>
</template>

<style scoped>
.marketplace-hint {
  margin: -8px 0 16px;
  font-size: 0.875rem;
}
</style>
