<script setup>
import { useRoute, useRouter } from "vue-router";
import UiIcon from "../components/UiIcon.vue";

defineOptions({ name: "SettingsLayout" });

const route = useRoute();
const router = useRouter();

const navGroups = [
  {
    label: "工作区",
    items: [
      { to: "/settings/general", label: "通用", icon: "wrench" },
      { to: "/settings/connection", label: "模型与连接", icon: "plug" },
    ],
  },
  {
    label: "工具与运行",
    items: [
      { to: "/settings/mcp", label: "全局 MCP 服务", icon: "server" },
      { to: "/settings/linux-channels", label: "全局 Linux 通道", icon: "square-terminal" },
      { to: "/settings/capabilities", label: "能力", icon: "monitor" },
      { to: "/settings/skills", label: "技能", icon: "sparkles" },
    ],
  },
  {
    label: "智能体与自动化",
    items: [
      { to: "/settings/agents", label: "智能体列表", match: "/settings/agents", icon: "bot" },
      { to: "/settings/triggers", label: "定时任务", icon: "clock" },
    ],
  },
  {
    label: "系统",
    items: [
      { to: "/settings/security", label: "输出防护", icon: "shield-check" },
      { to: "/settings/context", label: "上下文", icon: "brain" },
      { to: "/settings/about", label: "关于", icon: "info" },
    ],
  },
  {
    label: "支持",
    items: [{ to: "/settings/feedback", label: "帮助与反馈", icon: "message-circle" }],
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
            <UiIcon name="arrow-left" :size="15" />
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
              <UiIcon class="settings-layout__link-icon" :name="item.icon" :size="18" />
              <span>{{ item.label }}</span>
            </router-link>
          </section>
        </div>
      </nav>
      <main class="settings-layout__main">
        <router-view v-slot="{ Component, route: viewRoute }">
          <Transition name="settings-page-transition" mode="out-in">
            <component :is="Component" :key="viewRoute.fullPath" />
          </Transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style>
.settings-page-transition-enter-active,
.settings-page-transition-leave-active {
  transition: opacity 280ms ease, transform 280ms ease;
}

.settings-page-transition-enter-from {
  opacity: 0;
  transform: translateY(8px);
}

.settings-page-transition-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .settings-page-transition-enter-active,
  .settings-page-transition-leave-active {
    transition: none;
  }
}
</style>
