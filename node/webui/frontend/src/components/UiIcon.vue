<script setup>
defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 16 },
});
</script>

<template>
  <svg class="ui-icon" :width="size" :height="size" viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <template v-if="name === 'close'">
      <path d="m6 6 12 12M18 6 6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'check'">
      <path d="m5 12 4.2 4.2L19 6.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'pending'">
      <circle cx="12" cy="12" r="8" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 8v4l2.5 1.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'transfer'">
      <path d="M8 3v14M4.5 6.5 8 3l3.5 3.5M16 21V7M12.5 17.5 16 21l3.5-3.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'alert'">
      <path d="m12 3 9 17H3L12 3Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="M12 9v4M12 16.5h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'plus'">
      <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'chevron-right'">
      <path d="m9 5 7 7-7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'chevron-left'">
      <path d="m15 5-7 7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'chevron-up'">
      <path d="m5 15 7-7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'chevron-down'">
      <path d="m5 9 7 7 7-7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'trash'">
      <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5M14 11v5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
  </svg>
</template>

<style scoped>
.ui-icon {
  display: block;
  flex: 0 0 auto;
}
</style>
