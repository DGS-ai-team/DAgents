<script setup>
defineProps({
  title: { type: String, required: true },
  eyebrow: { type: String, default: "设置" },
  description: { type: String, default: "" },
});
</script>

<template>
  <header class="settings-page__header">
    <div class="settings-page__header-main">
      <span v-if="eyebrow" class="settings-page__eyebrow">{{ eyebrow }}</span>
      <h1 class="settings-page__title">{{ title }}</h1>
      <p v-if="description" class="settings-page__desc">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="settings-page__header-actions">
      <slot name="actions" />
    </div>
  </header>
</template>
