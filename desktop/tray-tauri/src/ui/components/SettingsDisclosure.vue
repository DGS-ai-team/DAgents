<script setup>
import { computed, nextTick, ref, useId } from "vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  title: { type: String, required: true },
  initiallyOpen: { type: Boolean, default: false },
  expanded: { type: Boolean, default: undefined },
});
const emit = defineEmits(["update:expanded"]);

const localOpen = ref(props.initiallyOpen);
const isOpen = computed(() => props.expanded === undefined ? localOpen.value : props.expanded);
const trigger = ref(null);
const contentId = `settings-disclosure-${useId()}`;

function setOpen(open) {
  if (props.expanded === undefined) localOpen.value = open;
  emit("update:expanded", open);
}

function toggle() {
  if (isOpen.value && trigger.value?.parentElement?.contains(document.activeElement)) {
    trigger.value.focus();
  }
  setOpen(!isOpen.value);
}

async function open() {
  setOpen(true);
  await nextTick();
}

defineExpose({ open });
</script>

<template>
  <div class="settings-disclosure" :class="{ 'settings-disclosure--open': isOpen }">
    <div
      :id="`${contentId}-trigger`"
      ref="trigger"
      role="button"
      tabindex="0"
      class="settings-disclosure__trigger"
      :aria-expanded="isOpen"
      :aria-controls="contentId"
      @click="toggle"
      @keydown.enter.prevent="toggle"
      @keydown.space.prevent="toggle"
    >
      <span class="settings-disclosure__title">{{ title }}</span>
      <UiIcon class="settings-disclosure__chevron" name="chevron-down" :size="16" aria-hidden="true" />
    </div>
    <div
      :id="contentId"
      class="settings-disclosure__content"
      role="region"
      :aria-labelledby="`${contentId}-trigger`"
      :aria-hidden="!isOpen"
      :inert="isOpen ? undefined : ''"
    >
      <div class="settings-disclosure__content-inner">
        <div class="settings-disclosure__content-padding"><slot /></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-disclosure {
  min-width: 0;
  border-top: 1px solid var(--border-subtle);
}

.settings-disclosure__trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  width: 100%;
  min-height: 52px;
  padding: 8px 2px;
  border: 0;
  border-radius: 0;
  background: transparent;
  color: var(--color-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.settings-disclosure__trigger:hover {
  border-radius: 6px;
  background: color-mix(in srgb, var(--color-text) 4%, transparent);
}

.settings-disclosure__trigger:focus-visible {
  outline: 2px solid var(--color-primary-strong);
  outline-offset: 2px;
}

.settings-disclosure__title {
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
}

.settings-disclosure__chevron {
  flex: 0 0 auto;
  color: var(--color-text-subtle);
  transition: transform 200ms ease;
}

.settings-disclosure--open .settings-disclosure__chevron {
  transform: rotate(180deg);
}

.settings-disclosure__content {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  pointer-events: none;
  transition: grid-template-rows 200ms ease, opacity 200ms ease;
}

.settings-disclosure--open .settings-disclosure__content {
  grid-template-rows: 1fr;
  opacity: 1;
  pointer-events: auto;
}

.settings-disclosure__content-inner {
  min-height: 0;
  overflow: hidden;
}

.settings-disclosure__content-padding {
  padding: 0 2px 14px;
}

@media (prefers-reduced-motion: reduce) {
  .settings-disclosure__chevron,
  .settings-disclosure__content {
    transition-duration: 1ms;
  }
}

@media (max-width: 480px) {
  .settings-disclosure__trigger { gap: 8px; }
}
</style>
