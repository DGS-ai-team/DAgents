<script setup>
import { computed, onMounted, ref } from "vue";
import * as desktopApi from "../api/desktop.js";

defineProps({
  embedded: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const applying = ref(false);
const error = ref("");
const data = ref(null);
const source = ref("node");

const applyCommand = computed(() => {
  const cmd = String(data.value?.apply_command || "dagents update").trim();
  return cmd || "dagents update";
});

const shellManaged = computed(() => source.value === "shell");
const canApplyInUI = computed(() => shellManaged.value && !!data.value?.upgrade_available);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const result = await desktopApi.getUpdateStatus();
    source.value = result.source;
    data.value = result.data;
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

async function applyUpgrade() {
  if (!canApplyInUI.value || applying.value) return;
  const latest = String(data.value?.latest_version || "").trim();
  if (!latest) return;
  if (!window.confirm(`升级到 ${latest}？升级期间 Node 将短暂停止。`)) return;
  applying.value = true;
  error.value = "";
  try {
    const result = await desktopApi.applyDesktopUpdate({ force: false });
    if (result?.message) {
      window.alert(result.message);
    }
    await load();
  } catch (e) {
    error.value = e.message;
  } finally {
    applying.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel status-panel" :class="{ 'settings-embedded-panel': embedded }">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">版本与更新</div>
        <div v-if="!embedded" class="command-panel__subtitle">检查可用更新</div>
      </div>
      <div class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading || applying" @click="load">刷新</button>
        <button v-if="!embedded" type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
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
          <template v-if="canApplyInUI">
            <p class="command-panel__hint">Shell 可在此直接升级（会先检查 Node 是否空闲）。</p>
            <button type="button" class="btn btn--primary btn--sm" :disabled="applying" @click="applyUpgrade">
              {{ applying ? "升级中…" : `升级到 ${data.latest_version}` }}
            </button>
          </template>
          <template v-else>
            <p class="command-panel__hint">请在安装目录终端执行：</p>
            <div class="command-panel__copy-row">
              <code class="command-kv__mono">{{ applyCommand }}</code>
              <button type="button" class="btn btn--ghost btn--sm" @click="copyApplyCommand">复制</button>
            </div>
          </template>
        </section>
      </template>
    </div>
  </section>
</template>

<style scoped>
.command-panel__source {
  color: var(--color-text-muted);
  font-size: 12px;
}
</style>
