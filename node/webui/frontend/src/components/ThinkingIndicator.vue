<script setup>
defineProps({
  label: { type: String, default: "思考中" },
});
</script>

<template>
  <div class="msg__hint msg__hint--stream-meta" role="status" :aria-label="label">
    <span class="msg__meta-label">{{ label }}</span>
    <span class="msg__meta-dots" aria-hidden="true">
      <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
    </span>
  </div>
</template>
