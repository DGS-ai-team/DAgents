<script setup>
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import * as api from "../../api/node.js";
import AgentTemplateCreateModal from "../../components/AgentTemplateCreateModal.vue";
import { agentHostLabel } from "../../utils/agentTemplateForm.js";

const router = useRouter();
const loading = ref(true);
const templatesLoading = ref(true);
const error = ref("");
const templatesError = ref("");
const agents = ref([]);
const templates = ref([]);
const showTemplateCreateModal = ref(false);
const deletingTemplateId = ref("");

async function loadAgents() {
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

async function loadTemplates() {
  templatesLoading.value = true;
  templatesError.value = "";
  try {
    const res = await api.listAgentTemplates();
    templates.value = Array.isArray(res?.templates) ? res.templates : [];
  } catch (e) {
    templatesError.value = e.message || "加载模板失败";
    templates.value = [];
  } finally {
    templatesLoading.value = false;
  }
}

async function load() {
  await Promise.all([loadAgents(), loadTemplates()]);
}

function openAgent(agent) {
  const id = String(agent?.agent_id || "").trim();
  if (!id) return;
  router.push({ name: "settings-agent-detail", params: { agentId: id } });
}

function hostLabel(agent) {
  const os = agentHostLabel(agent);
  return os ? `本机 · ${os}` : "本机";
}

function sourceLabel(source) {
  return source === "user" ? "自定义" : "内置";
}

async function onDeleteTemplate(tpl) {
  const id = String(tpl?.id || "").trim();
  if (!id || tpl?.source !== "user") return;
  const name = tpl.display_name || id;
  if (!window.confirm(`确定删除模板「${name}」？此操作不可恢复。`)) return;
  deletingTemplateId.value = id;
  templatesError.value = "";
  try {
    await api.deleteAgentTemplate(id);
    await loadTemplates();
  } catch (e) {
    templatesError.value = e.message || "删除失败";
  } finally {
    deletingTemplateId.value = "";
  }
}

function onTemplateCreated() {
  void loadTemplates();
}

onMounted(load);
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">Agents</h1>
    <p class="settings-page__desc">
      管理每个 Agent 的独立配置（工具、沙箱、侧车等）。此处不是 Node 全局能力页。
    </p>

    <section class="agents-settings__section">
      <h2 class="agents-settings__section-title">已创建的 Agent</h2>
      <p v-if="loading" class="agents-settings__status">加载中…</p>
      <p v-else-if="error" class="agents-settings__error">{{ error }}</p>
      <p v-else-if="!agents.length" class="agents-settings__status">暂无 Agent，请先在对话页创建。</p>

      <ul v-else class="agents-settings__list">
        <li v-for="a in agents" :key="a.agent_id">
          <button type="button" class="agents-settings__card" @click="openAgent(a)">
            <div class="agents-settings__card-main">
              <span class="agents-settings__name">{{ a.display_name || a.agent_id }}</span>
              <span class="agents-settings__meta">
                {{ hostLabel(a) }}
                <template v-if="a.template_id"> · 源自 {{ a.template_id }}</template>
              </span>
            </div>
            <span class="agents-settings__id">{{ a.agent_id }}</span>
          </button>
        </li>
      </ul>
    </section>

    <section class="agents-settings__section">
      <div class="agents-settings__section-head">
        <div>
          <h2 class="agents-settings__section-title">Agent 模板</h2>
          <p class="agents-settings__section-desc">创建 Agent 时可从模板预填；自定义模板保存在运行时目录。</p>
        </div>
        <button type="button" class="btn btn--primary btn--sm" @click="showTemplateCreateModal = true">
          新建模板
        </button>
      </div>

      <p v-if="templatesLoading" class="agents-settings__status">加载模板…</p>
      <p v-else-if="templatesError" class="agents-settings__error">{{ templatesError }}</p>
      <p v-else-if="!templates.length" class="agents-settings__status">暂无模板；可新建自定义模板，或在对话页使用空白配置创建 Agent。</p>

      <ul v-else class="agents-settings__template-list">
        <li v-for="tpl in templates" :key="tpl.id" class="agents-settings__template-item">
          <div class="agents-settings__template-main">
            <div class="agents-settings__template-head">
              <span class="agents-settings__name">{{ tpl.display_name || tpl.id }}</span>
              <span class="agents-settings__badge" :data-source="tpl.source">{{ sourceLabel(tpl.source) }}</span>
            </div>
            <p class="agents-settings__template-desc">{{ tpl.description || "无描述" }}</p>
            <span class="agents-settings__id">{{ tpl.id }}</span>
          </div>
          <button
            v-if="tpl.source === 'user'"
            type="button"
            class="btn btn--ghost btn--sm agents-settings__delete"
            :disabled="deletingTemplateId === tpl.id"
            @click="onDeleteTemplate(tpl)"
          >
            {{ deletingTemplateId === tpl.id ? "删除中…" : "删除" }}
          </button>
        </li>
      </ul>
    </section>

    <AgentTemplateCreateModal
      :open="showTemplateCreateModal"
      @close="showTemplateCreateModal = false"
      @created="onTemplateCreated"
    />
  </div>
</template>

<style scoped>
.settings-page__desc {
  margin: 0 0 16px;
  font-size: 13px;
  color: var(--color-text-muted);
  line-height: 1.5;
}

.agents-settings__section {
  margin-top: 28px;
}

.agents-settings__section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.agents-settings__section-title {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 600;
}

.agents-settings__section-desc {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-subtle);
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

.agents-settings__list,
.agents-settings__template-list {
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
  padding: 12px 14px;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: var(--color-surface-muted);
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
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
  color: var(--color-text-muted);
}

.agents-settings__id {
  flex: 0 0 auto;
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-text-subtle);
}

.agents-settings__template-item {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: var(--color-surface-muted);
}

.agents-settings__template-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.agents-settings__template-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.agents-settings__badge {
  font-size: 10px;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--color-surface-elevated);
  color: var(--color-text-subtle);
}

.agents-settings__badge[data-source="user"] {
  background: var(--color-primary-soft);
  color: var(--color-primary-strong);
}

.agents-settings__template-desc {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text-muted);
}

.agents-settings__delete {
  flex: 0 0 auto;
  color: var(--color-danger);
}
</style>
