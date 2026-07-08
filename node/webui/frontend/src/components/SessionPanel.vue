<script setup>
import { ref, onMounted, computed } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { formatRelativeTime, sessionDisplayTitle, sessionRecordId } from "../utils/format.js";

const emit = defineEmits(["switch", "new", "delete"]);

const sessions = ref([]);
const loading = ref(false);
const deletingId = ref("");

function sessionSortTime(session) {
  const iso = session?.updated_at || session?.UpdatedAt;
  const ts = Date.parse(iso || "");
  if (Number.isFinite(ts)) return ts;
  if (session?.active || session?.Active) return Date.now();
  return 0;
}

const sortedSessions = computed(() => {
  const currentId = String(sessionStore.sessionId || "").trim();
  return [...sessions.value].sort((a, b) => {
    const aId = sessionRecordId(a);
    const bId = sessionRecordId(b);
    const aCurrent = currentId && aId === currentId;
    const bCurrent = currentId && bId === currentId;
    if (aCurrent && !bCurrent) return -1;
    if (!aCurrent && bCurrent) return 1;
    return sessionSortTime(b) - sessionSortTime(a);
  });
});

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

function onDelete(session) {
  const id = sessionRecordId(session);
  if (!id || deletingId.value === id) return;
  emit("delete", { id, session });
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
      <div class="panel__title">历史会话</div>
      <button type="button" class="session-panel__icon-btn" title="新建会话" @click="createNew">+</button>
    </header>
    <div class="panel__body session-panel__body">
      <div v-if="loading" class="session-panel__loading">加载中…</div>
      <ul v-else class="session-history-list">
        <li
          v-for="s in sortedSessions"
          :key="sessionRecordId(s)"
          class="session-history-item"
          :class="{
            'session-history-item--active': sessionRecordId(s) === sessionStore.sessionId,
            'session-history-item--running': s.has_active_turn || s.HasActiveTurn,
          }"
          @click="select(sessionRecordId(s))"
        >
          <div class="session-history-item__main">
            <div class="session-history-item__title-row">
              <span class="session-history-item__title">{{ sessionDisplayTitle(s) }}</span>
              <span v-if="s.has_unread || s.HasUnread" class="session-history-item__badge session-history-item__badge--unread" title="有新消息">未读</span>
              <span
                v-if="s.has_pending_hitl || s.HasPendingHITL || (s.pending_hitl_items ?? s.PendingHITLItems) > 0"
                class="session-history-item__badge session-history-item__badge--hitl"
                title="待你确认"
              >待确认</span>
              <span v-if="s.active" class="session-history-item__badge session-history-item__badge--live">活跃</span>
              <span v-else-if="s.has_active_turn || s.HasActiveTurn" class="session-history-item__badge session-history-item__badge--running">进行中</span>
            </div>
            <div class="session-history-item__meta">
              <span class="session-history-item__count">{{ s.message_count ?? s.MessageCount ?? 0 }} 条</span>
              <span v-if="s.updated_at" class="session-history-item__time">{{ formatRelativeTime(s.updated_at) }}</span>
            </div>
          </div>
          <button
            type="button"
            class="session-history-item__delete"
            title="删除会话"
            :disabled="deletingId === sessionRecordId(s)"
            @click.stop="onDelete(s)"
          >
            {{ deletingId === sessionRecordId(s) ? "…" : "×" }}
          </button>
        </li>
        <li v-if="!sessions.length" class="session-panel__empty">暂无历史会话</li>
      </ul>
    </div>
  </section>
</template>
