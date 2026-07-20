<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { statusStore, hasStatus } from "../stores/statusLines.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";

const props = defineProps({
  llmSettings: { type: Object, default: null },
  disabled: { type: Boolean, default: false },
});

const emit = defineEmits(["switch-profile"]);

const open = ref(false);
const rootRef = ref(null);
const menuRef = ref(null);

const profiles = computed(() => {
  const list = props.llmSettings?.profiles;
  return Array.isArray(list) ? list.map((id) => String(id || "").trim()).filter(Boolean) : [];
});
const activeProfile = computed(() => String(props.llmSettings?.active_profile || "").trim());
const showProfileSwitch = computed(() => profiles.value.length > 0);
const canSwitch = computed(() => !props.disabled && profiles.value.length > 1);
const activeLabel = computed(() => activeProfile.value || props.llmSettings?.model || "Auto");
const titleText = computed(() => {
  const provider = String(props.llmSettings?.provider || "").trim();
  const model = String(props.llmSettings?.model || activeLabel.value).trim();
  if (provider && model) return `${provider} / ${model}`;
  return model || provider || activeLabel.value;
});

const prefillingActive = computed(() => {
  void statusStore.tick;
  return hasStatus("prefilling") && !hasStreamingTextContent();
});
const thinkingActive = computed(() => {
  void statusStore.tick;
  return hasStatus("thinking") && !hasStreamingKind("reasoning");
});

function closeMenu() {
  open.value = false;
}

function toggleMenu() {
  if (!canSwitch.value) return;
  open.value = !open.value;
  if (open.value) {
    nextTick(() => menuRef.value?.querySelector('[aria-selected="true"]')?.focus?.());
  }
}

function pickProfile(id) {
  closeMenu();
  if (!id || id === activeProfile.value) return;
  emit("switch-profile", id);
}

function onDocPointerDown(event) {
  if (!open.value) return;
  const root = rootRef.value;
  if (root && !root.contains(event.target)) closeMenu();
}

function onKeydown(event) {
  if (!open.value) return;
  if (event.key === "Escape") {
    event.preventDefault();
    closeMenu();
  }
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocPointerDown, true);
  document.addEventListener("keydown", onKeydown, true);
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocPointerDown, true);
  document.removeEventListener("keydown", onKeydown, true);
});
</script>

<template>
  <div class="composer-toolbar">
    <span v-if="prefillingActive" class="composer-toolbar__pulse composer-toolbar__pulse--prefill" title="prefilling" />
    <span v-if="thinkingActive" class="composer-toolbar__pulse composer-toolbar__pulse--think" title="thinking" />

    <div v-if="showProfileSwitch" ref="rootRef" class="composer-toolbar__profile">
      <button
        type="button"
        class="composer-toolbar__trigger"
        :class="{ 'composer-toolbar__trigger--open': open }"
        :disabled="!canSwitch"
        :title="titleText"
        :aria-expanded="open"
        aria-haspopup="listbox"
        @click="toggleMenu"
      >
        <span class="composer-toolbar__trigger-label">{{ activeLabel }}</span>
        <svg
          v-if="canSwitch"
          class="composer-toolbar__chevron"
          viewBox="0 0 12 12"
          width="12"
          height="12"
          aria-hidden="true"
        >
          <path
            d="M3 4.5L6 7.5L9 4.5"
            fill="none"
            stroke="currentColor"
            stroke-width="1.4"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>

      <div
        v-if="open"
        ref="menuRef"
        class="composer-toolbar__menu"
        role="listbox"
        :aria-label="'选择 LLM 配置'"
      >
        <button
          v-for="id in profiles"
          :key="id"
          type="button"
          class="composer-toolbar__option"
          role="option"
          :aria-selected="id === activeProfile"
          :class="{ 'composer-toolbar__option--active': id === activeProfile }"
          @click="pickProfile(id)"
        >
          <span class="composer-toolbar__option-label">{{ id }}</span>
          <span v-if="id === activeProfile" class="composer-toolbar__option-check" aria-hidden="true">✓</span>
        </button>
      </div>
    </div>

    <span
      v-else
      class="composer-toolbar__model"
      :title="titleText"
    >
      {{ activeLabel }}
    </span>
  </div>
</template>
