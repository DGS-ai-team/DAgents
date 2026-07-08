<script setup>
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { chromeStore } from "../stores/chrome.js";

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

function openHelp() {
  if (inSettings.value) {
    router.push({ name: "chat" });
  }
  chromeStore.panel = "help";
}

function openChat() {
  router.push({ name: "chat" });
}
</script>

<template>
  <header class="app__header product-header">
    <div class="app__brand product-header__brand">
      <div class="app__brand-mark" aria-hidden="true" />
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
      <button v-if="inSettings" type="button" class="btn btn--ghost product-header__btn" @click="openChat">
        返回对话
      </button>
      <router-link v-else to="/settings/general" class="btn btn--ghost product-header__btn">设置</router-link>
      <button type="button" class="btn btn--ghost product-header__btn" @click="openHelp">帮助</button>
    </div>
  </header>
</template>
