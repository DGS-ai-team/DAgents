<script setup>
import SharedUiIcon from '@shared-icons/ui/ManageUiIcon.vue'
defineProps({ name: { type: String, required: true }, size: { type: [Number, String], default: 16 } })
</script>
<template><SharedUiIcon :name="name" :size="size" /></template>
