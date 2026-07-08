<script setup>
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { chromeStore } from "../stores/chrome.js";
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

function openChat() {
  router.push({ name: "chat" });
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
        v-if="inSettings"
        type="button"
        class="app__icon-btn product-header__icon-btn"
        title="返回对话"
        aria-label="返回对话"
        @click="openChat"
      >
        <svg viewBox="0 0 20 20" fill="currentColor" width="20" height="20" aria-hidden="true">
          <path
            fill-rule="evenodd"
            d="M17 10a.75.75 0 01-.75.75H5.612l4.158 3.96a.75.75 0 11-1.04 1.08l-5.5-5.25a.75.75 0 010-1.08l5.5-5.25a.75.75 0 111.04 1.08L5.612 9.25H16.25A.75.75 0 0117 10z"
            clip-rule="evenodd"
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
        <svg viewBox="0 0 20 20" fill="currentColor" width="20" height="20" aria-hidden="true">
          <path
            fill-rule="evenodd"
            d="M11.49 3.17c-.38-1.56-2.6-1.56-2.98 0a1.532 1.532 0 01-2.286.948c-1.372-.836-2.942.734-2.106 2.106.54.886 0 1.951-.864 2.494-1.56.384-1.56 2.6 0 2.978a1.532 1.532 0 01.948 2.286c-.836 1.372.734 2.942 2.106 2.106 1.372-.836 2.942.734 2.106 2.106a1.532 1.532 0 012.286.948c.384 1.56 2.6 1.56 2.978 0a1.533 1.533 0 012.286-.948c1.372.836 2.942-.734 2.106-2.106a1.533 1.533 0 01.948-2.286c1.372-.836.734-2.942-2.106-2.106a1.532 1.532 0 01-.948-2.286c.836-1.372-.734-2.942-2.106-2.106a1.532 1.532 0 01-2.286-.948zM10 13a3 3 0 100-6 3 3 0 000 6z"
            clip-rule="evenodd"
          />
        </svg>
      </router-link>
    </div>
  </header>
</template>
