<script setup>
import { useRoute, useRouter } from "vue-router";

defineOptions({ name: "SettingsLayout" });

const route = useRoute();
const router = useRouter();

const navGroups = [
  {
    label: "工作区",
    items: [
      { to: "/settings/general", label: "通用" },
      { to: "/settings/connection", label: "模型与连接" },
    ],
  },
  {
    label: "工具与运行",
    items: [
      { to: "/settings/mcp", label: "全局 MCP 服务" },
      { to: "/settings/linux-channels", label: "全局 Linux 通道" },
      { to: "/settings/capabilities", label: "能力" },
      { to: "/settings/skills", label: "技能" },
    ],
  },
  {
    label: "智能体与自动化",
    items: [
      { to: "/settings/agents", label: "智能体列表", match: "/settings/agents" },
      { to: "/settings/triggers", label: "定时任务" },
    ],
  },
  {
    label: "系统",
    items: [
      { to: "/settings/security", label: "输出防护" },
      { to: "/settings/context", label: "上下文" },
      { to: "/settings/about", label: "关于" },
    ],
  },
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
      <nav class="settings-layout__nav" aria-label="设置导航">
        <div class="settings-layout__nav-head">
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
          <div class="settings-layout__identity">
            <strong>设置</strong>
          </div>
        </div>
        <div class="settings-layout__nav-scroll">
          <section v-for="group in navGroups" :key="group.label" class="settings-layout__group">
            <h2 class="settings-layout__group-title">{{ group.label }}</h2>
            <router-link
              v-for="item in group.items"
              :key="item.to"
              :to="item.to"
              class="settings-layout__link"
              :class="{ 'settings-layout__link--active': isActive(item) }"
              :aria-current="isActive(item) ? 'page' : undefined"
            >
              {{ item.label }}
            </router-link>
          </section>
        </div>
      </nav>
      <main class="settings-layout__main">
        <router-view />
      </main>
    </div>
  </div>
</template>
