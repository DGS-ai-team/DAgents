<script setup>
import { computed } from "vue";
import { statusStore, hasStatus } from "../stores/statusLines.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";

const props = defineProps({
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
  disabled: { type: Boolean, default: false },
});

const emit = defineEmits(["toggle-thinking", "cycle-effort", "switch-profile"]);

const thinkingEnabled = computed(() => {
  const t = String(props.llmSettings?.thinking || "").toLowerCase();
  return !["disabled", "off"].includes(t);
});
const effort = computed(() => String(props.llmSettings?.reasoning_effort || "high").toLowerCase());

const profiles = computed(() => {
  const list = props.llmSettings?.profiles;
  return Array.isArray(list) ? list : [];
});
const activeProfile = computed(() => String(props.llmSettings?.active_profile || "").trim());
const showProfileSwitch = computed(() => profiles.value.length > 1);

const prefillingActive = computed(() => {
  void statusStore.tick;
  return hasStatus("prefilling") && !hasStreamingTextContent();
});
const thinkingActive = computed(() => {
  void statusStore.tick;
  return hasStatus("thinking") && !hasStreamingKind("reasoning");
});

function onProfileChange(event) {
  const id = event?.target?.value;
  if (id) emit("switch-profile", id);
}
</script>

<template>
  <div class="composer-toolbar">
    <span v-if="prefillingActive" class="composer-toolbar__pulse composer-toolbar__pulse--prefill" title="prefilling" />
    <span v-if="thinkingActive" class="composer-toolbar__pulse composer-toolbar__pulse--think" title="thinking" />
    <label v-if="showProfileSwitch" class="composer-toolbar__profile">
      <select
        class="composer-toolbar__select"
        :value="activeProfile"
        :disabled="disabled"
        :title="`${llmSettings?.provider || ''} / ${llmSettings?.model || ''}`"
        @change="onProfileChange"
      >
        <option v-for="id in profiles" :key="id" :value="id">{{ id }}</option>
      </select>
    </label>
    <span
      v-else
      class="composer-toolbar__model"
      :title="`${llmSettings?.provider || ''} / ${llmSettings?.model || activeProfile || 'model'}`"
    >
      {{ llmSettings?.model || activeProfile || "Auto" }}
    </span>
    <button
      v-if="thinkingSupported"
      type="button"
      class="composer-toolbar__btn"
      :class="{ 'composer-toolbar__btn--active': thinkingEnabled }"
      :title="thinkingEnabled ? '思考模式已开启，点击关闭' : '思考模式已关闭，点击开启'"
      :disabled="disabled"
      @click="emit('toggle-thinking')"
    >
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
      <span class="composer-toolbar__label">{{ effort }}</span>
    </button>
  </div>
</template>
