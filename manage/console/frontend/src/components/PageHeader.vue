<script setup>
defineProps({
  title: { type: String, required: true },
  trail: { type: String, default: "" },
  subtitle: { type: String, default: "" },
});
</script>

<template>
  <header class="page-header">
    <div class="page-header-text">
      <h1>
        <template v-if="trail">
          <span class="page-header-crumb">{{ title }}</span>
          <span class="page-header-sep" aria-hidden="true">›</span>
          <span>{{ trail }}</span>
        </template>
        <template v-else>{{ title }}</template>
      </h1>
      <p v-if="subtitle" class="page-subtitle">{{ subtitle }}</p>
    </div>
    <div v-if="$slots.actions" class="page-header-actions">
      <slot name="actions" />
    </div>
  </header>
</template>
