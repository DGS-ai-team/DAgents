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

async function saveContext() {
  const ok = await save({
    compression: { ...form.compression },
    hooks: {
      tool_result_enabled: form.hooks.tool_result_enabled,
      tool_result_spill_threshold_tokens: form.hooks.tool_result_spill_threshold_tokens,
      inject_today_date_enabled: form.hooks.inject_today_date_enabled,
    },
  });
  if (ok) await refreshAgentInfo();
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="上下文设置"
    subtitle="进程级压缩阈值与钩子"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveContext"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">自动压缩</h2>
      <p class="settings-section__desc">上下文过长时自动摘要。设为 0 可关闭对应档位；阻塞阈值应不低于静默阈值。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">静默压缩阈值（tokens）</span>
          <input v-model.number="form.compression.silent_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">阻塞压缩阈值（tokens）</span>
          <input v-model.number="form.compression.blocking_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
        </label>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">空闲自动压缩</h2>
      <p class="settings-section__desc">长时间无操作时自动压缩；空闲秒数设为 0 表示关闭。</p>
      <div class="setup-config-panel__field-grid">
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
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">当天日期</h2>
      <p class="settings-section__desc">在对话中自动告知模型今天的日期。</p>
      <label class="settings-toggle">
        <input v-model="form.hooks.inject_today_date_enabled" type="checkbox" />
        <span>启用当天日期提示</span>
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">工具结果过长处理</h2>
      <p class="settings-section__desc">超长工具输出会自动摘要，避免占满上下文。</p>
      <label class="settings-toggle">
        <input v-model="form.hooks.tool_result_enabled" type="checkbox" />
        <span>启用工具结果摘要</span>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">摘要阈值（tokens）</span>
        <input
          v-model.number="form.hooks.tool_result_spill_threshold_tokens"
          class="settings-field__input"
          type="number"
          min="1"
          step="500"
        />
      </label>
    </section>
  </ConfigPanelShell>
</template>
