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
</script>

<template>
  <div v-if="state" class="msg msg--status">
    <div class="msg__body msg__body--hint-only">
      <div class="msg__hint msg__hint--stream-meta msg__hint--dots-only" :aria-label="label">
        <span class="msg__meta-dots" aria-hidden="true">
          <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
        </span>
      </div>
    </div>
  </div>
</template>
