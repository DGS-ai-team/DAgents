<script setup>
import { computed } from "vue";
import { statusPhaseLabel, statusStore } from "../stores/statusLines.js";
import ThinkingIndicator from "./ThinkingIndicator.vue";
import BrandActivityIndicator from "./BrandActivityIndicator.vue";

const props = defineProps({
  phase: { type: String, required: true },
  inline: { type: Boolean, default: false },
});

const state = computed(() => {
  void statusStore.tick;
  return statusStore.phases[props.phase];
});

const label = computed(() => statusPhaseLabel(props.phase));
</script>

<template>
  <component
    :is="inline ? 'span' : 'div'"
    v-if="state"
    :class="inline ? 'chat__turn-status' : 'msg msg--status'"
  >
    <span :class="inline ? 'chat__turn-status-body' : 'msg__body msg__body--hint-only'">
      <ThinkingIndicator v-if="phase === 'thinking'" :compact="inline" />
      <BrandActivityIndicator
        v-else
        :label="label"
        mode="generating"
        :show-label="true"
        :compact="inline"
      />
    </span>
  </component>
</template>

<style scoped>
.chat__turn-status {
  display: inline-flex;
  align-items: center;
  flex: 0 1 auto;
  min-width: 0;
  color: var(--color-text-subtle, var(--chat-text-muted, #8893a7));
}

.chat__turn-status-body {
  display: inline-flex;
  align-items: center;
  min-width: 0;
}

.chat__turn-status :deep(.brand-activity) {
  max-width: 100%;
  column-gap: 5px;
  font-size: 11.5px;
}

.chat__turn-status :deep(.brand-activity__frame) {
  flex-basis: 20px;
  width: 20px;
  height: 20px;
}

.chat__turn-status :deep(.brand-activity__mark) {
  width: 17px;
  height: 17px;
}

.chat__turn-status :deep(.brand-activity__label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
