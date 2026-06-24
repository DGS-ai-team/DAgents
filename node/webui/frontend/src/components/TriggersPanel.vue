<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";
import { formatTriggerCondition, formatUnixTime, shortId, truncateText } from "../utils/panelFormat.js";

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const showRaw = ref(false);
const data = ref(null);

const triggers = computed(() => {
  const rows = data.value?.triggers;
  return Array.isArray(rows) ? rows : [];
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.listTriggers();
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

function sessionHint(item) {
  let hint = String(item.session_target_mode || "—");
  if (item.target_session_id) hint += ` · ${shortId(item.target_session_id, 20)}`;
  return hint;
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel triggers-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">Triggers</div>
        <div class="command-panel__subtitle">已配置触发器</div>
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
          <h3 class="command-section__title">触发器 ({{ triggers.length }})</h3>
          <ul v-if="triggers.length" class="command-card-list">
            <li v-for="item in triggers" :key="item.trigger_id" class="command-card">
              <div class="command-card__main">
                <div class="command-card__title">
                  {{ item.name || "(未命名)" }}
                  <span
                    class="command-card__badge"
                    :class="item.enabled ? 'command-card__badge--active' : 'command-card__badge--muted'"
                  >
                    {{ item.enabled ? "enabled" : "disabled" }}
                  </span>
                </div>
                <div class="command-card__meta command-card__meta--mono">{{ shortId(item.trigger_id, 36) }}</div>
                <dl class="command-kv-list command-kv-list--compact">
                  <div class="command-kv">
                    <dt>调度</dt>
                    <dd>{{ formatTriggerCondition(item.condition) }}</dd>
                  </div>
                  <div class="command-kv">
                    <dt>下次</dt>
                    <dd>{{ formatUnixTime(item.next_fire_at) }}</dd>
                  </div>
                  <div class="command-kv">
                    <dt>触发</dt>
                    <dd>{{ item.fire_count ?? 0 }} 次 · 上次 {{ formatUnixTime(item.last_fired_at) }}</dd>
                  </div>
                  <div class="command-kv">
                    <dt>会话</dt>
                    <dd>{{ sessionHint(item) }}</dd>
                  </div>
                </dl>
                <div v-if="item.task_template" class="command-card__preview">
                  任务: {{ truncateText(item.task_template, 120) }}
                </div>
              </div>
            </li>
          </ul>
          <p v-else class="command-panel__empty">无已配置触发器</p>
        </section>
      </template>
    </div>
  </section>
</template>
