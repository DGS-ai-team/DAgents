<script setup>
import { ref } from "vue";
import LLMView from "./LLMView.vue";
import SkillsView from "./SkillsView.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const tab = ref("llm");
</script>

<template>
  <div class="subtabs" role="tablist">
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
  </div>

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
</template>

<style scoped>
.subtabs {
  display: flex;
  gap: 6px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border, #e5e7eb);
}
.subtab {
  appearance: none;
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  padding: 8px 14px;
  font-size: 14px;
  font-weight: 500;
  color: var(--muted, #6b7280);
  cursor: pointer;
}
.subtab:hover {
  color: var(--text, #111827);
}
.subtab.active {
  color: var(--accent, #6366f1);
  border-bottom-color: var(--accent, #6366f1);
}
</style>
