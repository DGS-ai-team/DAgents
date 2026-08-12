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
        LLM
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
.settings-view {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.settings-view :deep(.subtabs) {
  margin-bottom: 0;
}
</style>
