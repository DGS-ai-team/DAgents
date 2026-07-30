<script setup>
import { ref, onMounted, computed } from "vue";
import { useRouter } from "vue-router";
import * as api from "../api/node.js";
import { agentStore } from "../stores/agent.js";
import {
  formatCompactRelativeTime,
  agentDisplayTitle,
  agentRecordId,
} from "../utils/format.js";
import { agentHostLabel } from "../utils/agentTemplateForm.js";

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
  const os = agentHostLabel(agent);
  if (agentOrigin(agent) === "remote") {
    return os ? `远端 · ${os}` : "远端 Agent";
  }
  return os ? `本地 · ${os}` : "本地 Agent";
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
      <div class="panel__title agent-panel__title">Agents</div>
      <button
        type="button"
        class="agent-panel__icon-btn"
        title="工作组"
        aria-label="工作组"
        @click="router.push({ name: 'workgroups' })"
      >
        WG
      </button>
      <button type="button" class="agent-panel__icon-btn" title="新建 Agent" aria-label="新建 Agent" @click="openCreate">
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
          <path d="M8 3v10M3 8h10" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
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
          <div class="agent-list-item__main">
            <input
              v-if="renamingId === agentRecordId(a)"
              v-model="renameDraft"
              class="agent-list-item__title-input"
              @click.stop
              @keydown.enter.prevent="commitRename(a)"
              @keydown.esc.prevent="renamingId = ''"
              @blur="commitRename(a)"
            />
            <span
              v-else
              class="agent-list-item__title"
              :title="agentDisplayTitle(a)"
              @dblclick.stop="startRename(a)"
            >{{ agentDisplayTitle(a) }}</span>
          </div>

          <div class="agent-list-item__trail">
            <span
              v-if="agentHostLabel(a)"
              class="agent-list-item__os"
              :title="agentOriginLabel(a)"
            >{{ agentHostLabel(a) }}</span>
            <span
              class="agent-list-item__glyph"
              :class="`agent-list-item__glyph--${agentOrigin(a)}`"
              :title="agentOriginLabel(a)"
              :aria-label="agentOriginLabel(a)"
            >
              <svg
                v-if="agentOrigin(a) === 'local'"
                viewBox="0 0 16 16"
                width="13"
                height="13"
                aria-hidden="true"
              >
                <circle cx="4.5" cy="8" r="1.4" fill="currentColor" />
                <circle cx="11.5" cy="4.5" r="1.4" fill="currentColor" />
                <circle cx="11.5" cy="11.5" r="1.4" fill="currentColor" />
                <path d="M5.7 7.3 10.2 5.1M5.7 8.7l4.5 2.2" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
              </svg>
              <svg
                v-else
                viewBox="0 0 16 16"
                width="13"
                height="13"
                aria-hidden="true"
              >
                <path
                  d="M5.2 12.2h6.1c1.5 0 2.7-1.2 2.7-2.6 0-1.3-1-2.4-2.3-2.6-.2-1.8-1.7-3.2-3.6-3.2-1.5 0-2.8.9-3.3 2.2-.2 0-.3-.1-.5-.1-1.3 0-2.3 1-2.3 2.3 0 1.3 1 2.4 2.2 2.4z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.25"
                  stroke-linejoin="round"
                />
              </svg>
            </span>
            <span
              v-if="a.sandbox_enabled"
              class="agent-list-item__glyph agent-list-item__glyph--sandbox"
              title="沙箱"
              aria-label="沙箱"
            >
              <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
                <rect x="3" y="4" width="10" height="9" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.25" />
                <path d="M6 4V3.2A2 2 0 0 1 8 1.2 2 2 0 0 1 10 3.2V4" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" />
              </svg>
            </span>
            <span
              v-if="a.updated_at"
              class="agent-list-item__time"
              :title="a.updated_at"
            >{{ formatCompactRelativeTime(a.updated_at) }}</span>
          </div>

          <div class="agent-list-item__actions" @click.stop>
            <button
              type="button"
              class="agent-list-item__action"
              title="Agent 配置"
              aria-label="Agent 配置"
              @click="openAgentSettings(a)"
            >
              <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                <circle cx="8" cy="8" r="2.2" fill="none" stroke="currentColor" stroke-width="1.25" />
                <path
                  d="M8 2.2v1.4M8 12.4v1.4M2.2 8h1.4M12.4 8h1.4M3.9 3.9l1 1M11.1 11.1l1 1M3.9 12.1l1-1M11.1 4.9l1-1"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.25"
                  stroke-linecap="round"
                />
              </svg>
            </button>
            <button
              type="button"
              class="agent-list-item__action"
              title="重命名"
              aria-label="重命名"
              @click="startRename(a)"
            >
              <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                <path d="M3.5 12.5 6 12l6.2-6.2a1.4 1.4 0 0 0-2-2L4 10l-.5 2.5Z" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linejoin="round" />
              </svg>
            </button>
            <button
              type="button"
              class="agent-list-item__action agent-list-item__action--danger"
              title="删除 Agent"
              aria-label="删除 Agent"
              :disabled="deletingId === agentRecordId(a)"
              @click="onDelete(a)"
            >
              <svg v-if="deletingId !== agentRecordId(a)" viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" />
              </svg>
              <span v-else>…</span>
            </button>
          </div>
        </li>
        <li v-if="!agents.length" class="agent-panel__empty">暂无 Agent，点击 + 创建</li>
      </ul>
    </div>
  </section>
</template>
