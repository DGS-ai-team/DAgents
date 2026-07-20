<script setup>
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { chromeStore } from "../stores/chrome.js";
import { sessionStore } from "../stores/session.js";
import { themeStore, toggleTheme } from "../stores/theme.js";
import brandIcon from "../assets/brand-icon.png";

const router = useRouter();
const route = useRoute();

const online = computed(() => chromeStore.sseStatus === "connected");
const statusClass = computed(() => {
  if (online.value) return "product-header__dot--online";
  if (chromeStore.sseStatus === "connecting") return "product-header__dot--connecting";
  return "product-header__dot--offline";
});
const statusLabel = computed(() => {
  if (online.value) return "在线";
  if (chromeStore.sseStatus === "connecting") return "连接中";
  return "离线";
});
const inSettings = computed(() => route.path.startsWith("/settings"));
const canOpenActivity = computed(() => !inSettings.value && !!sessionStore.sessionId);
const activityOpen = computed(() => chromeStore.panel === "activity");
const themeLabel = computed(() => (themeStore.mode === "dark" ? "切换浅色主题" : "切换深色主题"));

function openChat() {
  router.push({ name: "agents" });
}

function openActivity() {
  if (!sessionStore.sessionId) return;
  chromeStore.panel = chromeStore.panel === "activity" ? null : "activity";
}

function onToggleTheme() {
  toggleTheme();
}
</script>

<template>
  <header class="app__header product-header">
    <div class="app__brand product-header__brand">
      <img class="app__brand-mark" :src="brandIcon" width="24" height="24" alt="" aria-hidden="true" />
      <div>
        <div class="app__title">DAgents</div>
        <div class="app__subtitle">本机智能助手</div>
      </div>
    </div>
    <div class="product-header__actions">
      <span class="product-header__status" :title="`实时连接：${statusLabel}`">
        <span class="product-header__dot" :class="statusClass" aria-hidden="true" />
        <span class="product-header__status-text">{{ statusLabel }}</span>
      </span>
      <button
        type="button"
        class="app__icon-btn product-header__icon-btn"
        :title="themeLabel"
        :aria-label="themeLabel"
        @click="onToggleTheme"
      >
        <svg v-if="themeStore.mode === 'dark'" class="product-header__svg" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <circle cx="8" cy="8" r="2.1" stroke="currentColor" stroke-width="1.2" />
          <path d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.2 3.2l1.1 1.1M11.7 11.7l1.1 1.1M3.2 12.8l1.1-1.1M11.7 4.3l1.1-1.1" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
        </svg>
        <svg v-else class="product-header__svg" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path d="M10.9 2.3a5.8 5.8 0 1 0 2.8 10 5.9 5.9 0 0 1-2.8-10Z" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" />
        </svg>
      </button>
      <button
        v-if="canOpenActivity"
        type="button"
        class="app__icon-btn product-header__icon-btn"
        :class="{ 'product-header__icon-btn--active': activityOpen }"
        title="变更与上下文"
        aria-label="变更与上下文"
        :aria-pressed="activityOpen"
        @click="openActivity"
      >
        <svg class="product-header__svg" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path
            d="M3.5 3.5h9v2h-9v-2Zm0 3.5h9v2h-9V7Zm0 3.5h6V13h-6v-2.5Z"
            stroke="currentColor"
            stroke-width="1.1"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button
        v-if="inSettings"
        type="button"
        class="app__icon-btn product-header__icon-btn"
        title="返回对话"
        aria-label="返回对话"
        @click="openChat"
      >
        <svg class="product-header__svg" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path
            d="M10.5 3.5 6 8l4.5 4.5"
            stroke="currentColor"
            stroke-width="1.25"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <router-link
        v-else
        to="/settings/general"
        class="app__icon-btn product-header__icon-btn"
        title="设置"
        aria-label="设置"
      >
        <svg class="product-header__svg" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path
            d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"
            stroke="currentColor"
            stroke-width="1.75"
          />
          <path
            d="M19.4 13.5a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V20a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H4a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V4a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.91 1 1.51 1H20a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
            stroke="currentColor"
            stroke-width="1.75"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </router-link>
    </div>
  </header>
</template>
