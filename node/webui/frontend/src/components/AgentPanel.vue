<script setup>
import { ref, onMounted, computed } from "vue";
import { useRouter } from "vue-router";
import * as api from "../api/node.js";
import { agentStore } from "../stores/agent.js";
import { formatRelativeTime, agentDisplayTitle, agentRecordId } from "../utils/format.js";

const router = useRouter();
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

function agentOrigin(agent) {
  const raw = String(agent?.origin || "").trim().toLowerCase();
  return raw === "remote" ? "remote" : "local";
}

function agentOriginLabel(agent) {
  return agentOrigin(agent) === "remote" ? "远端 Agent" : "本地 Agent";
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

function openAgentSettings(agent) {
  const id = agentRecordId(agent);
  if (!id) return;
  router.push({ name: "settings-agent-detail", params: { agentId: id } });
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
            'agent-list-item--remote': agentOrigin(a) === 'remote',
          }"
          @click="select(agentRecordId(a))"
        >
          <span
            class="agent-list-item__origin"
            :class="`agent-list-item__origin--${agentOrigin(a)}`"
            :title="agentOriginLabel(a)"
            :aria-label="agentOriginLabel(a)"
          >
            <svg
              v-if="agentOrigin(a) === 'local'"
              viewBox="0 0 16 16"
              width="14"
              height="14"
              aria-hidden="true"
            >
              <rect x="2.5" y="2.5" width="11" height="8" rx="1.2" fill="none" stroke="currentColor" stroke-width="1.4" />
              <path d="M5.5 13.5h5M8 10.5v3" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
            </svg>
            <svg
              v-else
              viewBox="0 0 16 16"
              width="14"
              height="14"
              aria-hidden="true"
            >
              <path
                d="M5.2 12.2h6.1c1.5 0 2.7-1.2 2.7-2.6 0-1.3-1-2.4-2.3-2.6-.2-1.8-1.7-3.2-3.6-3.2-1.5 0-2.8.9-3.3 2.2-.2 0-.3-.1-.5-.1-1.3 0-2.3 1-2.3 2.3 0 1.3 1 2.4 2.2 2.4z"
                fill="none"
                stroke="currentColor"
                stroke-width="1.3"
                stroke-linejoin="round"
              />
            </svg>
          </span>
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
              <span v-if="a.sandbox_enabled" class="agent-list-item__badge agent-list-item__badge--live">沙箱</span>
              <span v-if="a.updated_at" class="agent-list-item__time">{{ formatRelativeTime(a.updated_at) }}</span>
            </div>
          </div>
          <div class="agent-list-item__actions">
            <button
              type="button"
              class="agent-list-item__edit"
              title="Agent 配置"
              @click.stop="openAgentSettings(a)"
            >
              ⚙
            </button>
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
