<script setup>
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { chromeStore } from "../stores/chrome.js";
import { sessionStore } from "../stores/session.js";
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

function openChat() {
  router.push({ name: "agents" });
}

function openActivity() {
  if (!sessionStore.sessionId) return;
  chromeStore.panel = chromeStore.panel === "activity" ? null : "activity";
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
        <svg class="product-header__svg" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <circle cx="8" cy="8" r="2" stroke="currentColor" stroke-width="1.25" />
          <path
            d="M8 2.25v1.5M8 12.25v1.5M2.25 8h1.5M12.25 8h1.5M3.7 3.7l1.06 1.06M11.24 11.24l1.06 1.06M3.7 12.3l1.06-1.06M11.24 4.76l1.06-1.06"
            stroke="currentColor"
            stroke-width="1.25"
            stroke-linecap="round"
          />
        </svg>
      </router-link>
    </div>
  </header>
</template>
