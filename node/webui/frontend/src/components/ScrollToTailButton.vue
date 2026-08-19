<script setup>
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
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
      <path
        d="M3.25 6.25 8 11l4.75-4.75M8 2.5V10.5"
        fill="none"
        stroke="currentColor"
        stroke-width="1.6"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </svg>
    <span v-if="unreadCount > 0" class="chat__scroll-tail-badge" aria-live="polite">
      {{ unreadCount > 99 ? "99+" : unreadCount }}
    </span>
  </button>
</template>
