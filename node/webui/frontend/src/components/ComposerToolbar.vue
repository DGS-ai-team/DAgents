<script setup>
import { computed } from "vue";
import { statusStore, hasStatus } from "../stores/statusLines.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";

const props = defineProps({
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
  disabled: { type: Boolean, default: false },
});

const emit = defineEmits(["toggle-thinking", "cycle-effort"]);

const thinkingEnabled = computed(() => {
  const t = String(props.llmSettings?.thinking || "").toLowerCase();
  return !["disabled", "off"].includes(t);
});
const effort = computed(() => String(props.llmSettings?.reasoning_effort || "high").toLowerCase());

const prefillingActive = computed(() => {
  void statusStore.tick;
  return hasStatus("prefilling") && !hasStreamingTextContent();
});
const thinkingActive = computed(() => {
  void statusStore.tick;
  return hasStatus("thinking") && !hasStreamingKind("reasoning");
});
</script>

<template>
  <div class="composer-toolbar">
    <span v-if="prefillingActive" class="composer-toolbar__pulse composer-toolbar__pulse--prefill" title="prefilling" />
    <span v-if="thinkingActive" class="composer-toolbar__pulse composer-toolbar__pulse--think" title="thinking" />
    <button
      v-if="thinkingSupported"
      type="button"
      class="composer-toolbar__btn"
      :class="{ 'composer-toolbar__btn--active': thinkingEnabled }"
      :title="thinkingEnabled ? '思考模式已开启，点击关闭' : '思考模式已关闭，点击开启'"
      :disabled="disabled"
      @click="emit('toggle-thinking')"
    >
      <span class="composer-toolbar__icon" aria-hidden="true">◔</span>
      <span class="composer-toolbar__label">{{ thinkingEnabled ? "思考" : "思考关" }}</span>
    </button>
    <button
      v-if="thinkingSupported && thinkingEnabled"
      type="button"
      class="composer-toolbar__btn composer-toolbar__btn--secondary"
      :title="`推理强度 ${effort}，点击切换 high/max`"
      :disabled="disabled"
      @click="emit('cycle-effort')"
    >
      <span class="composer-toolbar__icon" aria-hidden="true">⤒</span>
      <span class="composer-toolbar__label">{{ effort }}</span>
    </button>
  </div>
</template>
