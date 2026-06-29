<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const data = ref(null);

const applyCommand = computed(() => {
  const cmd = String(data.value?.apply_command || "dagents update").trim();
  return cmd || "dagents update";
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.getAgentUpdate();
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

async function copyApplyCommand() {
  try {
    await navigator.clipboard.writeText(applyCommand.value);
  } catch {
    // ignore
  }
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel status-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">Update</div>
        <div class="command-panel__subtitle">Local Assistant 版本与 Release Hub 检查</div>
      </div>
      <div class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
        <button type="button" class="btn btn--ghost btn--sm" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <template v-else-if="data">
        <div class="command-panel__stats">
          <div class="command-stat">
            <span class="command-stat__label">当前版本</span>
            <span class="command-stat__value">{{ data.current_version || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">最新版本</span>
            <span class="command-stat__value">{{ data.latest_version || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">平台</span>
            <span class="command-stat__value">{{ data.platform || "—" }}</span>
          </div>
          <div class="command-stat">
            <span class="command-stat__label">Manage</span>
            <span class="command-stat__value">{{ data.manage_reachable ? "可达" : "不可达" }}</span>
          </div>
        </div>

        <p v-if="data.message" class="command-panel__hint">{{ data.message }}</p>

        <section v-if="data.release_notes" class="command-section">
          <h3 class="command-section__title">Release notes</h3>
          <pre class="command-panel__raw command-panel__raw--compact">{{ data.release_notes }}</pre>
        </section>

        <section v-if="data.upgrade_available" class="command-section">
          <h3 class="command-section__title">安装</h3>
          <p class="command-panel__hint">Web UI 无法直接更新二进制，请在安装目录终端执行：</p>
          <div class="command-panel__copy-row">
            <code class="command-kv__mono">{{ applyCommand }}</code>
            <button type="button" class="btn btn--ghost btn--sm" @click="copyApplyCommand">复制</button>
          </div>
        </section>
      </template>
    </div>
  </section>
</template>
