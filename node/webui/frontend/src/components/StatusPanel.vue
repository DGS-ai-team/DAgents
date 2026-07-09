<script setup>
import { onMounted, ref } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { chromeStore } from "../stores/chrome.js";

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const showRaw = ref(false);
const data = ref(null);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [health, info, llm] = await Promise.all([api.getHealth(), api.getAgentInfo(), api.getLLMSettings()]);
    data.value = {
      health,
      agent: info,
      llm,
      session_id: sessionStore.sessionId,
    };
    chromeStore.agentInfo = { ...health, ...info };
    chromeStore.llmSettings = llm;
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel status-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">Status</div>
        <div class="command-panel__subtitle">Agent 与 LLM 运行状态</div>
      </div>
      <div class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" @click="showRaw = !showRaw">
          {{ showRaw ? "友好视图" : "JSON" }}
        </button>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
        <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <pre v-else-if="showRaw && data" class="command-panel__raw">{{ JSON.stringify(data, null, 2) }}</pre>
      <template v-else-if="data">
        <div class="command-panel__stats">
          <div class="command-stat">
            <span class="command-stat__label">Agent</span>
            <span class="command-stat__value">{{ data.agent?.agent_id || data.health?.agent_id || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">Version</span>
            <span class="command-stat__value">{{ data.health?.version || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">Session</span>
            <span class="command-stat__value command-stat__value--mono">{{ data.session_id || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">Health</span>
            <span class="command-stat__value">{{ data.health?.status || "ok" }}</span>
          </div>
        </div>

        <section class="command-section">
          <h3 class="command-section__title">LLM</h3>
          <dl class="command-kv-list">
            <div class="command-kv"><dt>Model</dt><dd>{{ data.llm?.model || "—" }}</dd></div>
            <div class="command-kv"><dt>Thinking</dt><dd>{{ data.llm?.thinking || "—" }}</dd></div>
            <div class="command-kv"><dt>Effort</dt><dd>{{ data.llm?.reasoning_effort || "—" }}</dd></div>
            <div class="command-kv"><dt>Base URL</dt><dd class="command-kv__mono">{{ data.llm?.base_url || "—" }}</dd></div>
          </dl>
        </section>
      </template>
    </div>
  </section>
</template>
