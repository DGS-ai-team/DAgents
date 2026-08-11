<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { themeStore, toggleTheme } from "../theme.js";
import brandIcon from "../../../../../node/webui/frontend/src/assets/brand-icon.png";

const props = defineProps({
  view: { type: String, required: true },
  variant: { type: String, default: "app" },
  healthLabel: { type: String, default: "连接中…" },
  healthOnline: { type: Boolean, default: false },
  sessionLabel: { type: String, default: "" },
  lastRefreshed: { type: String, default: "—" },
  /** 对话独立浏览器页：隐藏模块导航 */
  hideModules: { type: Boolean, default: false },
});

const emit = defineEmits(["navigate", "logout"]);

const showModuleNav = computed(() => props.variant === "app" && !props.hideModules);
const adminOpen = ref(false);
const adminWrap = ref(null);

const themeLabel = computed(() =>
  themeStore.resolved === "dark" ? "切换浅色主题" : "切换深色主题",
);

const primaryModules = [
  { id: "workgroup", label: "工作组" },
  { id: "templates", label: "Agent 模板" },
  { id: "marketplace", label: "能力市场" },
];

const adminModules = [
  { id: "nodes", label: "Node 列表", hint: "注册与在线状态" },
  { id: "permissions", label: "发现组", hint: "Node 可见性分组" },
  { id: "settings", label: "配置", hint: "LLM 与发布" },
];

const adminActive = computed(() =>
  adminModules.some((item) => item.id === props.view),
);

const themeLabelComputed = themeLabel;

function toggleAdmin() {
  adminOpen.value = !adminOpen.value;
}

function pickAdmin(id) {
  adminOpen.value = false;
  emit("navigate", id);
}

function onDocClick(event) {
  if (!adminOpen.value) return;
  const el = adminWrap.value;
  if (el && !el.contains(event.target)) {
    adminOpen.value = false;
  }
}

function onKeydown(event) {
  if (event.key === "Escape") adminOpen.value = false;
}

onMounted(() => {
  document.addEventListener("click", onDocClick);
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("click", onDocClick);
  document.removeEventListener("keydown", onKeydown);
});
</script>

<template>
  <header class="topnav" :class="`topnav--${variant}`" aria-label="主导航">
    <button type="button" class="topnav-brand" @click="emit('navigate', 'home')">
      <span class="brand-logo" aria-hidden="true">
        <img :src="brandIcon" alt="" />
      </span>
      <span class="topnav-brand-name">Manage</span>
    </button>

    <nav v-if="showModuleNav" class="topnav-nav" aria-label="功能模块">
      <button
        v-for="item in primaryModules"
        :key="item.id"
        type="button"
        class="topnav-link"
        :class="{ active: view === item.id }"
        @click="emit('navigate', item.id)"
      >
        {{ item.label }}
      </button>
    </nav>
    <div v-else class="topnav-spacer" aria-hidden="true" />

    <div v-if="!hideModules" class="topnav-meta">
      <span
        class="topnav-health"
        :class="{ 'is-online': healthOnline }"
        :title="healthLabel"
      >
        {{ healthLabel }}
      </span>
      <span v-if="sessionLabel" class="topnav-session" :title="sessionLabel">
        {{ sessionLabel }}
      </span>
      <button
        type="button"
        class="theme-toggle"
        :title="themeLabelComputed"
        :aria-label="themeLabelComputed"
        @click="toggleTheme"
      >
        <svg v-if="themeStore.resolved === 'dark'" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <circle cx="8" cy="8" r="2.1" stroke="currentColor" stroke-width="1.2" />
          <path
            d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.2 3.2l1.1 1.1M11.7 11.7l1.1 1.1M3.2 12.8l1.1-1.1M11.7 4.3l1.1-1.1"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linecap="round"
          />
        </svg>
        <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path
            d="M10.9 2.3a5.8 5.8 0 1 0 2.8 10 5.9 5.9 0 0 1-2.8-10Z"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linejoin="round"
          />
        </svg>
      </button>

      <div v-if="!hideModules" ref="adminWrap" class="topnav-admin">
        <button
          type="button"
          class="topnav-admin-btn"
          :class="{ active: adminActive || adminOpen }"
          :aria-expanded="adminOpen"
          aria-haspopup="menu"
          @click.stop="toggleAdmin"
        >
          管理
          <svg viewBox="0 0 12 12" fill="none" aria-hidden="true">
            <path d="M3 4.5L6 7.5L9 4.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
          </svg>
        </button>
        <div v-if="adminOpen" class="topnav-admin-menu" role="menu">
          <button
            v-for="item in adminModules"
            :key="item.id"
            type="button"
            class="topnav-admin-item"
            role="menuitem"
            :class="{ active: view === item.id }"
            @click="pickAdmin(item.id)"
          >
            <strong>{{ item.label }}</strong>
            <span>{{ item.hint }}</span>
          </button>
        </div>
      </div>

      <button type="button" class="topnav-logout" @click="emit('logout')">退出</button>
    </div>
  </header>
</template>
