<script setup>
import { computed, onMounted, ref, watch } from "vue";
import {
  createDiscoveryGroup,
  deleteDiscoveryGroup,
  fetchAgents,
  fetchDiscoveryGroups,
  saveAgentGroups,
} from "../api.js";
import { statusPillClass } from "../utils.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const loading = ref(false);
const saving = ref(false);
const groups = ref([]);
const agents = ref([]);
const selectedGroup = ref("");
const createName = ref("");
const creating = ref(false);
const selectedNodeIds = ref([]);
const groupQuery = ref("");
const nodeQuery = ref("");

const selectedMeta = computed(() => groups.value.find((g) => g.name === selectedGroup.value) || null);

const filteredGroups = computed(() => {
  const q = groupQuery.value.trim().toLowerCase();
  const list = [...(groups.value || [])];
  list.sort((a, b) => String(a.name || "").localeCompare(String(b.name || ""), "zh"));
  if (!q) return list;
  return list.filter((g) => String(g.name || "").toLowerCase().includes(q));
});

const agentRows = computed(() => {
  const list = [...(agents.value || [])];
  list.sort((a, b) =>
    String(a.name || a.agent_id || "").localeCompare(String(b.name || b.agent_id || ""), "zh"),
  );
  return list;
});

const filteredAgents = computed(() => {
  const q = nodeQuery.value.trim().toLowerCase();
  if (!q) return agentRows.value;
  return agentRows.value.filter((a) => {
    const hay = [
      a.name,
      a.agent_id,
      a.description,
      ...(Array.isArray(a.discovery_group) ? a.discovery_group : []),
    ]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();
    return hay.includes(q);
  });
});

const savedNodeIds = computed(() => {
  const name = selectedGroup.value;
  if (!name) return [];
  return agentRows.value.filter((a) => agentInGroup(a, name)).map((a) => a.agent_id);
});

const isDirty = computed(() => {
  if (!selectedGroup.value) return false;
  const a = [...selectedNodeIds.value].sort();
  const b = [...savedNodeIds.value].sort();
  if (a.length !== b.length) return true;
  return a.some((id, i) => id !== b[i]);
});

const membershipSummary = computed(() => {
  const saved = selectedMeta.value?.node_count ?? savedNodeIds.value.length;
  if (!isDirty.value) return `本组 ${saved} 个 Node`;
  return `已改未保存 · 勾选 ${selectedNodeIds.value.length} 个（原 ${saved} 个）`;
});

const groupListMeta = computed(() => {
  if (!groupQuery.value.trim()) return String(groups.value.length);
  return `${filteredGroups.value.length}/${groups.value.length}`;
});

const nodeListMeta = computed(() => {
  if (!nodeQuery.value.trim()) return `${agentRows.value.length} 个 Node`;
  return `显示 ${filteredAgents.value.length}/${agentRows.value.length}`;
});

function agentInGroup(agent, groupName) {
  const gs = Array.isArray(agent?.discovery_group) ? agent.discovery_group : [];
  return gs.includes(groupName);
}

function statusLabel(status) {
  if (status === "online") return "在线";
  if (status === "offline") return "离线";
  return status || "—";
}

function otherGroups(agent) {
  const name = selectedGroup.value;
  const gs = Array.isArray(agent?.discovery_group) ? agent.discovery_group : [];
  return gs.filter((g) => g && g !== name);
}

async function loadAll() {
  loading.value = true;
  try {
    const [groupList, agentPage] = await Promise.all([
      fetchDiscoveryGroups(),
      fetchAgents({ page: 1, page_size: 200, status: "all" }),
    ]);
    groups.value = Array.isArray(groupList) ? groupList : [];
    agents.value = Array.isArray(agentPage?.agents) ? agentPage.agents : [];
    if (selectedGroup.value && !groups.value.some((g) => g.name === selectedGroup.value)) {
      selectedGroup.value = "";
      selectedNodeIds.value = [];
    }
    if (!selectedGroup.value && groups.value.length) {
      const sorted = [...groups.value].sort((a, b) =>
        String(a.name || "").localeCompare(String(b.name || ""), "zh"),
      );
      selectedGroup.value = sorted[0].name;
    }
    if (selectedGroup.value) {
      syncSelectionFromGroup();
    }
  } catch (err) {
    emit("toast", { message: err.message || "加载发现组失败", type: "error" });
  } finally {
    loading.value = false;
  }
}

function syncSelectionFromGroup() {
  const name = selectedGroup.value;
  if (!name) {
    selectedNodeIds.value = [];
    return;
  }
  selectedNodeIds.value = agentRows.value
    .filter((a) => agentInGroup(a, name))
    .map((a) => a.agent_id);
}

function selectGroup(name, { force = false } = {}) {
  if (!force && isDirty.value && selectedGroup.value && selectedGroup.value !== name) {
    if (!window.confirm("当前修改尚未保存，切换组将丢弃改动。继续？")) return;
  }
  selectedGroup.value = name;
  nodeQuery.value = "";
  syncSelectionFromGroup();
}

function toggleNode(agentId) {
  const set = new Set(selectedNodeIds.value);
  if (set.has(agentId)) set.delete(agentId);
  else set.add(agentId);
  selectedNodeIds.value = [...set];
}

async function onCreateGroup() {
  const name = createName.value.trim();
  if (!name || creating.value) return;
  creating.value = true;
  try {
    await createDiscoveryGroup(name);
    createName.value = "";
    groupQuery.value = "";
    emit("toast", { message: `已创建发现组 ${name}`, type: "success" });
    await loadAll();
    selectGroup(name, { force: true });
  } catch (err) {
    emit("toast", { message: err.message || "创建失败", type: "error" });
  } finally {
    creating.value = false;
  }
}

async function onDeleteGroup() {
  const name = selectedGroup.value;
  if (!name || saving.value) return;
  if (!window.confirm(`删除发现组「${name}」？\n将从所有 Node 上移除该组。`)) return;
  saving.value = true;
  try {
    await deleteDiscoveryGroup(name, { detachNodes: true });
    selectedGroup.value = "";
    selectedNodeIds.value = [];
    emit("toast", { message: `已删除发现组 ${name}`, type: "success" });
    await loadAll();
  } catch (err) {
    emit("toast", { message: err.message || "删除失败", type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onSaveMembership() {
  const name = selectedGroup.value;
  if (!name || saving.value || !isDirty.value) return;
  saving.value = true;
  try {
    const want = new Set(selectedNodeIds.value);
    const tasks = [];
    for (const agent of agentRows.value) {
      const id = agent.agent_id;
      const current = Array.isArray(agent.discovery_group) ? [...agent.discovery_group] : [];
      const has = current.includes(name);
      const should = want.has(id);
      if (has === should) continue;
      let next = current;
      if (should) next = [...current, name];
      else next = current.filter((g) => g !== name);
      tasks.push(saveAgentGroups(id, next));
    }
    await Promise.all(tasks);
    emit("toast", { message: `已更新「${name}」的 Node 关联`, type: "success" });
    await loadAll();
    selectGroup(name, { force: true });
  } catch (err) {
    emit("toast", { message: err.message || "保存关联失败", type: "error" });
  } finally {
    saving.value = false;
  }
}

watch(
  () => props.active,
  (active) => {
    if (active) void loadAll();
  },
);

onMounted(() => {
  if (props.active) void loadAll();
});
</script>

<template>
  <section class="permissions-page" aria-label="发现组">
    <p v-if="loading" class="state">加载中…</p>

    <div v-else class="permissions-layout">
      <aside class="permissions-groups panel">
        <div class="permissions-groups__head">
          <h3>全部组</h3>
          <span class="muted">{{ groupListMeta }}</span>
        </div>
        <form class="permissions-create" @submit.prevent="onCreateGroup">
          <input
            v-model="createName"
            type="text"
            placeholder="新组名，例如 ops"
            :disabled="creating"
          />
          <button type="submit" class="btn btn-primary btn-sm" :disabled="creating || !createName.trim()">
            {{ creating ? "创建中…" : "创建" }}
          </button>
        </form>
        <input
          v-model="groupQuery"
          type="search"
          class="permissions-filter"
          placeholder="搜索组名…"
          autocomplete="off"
        />
        <ul class="permissions-group-list">
          <li v-if="!groups.length" class="muted permissions-empty">暂无组，请先创建</li>
          <li v-else-if="!filteredGroups.length" class="muted permissions-empty">无匹配组</li>
          <li v-for="g in filteredGroups" :key="g.name">
            <button
              type="button"
              class="permissions-group-item"
              :class="{ active: selectedGroup === g.name }"
              @click="selectGroup(g.name)"
            >
              <strong>{{ g.name }}</strong>
              <span class="muted">{{ g.node_count }} 个 Node</span>
            </button>
          </li>
        </ul>
      </aside>

      <div v-if="!selectedGroup" class="permissions-idle">
        <p class="permissions-idle__title">尚未选择组</p>
        <p class="permissions-idle__hint">
          {{ groups.length ? "从左侧点选一个组，勾选要关联的 Node" : "先在左侧创建一个发现组" }}
        </p>
      </div>

      <section v-else class="permissions-detail panel">
        <header class="permissions-detail__head">
          <div class="permissions-detail__title">
            <h3>{{ selectedGroup }}</h3>
            <p class="permissions-detail__summary" :class="{ dirty: isDirty }">
              {{ membershipSummary }}
              <span v-if="nodeQuery.trim()" class="permissions-detail__filter-hint">
                · {{ nodeListMeta }}
              </span>
            </p>
          </div>
          <div class="permissions-detail__actions">
            <button
              type="button"
              class="btn btn-danger btn-sm"
              :disabled="saving"
              @click="onDeleteGroup"
            >
              删除组
            </button>
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="saving || !isDirty"
              @click="onSaveMembership"
            >
              {{ saving ? "保存中…" : "保存关联" }}
            </button>
          </div>
        </header>

        <input
          v-model="nodeQuery"
          type="search"
          class="permissions-filter"
          placeholder="搜索 Node 名称 / ID / 描述 / 所属组…"
          autocomplete="off"
        />

        <ul class="permissions-node-list">
          <li v-if="!agentRows.length" class="muted permissions-empty">暂无已注册 Node</li>
          <li v-else-if="!filteredAgents.length" class="muted permissions-empty">无匹配 Node</li>
          <li
            v-for="agent in filteredAgents"
            :key="agent.agent_id"
            class="permissions-node-item"
          >
            <label class="permissions-node-check">
              <input
                type="checkbox"
                :checked="selectedNodeIds.includes(agent.agent_id)"
                :disabled="saving"
                @change="toggleNode(agent.agent_id)"
              />
              <span class="permissions-node-main">
                <span class="permissions-node-name">{{ agent.name || agent.agent_id }}</span>
                <span class="permissions-node-sub">
                  <code class="mono">{{ agent.agent_id }}</code>
                  <template v-if="otherGroups(agent).length">
                    · 另属 {{ otherGroups(agent).join(", ") }}
                  </template>
                </span>
              </span>
            </label>
            <span class="pill" :class="statusPillClass(agent.status)">
              {{ statusLabel(agent.status) }}
            </span>
          </li>
        </ul>
      </section>
    </div>
  </section>
</template>

<style scoped>
.permissions-page {
  display: flex;
  flex-direction: column;
  gap: 0;
  min-height: 0;
  flex: 1;
}

.permissions-layout {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(220px, 280px) 1fr;
  gap: 14px;
  align-items: stretch;
}

.permissions-groups,
.permissions-detail {
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
  margin-bottom: 0;
}

.permissions-idle {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 0;
  height: 100%;
  padding: 32px 24px;
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--bg-surface) 55%, transparent);
  text-align: center;
}

.permissions-idle__title {
  margin: 0;
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--text-secondary);
}

.permissions-idle__hint {
  margin: 0;
  max-width: 22rem;
  font-size: 0.85rem;
  color: var(--text-muted);
  line-height: 1.5;
}

.permissions-groups__head,
.permissions-detail__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.permissions-groups__head h3,
.permissions-detail__head h3 {
  margin: 0;
  font-size: 0.95rem;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.permissions-detail__title {
  min-width: 0;
}

.permissions-detail__summary {
  margin: 4px 0 0;
  font-size: 0.8rem;
  color: var(--text-muted);
}

.permissions-detail__summary.dirty {
  color: var(--warning);
  font-weight: 550;
}

.permissions-detail__filter-hint {
  font-weight: 400;
  color: var(--text-muted);
}

.permissions-create {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}

.permissions-create input {
  flex: 1;
  min-width: 0;
  padding: 7px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text);
  font: inherit;
  font-size: 0.9rem;
}

.permissions-filter {
  display: block;
  flex: 0 0 auto;
  width: 100%;
  box-sizing: border-box;
  height: 36px;
  margin-bottom: 10px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text);
  font: inherit;
  font-size: 0.9rem;
  line-height: 34px;
}

.permissions-create input:focus,
.permissions-filter:focus {
  outline: none;
  border-color: var(--primary);
  box-shadow: 0 0 0 3px var(--focus-ring);
}

.permissions-group-list,
.permissions-node-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.permissions-empty {
  padding: 8px 2px;
  font-size: 0.875rem;
}

.permissions-group-item {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding: 8px 10px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
  font: inherit;
}

.permissions-group-item:hover {
  background: var(--bg-hover);
}

.permissions-group-item.active {
  border-color: color-mix(in srgb, var(--primary) 35%, var(--border));
  background: var(--primary-soft);
}

.permissions-detail__actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.permissions-node-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  background: var(--bg-surface);
}

.permissions-node-check {
  display: inline-flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 0;
  cursor: pointer;
}

.permissions-node-check input[type="checkbox"] {
  margin-top: 3px;
  flex-shrink: 0;
  width: 15px;
  height: 15px;
  accent-color: var(--primary);
  cursor: pointer;
}

.permissions-node-main {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.permissions-node-name {
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--text);
  line-height: 1.3;
}

.permissions-node-sub {
  font-size: 0.78rem;
  color: var(--text-muted);
  line-height: 1.35;
  word-break: break-all;
}

.permissions-node-sub .mono {
  font-size: inherit;
  color: var(--text-secondary);
  background: transparent;
  padding: 0;
}

@media (max-width: 840px) {
  .permissions-layout {
    grid-template-columns: 1fr;
    min-height: 0;
  }

  .permissions-groups,
  .permissions-detail,
  .permissions-idle {
    height: auto;
    min-height: 280px;
  }

  .permissions-detail__head {
    flex-direction: column;
  }

  .permissions-group-list,
  .permissions-node-list {
    max-height: 360px;
  }
}
</style>
