<script setup>
import BrandActivityIndicator from "./BrandActivityIndicator.vue";

defineProps({
  label: { type: String, default: "思考中" },
});
</script>

<template>
  <BrandActivityIndicator :label="label" mode="thinking" />
</template>
