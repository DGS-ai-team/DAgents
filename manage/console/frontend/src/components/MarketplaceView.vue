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
        Hook
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
/* layout 由 main.css .marketplace-view 负责 */
</style>
