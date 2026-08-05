<script setup>
import { ref } from "vue";
import LLMView from "./LLMView.vue";
import SkillsView from "./SkillsView.vue";
import ExternalToolsView from "./ExternalToolsView.vue";
import PluginsView from "./PluginsView.vue";
import ReleasesView from "./ReleasesView.vue";

defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const tab = ref("llm");
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
        LLM
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
        :class="{ active: tab === 'externaltools' }"
        role="tab"
        :aria-selected="tab === 'externaltools'"
        @click="tab = 'externaltools'"
      >
        External Tools
      </button>
      <button
        type="button"
        class="subtab"
        :class="{ active: tab === 'plugins' }"
        role="tab"
        :aria-selected="tab === 'plugins'"
        @click="tab = 'plugins'"
      >
        Plugins
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
    <ExternalToolsView
      v-if="tab === 'externaltools'"
      :active="active && tab === 'externaltools'"
      @toast="emit('toast', $event)"
    />
    <PluginsView
      v-if="tab === 'plugins'"
      :active="active && tab === 'plugins'"
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
.node-admin {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.node-admin :deep(.subtabs) {
  margin-bottom: 0;
}
</style>
