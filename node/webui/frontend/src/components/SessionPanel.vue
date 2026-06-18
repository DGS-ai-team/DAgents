<script setup>
import { ref, onMounted } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { formatRelativeTime, sessionDisplayTitle } from "../utils/format.js";

const emit = defineEmits(["switch", "new", "delete"]);

const sessions = ref([]);
const loading = ref(false);
const deletingId = ref("");

async function refresh() {
  loading.value = true;
  try {
    const res = await api.listSessions();
    sessions.value = res.sessions || res.items || [];
  } catch {
    sessions.value = [];
  } finally {
    loading.value = false;
  }
}

function select(id) {
  emit("switch", id);
}

function createNew() {
  emit("new");
}

function onDelete(id) {
  if (!id || deletingId.value) return;
  emit("delete", id);
}

function setDeleting(id) {
  deletingId.value = id || "";
}

onMounted(refresh);

defineExpose({ refresh, setDeleting });
</script>

<template>
  <section class="panel session-panel">
    <header class="panel__header session-panel__header">
      <div>
        <div class="panel__title">历史会话</div>
        <div class="session-panel__subtitle">点击切换，+ 新建</div>
      </div>
      <button type="button" class="session-panel__icon-btn" title="新建会话" @click="createNew">+</button>
    </header>
    <div class="panel__body session-panel__body">
      <div v-if="loading" class="session-panel__loading">加载中…</div>
      <ul v-else class="session-history-list">
        <li
          v-for="s in sessions"
          :key="s.session_id"
          class="session-history-item"
          :class="{
            'session-history-item--active': s.session_id === sessionStore.sessionId,
            'session-history-item--running': s.has_active_turn || s.HasActiveTurn,
          }"
          @click="select(s.session_id)"
        >
          <div class="session-history-item__avatar" aria-hidden="true">
            <span v-if="s.has_active_turn || s.HasActiveTurn" class="session-history-item__pulse" />
            {{ sessionDisplayTitle(s).slice(0, 1).toUpperCase() }}
          </div>
          <div class="session-history-item__main">
            <div class="session-history-item__title-row">
              <span class="session-history-item__title">{{ sessionDisplayTitle(s) }}</span>
              <span v-if="s.active" class="session-history-item__badge session-history-item__badge--live">活跃</span>
              <span v-else-if="s.has_active_turn || s.HasActiveTurn" class="session-history-item__badge">进行中</span>
            </div>
            <div class="session-history-item__meta">
              <span class="session-history-item__count">{{ s.message_count ?? s.MessageCount ?? 0 }} 条</span>
              <span v-if="s.updated_at" class="session-history-item__time">{{ formatRelativeTime(s.updated_at) }}</span>
              <span class="session-history-item__id" :title="s.session_id">{{ s.session_id?.slice(0, 12) }}…</span>
            </div>
          </div>
          <button
            type="button"
            class="session-history-item__delete"
            title="删除会话"
            :disabled="deletingId === s.session_id"
            @click.stop="onDelete(s.session_id)"
          >
            {{ deletingId === s.session_id ? "…" : "×" }}
          </button>
        </li>
        <li v-if="!sessions.length" class="session-panel__empty">暂无历史会话</li>
      </ul>
    </div>
  </section>
</template>
