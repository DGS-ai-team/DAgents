<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  llmSettings: { type: Object, default: null },
  settingsProfileId: { type: String, default: "" },
  disabled: { type: Boolean, default: false },
  desktopCompact: { type: Boolean, default: false },
  statusLabel: { type: String, default: "" },
});

const emit = defineEmits(["switch-profile", "open-model-settings"]);

const open = ref(false);
const rootRef = ref(null);
const menuRef = ref(null);

const profiles = computed(() => {
  const list = props.llmSettings?.profiles;
  return Array.isArray(list) ? list.map((id) => String(id || "").trim()).filter(Boolean) : [];
});
const activeProfile = computed(() => String(props.llmSettings?.active_profile || props.settingsProfileId || "").trim());
const showProfileSwitch = computed(() => profiles.value.length > 0 || Boolean(activeProfile.value));
const canSwitch = computed(() => !props.disabled && showProfileSwitch.value);
const hasProfileChoice = computed(() => profiles.value.length > 1);
const canOpenSettings = computed(() => Boolean(activeProfile.value));
const activeLabel = computed(() => activeProfile.value || props.llmSettings?.model || "Auto");
const displayLabel = computed(() => props.desktopCompact
  ? String(props.llmSettings?.model || "Auto").trim()
  : activeLabel.value);
const titleText = computed(() => {
  const provider = String(props.llmSettings?.provider || "").trim();
  const model = String(props.llmSettings?.model || displayLabel.value).trim();
  const profile = props.desktopCompact && activeProfile.value ? `配置: ${activeProfile.value}` : "";
  const providerModel = provider && model ? `${provider} / ${model}` : model || provider || displayLabel.value;
  return profile ? `${providerModel} · ${profile}` : providerModel;
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
  if (!canSwitch.value || !id || id === activeProfile.value) return;
  emit("switch-profile", id);
}

function openModelSettings() {
  closeMenu();
  if (!props.disabled && canOpenSettings.value) emit("open-model-settings", activeProfile.value);
}

watch(canSwitch, (available) => {
  if (!available) closeMenu();
});

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
    <span
      class="composer-toolbar__status"
      role="status"
      aria-live="polite"
      aria-atomic="true"
      :title="statusLabel || undefined"
    >
      <span class="visually-hidden">{{ statusLabel }}</span>
      <span class="composer-toolbar__status-window" aria-hidden="true">
        <Transition name="composer-status" appear>
          <span v-if="statusLabel" :key="statusLabel" class="composer-toolbar__status-line">
            <span class="composer-toolbar__status-text">{{ statusLabel }}</span>
          </span>
        </Transition>
      </span>
    </span>

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
        <span class="composer-toolbar__trigger-label">{{ displayLabel }}</span>
        <UiIcon v-if="hasProfileChoice || canOpenSettings" class="composer-toolbar__chevron" name="chevron-down" :size="14" />
      </button>

      <div
        v-if="open"
        ref="menuRef"
        class="composer-toolbar__menu"
      >
        <div role="listbox" aria-label="选择模型配置">
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
            <UiIcon v-if="id === activeProfile" class="composer-toolbar__option-check" name="check" :size="14" />
          </button>
        </div>
        <button v-if="canOpenSettings" type="button" class="composer-toolbar__option composer-toolbar__settings-action" @click="openModelSettings">
          模型设置…
        </button>
      </div>
    </div>

    <span
      v-else
      class="composer-toolbar__model"
      :title="titleText"
    >
      {{ displayLabel }}
    </span>
  </div>
</template>
