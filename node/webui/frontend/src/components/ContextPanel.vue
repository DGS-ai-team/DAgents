<script setup>
import { ref, watch, onMounted } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { formatNumber } from "../utils/markdown.js";

const props = defineProps({
  sessionId: { type: String, default: "" },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const ctx = ref(null);
const showRaw = ref(false);

async function load() {
  const sid = props.sessionId || sessionStore.sessionId;
  if (!sid) return;
  loading.value = true;
  error.value = "";
  try {
    ctx.value = await api.getSessionContext(sid);
  } catch (e) {
    error.value = e.message;
    ctx.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(() => props.sessionId || sessionStore.sessionId, load);
</script>

<template>
  <section class="panel panel-overlay__card context-panel">
    <header class="panel__header context-panel__header">
      <div>
        <div class="panel__title">Session Context</div>
        <div class="context-panel__subtitle">{{ sessionId || sessionStore.sessionId || "—" }}</div>
      </div>
      <div class="context-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" @click="showRaw = !showRaw">
          {{ showRaw ? "友好视图" : "JSON" }}
        </button>
        <button type="button" class="btn btn--ghost btn--sm" @click="load">刷新</button>
        <button type="button" class="btn btn--ghost btn--sm" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body context-panel__body">
      <div v-if="loading" class="context-panel__loading">加载中…</div>
      <div v-else-if="error" class="context-panel__error">{{ error }}</div>
      <pre v-else-if="showRaw && ctx" class="context-panel__raw">{{ JSON.stringify(ctx, null, 2) }}</pre>
      <template v-else-if="ctx">
        <div class="context-panel__stats">
          <div class="context-stat">
            <span class="context-stat__label">Messages</span>
            <span class="context-stat__value">{{ ctx.messages_count ?? 0 }}</span>
          </div>
          <div class="context-stat">
            <span class="context-stat__label">Tokens</span>
            <span class="context-stat__value">{{ formatNumber(ctx.messages_total_tokens ?? 0) }}</span>
          </div>
          <div class="context-stat">
            <span class="context-stat__label">Tool loops</span>
            <span class="context-stat__value">{{ ctx.tool_loop_count ?? 0 }}</span>
          </div>
          <div class="context-stat">
            <span class="context-stat__label">Turn</span>
            <span class="context-stat__value">{{ ctx.run_turn_phase || ctx.turn_state || "idle" }}</span>
          </div>
        </div>

        <section v-if="ctx.loaded_skills?.length" class="context-section">
          <h3 class="context-section__title">已加载 Skills</h3>
          <ul class="context-skill-list">
            <li v-for="sk in ctx.loaded_skills" :key="sk.name || sk.skill_name">{{ sk.name || sk.skill_name }}</li>
          </ul>
        </section>

        <section class="context-section">
          <h3 class="context-section__title">最近消息</h3>
          <ul class="context-message-list">
            <li v-for="(m, i) in ctx.recent_messages || []" :key="i" class="context-message-item">
              <span class="context-message-item__role">{{ m.role }}</span>
              <span class="context-message-item__text">{{ m.content }}</span>
            </li>
            <li v-if="!(ctx.recent_messages || []).length" class="context-panel__empty">暂无消息</li>
          </ul>
        </section>
      </template>
    </div>
  </section>
</template>
