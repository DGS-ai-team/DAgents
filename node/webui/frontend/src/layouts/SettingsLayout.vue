<script setup>
import { useRoute, useRouter } from "vue-router";

defineOptions({ name: "SettingsLayout" });

const route = useRoute();
const router = useRouter();

const nav = [
  { to: "/settings/general", label: "通用" },
  { to: "/settings/connection", label: "连接" },
  { to: "/settings/mcp", label: "MCP" },
  { to: "/settings/linux-channels", label: "Linux 通道" },
  { to: "/settings/agents", label: "智能体", match: "/settings/agents" },
  { to: "/settings/capabilities", label: "能力" },
  { to: "/settings/skills", label: "技能" },
  { to: "/settings/triggers", label: "定时任务" },
  { to: "/settings/security", label: "输出防护" },
  { to: "/settings/context", label: "上下文" },
  { to: "/settings/about", label: "关于" },
];

function isActive(item) {
  if (item.match) return route.path === item.match || route.path.startsWith(`${item.match}/`);
  return route.path === item.to;
}

function backToChat() {
  router.push({ name: "agents" });
}
</script>

<template>
  <div class="app">
    <div class="settings-layout">
      <nav class="settings-layout__nav">
        <button type="button" class="settings-layout__back" @click="backToChat">
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <path
              d="M10.5 3.5 6 8l4.5 4.5"
              fill="none"
              stroke="currentColor"
              stroke-width="1.25"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          <span>返回对话</span>
        </button>
        <router-link
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="settings-layout__link"
          :class="{ 'settings-layout__link--active': isActive(item) }"
        >
          {{ item.label }}
        </router-link>
      </nav>
      <main class="settings-layout__main">
        <router-view />
      </main>
    </div>
  </div>
</template>
