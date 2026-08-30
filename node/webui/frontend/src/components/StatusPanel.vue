<script setup>
import { onMounted, ref } from "vue";
import * as api from "../api/node.js";

defineProps({
  embedded: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const data = ref(null);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    data.value = { health: await api.getHealth() };
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section
    class="panel panel-overlay__card command-panel status-panel"
    :class="{ 'settings-embedded-panel': embedded }"
  >
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">运行状态</div>
        <div v-if="!embedded" class="command-panel__subtitle">当前 Node 与 Agent 状态</div>
      </div>
      <div v-if="!embedded" class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
      <button
        v-else
        type="button"
        class="btn btn--ghost btn--sm status-panel__refresh"
        :disabled="loading"
        :title="loading ? '正在刷新运行状态' : '刷新运行状态'"
        :aria-label="loading ? '正在刷新运行状态' : '刷新运行状态'"
        @click="load"
      >
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M15.8 7.2A6 6 0 1 0 16 12M15.8 7.2V3.8M15.8 7.2h-3.4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <template v-else-if="data">
        <div class="command-panel__stats">
          <div class="command-stat">
            <span class="command-stat__label">版本</span>
            <span class="command-stat__value">{{ data.health?.version || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">服务状态</span>
            <span class="command-stat__value status-panel__health">
              <span class="status-panel__health-dot" aria-hidden="true"></span>
              {{ data.health?.status === "ok" ? "运行正常" : (data.health?.status || "运行正常") }}
            </span>
          </div>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.status-panel__refresh { width: 30px; height: 30px; padding: 0; }
.status-panel__refresh svg { width: 15px; height: 15px; }
.status-panel__health { display: inline-flex; align-items: center; gap: 7px; }
.status-panel__health-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--color-success, #3d9a5f); }
</style>
