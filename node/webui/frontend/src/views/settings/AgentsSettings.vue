<script setup>
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import * as api from "../../api/node.js";

const router = useRouter();
const loading = ref(true);
const error = ref("");
const agents = ref([]);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const res = await api.listAgents();
    agents.value = Array.isArray(res?.agents) ? res.agents : [];
  } catch (e) {
    error.value = e.message || "加载失败";
    agents.value = [];
  } finally {
    loading.value = false;
  }
}

function openAgent(agent) {
  const id = String(agent?.agent_id || "").trim();
  if (!id) return;
  router.push({ name: "settings-agent-detail", params: { agentId: id } });
}

function originLabel(origin) {
  return origin === "remote" ? "远端" : "本地";
}

onMounted(load);
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">Agents</h1>
    <p class="settings-page__desc">
      管理每个 Agent 的独立配置（工具、沙箱、侧车等）。此处不是 Node 全局能力页。
    </p>

    <p v-if="loading" class="agents-settings__status">加载中…</p>
    <p v-else-if="error" class="agents-settings__error">{{ error }}</p>
    <p v-else-if="!agents.length" class="agents-settings__status">暂无 Agent，请先在对话页创建。</p>

    <ul v-else class="agents-settings__list">
      <li v-for="a in agents" :key="a.agent_id">
        <button type="button" class="agents-settings__card" @click="openAgent(a)">
          <div class="agents-settings__card-main">
            <span class="agents-settings__name">{{ a.display_name || a.agent_id }}</span>
            <span class="agents-settings__meta">
              {{ originLabel(a.origin) }}
              <template v-if="a.sandbox_enabled"> · 沙箱</template>
              <template v-if="a.template_id"> · 源自 {{ a.template_id }}</template>
            </span>
          </div>
          <span class="agents-settings__id">{{ a.agent_id }}</span>
        </button>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.settings-page__desc {
  margin: 0 0 16px;
  font-size: 13px;
  color: var(--color-text-muted);
  line-height: 1.5;
}

.agents-settings__status,
.agents-settings__error {
  margin: 12px 0;
  font-size: 13px;
  color: var(--color-text-subtle);
}

.agents-settings__error {
  color: var(--color-danger);
}

.agents-settings__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.agents-settings__card {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  text-align: left;
  padding: 14px 16px;
  border-radius: 10px;
  border: 1px solid var(--color-border);
  background: var(--color-surface-muted);
  color: inherit;
  cursor: pointer;
}

.agents-settings__card:hover {
  border-color: var(--color-border-strong);
  background: var(--color-surface-hover);
}

.agents-settings__card-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.agents-settings__name {
  font-size: 14px;
  font-weight: 600;
}

.agents-settings__meta {
  font-size: 12px;
  color: var(--color-text-subtle);
}

.agents-settings__id {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-text-subtle);
  flex-shrink: 0;
}
</style>
