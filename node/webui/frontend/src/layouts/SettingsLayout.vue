<script setup>
import { inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import UiIcon from "../components/UiIcon.vue";
import FirstRunNodeProfile from "../views/FirstRunNodeProfile.vue";

defineOptions({ name: "SettingsLayout" });

const route = useRoute();
const router = useRouter();
const isDesktop = typeof window !== "undefined" && !!window.__TAURI_INTERNALS__;
const desktopNodeSetupRequired = inject("desktopNodeSetupRequired", ref(false));
const desktopFirstRunCompleted = inject("desktopFirstRunCompleted", () => {});
const navScroll = ref(null);
const navContent = ref(null);
const fadeStart = ref(false);
const fadeEnd = ref(false);
let resizeObserver;

function updateScrollState() {
  const el = navScroll.value;
  if (!el) return;
  const horizontal = window.matchMedia("(max-width: 720px)").matches;
  const position = horizontal ? el.scrollLeft : el.scrollTop;
  const extent = horizontal ? el.scrollWidth : el.scrollHeight;
  const viewport = horizontal ? el.clientWidth : el.clientHeight;
  const max = Math.max(0, extent - viewport);
  fadeStart.value = position > 1;
  fadeEnd.value = max - position > 1;
}

function keepActiveLinkVisible() {
  const el = navScroll.value;
  const link = el?.querySelector('[aria-current="page"]');
  if (!el || !link) return;
  const horizontal = window.matchMedia("(max-width: 720px)").matches;
  const viewportRect = el.getBoundingClientRect();
  const linkRect = link.getBoundingClientRect();
  if (horizontal) {
    if (linkRect.left < viewportRect.left) el.scrollLeft += linkRect.left - viewportRect.left;
    else if (linkRect.right > viewportRect.right) el.scrollLeft += linkRect.right - viewportRect.right;
  } else {
    if (linkRect.top < viewportRect.top) el.scrollTop += linkRect.top - viewportRect.top;
    else if (linkRect.bottom > viewportRect.bottom) el.scrollTop += linkRect.bottom - viewportRect.bottom;
  }
}

async function refreshNavigation() {
  await nextTick();
  keepActiveLinkVisible();
  updateScrollState();
}

onMounted(() => {
  const el = navScroll.value;
  if (!el) return;
  el.addEventListener("scroll", updateScrollState, { passive: true });
  window.addEventListener("resize", refreshNavigation);
  resizeObserver = new ResizeObserver(refreshNavigation);
  resizeObserver.observe(el);
  if (navContent.value) resizeObserver.observe(navContent.value);
  refreshNavigation();
});

onBeforeUnmount(() => {
  navScroll.value?.removeEventListener("scroll", updateScrollState);
  window.removeEventListener("resize", refreshNavigation);
  resizeObserver?.disconnect();
});

watch(() => route.fullPath, refreshNavigation);

const sharedNavGroups = [
  {
    label: "工作区",
    items: [
      { to: "/settings/general", label: "通用", icon: "settings" },
      ...(window.__TAURI_INTERNALS__ ? [{ to: "/settings/desktop", label: "Desktop", icon: "monitor" }] : []),
      { to: "/settings/connection", label: "模型与连接", icon: "plug" },
    ],
  },
  {
    label: "工具与运行",
    items: [
      { to: "/settings/mcp", label: "全局 MCP 服务", icon: "server" },
      { to: "/settings/linux-channels", label: "全局 Linux 通道", icon: "square-terminal" },
      { to: "/settings/capabilities", label: "能力", icon: "monitor" },
      { to: "/settings/skills", label: "技能", icon: "skills" },
    ],
  },
  {
    label: "智能体与自动化",
    items: [
      { to: "/settings/agents", label: "智能体列表", match: "/settings/agents", icon: "agent" },
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

const navGroups = window.__TAURI_INTERNALS__
  ? [
      { label: "本机应用", items: [{ to: "/settings/desktop", label: "Desktop 设置", icon: "monitor" }] },
      {
        label: "当前 Node",
        items: [
          { to: "/settings/general", label: "Node 通用", icon: "settings" },
          { to: "/settings/connection", label: "模型与连接", icon: "plug" },
        ],
      },
      ...sharedNavGroups.slice(1),
    ]
  : sharedNavGroups;

function isActive(item) {
  if (item.match) return route.path === item.match || route.path.startsWith(`${item.match}/`);
  return route.path === item.to;
}

function shouldGateNodeSettings(viewRoute) {
  return isDesktop && desktopNodeSetupRequired.value && viewRoute.path !== "/settings/desktop";
}

function backToChat() {
  if (window.__TAURI_INTERNALS__) {
    window.dispatchEvent(new CustomEvent("desktop-back-to-chat"));
    return;
  }
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
        <div
          class="settings-layout__nav-viewport"
          :class="{
            'settings-layout__nav-viewport--fade-start': fadeStart,
            'settings-layout__nav-viewport--fade-end': fadeEnd,
          }"
        >
          <div ref="navScroll" class="settings-layout__nav-scroll" role="region" tabindex="0" aria-label="设置项目">
            <div ref="navContent" class="settings-layout__nav-content">
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
                  <UiIcon
                    class="settings-layout__link-icon"
                    :class="{ 'settings-layout__link-icon--skills': item.icon === 'skills' }"
                    :name="item.icon"
                    :size="item.icon === 'skills' ? 14 : 16"
                  />
                  <span>{{ item.label }}</span>
                </router-link>
              </section>
            </div>
          </div>
        </div>
      </nav>
      <main class="settings-layout__main">
        <router-view v-slot="{ Component, route: viewRoute }">
          <Transition name="settings-page-transition" mode="out-in">
            <FirstRunNodeProfile
              v-if="shouldGateNodeSettings(viewRoute)"
              :key="`first-run-${viewRoute.fullPath}`"
              @completed="desktopFirstRunCompleted"
            />
            <component v-else :is="Component" :key="viewRoute.fullPath" />
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
