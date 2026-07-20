<script setup>
import { ref, onMounted, computed } from "vue";
import * as api from "../api/node.js";
import { agentStore } from "../stores/agent.js";
import { formatRelativeTime, agentDisplayTitle, agentRecordId } from "../utils/format.js";

const emit = defineEmits(["switch", "created", "delete", "create", "agents-updated"]);

const agents = ref([]);
const loading = ref(false);
const deletingId = ref("");
const renamingId = ref("");
const renameDraft = ref("");

function agentSortTime(agent) {
  const iso = agent?.updated_at || agent?.UpdatedAt;
  const ts = Date.parse(iso || "");
  return Number.isFinite(ts) ? ts : 0;
}

const sortedAgents = computed(() => {
  const currentId = String(agentStore.agentId || "").trim();
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

async function refresh() {
  loading.value = true;
  try {
    const res = await api.listAgents();
    agents.value = res.agents || [];
  } catch {
    agents.value = [];
  } finally {
    loading.value = false;
    emit("agents-updated", agents.value.slice());
  }
}

function select(id) {
  emit("switch", id);
}

function openCreate() {
  emit("create");
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
    agentStore.error = e.message;
  }
}

function setDeleting(id) {
  deletingId.value = id || "";
}

onMounted(refresh);

defineExpose({ refresh, setDeleting, openCreate });
</script>

<template>
  <section class="panel agent-panel">
    <header class="panel__header agent-panel__header">
      <div class="panel__title">我的 Agent</div>
      <button type="button" class="agent-panel__icon-btn" title="新建 Agent" @click="openCreate">+</button>
    </header>
    <div class="panel__body agent-panel__body">
      <div v-if="loading" class="agent-panel__loading">加载中…</div>
      <ul v-else class="agent-list">
        <li
          v-for="a in sortedAgents"
          :key="agentRecordId(a)"
          class="agent-list-item"
          :class="{
            'agent-list-item--active': agentRecordId(a) === agentStore.agentId,
          }"
          @click="select(agentRecordId(a))"
        >
          <div class="agent-list-item__main">
            <div class="agent-list-item__title-row">
              <input
                v-if="renamingId === agentRecordId(a)"
                v-model="renameDraft"
                class="agent-list-item__title-input"
                @click.stop
                @keydown.enter.prevent="commitRename(a)"
                @keydown.esc.prevent="renamingId = ''"
                @blur="commitRename(a)"
              />
              <span v-else class="agent-list-item__title" @dblclick.stop="startRename(a)">{{ agentDisplayTitle(a) }}</span>
            </div>
            <div class="agent-list-item__meta">
              <span class="agent-list-item__count">{{ a.template_id || "—" }}</span>
              <span v-if="a.sandbox_enabled" class="agent-list-item__badge agent-list-item__badge--live">沙箱</span>
              <span v-if="a.updated_at" class="agent-list-item__time">{{ formatRelativeTime(a.updated_at) }}</span>
            </div>
          </div>
          <div class="agent-list-item__actions">
            <button
              type="button"
              class="agent-list-item__edit"
              title="重命名"
              @click.stop="startRename(a)"
            >
              ✎
            </button>
            <button
              type="button"
              class="agent-list-item__delete"
              title="删除 Agent"
              :disabled="deletingId === agentRecordId(a)"
              @click.stop="onDelete(a)"
            >
              {{ deletingId === agentRecordId(a) ? "…" : "×" }}
            </button>
          </div>
        </li>
        <li v-if="!agents.length" class="agent-panel__empty">暂无 Agent，点击 + 从模板创建</li>
      </ul>
    </div>
  </section>
</template>
