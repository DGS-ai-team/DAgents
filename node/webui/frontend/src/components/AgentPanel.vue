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

function agentHostTitle(agent) {
  const os = agentHostLabel(agent);
  return os ? `本机 · ${os}` : "本机 Agent";
}

const sortedAgents = computed(() => {
  return [...agents.value].sort((a, b) => agentSortTime(b) - agentSortTime(a));
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
      <span class="agent-panel__tab agent-panel__tab--active">Agents</span>
      <button
        type="button"
        class="agent-panel__tab"
        title="工作组"
        @click="router.push({ name: 'workgroups' })"
      >
        工作组
      </button>
      <button type="button" class="agent-panel__icon-btn" title="新建 Agent" aria-label="新建 Agent" @click="openCreate">
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
          <path d="M8 3v10M3 8h10" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" />
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
              :title="agentHostTitle(a)"
            >{{ agentHostLabel(a) }}</span>
            <span
              class="agent-list-item__glyph agent-list-item__glyph--local"
              :title="agentHostTitle(a)"
              :aria-label="agentHostTitle(a)"
            >
              <svg
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
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" aria-hidden="true">
                <path
                  d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"
                  stroke="currentColor"
                  stroke-width="1.75"
                />
                <path
                  d="M19.4 13.5a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V20a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H4a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V4a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.91 1 1.51 1H20a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
                  stroke="currentColor"
                  stroke-width="1.75"
                  stroke-linecap="round"
                  stroke-linejoin="round"
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
