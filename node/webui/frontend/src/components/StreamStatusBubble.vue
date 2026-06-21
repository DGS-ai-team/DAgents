<script setup>
import { computed } from "vue";
import { statusPhaseLabel, statusStore } from "../stores/statusLines.js";

const props = defineProps({
  phase: { type: String, required: true },
});

const state = computed(() => {
  void statusStore.tick;
  return statusStore.phases[props.phase];
});

const label = computed(() => statusPhaseLabel(props.phase));

const elapsed = computed(() => {
  void statusStore.tick;
  if (!state.value) return 0;
  return Math.max(0, Math.floor((statusStore.tick - state.value.startedAt) / 1000));
});
</script>

<template>
  <div v-if="state" class="msg msg--status">
    <div class="msg__body msg__body--hint-only">
      <div class="msg__hint msg__hint--stream-meta">
        <span class="msg__meta-label">{{ label }}</span>
        <span v-if="!state.done" class="msg__meta-dots" aria-hidden="true">
          <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
        </span>
        <span v-else class="msg__meta-done">done · {{ elapsed }}s</span>
        <span v-if="!state.done" class="msg__meta-elapsed">{{ elapsed }}s</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.msg__meta-elapsed {
  margin-left: 6px;
  color: var(--color-text-subtle);
  font-variant-numeric: tabular-nums;
}
</style>
