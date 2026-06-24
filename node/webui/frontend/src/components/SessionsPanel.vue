<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { shortId, truncateText } from "../utils/panelFormat.js";

const emit = defineEmits(["close", "switch"]);

const loading = ref(false);
const error = ref("");
const showRaw = ref(false);
const data = ref(null);

const sessions = computed(() => {
  const rows = data.value?.sessions;
  return Array.isArray(rows) ? rows : [];
});

const activeSessions = computed(() => sessions.value.filter((s) => s.active));
const persistedSessions = computed(() => sessions.value.filter((s) => !s.active));

async function load() {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.listSessions();
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

function isCurrent(id) {
  return String(id || "") === String(sessionStore.sessionId || "");
}

function sessionMeta(s) {
  let meta = `msgs=${s.message_count ?? 0}`;
  if (s.active) {
    meta += ` · pending=${s.queue_pending ?? 0} · ${s.run_turn_phase || "idle"}`;
  } else if (s.updated_at) {
    meta += ` · ${s.updated_at}`;
  }
  return meta;
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel sessions-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">Sessions</div>
        <div class="command-panel__subtitle">当前 · {{ sessionStore.sessionId || "—" }}</div>
      </div>
      <div class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" @click="showRaw = !showRaw">
          {{ showRaw ? "友好视图" : "JSON" }}
        </button>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
        <button type="button" class="btn btn--ghost btn--sm" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <pre v-else-if="showRaw && data" class="command-panel__raw">{{ JSON.stringify(data, null, 2) }}</pre>
      <template v-else-if="data">
        <section class="command-section">
          <h3 class="command-section__title">内存中 ({{ activeSessions.length }})</h3>
          <ul v-if="activeSessions.length" class="command-card-list">
            <li
              v-for="s in activeSessions"
              :key="s.session_id"
              class="command-card"
              :class="{ 'command-card--current': isCurrent(s.session_id) }"
            >
              <div class="command-card__main">
                <div class="command-card__title">
                  {{ shortId(s.session_id, 36) }}
                  <span v-if="isCurrent(s.session_id)" class="command-card__badge">当前</span>
                  <span class="command-card__badge command-card__badge--active">active</span>
                </div>
                <div class="command-card__meta">{{ sessionMeta(s) }}</div>
              </div>
              <button
                v-if="!isCurrent(s.session_id)"
                type="button"
                class="btn btn--primary btn--sm"
                @click="emit('switch', s.session_id)"
              >
                切换
              </button>
            </li>
          </ul>
          <p v-else class="command-panel__empty">无活跃 session</p>
        </section>

        <section class="command-section">
          <h3 class="command-section__title">已持久化 ({{ persistedSessions.length }})</h3>
          <ul v-if="persistedSessions.length" class="command-card-list">
            <li
              v-for="s in persistedSessions"
              :key="s.session_id"
              class="command-card"
              :class="{ 'command-card--current': isCurrent(s.session_id) }"
            >
              <div class="command-card__main">
                <div class="command-card__title">
                  {{ shortId(s.session_id, 36) }}
                  <span v-if="isCurrent(s.session_id)" class="command-card__badge">当前</span>
                </div>
                <div class="command-card__meta">{{ sessionMeta(s) }}</div>
                <div v-if="s.first_user_message" class="command-card__preview">
                  {{ truncateText(s.first_user_message, 80) }}
                </div>
              </div>
              <button
                v-if="!isCurrent(s.session_id)"
                type="button"
                class="btn btn--primary btn--sm"
                @click="emit('switch', s.session_id)"
              >
                切换
              </button>
            </li>
          </ul>
          <p v-else class="command-panel__empty">无持久化 session</p>
        </section>
      </template>
    </div>
  </section>
</template>
