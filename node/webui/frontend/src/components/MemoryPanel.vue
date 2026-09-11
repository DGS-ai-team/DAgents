<script setup>
import { computed, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({
  agentId: { type: String, required: true },
  scope: { type: String, default: "agent" },
});

const loading = ref(true);
const refreshing = ref(false);
const error = ref("");
const viewScope = ref(normalizeScope(props.scope));
const agentEntries = ref([]);
const globalEntries = ref([]);
const editingId = ref("");
const editContent = ref("");
const savingId = ref("");
const deletingId = ref("");
const mutationError = ref("");

const currentScope = computed(() => normalizeScope(props.scope));
const currentScopeLabel = computed(() => (currentScope.value === "global" ? "全局" : "本智能体"));
const visibleEntries = computed(() => (viewScope.value === "global" ? globalEntries.value : agentEntries.value));

function normalizeScope(value) {
  return String(value || "").trim() === "global" ? "global" : "agent";
}

function normalizeEntries(entries) {
  if (!Array.isArray(entries)) return [];
  return entries
    .map((entry) => ({
      id: String(entry?.id || "").trim(),
      content: String(entry?.content || "").trim(),
      createdAt: String(entry?.created_at || "").trim(),
      updatedAt: String(entry?.updated_at || "").trim(),
    }))
    .filter((entry) => entry.content);
}

function applyContext(context) {
  agentEntries.value = normalizeEntries(context?.memory_entries);
  globalEntries.value = normalizeEntries(context?.global_memory_entries);
}

function formatDate(value) {
  const raw = String(value || "").trim();
  if (/^\d{8}$/.test(raw)) return raw;
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return "日期未知";
  const year = date.getUTCFullYear();
  const month = String(date.getUTCMonth() + 1).padStart(2, "0");
  const day = String(date.getUTCDate()).padStart(2, "0");
  return `${year}${month}${day}`;
}

async function load({ silent = false } = {}) {
  if (!props.agentId) {
    error.value = "缺少 agent_id";
    loading.value = false;
    return;
  }
  if (silent) refreshing.value = true;
  else loading.value = true;
  error.value = "";
  try {
    const context = await api.getAgentPromptContext(props.agentId);
    applyContext(context);
    if (editingId.value && !visibleEntries.value.some((entry) => entry.id === editingId.value)) {
      cancelEdit();
    }
  } catch (e) {
    error.value = e?.message || "加载记忆清单失败";
  } finally {
    loading.value = false;
    refreshing.value = false;
  }
}

function startEdit(entry) {
  if (!entry?.id) return;
  mutationError.value = "";
  editingId.value = entry.id;
  editContent.value = entry.content;
}

function cancelEdit() {
  editingId.value = "";
  editContent.value = "";
}

async function saveEdit(entry) {
  const content = String(editContent.value || "").trim();
  if (!content) {
    mutationError.value = "记忆内容不能为空。";
    return;
  }
  savingId.value = entry.id;
  mutationError.value = "";
  try {
    const result = await api.patchAgentMemoryEntry(props.agentId, viewScope.value, entry.id, content);
    applyContext(result?.prompt_context || result);
    cancelEdit();
  } catch (e) {
    mutationError.value = e?.message || "保存记忆失败";
  } finally {
    savingId.value = "";
  }
}

async function deleteEntry(entry) {
  if (!entry?.id || deletingId.value) return;
  if (typeof window !== "undefined" && !window.confirm("确定删除这条长期记忆吗？")) return;
  deletingId.value = entry.id;
  mutationError.value = "";
  try {
    const result = await api.deleteAgentMemoryEntry(props.agentId, viewScope.value, entry.id);
    applyContext(result?.prompt_context || result);
    if (editingId.value === entry.id) cancelEdit();
  } catch (e) {
    mutationError.value = e?.message || "删除记忆失败";
  } finally {
    deletingId.value = "";
  }
}

async function refresh() {
  await load({ silent: true });
}

watch(
  () => props.agentId,
  () => {
    cancelEdit();
    mutationError.value = "";
    viewScope.value = currentScope.value;
    void load();
  },
);

watch(
  () => props.scope,
  (value) => {
    cancelEdit();
    mutationError.value = "";
    viewScope.value = normalizeScope(value);
    void load({ silent: true });
  },
);

onMounted(() => {
  void load();
});

defineExpose({ refresh });
</script>

<template>
  <section class="memory-panel">
    <div class="memory-panel__header">
      <div>
        <h3 class="memory-panel__title">长期记忆</h3>
        <p class="memory-panel__description">查看并维护该智能体可使用的结构化记忆条目。</p>
      </div>
      <button
        type="button"
        class="btn btn--ghost btn--sm"
        :disabled="loading || refreshing"
        @click="refresh"
      >
        {{ refreshing ? "刷新中…" : "刷新" }}
      </button>
    </div>

    <div class="memory-panel__meta">
      <span class="memory-panel__scope">当前生效：{{ currentScopeLabel }}</span>
      <span class="memory-panel__count">{{ visibleEntries.length }} 条</span>
    </div>

    <div class="memory-panel__tabs" role="tablist" aria-label="记忆作用域">
      <button
        type="button"
        class="memory-panel__tab"
        :class="{ 'memory-panel__tab--active': viewScope === 'agent' }"
        :aria-selected="viewScope === 'agent'"
        role="tab"
        @click="viewScope = 'agent'"
      >
        本智能体 <span>{{ agentEntries.length }}</span>
      </button>
      <button
        type="button"
        class="memory-panel__tab"
        :class="{ 'memory-panel__tab--active': viewScope === 'global' }"
        :aria-selected="viewScope === 'global'"
        role="tab"
        @click="viewScope = 'global'"
      >
        全局 <span>{{ globalEntries.length }}</span>
      </button>
    </div>

    <p v-if="viewScope !== currentScope" class="memory-panel__hint">
      当前配置使用的是{{ currentScopeLabel }}记忆；这里可以查看另一作用域的内容。
    </p>

    <p v-if="mutationError" class="memory-panel__mutation-error">{{ mutationError }}</p>

    <p v-if="loading" class="memory-panel__state">加载中…</p>
    <div v-else-if="error" class="memory-panel__error">
      <span>{{ error }}</span>
      <button type="button" class="btn btn--ghost btn--sm" @click="refresh">重试</button>
    </div>
    <p v-else-if="!visibleEntries.length" class="memory-panel__state">当前作用域暂无记忆条目。</p>
    <ul v-else class="memory-panel__list">
      <li v-for="entry in visibleEntries" :key="entry.id || entry.content" class="memory-panel__item">
        <textarea
          v-if="editingId === entry.id"
          v-model="editContent"
          class="memory-panel__editor"
          rows="3"
          aria-label="编辑记忆内容"
          :disabled="savingId === entry.id"
        />
        <div v-else class="memory-panel__content">{{ entry.content }}</div>
        <div class="memory-panel__item-meta">
          <div class="memory-panel__item-info">
            <span>最后更新 {{ formatDate(entry.updatedAt || entry.createdAt) }}</span>
            <span v-if="entry.id" class="memory-panel__id">{{ entry.id }}</span>
          </div>
          <div v-if="entry.id" class="memory-panel__item-actions">
            <template v-if="editingId === entry.id">
              <button
                type="button"
                class="btn btn--primary btn--xs"
                :disabled="savingId === entry.id"
                @click="saveEdit(entry)"
              >
                {{ savingId === entry.id ? "保存中…" : "保存" }}
              </button>
              <button type="button" class="btn btn--ghost btn--xs" :disabled="savingId === entry.id" @click="cancelEdit">
                取消
              </button>
            </template>
            <template v-else>
              <button type="button" class="btn btn--ghost btn--xs" @click="startEdit(entry)">编辑</button>
              <button
                type="button"
                class="btn btn--ghost btn--xs memory-panel__delete"
                :disabled="deletingId === entry.id"
                @click="deleteEntry(entry)"
              >
                {{ deletingId === entry.id ? "删除中…" : "删除" }}
              </button>
            </template>
          </div>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.memory-panel {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid color-mix(in srgb, var(--color-border) 80%, transparent);
}

.memory-panel__header,
.memory-panel__meta,
.memory-panel__error,
.memory-panel__item-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.memory-panel__mutation-error {
  margin: 10px 0 0;
  font-size: 12px;
  line-height: 1.45;
  color: var(--color-danger);
}

.memory-panel__title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--color-text);
}

.memory-panel__description,
.memory-panel__hint,
.memory-panel__state,
.memory-panel__error {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.45;
  color: var(--color-text-subtle);
}

.memory-panel__meta {
  justify-content: flex-start;
  margin-top: 12px;
  font-size: 11.5px;
}

.memory-panel__scope {
  color: var(--color-primary);
}

.memory-panel__count {
  color: var(--color-text-subtle);
}

.memory-panel__tabs {
  display: flex;
  gap: 4px;
  margin-top: 12px;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--color-border);
}

.memory-panel__tab {
  border: 0;
  border-bottom: 2px solid transparent;
  padding: 7px 10px;
  background: transparent;
  color: var(--color-text-subtle);
  font-size: 12px;
  cursor: pointer;
}

.memory-panel__tab span {
  margin-left: 4px;
  color: var(--color-text-muted);
}

.memory-panel__tab:hover,
.memory-panel__tab--active {
  color: var(--color-primary);
}

.memory-panel__tab--active {
  border-bottom-color: var(--color-primary);
}

.memory-panel__hint {
  margin-top: 10px;
}

.memory-panel__error {
  align-items: center;
  justify-content: flex-start;
  color: var(--color-danger);
}

.memory-panel__list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 12px 0 0;
  padding: 0;
  list-style: none;
}

.memory-panel__item {
  padding: 10px 12px;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface-muted);
}

.memory-panel__content {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 13px;
  line-height: 1.55;
  color: var(--color-text);
}

.memory-panel__editor {
  display: block;
  width: 100%;
  min-height: 72px;
  box-sizing: border-box;
  resize: vertical;
  border: 1px solid var(--color-primary);
  border-radius: 6px;
  padding: 8px 10px;
  background: var(--color-surface, #fff);
  color: var(--color-text);
  font: inherit;
  line-height: 1.55;
}

.memory-panel__item-meta {
  justify-content: flex-start;
  margin-top: 8px;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.memory-panel__item-info,
.memory-panel__item-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.memory-panel__item-actions {
  margin-left: auto;
}

.memory-panel__delete:hover:not(:disabled) {
  color: var(--color-danger);
}

.btn--xs {
  padding: 3px 7px;
  font-size: 11px;
}

.memory-panel__id {
  font-family: var(--font-mono);
  color: var(--color-text-muted);
}
</style>
