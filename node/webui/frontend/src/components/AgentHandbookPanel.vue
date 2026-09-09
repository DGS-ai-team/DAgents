<script setup>
import { computed, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({ agentId: { type: String, required: true } });
const page = ref(null),
  selectedPath = ref(""),
  directoryDraft = ref(""),
  directoryLoaded = ref(false);
const history = ref([]),
  error = ref(""),
  notice = ref(""),
  loading = ref(false),
  saving = ref(false),
  restoring = ref(null);
let loadToken = 0,
  mutationToken = 0;
const breadcrumbs = computed(() => {
  const parts = selectedPath.value.split("/").filter(Boolean);
  return [
    { label: "根目录", path: "" },
    ...parts.map((part, i) => ({
      label: part,
      path: parts.slice(0, i + 1).join("/"),
    })),
  ];
});
function current(token, agentId) {
  return token === loadToken && agentId === props.agentId;
}
function reset() {
  loadToken += 1;
  mutationToken += 1;
  page.value = null;
  selectedPath.value = "";
  directoryDraft.value = "";
  directoryLoaded.value = false;
  history.value = [];
  error.value = "";
  notice.value = "";
  loading.value = false;
  saving.value = false;
  restoring.value = null;
}
async function load(path = "", { preserveError = false } = {}) {
  const token = ++loadToken,
    agentId = props.agentId;
  loading.value = true;
  if (!preserveError) error.value = "";
  notice.value = "";
  try {
    const data = await api.getAgentHandbook(agentId, path);
    if (!current(token, agentId)) return false;
    page.value = data || {};
    selectedPath.value = path;
    history.value = [];
    if (!directoryLoaded.value || path === "") {
      directoryDraft.value = data?.directory ?? "";
      directoryLoaded.value = true;
    }
    if (!data?.is_dir) {
      const result = await api.getAgentHandbookHistory(agentId, path);
      if (!current(token, agentId)) return false;
      history.value = result?.history || [];
    }
    return true;
  } catch (cause) {
    if (current(token, agentId)) error.value = cause?.message || "手册读取失败";
    return false;
  } finally {
    if (current(token, agentId)) loading.value = false;
  }
}
async function saveDirectory() {
  const token = ++mutationToken,
    agentId = props.agentId;
  saving.value = true;
  error.value = "";
  notice.value = "";
  loadToken += 1;
  try {
    await api.patchAgent(agentId, {
      handbook: { directory: directoryDraft.value },
    });
    if (token !== mutationToken || agentId !== props.agentId) return;
    notice.value = "手册目录已保存";
    directoryLoaded.value = false;
    await load("");
  } catch (cause) {
    if (token === mutationToken && agentId === props.agentId)
      error.value = cause?.message || "保存手册目录失败";
  } finally {
    if (token === mutationToken && agentId === props.agentId)
      saving.value = false;
  }
}
function openPath(path) {
  if (!saving.value && restoring.value === null) void load(path);
}
async function restore(entry) {
  const token = ++mutationToken,
    agentId = props.agentId,
    path = selectedPath.value;
  restoring.value = entry.revision;
  error.value = "";
  notice.value = "";
  loadToken += 1;
  try {
    await api.restoreAgentHandbook(agentId, {
      path,
      revision: entry.revision,
      expected_digest: page.value?.digest || "",
    });
    if (token !== mutationToken || agentId !== props.agentId) return;
    const parts = path.split("/").filter(Boolean);
    if (!entry.before_exists) parts.pop();
    await load(parts.join("/"));
  } catch (cause) {
    if (token === mutationToken && agentId === props.agentId)
      error.value =
        cause?.status === 409
          ? "文件已被修改，请刷新后重试"
          : cause?.message || "恢复失败";
  } finally {
    if (token === mutationToken && agentId === props.agentId)
      restoring.value = null;
  }
}
function refresh() {
  void load(selectedPath.value);
}
onMounted(() => void load());
watch(
  () => props.agentId,
  () => {
    reset();
    void load();
  },
);
</script>

<template>
  <section class="agent-handbook-panel">
    <div class="agent-handbook-panel__header">
      <div>
        <h3>Agent 手册</h3>
        <p class="agent-handbook-panel__intro">
          管理手册目录，浏览文件并恢复历史版本。
        </p>
      </div>
      <button
        class="btn btn--ghost btn--sm"
        :disabled="loading || saving || restoring !== null"
        @click="refresh"
      >
        {{ loading ? "刷新中…" : "刷新" }}
      </button>
    </div>
    <form
      class="agent-handbook-panel__directory"
      @submit.prevent="saveDirectory"
    >
      <label class="settings-field"
        ><span class="settings-field__label">手册目录</span
        ><input
          v-model="directoryDraft"
          class="settings-field__input"
          aria-label="手册目录"
          placeholder="可留空使用默认目录" /></label
      ><button
        class="btn btn--primary"
        type="submit"
        :disabled="saving || loading"
      >
        {{ saving ? "保存中…" : "保存目录" }}
      </button>
    </form>
    <p v-if="error" class="agent-detail__error" role="alert">{{ error }}</p>
    <p v-if="notice" class="agent-handbook-panel__notice" role="status">
      {{ notice }}
    </p>
    <div v-if="page" class="agent-handbook-panel__browser">
      <nav class="agent-handbook-panel__breadcrumbs" aria-label="手册路径">
        <button
          v-for="crumb in breadcrumbs"
          :key="crumb.path || 'root'"
          class="link-button"
          type="button"
          :disabled="crumb.path === selectedPath"
          @click="openPath(crumb.path)"
        >
          {{ crumb.label }}
        </button>
      </nav>
      <div v-if="loading" class="agent-handbook-panel__loading">读取中…</div>
      <template v-else-if="page.is_dir"
        ><div v-if="page.items?.length" class="agent-handbook-panel__items">
          <button
            v-for="item in page.items"
            :key="item.path"
            class="agent-handbook-panel__item"
            type="button"
            @click="openPath(item.path)"
          >
            <span>{{ item.name || item.path }}</span
            ><small>{{ item.is_dir ? "目录" : "文件" }}</small>
          </button>
        </div>
        <p v-else class="agent-handbook-panel__empty">此目录为空。</p></template
      >
      <template v-else>
        <pre class="agent-handbook-panel__content">{{
          page.content || ""
        }}</pre>
        <div class="agent-handbook-panel__history">
          <h4>历史版本</h4>
          <p v-if="!history.length" class="agent-handbook-panel__empty">
            暂无历史版本。
          </p>
          <div
            v-for="entry in history"
            :key="entry.revision"
            class="agent-handbook-panel__history-row"
          >
            <span>版本 {{ entry.revision }}</span
            ><button
              class="btn btn--ghost btn--sm"
              type="button"
              :disabled="restoring !== null || loading"
              @click="restore(entry)"
            >
              {{
                restoring === entry.revision
                  ? "恢复中…"
                  : entry.before_exists
                    ? "恢复至修改前"
                    : "撤销首次新建"
              }}
            </button>
          </div>
        </div></template
      >
    </div>
  </section>
</template>

<style scoped>
.agent-handbook-panel__header,
.agent-handbook-panel__directory,
.agent-handbook-panel__history-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.agent-handbook-panel__header h3 {
  margin-bottom: 4px;
}
.agent-handbook-panel__intro,
.agent-handbook-panel__empty {
  color: var(--text-secondary);
  font-size: 13px;
}
.agent-handbook-panel__directory {
  margin: 10px 0 14px;
}
.agent-handbook-panel__directory .settings-field {
  flex: 1;
  margin: 0;
}
.agent-handbook-panel__breadcrumbs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin: 14px 0;
}
.link-button {
  border: 0;
  background: none;
  color: var(--accent);
  cursor: pointer;
  padding: 3px 5px;
}
.link-button + .link-button::before {
  content: "/";
  color: var(--text-secondary);
  margin-right: 8px;
}
.agent-handbook-panel__items {
  display: grid;
  gap: 6px;
}
.agent-handbook-panel__item {
  display: flex;
  justify-content: space-between;
  text-align: left;
  border: 1px solid var(--border-subtle);
  background: transparent;
  color: inherit;
  padding: 10px 12px;
  cursor: pointer;
}
.agent-handbook-panel__item small {
  color: var(--text-secondary);
}
.agent-handbook-panel__content {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  border: 1px solid var(--border-subtle);
  border-radius: 4px;
  padding: 12px;
  min-height: 80px;
}
.agent-handbook-panel__history {
  margin-top: 18px;
  border-top: 1px solid var(--border-subtle);
  padding-top: 12px;
}
.agent-handbook-panel__history h4 {
  margin: 0 0 8px;
}
.agent-handbook-panel__history-row {
  padding: 8px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.agent-handbook-panel__notice {
  color: var(--success, #287a45);
}
@media (max-width: 640px) {
  .agent-handbook-panel__header,
  .agent-handbook-panel__directory,
  .agent-handbook-panel__history-row {
    align-items: stretch;
    flex-direction: column;
  }
  .agent-handbook-panel__directory .settings-field {
    width: 100%;
  }
}
</style>
