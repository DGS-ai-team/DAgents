<script setup>
import { computed } from "vue";
import { statusStore, hasStatus } from "../stores/statusLines.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";

const props = defineProps({
  llmSettings: { type: Object, default: null },
  disabled: { type: Boolean, default: false },
});

const emit = defineEmits(["switch-profile"]);

const profiles = computed(() => {
  const list = props.llmSettings?.profiles;
  return Array.isArray(list) ? list.map((id) => String(id || "").trim()).filter(Boolean) : [];
});
const activeProfile = computed(() => String(props.llmSettings?.active_profile || "").trim());
const showProfileSwitch = computed(() => profiles.value.length > 0);
const activeLabel = computed(() => activeProfile.value || props.llmSettings?.model || "Auto");

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
        :disabled="disabled || profiles.length <= 1"
        :title="`${llmSettings?.provider || ''} / ${llmSettings?.model || activeLabel}`"
        @change="onProfileChange"
      >
        <option v-for="id in profiles" :key="id" :value="id">{{ id }}</option>
      </select>
    </label>
    <span
      v-else
      class="composer-toolbar__model"
      :title="`${llmSettings?.provider || ''} / ${llmSettings?.model || activeLabel}`"
    >
      {{ activeLabel }}
    </span>
  </div>
</template>
