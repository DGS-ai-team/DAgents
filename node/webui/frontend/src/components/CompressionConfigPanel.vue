<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";
import { chromeStore } from "../stores/chrome.js";
import * as api from "../api/node.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function refreshAgentInfo() {
  try {
    const [health, info] = await Promise.all([api.getHealth(), api.getAgentInfo()]);
    chromeStore.agentInfo = { ...health, ...info };
  } catch {
    /* ignore */
  }
}

async function saveCompression() {
  const ok = await save({ compression: { ...form.compression } });
  if (ok) await refreshAgentInfo();
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="压缩阈值"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveCompression"
  >
    <p class="setup-config-panel__hint">
      Silent 在后台摘要；Blocking 会阻塞当前 turn。设为 <code>0</code> 关闭对应档位。Blocking 应 ≥ Silent。
    </p>

    <section class="settings-section">
      <h2 class="settings-section__title">Token 阈值</h2>
      <label class="settings-field">
        <span class="settings-field__label">Silent 触发（tokens）</span>
        <input v-model.number="form.compression.silent_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Blocking 触发（tokens）</span>
        <input v-model.number="form.compression.blocking_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">无动作自动压缩</h2>
      <p class="settings-section__desc">Session 长时间无交互时自动压缩；秒数设为 0 表示关闭。</p>
      <label class="settings-field">
        <span class="settings-field__label">空闲秒数</span>
        <input v-model.number="form.compression.idle_auto_compress_seconds" class="settings-field__input" type="number" min="0" step="60" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">扫描间隔（秒）</span>
        <input v-model.number="form.compression.idle_auto_compress_poll_seconds" class="settings-field__input" type="number" min="0" step="10" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">最低 tokens</span>
        <input v-model.number="form.compression.idle_auto_compress_min_tokens" class="settings-field__input" type="number" min="0" step="1000" />
      </label>
    </section>
  </ConfigPanelShell>
</template>
