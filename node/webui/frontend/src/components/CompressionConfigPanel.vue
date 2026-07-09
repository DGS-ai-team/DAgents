<script setup>
import { onMounted, reactive, ref } from "vue";
import * as api from "../api/node.js";
import { chromeStore } from "../stores/chrome.js";

const loading = ref(false);
const saving = ref(false);
const error = ref("");
const statusMessage = ref("");
const configPath = ref("");
const configWritable = ref(false);

const form = reactive({
  silent_trigger_tokens: 80000,
  blocking_trigger_tokens: 100000,
  idle_auto_compress_seconds: 0,
  idle_auto_compress_poll_seconds: 60,
  idle_auto_compress_min_tokens: 0,
});

function fillForm(data) {
  if (!data) return;
  configPath.value = data.config_path || "";
  configWritable.value = Boolean(data.config_writable);
  Object.assign(form, data.compression || {});
}

async function refreshAgentInfo() {
  try {
    const [health, info] = await Promise.all([api.getHealth(), api.getAgentInfo()]);
    chromeStore.agentInfo = { ...health, ...info };
  } catch {
    /* ignore */
  }
}

async function load() {
  loading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const data = await api.getSetupConfig();
    fillForm(data);
  } catch (e) {
    error.value = e.message;
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!configWritable.value) {
    error.value = "当前环境无法写入 config.yaml";
    return;
  }
  saving.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const data = await api.patchSetupConfig({ compression: { ...form } });
    fillForm(data);
    await refreshAgentInfo();
    statusMessage.value = data.restart_required
      ? "已保存到 config.yaml。请重启 Node 使压缩行为与无动作扫描生效；进度环会立即反映新阈值。"
      : "已保存。";
  } catch (e) {
    error.value = e.message;
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section class="panel settings-embedded-panel setup-config-panel compression-config-panel">
    <header class="panel__header">
      <div>
        <div class="panel__title">压缩阈值</div>
        <div class="setup-config-panel__subtitle">
          写入 <code v-if="configPath">{{ configPath }}</code><span v-else>config.yaml</span>
          的 <code>compression</code> 块
          <span v-if="!configWritable"> · 只读</span>
        </div>
      </div>
      <div class="setup-config-panel__actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading || saving" @click="load">刷新</button>
        <button type="button" class="btn btn--primary btn--sm" :disabled="loading || saving || !configWritable" @click="save">
          {{ saving ? "保存中…" : "保存" }}
        </button>
      </div>
    </header>

    <div class="panel__body setup-config-panel__body">
      <div v-if="loading && !configPath" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <template v-else>
        <p v-if="statusMessage" class="setup-config-panel__status">{{ statusMessage }}</p>
        <p class="setup-config-panel__hint">
          Silent 在后台摘要；Blocking 会阻塞当前 turn。设为 <code>0</code> 关闭对应档位。Blocking 应 ≥ Silent。
        </p>

        <section class="settings-section">
          <h2 class="settings-section__title">Token 阈值</h2>
          <label class="settings-field">
            <span class="settings-field__label">Silent 触发（tokens）</span>
            <input v-model.number="form.silent_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">Blocking 触发（tokens）</span>
            <input v-model.number="form.blocking_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
          </label>
        </section>

        <section class="settings-section">
          <h2 class="settings-section__title">无动作自动压缩</h2>
          <p class="settings-section__desc">Session 长时间无交互时自动压缩；秒数设为 0 表示关闭。</p>
          <label class="settings-field">
            <span class="settings-field__label">空闲秒数</span>
            <input v-model.number="form.idle_auto_compress_seconds" class="settings-field__input" type="number" min="0" step="60" />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">扫描间隔（秒）</span>
            <input v-model.number="form.idle_auto_compress_poll_seconds" class="settings-field__input" type="number" min="0" step="10" />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">最低 tokens</span>
            <input v-model.number="form.idle_auto_compress_min_tokens" class="settings-field__input" type="number" min="0" step="1000" />
          </label>
        </section>
      </template>
    </div>
  </section>
</template>
