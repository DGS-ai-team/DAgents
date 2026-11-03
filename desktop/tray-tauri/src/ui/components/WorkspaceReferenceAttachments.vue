<script setup>
import UiIcon from './UiIcon.vue'

defineProps({ references: { type: Array, default: () => [] } })
const emit = defineEmits(['remove'])
</script>

<template>
  <div
    v-for="reference in references"
    :key="reference.id"
    class="composer-attachment reference-attachment"
    :title="`${reference.path}\n${reference.anchor?.label || '整个文件'}`"
  >
    <UiIcon name="paperclip" :size="16" aria-hidden="true" />
    <span>{{ reference.name }} · {{ reference.anchor?.label || '整个文件' }}</span>
    <button
      class="composer-attachment-remove"
      type="button"
      :aria-label="`移除引用：${reference.name}`"
      :title="`移除引用：${reference.name}`"
      @click="emit('remove', reference.id)"
    >×</button>
  </div>
</template>
