<script setup>
import { computed } from "vue";
import { statusPhaseLabel, statusStore } from "../stores/statusLines.js";
import ThinkingIndicator from "./ThinkingIndicator.vue";
import BrandActivityIndicator from "./BrandActivityIndicator.vue";

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
      <ThinkingIndicator v-if="phase === 'thinking'" />
      <BrandActivityIndicator
        v-else
        :label="label"
        mode="generating"
        :show-label="false"
      />
    </div>
  </div>
</template>
