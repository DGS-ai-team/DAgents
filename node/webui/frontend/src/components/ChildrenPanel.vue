<script setup>
import { computed, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { shortId } from "../utils/panelFormat.js";

const props = defineProps({
  sessionId: { type: String, default: "" },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const showRaw = ref(false);
const data = ref(null);

const items = computed(() => {
  const rows = data.value?.items;
  return Array.isArray(rows) ? rows : [];
});

async function load() {
  const sid = props.sessionId || sessionStore.sessionId;
  if (!sid) return;
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.listChildAgents(sid);
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

function isActive(status) {
  const s = String(status || "").toLowerCase();
  return s && !["completed", "failed", "cancelled", "expired"].includes(s);
}

onMounted(load);
watch(() => props.sessionId || sessionStore.sessionId, load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel children-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">Children</div>
        <div class="command-panel__subtitle">{{ sessionId || sessionStore.sessionId || "—" }}</div>
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
          <h3 class="command-section__title">临时 Agent ({{ items.length }})</h3>
          <ul v-if="items.length" class="command-card-list">
            <li
              v-for="(item, index) in items"
              :key="item.child_session_id"
              class="command-card"
              :class="{ 'command-card--active-child': isActive(item.status) }"
            >
              <div class="command-card__main">
                <div class="command-card__title">
                  {{ index + 1 }}. {{ shortId(item.child_session_id, 32) }}
                  <span class="command-card__badge" :class="isActive(item.status) ? 'command-card__badge--active' : ''">
                    {{ item.status || "unknown" }}
                  </span>
                </div>
                <dl class="command-kv-list command-kv-list--compact">
                  <div class="command-kv"><dt>Purpose</dt><dd>{{ item.purpose || "—" }}</dd></div>
                  <div class="command-kv">
                    <dt>Tools</dt>
                    <dd>{{ (item.allowed_tools || []).join(", ") || "—" }}</dd>
                  </div>
                  <div class="command-kv">
                    <dt>Turns</dt>
                    <dd>{{ item.turn_count ?? 0 }} / {{ item.max_turns ?? "—" }}</dd>
                  </div>
                  <div class="command-kv"><dt>Expires</dt><dd>{{ item.expires_at || "—" }}</dd></div>
                </dl>
              </div>
            </li>
          </ul>
          <p v-else class="command-panel__empty">当前 session 无活跃临时 Agent</p>
        </section>
      </template>
    </div>
  </section>
</template>
