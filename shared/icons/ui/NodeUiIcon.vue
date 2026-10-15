<script setup>
import { computed } from 'vue'
import { resolveNodeIcon } from './node-registry.js'
import { parseIconMarkup } from './icon-render.js'

const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 16 },
})

const parsedIcon = computed(() => parseIconMarkup(resolveNodeIcon(props.name)))
const iconMarkup = computed(() => parsedIcon.value.markup)
const rootAttrs = computed(() => parsedIcon.value.attributes)
</script>

<template>
  <svg class="ui-icon" :width="size" :height="size" v-bind="rootAttrs" aria-hidden="true" v-html="iconMarkup"></svg>
</template>

<style scoped>
.ui-icon { display: block; flex: 0 0 auto; }
</style>
