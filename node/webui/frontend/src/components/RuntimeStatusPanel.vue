<script setup>
import { computed } from "vue";
import { chromeStore } from "../stores/chrome.js";
import { sessionStore } from "../stores/session.js";

const props = defineProps({
  apiBase: { type: String, default: "同源" },
});

const sseConnected = computed(() => chromeStore.sseStatus === "connected");
const hint = computed(() => {
  if (!sseConnected.value) return `未收到实时事件。请确认 Node 正在运行，且浏览器可访问 ${props.apiBase}。`;
  if (sessionStore.error) return "请求失败时可先检查后端地址与服务端口。";
  return "连接正常。";
});
</script>

<template>
  <section class="panel runtime-panel--compact">
    <header class="panel__header">
      <div class="panel__title">运行状态</div>
    </header>
    <div class="panel__body runtime-panel__body--compact">
      <div class="runtime-status-row">
        <span class="runtime__label">SSE</span>
        <span class="runtime-sse-status" :class="sseConnected ? 'runtime-sse-status--online' : 'runtime-sse-status--offline'">
          {{ sseConnected ? "已连接" : "已断开" }}
        </span>
      </div>
      <div class="runtime-api-base" :title="apiBase">
        <span class="runtime-api-base__label">API</span>
        <span class="runtime-api-base__value">{{ apiBase }}</span>
      </div>
      <div class="runtime__hint" :class="{ 'runtime__hint--ok': sseConnected && !sessionStore.error }">{{ hint }}</div>
      <div v-if="sessionStore.error" class="runtime__error">{{ sessionStore.error }}</div>
      <div v-if="chromeStore.llmSettings?.model" class="runtime__row">
        <span class="runtime__label">模型</span>
        <span class="runtime__value">{{ chromeStore.llmSettings.model }}</span>
      </div>
      <div v-if="sessionStore.statusLine" class="runtime__row">
        <span class="runtime__label">状态</span>
        <span class="runtime__value">{{ sessionStore.statusLine }}</span>
      </div>
    </div>
  </section>
</template>
