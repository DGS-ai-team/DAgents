<script setup>
import { computed, onMounted, ref } from "vue";
import { chromeStore } from "../../stores/chrome.js";
import { sessionStore } from "../../stores/session.js";
import { hitlStore } from "../../stores/hitl.js";

const STORAGE_KEY = "dagents.ui.diagnosticsExpanded";

const props = defineProps({
  apiBase: { type: String, default: "" },
});

const emit = defineEmits(["open-context"]);

const expanded = ref(false);

onMounted(() => {
  try {
    expanded.value = localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    expanded.value = false;
  }
});

function toggle() {
  expanded.value = !expanded.value;
  try {
    localStorage.setItem(STORAGE_KEY, expanded.value ? "1" : "0");
  } catch {
    /* ignore */
  }
}

const sseConnected = computed(() => chromeStore.sseStatus === "connected");
const summary = computed(() => {
  if (!sseConnected.value) return "连接中断，正在重试…";
  if (sessionStore.error) return "请求异常，请查看详情";
  return "连接正常";
});
</script>

<template>
  <aside class="diagnostics-panel" :class="{ 'diagnostics-panel--expanded': expanded }">
    <button type="button" class="diagnostics-panel__toggle" @click="toggle">
      <span class="diagnostics-panel__toggle-label">{{ expanded ? "收起诊断" : "诊断" }}</span>
      <span class="diagnostics-panel__toggle-icon" aria-hidden="true">{{ expanded ? "›" : "‹" }}</span>
    </button>
    <div v-if="!expanded" class="diagnostics-panel__summary" :title="summary">
      {{ summary }}
    </div>
    <section v-else class="panel diagnostics-panel__body">
      <header class="panel__header">
        <div class="panel__title">诊断</div>
      </header>
      <div class="panel__body runtime-panel__body--compact">
        <div class="runtime-status-row">
          <span class="runtime__label">连接</span>
          <span
            class="runtime-sse-status"
            :class="sseConnected ? 'runtime-sse-status--online' : 'runtime-sse-status--offline'"
          >
            {{ sseConnected ? "正常" : "断开" }}
          </span>
        </div>
        <div v-if="props.apiBase" class="runtime-api-base" :title="props.apiBase">
          <span class="runtime-api-base__label">服务</span>
          <span class="runtime-api-base__value">{{ props.apiBase }}</span>
        </div>
        <div v-if="sessionStore.error" class="runtime__error">{{ sessionStore.error }}</div>
        <div v-if="chromeStore.llmSettings?.model" class="runtime__row">
          <span class="runtime__label">模型</span>
          <span class="runtime__value">{{ chromeStore.llmSettings.model }}</span>
        </div>
        <div v-if="sessionStore.sessionId" class="runtime__row">
          <span class="runtime__label">会话</span>
          <span class="runtime__value runtime__value--mono">{{ sessionStore.sessionId.slice(0, 16) }}…</span>
        </div>
        <div v-if="sessionStore.statusLine" class="runtime__row">
          <span class="runtime__label">状态</span>
          <span class="runtime__value">{{ sessionStore.statusLine }}</span>
        </div>
        <div v-if="hitlStore.queue.length" class="runtime__row">
          <span class="runtime__label">待处理</span>
          <span class="runtime__value">{{ hitlStore.queue.length }} 项</span>
        </div>
        <button type="button" class="btn btn--ghost diagnostics-panel__link" @click="emit('open-context')">
          查看上下文详情
        </button>
      </div>
    </section>
  </aside>
</template>
