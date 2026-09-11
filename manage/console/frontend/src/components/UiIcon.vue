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
    <template v-else-if="name === 'arrow-left'">
      <path d="m15 18-6-6 6-6M9 12h8" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'arrow-up'">
      <path d="M12 19V5M6 11l6-6 6 6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'arrow-down'">
      <path d="M12 5v14M6 13l6 6 6-6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'paperclip'">
      <path d="m20.5 11.5-8.9 8.9a5 5 0 0 1-7.1-7.1l9.6-9.6a3.5 3.5 0 1 1 5 5L9.5 18.3a2 2 0 0 1-2.8-2.8l9-9" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'image'">
      <rect x="3.5" y="4.5" width="17" height="15" rx="2" stroke="currentColor" stroke-width="1.8" />
      <circle cx="8.5" cy="9" r="1.35" stroke="currentColor" stroke-width="1.6" />
      <path d="m5 17 4.2-4 3 2.6 2.1-2 4.7 4.4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'file'">
      <path d="M6.5 3.5h7l4 4V20H6.5a2 2 0 0 1-2-2V5.5a2 2 0 0 1 2-2Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="M13.5 3.5V8h4M8 12h6M8 16h6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'file-plus'">
      <path d="M6.5 3.5h7l4 4V20H6.5a2 2 0 0 1-2-2V5.5a2 2 0 0 1 2-2Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="M13.5 3.5V8h4M12 11v6M9 14h6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'receipt-text'">
      <path d="M5 3.5v17l2-1 2 1 2-1 2 1 2-1 2 1v-17l-2 1-2-1-2 1-2-1-2 1-2-1Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="M8 9h8M8 13h8M8 17h5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'circle-plus'">
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 8v8M8 12h8" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'code-2'">
      <path d="m8 9-3 3 3 3M16 9l3 3-3 3M14 6l-4 12" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'briefcase'">
      <rect x="3.5" y="7" width="17" height="13" rx="2" stroke="currentColor" stroke-width="1.8" />
      <path d="M8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M3.5 12h17M10 12v2h4v-2" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'package'">
      <path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="m4.5 7.5 7.5 4 7.5-4M12 11.5V21" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'panel-right'">
      <rect x="3.5" y="4" width="17" height="16" rx="2" stroke="currentColor" stroke-width="1.8" />
      <path d="M15 4v16" stroke="currentColor" stroke-width="1.8" />
    </template>
    <template v-else-if="name === 'send'">
      <path d="m4 4 16 8-16 8 2.5-8L4 4Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="M6.5 12H20" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'pencil'">
      <path d="m4 16.5-.7 3.7 3.7-.7L18.3 8.2a2.3 2.3 0 0 0-3.2-3.2L4 16.5Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="m13.8 6.2 4 4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'more-horizontal'">
      <circle cx="5" cy="12" r="1.25" fill="currentColor" />
      <circle cx="12" cy="12" r="1.25" fill="currentColor" />
      <circle cx="19" cy="12" r="1.25" fill="currentColor" />
    </template>
    <template v-else-if="name === 'square'">
      <rect x="5" y="5" width="14" height="14" rx="1.8" fill="currentColor" />
    </template>
    <template v-else-if="name === 'refresh-cw'">
      <path d="M20 11a8 8 0 1 0 1 4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
      <path d="M20 4v7h-7" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'settings'">
      <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.38a2 2 0 0 0-.73-2.73l-.15-.09a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.8" />
    </template>
    <template v-else-if="name === 'monitor'">
      <rect x="3.5" y="4" width="17" height="12" rx="2" stroke="currentColor" stroke-width="1.8" />
      <path d="M8 20h8M12 16v4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'sun'">
      <circle cx="12" cy="12" r="3.5" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 2.5v2M12 19.5v2M4.5 12h-2M21.5 12h-2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M18.7 5.3l-1.4 1.4M6.7 17.3l-1.4 1.4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'moon'">
      <path d="M20 15.5A8.5 8.5 0 0 1 8.5 4 8.5 8.5 0 1 0 20 15.5Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'terminal' || name === 'square-terminal'">
      <rect x="3.5" y="4" width="17" height="16" rx="2" stroke="currentColor" stroke-width="1.8" />
      <path d="m7 9 3 3-3 3M13 15h4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'message-square' || name === 'message-circle'">
      <path d="M5 5h14a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-8l-4 3v-3H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="M8 10h.01M12 10h.01M16 10h.01" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'bot'">
      <rect x="4" y="7" width="16" height="12" rx="3" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 3v4M8.5 12h.01M15.5 12h.01M8 16h8" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'sparkles'">
      <path d="m12 3 1.2 4.3a3 3 0 0 0 2.1 2.1L19.5 11l-4.2 1.2a3 3 0 0 0-2.1 2.1L12 18.5l-1.2-4.2a3 3 0 0 0-2.1-2.1L4.5 11l4.2-1.2a3 3 0 0 0 2.1-2.1L12 3Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="m19 3 .35 1.25L20.5 4.6l-1.15.35L19 6.1l-.35-1.15-1.15-.35 1.15-.35L19 3Z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'clock'">
      <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 7v5l3 2" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'shield' || name === 'shield-check'">
      <path d="M12 3 19 6v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6l7-3Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path v-if="name === 'shield-check'" d="m8.5 12 2.2 2.2 4.8-5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'brain'">
      <path d="M9.5 5a3 3 0 0 0-5.4 1.8A3 3 0 0 0 5 12a3 3 0 0 0-.9 5.8A3 3 0 0 0 9.5 19V5ZM14.5 5a3 3 0 0 1 5.4 1.8A3 3 0 0 1 19 12a3 3 0 0 1 .9 5.8A3 3 0 0 1 14.5 19V5Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
      <path d="M9.5 8H8M9.5 12H7M9.5 16H8M14.5 8H16M14.5 12H17M14.5 16H16" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'info'">
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 10.5v5M12 7.5h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'network'">
      <circle cx="6" cy="12" r="2.5" stroke="currentColor" stroke-width="1.8" />
      <circle cx="18" cy="7" r="2.5" stroke="currentColor" stroke-width="1.8" />
      <circle cx="18" cy="17" r="2.5" stroke="currentColor" stroke-width="1.8" />
      <path d="m8.3 11 7.4-3M8.3 13l7.4 3" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'plug'">
      <path d="M9 3v4M15 3v4M7 7h10v3a5 5 0 0 1-10 0V7ZM12 15v6M8 21h8" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'server'">
      <rect x="3.5" y="4" width="17" height="6" rx="1.8" stroke="currentColor" stroke-width="1.8" />
      <rect x="3.5" y="14" width="17" height="6" rx="1.8" stroke="currentColor" stroke-width="1.8" />
      <path d="M7 7h.01M7 17h.01" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'users'">
      <path d="M16 20v-1.5a4 4 0 0 0-4-4H7.5a4 4 0 0 0-4 4V20M9.75 10.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7ZM16 4a3.5 3.5 0 0 1 0 6.8M17.5 14.5h.5a4 4 0 0 1 4 4V20" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'globe'">
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.8" />
      <path d="M3 12h18M12 3c2.1 2.4 3.2 5.4 3.2 9S14.1 18.6 12 21M12 3c-2.1 2.4-3.2 5.4-3.2 9S9.9 18.6 12 21" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else-if="name === 'wrench'">
      <path d="M15.4 5.1a4.4 4.4 0 0 0-5.2 5.2l-5.6 5.6a2.1 2.1 0 1 0 3 3l5.6-5.6a4.4 4.4 0 0 0 5.2-5.2l-2.5 2.5-2.3-.6-.6-2.3 2.4-2.6Z" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'bookmark'">
      <path d="M6 4.5A1.5 1.5 0 0 1 7.5 3h9A1.5 1.5 0 0 1 18 4.5V21l-5.25-3L7.5 21V4.5Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
    </template>
    <template v-else-if="name === 'tool'">
      <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 8.5v7M8.5 12h7" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
    <template v-else>
      <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.8" />
      <path d="M12 8.5v7M12 7h.01" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </template>
  </svg>
</template>

<style scoped>
.ui-icon {
  display: block;
  flex: 0 0 auto;
}
</style>
