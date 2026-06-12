<script setup>
defineProps({
  toasts: { type: Array, default: () => [] },
});
</script>

<template>
  <div class="toast-container" aria-live="polite" aria-atomic="true">
    <div
      v-for="toast in toasts"
      :key="toast.id"
      class="toast"
      :class="[`toast-${toast.type}`, { 'toast-hide': !toast.visible }]"
    >
      {{ toast.message }}
    </div>
  </div>
</template>

<style scoped>
.toast-hide {
  opacity: 0;
  transition: opacity 0.2s ease;
}
</style>
