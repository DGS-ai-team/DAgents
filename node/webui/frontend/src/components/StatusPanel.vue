<script setup>
import { onMounted, ref } from "vue";
import * as api from "../api/node.js";
import { chromeStore } from "../stores/chrome.js";

defineProps({
  embedded: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
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
  <section
    class="panel panel-overlay__card command-panel status-panel"
    :class="{ 'settings-embedded-panel': embedded }"
  >
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">运行状态</div>
        <div v-if="!embedded" class="command-panel__subtitle">当前 Node 与 Agent 状态</div>
      </div>
      <div v-if="!embedded" class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <template v-else-if="data">
        <div class="command-panel__stats">
          <div class="command-stat">
            <span class="command-stat__label">Node</span>
            <span class="command-stat__value">{{ data.agent?.node_id || data.health?.node_id || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">Version</span>
            <span class="command-stat__value">{{ data.health?.version || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">Health</span>
            <span class="command-stat__value">{{ data.health?.status || "ok" }}</span>
          </div>
        </div>
      </template>
    </div>
  </section>
</template>
