<script setup>
import { ref, onMounted, computed } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { formatRelativeTime, agentDisplayTitle, agentRecordId } from "../utils/format.js";

const emit = defineEmits(["switch", "created", "delete"]);

const agents = ref([]);
const templates = ref([]);
const loading = ref(false);
const deletingId = ref("");
const renamingId = ref("");
const renameDraft = ref("");
const showWizard = ref(false);
const wizardBusy = ref(false);
const wizardError = ref("");
const wizard = ref({
  templateId: "",
  displayName: "",
  sandboxEnabled: null,
});

function agentSortTime(agent) {
  const iso = agent?.updated_at || agent?.UpdatedAt;
  const ts = Date.parse(iso || "");
  return Number.isFinite(ts) ? ts : 0;
}

const sortedAgents = computed(() => {
  const currentId = String(sessionStore.sessionId || "").trim();
  return [...agents.value].sort((a, b) => {
    const aId = agentRecordId(a);
    const bId = agentRecordId(b);
    const aCurrent = currentId && aId === currentId;
    const bCurrent = currentId && bId === currentId;
    if (aCurrent && !bCurrent) return -1;
    if (!aCurrent && bCurrent) return 1;
    return agentSortTime(b) - agentSortTime(a);
  });
});

const selectedTemplate = computed(() =>
  templates.value.find((t) => t.id === wizard.value.templateId) || null,
);

async function refresh() {
  loading.value = true;
  try {
    const res = await api.listAgents();
    agents.value = res.agents || [];
  } catch {
    agents.value = [];
  } finally {
    loading.value = false;
  }
}

async function loadTemplates() {
  try {
    const res = await api.listAgentTemplates();
    templates.value = res.templates || [];
  } catch {
    templates.value = [];
  }
}

function select(id) {
  emit("switch", id);
}

function openWizard() {
  wizardError.value = "";
  wizardBusy.value = false;
  const first = templates.value[0];
  wizard.value = {
    templateId: first?.id || "",
    displayName: first?.display_name || "",
    sandboxEnabled: first ? !!first.sandbox?.enabled : null,
  };
  showWizard.value = true;
  if (!templates.value.length) void loadTemplates().then(() => {
    const t = templates.value[0];
    if (t) {
      wizard.value.templateId = t.id;
      wizard.value.displayName = t.display_name || t.id;
      wizard.value.sandboxEnabled = !!t.sandbox?.enabled;
    }
  });
}

function onTemplatePick(tpl) {
  wizard.value.templateId = tpl.id;
  if (!wizard.value.displayName || wizard.value.displayName === selectedTemplate.value?.display_name) {
    wizard.value.displayName = tpl.display_name || tpl.id;
  }
  wizard.value.sandboxEnabled = !!tpl.sandbox?.enabled;
}

async function submitWizard() {
  const templateId = String(wizard.value.templateId || "").trim();
  if (!templateId) {
    wizardError.value = "请选择模板";
    return;
  }
  wizardBusy.value = true;
  wizardError.value = "";
  try {
    const body = {
      templateId,
      displayName: String(wizard.value.displayName || "").trim() || undefined,
    };
    if (wizard.value.sandboxEnabled !== null) {
      body.sandbox = { enabled: !!wizard.value.sandboxEnabled };
    }
    const created = await api.createAgent(body);
    showWizard.value = false;
    await refresh();
    emit("created", created);
  } catch (e) {
    wizardError.value = e.message || "创建失败";
  } finally {
    wizardBusy.value = false;
  }
}

function onDelete(agent) {
  const id = agentRecordId(agent);
  if (!id || deletingId.value === id) return;
  emit("delete", { id, agent });
}

function startRename(agent) {
  const id = agentRecordId(agent);
  renamingId.value = id;
  renameDraft.value = agentDisplayTitle(agent);
}

async function commitRename(agent) {
  const id = agentRecordId(agent);
  const name = String(renameDraft.value || "").trim();
  renamingId.value = "";
  if (!id || !name || name === agentDisplayTitle(agent)) return;
  try {
    await api.patchAgent(id, { display_name: name });
    await refresh();
  } catch (e) {
    sessionStore.error = e.message;
  }
}

function setDeleting(id) {
  deletingId.value = id || "";
}

onMounted(async () => {
  await Promise.all([refresh(), loadTemplates()]);
});

defineExpose({ refresh, setDeleting, openWizard });
</script>

<template>
  <section class="panel session-panel agent-panel">
    <header class="panel__header session-panel__header">
      <div class="panel__title">我的 Agent</div>
      <button type="button" class="session-panel__icon-btn" title="新建 Agent" @click="openWizard">+</button>
    </header>
    <div class="panel__body session-panel__body">
      <div v-if="showWizard" class="agent-wizard">
        <div class="agent-wizard__title">新建 Agent</div>
        <div class="agent-wizard__templates">
          <button
            v-for="tpl in templates"
            :key="tpl.id"
            type="button"
            class="agent-wizard__tpl"
            :class="{ 'agent-wizard__tpl--active': wizard.templateId === tpl.id }"
            @click="onTemplatePick(tpl)"
          >
            <span class="agent-wizard__tpl-name">{{ tpl.display_name || tpl.id }}</span>
            <span class="agent-wizard__tpl-id">{{ tpl.id }}</span>
          </button>
          <p v-if="!templates.length" class="session-panel__empty">暂无可用模板</p>
        </div>
        <label class="agent-wizard__field">
          <span>显示名称</span>
          <input v-model="wizard.displayName" type="text" class="agent-wizard__input" placeholder="Agent 名称" />
        </label>
        <label class="agent-wizard__check">
          <input v-model="wizard.sandboxEnabled" type="checkbox" />
          <span>沙箱运行</span>
        </label>
        <p v-if="wizardError" class="agent-wizard__error">{{ wizardError }}</p>
        <div class="agent-wizard__actions">
          <button type="button" class="btn btn--ghost btn--sm" :disabled="wizardBusy" @click="showWizard = false">取消</button>
          <button type="button" class="btn btn--primary btn--sm" :disabled="wizardBusy || !wizard.templateId" @click="submitWizard">
            {{ wizardBusy ? "创建中…" : "创建" }}
          </button>
        </div>
      </div>

      <div v-if="loading" class="session-panel__loading">加载中…</div>
      <ul v-else class="session-history-list">
        <li
          v-for="a in sortedAgents"
          :key="agentRecordId(a)"
          class="session-history-item"
          :class="{
            'session-history-item--active': agentRecordId(a) === sessionStore.sessionId,
          }"
          @click="select(agentRecordId(a))"
        >
          <div class="session-history-item__main">
            <div class="session-history-item__title-row">
              <input
                v-if="renamingId === agentRecordId(a)"
                v-model="renameDraft"
                class="session-history-item__title-input"
                @click.stop
                @keydown.enter.prevent="commitRename(a)"
                @keydown.esc.prevent="renamingId = ''"
                @blur="commitRename(a)"
              />
              <span v-else class="session-history-item__title" @dblclick.stop="startRename(a)">{{ agentDisplayTitle(a) }}</span>
            </div>
            <div class="session-history-item__meta">
              <span class="session-history-item__count">{{ a.template_id || "—" }}</span>
              <span v-if="a.sandbox_enabled" class="session-history-item__badge session-history-item__badge--live">沙箱</span>
              <span v-if="a.updated_at" class="session-history-item__time">{{ formatRelativeTime(a.updated_at) }}</span>
            </div>
          </div>
          <button
            type="button"
            class="session-history-item__edit"
            title="重命名"
            @click.stop="startRename(a)"
          >
            ✎
          </button>
          <button
            type="button"
            class="session-history-item__delete"
            title="删除 Agent"
            :disabled="deletingId === agentRecordId(a)"
            @click.stop="onDelete(a)"
          >
            {{ deletingId === agentRecordId(a) ? "…" : "×" }}
          </button>
        </li>
        <li v-if="!agents.length" class="session-panel__empty">暂无 Agent，点击 + 从模板创建</li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.agent-wizard {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 8px;
  margin-bottom: 8px;
  border: 1px solid var(--color-border, rgba(0, 0, 0, 0.08));
  border-radius: 8px;
  background: var(--color-surface-muted, rgba(0, 0, 0, 0.02));
}
.agent-wizard__title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--color-text, #222);
}
.agent-wizard__templates {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 160px;
  overflow: auto;
}
.agent-wizard__tpl {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  text-align: left;
  padding: 8px 10px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  color: inherit;
}
.agent-wizard__tpl:hover {
  background: rgba(0, 0, 0, 0.04);
}
.agent-wizard__tpl--active {
  border-color: var(--color-primary, #3b6ced);
  background: color-mix(in srgb, var(--color-primary, #3b6ced) 8%, transparent);
}
.agent-wizard__tpl-name {
  font-size: 13px;
  font-weight: 500;
}
.agent-wizard__tpl-id {
  font-size: 11px;
  color: var(--color-text-subtle, #888);
  font-family: ui-monospace, monospace;
}
.agent-wizard__field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: var(--color-text-muted, #666);
}
.agent-wizard__input {
  padding: 6px 8px;
  border: 1px solid var(--color-border, rgba(0, 0, 0, 0.12));
  border-radius: 6px;
  font-size: 13px;
  background: var(--color-surface, #fff);
  color: inherit;
}
.agent-wizard__check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--color-text-muted, #666);
}
.agent-wizard__error {
  margin: 0;
  font-size: 12px;
  color: var(--color-danger, #c0392b);
}
.agent-wizard__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
