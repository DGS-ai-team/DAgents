<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { statusPhaseLabel, statusPhaseOrder, hasStatus } from "../stores/statusLines.js";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  llmSettings: { type: Object, default: null },
  disabled: { type: Boolean, default: false },
  desktopCompact: { type: Boolean, default: false },
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

const statusShortLabels = {
  queued: "排队中",
  model_generating: "生成中",
  prefilling: "准备中",
  thinking: "思考中",
  assistant_generating: "回复中",
  tool_executing: "工具中",
  tool_waiting: "待审批",
  waiting_user: "待输入",
  compression: "压缩中",
};

const activeStatus = computed(() => {
  const phase = ["compression", ...statusPhaseOrder].find(hasStatus);
  return phase ? { phase, label: statusShortLabels[phase] || statusPhaseLabel(phase) } : null;
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
    <span
      v-if="activeStatus"
      class="composer-toolbar__status"
      role="status"
      aria-live="polite"
      :title="statusPhaseLabel(activeStatus.phase)"
    >
      <span class="composer-toolbar__status-dot" aria-hidden="true" />
      <span>{{ activeStatus.label }}</span>
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
        <UiIcon v-if="canSwitch" class="composer-toolbar__chevron" name="chevron-down" :size="14" />
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
          <UiIcon v-if="id === activeProfile" class="composer-toolbar__option-check" name="check" :size="14" />
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
