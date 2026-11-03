<script setup>
import { computed, ref } from "vue";
import SkillsView from "./SkillsView.vue";
import PluginsView from "./PluginsView.vue";
import ExternalToolsView from "./ExternalToolsView.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
  canManage: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const tab = ref("skills");
const directoryNotice = computed(() => props.canManage
  ? "此处管理 Manage 发布目录；发布不代表软件包已下发或安装到 Node。"
  : "此处浏览 Manage 已发布目录；安装与启用需在 Node 中另行配置。");
</script>

<template>
  <div class="marketplace-view">
    <p class="marketplace-view__notice" role="note">
      {{ directoryNotice }}
    </p>
    <nav class="subtabs" role="tablist" aria-label="能力类型">
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'skills' }"
        role="tab"
        :aria-selected="tab === 'skills'"
        @click="tab = 'skills'"
      >
        Skill
      </button>
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'hooks' }"
        role="tab"
        :aria-selected="tab === 'hooks'"
        @click="tab = 'hooks'"
      >
        Hook 插件
      </button>
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'tools' }"
        role="tab"
        :aria-selected="tab === 'tools'"
        @click="tab = 'tools'"
      >
        外置工具
      </button>
    </nav>

    <SkillsView
      v-if="tab === 'skills'"
      :active="active && tab === 'skills'"
      :can-manage="canManage"
      @toast="emit('toast', $event)"
    />
    <PluginsView
      v-if="tab === 'hooks'"
      :active="active && tab === 'hooks'"
      :can-manage="canManage"
      @toast="emit('toast', $event)"
    />
    <ExternalToolsView
      v-if="tab === 'tools'"
      :active="active && tab === 'tools'"
      :can-manage="canManage"
      @toast="emit('toast', $event)"
    />
  </div>
</template>

<style scoped>
/* layout 由 main.css .marketplace-view 负责 */
</style>
