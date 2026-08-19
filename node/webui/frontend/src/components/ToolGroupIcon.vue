<script setup>
/** 工具组矢量图标（24×24 stroke）。 */
defineProps({
  name: { type: String, required: true },
});
</script>

<template>
  <svg class="tool-group-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <!-- 命令行 -->
    <template v-if="name === 'bash' || name === 'shell'">
      <rect x="3.5" y="4.5" width="17" height="15" rx="2" stroke="currentColor" stroke-width="1.5" />
      <path d="M7 9.5 9.5 12 7 14.5M12 14.5h5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <!-- 终端会话 -->
    <template v-else-if="name === 'terminal'">
      <rect x="4" y="5" width="16" height="12" rx="2" stroke="currentColor" stroke-width="1.5" />
      <path d="M8 19h8M12 17v2M8 9.5l2.5 2.5L8 14.5M13 14.5h3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <!-- 浏览器 -->
    <template v-else-if="name === 'browser'">
      <rect x="3.5" y="4.5" width="17" height="15" rx="2" stroke="currentColor" stroke-width="1.5" />
      <path d="M3.5 8.5h17" stroke="currentColor" stroke-width="1.5" />
      <circle cx="6.5" cy="6.5" r="0.75" fill="currentColor" />
      <circle cx="9" cy="6.5" r="0.75" fill="currentColor" />
      <circle cx="11.5" cy="6.5" r="0.75" fill="currentColor" />
    </template>
    <!-- 子智能体 -->
    <template v-else-if="name === 'child_agents' || name === 'child'">
      <circle cx="12" cy="6.5" r="2.25" stroke="currentColor" stroke-width="1.5" />
      <circle cx="6.5" cy="16.5" r="2.25" stroke="currentColor" stroke-width="1.5" />
      <circle cx="17.5" cy="16.5" r="2.25" stroke="currentColor" stroke-width="1.5" />
      <path d="M12 8.75v3.5M12 12.25 7.75 15M12 12.25l4.25 2.75" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </template>
    <!-- MCP 服务 -->
    <template v-else-if="name === 'mcp'">
      <circle cx="6" cy="12" r="2.5" stroke="currentColor" stroke-width="1.5" />
      <circle cx="18" cy="7" r="2.5" stroke="currentColor" stroke-width="1.5" />
      <circle cx="18" cy="17" r="2.5" stroke="currentColor" stroke-width="1.5" />
      <path d="m8.3 11 7.4-3M8.3 13l7.4 3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </template>
    <!-- Linux 通道 -->
    <template v-else-if="name === 'linux'">
      <rect x="4" y="5" width="16" height="14" rx="2" stroke="currentColor" stroke-width="1.5" />
      <path d="M7.5 9.5 10 12l-2.5 2.5M12.5 14.5h4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
    </template>
    <!-- 文件 -->
    <template v-else-if="name === 'fs'">
      <path
        d="M7 4.5h6.5L17.5 8.5V19a1.5 1.5 0 0 1-1.5 1.5H7A1.5 1.5 0 0 1 5.5 19V6A1.5 1.5 0 0 1 7 4.5Z"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linejoin="round"
      />
      <path d="M13.5 4.5V8h3.5" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
    </template>
    <!-- 用户询问 -->
    <template v-else-if="name === 'hitl'">
      <path
        d="M5 11.5c0-3.6 3-6.5 7-6.5s7 2.9 7 6.5-3 6.5-7 6.5c-.7 0-1.4-.1-2-.2L5.5 19l1.2-2.6A6.4 6.4 0 0 1 5 11.5Z"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linejoin="round"
      />
      <path d="M9.5 11h.01M12 11h.01M14.5 11h.01" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
    </template>
    <!-- 记忆 -->
    <template v-else-if="name === 'memory'">
      <path
        d="M8 4.5h7.5A1.5 1.5 0 0 1 17 6v13.2l-4.5-2.4L8 19.2V6A1.5 1.5 0 0 1 9.5 4.5"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linejoin="round"
      />
      <path d="M10.5 9h4M10.5 12h4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </template>
    <!-- 技能 -->
    <template v-else-if="name === 'skills'">
      <path
        d="M12 4.5 13.7 9.3 18.75 9.5 14.9 12.7 16.3 17.5 12 14.9 7.7 17.5 9.1 12.7 5.25 9.5 10.3 9.3 12 4.5Z"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linejoin="round"
      />
    </template>
    <!-- 定时任务 -->
    <template v-else-if="name === 'triggers'">
      <circle cx="12" cy="12.5" r="7" stroke="currentColor" stroke-width="1.5" />
      <path d="M12 9v4l2.5 1.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
      <path d="M9 4.5h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </template>
    <!-- 企业微信 -->
    <template v-else-if="name === 'wecom'">
      <path
        d="M8.5 10.5c0-2.4 2.5-4.3 5.5-4.3s5.5 1.9 5.5 4.3-2.5 4.3-5.5 4.3c-.5 0-1-.05-1.45-.15L9.5 16l.7-1.55A5.2 5.2 0 0 1 8.5 10.5Z"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linejoin="round"
      />
      <path
        d="M4.5 14c0-1.9 1.9-3.4 4.3-3.5"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
      />
      <path
        d="M6.2 17.8 5 19.2l1.4-.35c.3-.08.6-.05.85.1A4.3 4.3 0 0 0 10.2 19"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </template>
    <!-- fallback -->
    <template v-else>
      <circle cx="12" cy="12" r="7" stroke="currentColor" stroke-width="1.5" />
      <path d="M12 9v3.5M12 15.5h.01" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </template>
  </svg>
</template>

<style scoped>
.tool-group-icon {
  width: 20px;
  height: 20px;
  display: block;
  flex-shrink: 0;
}
</style>
