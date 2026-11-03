<script setup>
import { computed } from "vue";

const props = defineProps({
  title: { type: String, required: true },
  description: { type: String, default: "" },
  level: { type: Number, default: 2, validator: (value) => [2, 3].includes(value) },
});

const headingTag = computed(() => `h${props.level}`);
</script>

<template>
  <header class="settings-section-header">
    <div class="settings-section-header__copy">
      <component :is="headingTag" class="settings-section-header__title">{{ title }}</component>
      <p v-if="description" class="settings-section-header__description">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="settings-section-header__actions">
      <slot name="actions" />
    </div>
  </header>
</template>

<style scoped>
.settings-section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}

.settings-section-header__copy {
  min-width: 0;
}

.settings-section-header__title {
  margin: 0;
  color: var(--color-text);
  font-size: 16px;
  font-weight: 600;
  line-height: 1.4;
}

.settings-section-header__title:is(h3) {
  font-size: 15px;
  line-height: 1.4;
}

.settings-section-header__description {
  margin: 4px 0 0;
  color: var(--color-text-muted);
  font-size: 13px;
  line-height: 1.5;
}

.settings-section-header__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex: 0 0 auto;
  gap: 8px;
}

</style>
