<script setup>
import { computed } from "vue";
import { statusStore, formatStatusText } from "../stores/statusLines.js";

const props = defineProps({
  phase: { type: String, required: true },
  state: { type: Object, required: true },
});

const text = computed(() => formatStatusText(props.phase, props.state));
const done = computed(() => !!props.state.done);
</script>

<template>
  <div class="msg msg--tool-centered">
    <div class="msg__body msg__body--wide">
      <div class="msg__hint msg__hint--stream-meta">
        <span class="msg__meta-label">{{ phase }}</span>
        <span v-if="!done" class="msg__meta-dots" aria-hidden="true">
          <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
        </span>
        <span v-else class="msg__meta-done">done</span>
      </div>
      <div class="msg__bubble msg__bubble--tool-centered status-line-bubble">{{ text }}</div>
    </div>
  </div>
</template>

<style scoped>
.status-line-bubble {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--color-text-muted);
}
</style>
