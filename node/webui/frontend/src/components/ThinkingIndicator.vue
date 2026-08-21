<script setup>
import BrandActivityIndicator from "./BrandActivityIndicator.vue";

defineProps({
  label: { type: String, default: "思考中" },
  compact: { type: Boolean, default: false },
});
</script>

<template>
  <BrandActivityIndicator :label="label" mode="thinking" :compact="compact" />
</template>
