<script setup>
import UiIcon from "./UiIcon.vue";

defineProps({
  visible: { type: Boolean, default: false },
  unreadCount: { type: Number, default: 0 },
});

defineEmits(["click"]);
</script>

<template>
  <button
    v-if="visible"
    type="button"
    class="chat__scroll-tail"
    :aria-label="unreadCount > 0 ? `直达底部消息，${unreadCount} 条新消息` : '直达底部消息'"
    :title="unreadCount > 0 ? `直达底部消息（${unreadCount} 条新消息）` : '直达底部消息'"
    @click="$emit('click')"
  >
    <UiIcon name="chevron-down" :size="17" />
    <span v-if="unreadCount > 0" class="chat__scroll-tail-badge" aria-live="polite">
      {{ unreadCount > 99 ? "99+" : unreadCount }}
    </span>
  </button>
</template>
