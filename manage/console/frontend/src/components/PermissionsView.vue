<script setup>
import { computed, onMounted, ref, watch } from "vue";
import {
  createDiscoveryGroup,
  deleteDiscoveryGroup,
  fetchAgents,
  fetchDiscoveryGroups,
  saveAgentGroups,
} from "../api.js";

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

const selectedMeta = computed(() => groups.value.find((g) => g.name === selectedGroup.value) || null);

const agentRows = computed(() => {
  const list = [...(agents.value || [])];
  list.sort((a, b) => String(a.agent_id || "").localeCompare(String(b.agent_id || "")));
  return list;
});

function agentInGroup(agent, groupName) {
  const gs = Array.isArray(agent?.discovery_group) ? agent.discovery_group : [];
  return gs.includes(groupName);
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
    if (selectedGroup.value) {
      syncSelectionFromGroup();
    }
  } catch (err) {
    emit("toast", { message: err.message || "加载权限数据失败", type: "error" });
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

function selectGroup(name) {
  selectedGroup.value = name;
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
    emit("toast", { message: `已创建组 ${name}`, type: "success" });
    await loadAll();
    selectGroup(name);
  } catch (err) {
    emit("toast", { message: err.message || "创建组失败", type: "error" });
  } finally {
    creating.value = false;
  }
}

async function onDeleteGroup() {
  const name = selectedGroup.value;
  if (!name || saving.value) return;
  if (!window.confirm(`删除组「${name}」？\n将从所有 Node 上移除该标签。`)) return;
  saving.value = true;
  try {
    await deleteDiscoveryGroup(name, { detachNodes: true });
    selectedGroup.value = "";
    selectedNodeIds.value = [];
    emit("toast", { message: `已删除组 ${name}`, type: "success" });
    await loadAll();
  } catch (err) {
    emit("toast", { message: err.message || "删除组失败", type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onSaveMembership() {
  const name = selectedGroup.value;
  if (!name || saving.value) return;
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
    emit("toast", { message: `已更新组「${name}」的 Node 关联`, type: "success" });
    await loadAll();
    selectGroup(name);
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
  <section class="permissions-page" aria-label="权限管理">
    <div class="permissions-intro">
      <h2 class="panel-title">权限管理</h2>
      <p class="muted">
        管理 discovery_group：创建组、把 Node 挂到组。Node 登录后只能看到同组节点，并在工作组中把同组 Node
        选为成员 Home。工作组 ACL 仍独立生效。
      </p>
    </div>

    <p v-if="loading" class="state">加载中…</p>

    <div v-else class="permissions-layout">
      <aside class="permissions-groups panel">
        <div class="permissions-groups__head">
          <h3>发现组</h3>
          <span class="muted">{{ groups.length }}</span>
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
        <ul class="permissions-group-list">
          <li v-if="!groups.length" class="muted">暂无组，请先创建</li>
          <li v-for="g in groups" :key="g.name">
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

      <section class="permissions-detail panel">
        <template v-if="!selectedGroup">
          <p class="muted">选择左侧组以关联 Node，或先创建一个组。</p>
        </template>
        <template v-else>
          <header class="permissions-detail__head">
            <div>
              <h3>{{ selectedGroup }}</h3>
              <p class="muted">
                已选 {{ selectedNodeIds.length }} 个 Node
                <template v-if="selectedMeta"> · 当前已挂 {{ selectedMeta.node_count }} 个</template>
              </p>
            </div>
            <div class="permissions-detail__actions">
              <button type="button" class="btn btn-ghost btn-sm" :disabled="saving" @click="onDeleteGroup">
                删除组
              </button>
              <button type="button" class="btn btn-primary btn-sm" :disabled="saving" @click="onSaveMembership">
                {{ saving ? "保存中…" : "保存关联" }}
              </button>
            </div>
          </header>

          <ul class="permissions-node-list">
            <li v-if="!agentRows.length" class="muted">暂无已注册 Node</li>
            <li v-for="agent in agentRows" :key="agent.agent_id" class="permissions-node-item">
              <label class="permissions-node-check">
                <input
                  type="checkbox"
                  :checked="selectedNodeIds.includes(agent.agent_id)"
                  :disabled="saving"
                  @change="toggleNode(agent.agent_id)"
                />
                <span class="permissions-node-id">{{ agent.name || agent.agent_id }}</span>
              </label>
              <span class="muted permissions-node-meta">
                {{ agent.agent_id }}
                ·
                {{ agent.status || "—" }}
                ·
                {{ (agent.discovery_group || []).join(", ") || "未分组" }}
              </span>
            </li>
          </ul>
        </template>
      </section>
    </div>
  </section>
</template>

<style scoped>
.permissions-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.permissions-intro .panel-title {
  margin: 0 0 6px;
}

.permissions-layout {
  display: grid;
  grid-template-columns: minmax(220px, 280px) 1fr;
  gap: 14px;
  min-height: 420px;
}

.permissions-groups,
.permissions-detail {
  padding: 14px 16px;
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
}

.permissions-create {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.permissions-create input {
  flex: 1;
  min-width: 0;
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text);
}

.permissions-group-list,
.permissions-node-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 520px;
  overflow: auto;
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
}

.permissions-node-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 10px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
}

.permissions-node-check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.permissions-node-id {
  font-family: var(--mono);
  font-size: 0.85rem;
}

.permissions-node-meta {
  font-size: 0.8rem;
  text-align: right;
}

@media (max-width: 840px) {
  .permissions-layout {
    grid-template-columns: 1fr;
  }
}
</style>
