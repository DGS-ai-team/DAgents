<script setup>
import { computed, onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";
import { chromeStore } from "../stores/chrome.js";
import * as api from "../api/node.js";

const { loading, saving, error, statusMessage, dirty, configPath, configWritable, form, load, save } =
  useSetupConfig();

const contextPolicySummary = computed(() => {
  const compressionEnabled =
    Number(form.compression.silent_trigger_tokens) > 0 ||
    Number(form.compression.blocking_trigger_tokens) > 0;
  const idleSeconds = Number(form.compression.idle_new_conversation_seconds) || 0;
  return `自动压缩：${compressionEnabled ? "启用" : "关闭"}；空闲归档：${idleSeconds > 0 ? `${idleSeconds} 秒后启用` : "关闭"}。`;
});

async function refreshAgentInfo() {
  try {
    const [health, info] = await Promise.all([api.getHealth(), api.getAgentInfo()]);
    chromeStore.agentInfo = { ...health, ...info };
  } catch {
    /* ignore */
  }
}

async function saveContext() {
  const silent = Number(form.compression.silent_trigger_tokens || 0);
  const blocking = Number(form.compression.blocking_trigger_tokens || 0);
  if (silent > 0 && blocking > 0 && blocking < silent) {
    error.value = "阻塞压缩阈值不能小于静默压缩阈值";
    statusMessage.value = "";
    return;
  }

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
    :dirty="dirty"
    @save="saveContext"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">工具结果过长处理</h2>
      <p class="settings-section__desc">超长工具输出会自动摘要，避免占满上下文。</p>
      <label class="settings-toggle">
        <input v-model="form.hooks.tool_result_enabled" type="checkbox" />
        <span>启用工具结果摘要</span>
      </label>
      <p class="settings-section__desc context-settings__policy-summary">{{ contextPolicySummary }}</p>
    </section>

    <details class="settings-section context-settings__advanced">
      <summary class="settings-section__title">高级上下文设置</summary>
      <p class="settings-section__desc">自动压缩阈值或空闲时长设为 0 可关闭对应行为；工具摘要阈值必须至少为 1。</p>
      <label class="settings-field">
        <span class="settings-field__label">工具摘要阈值（tokens）</span>
        <input v-model.number="form.hooks.tool_result_spill_threshold_tokens" class="settings-field__input" type="number" min="1" step="500" />
      </label>
      <section class="settings-section__nested">
        <h3 class="settings-section__title">自动压缩</h3>
        <p class="settings-section__desc">上下文过长时自动摘要；阻塞阈值应不低于静默阈值。</p>
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
      <section class="settings-section__nested">
        <h3 class="settings-section__title">空闲后开启新对话</h3>
        <p class="settings-section__desc">普通 Agent 空闲达到设定时间后，下条用户消息会开启新对话并归档旧上下文；历史消息仍保留。</p>
        <label class="settings-field">
          <span class="settings-field__label">空闲时长（秒）</span>
          <input v-model.number="form.compression.idle_new_conversation_seconds" class="settings-field__input" type="number" min="0" step="60" />
        </label>
      </section>
      <section class="settings-section__nested">
        <h3 class="settings-section__title">提示行为</h3>
        <label class="settings-toggle">
          <input v-model="form.hooks.inject_today_date_enabled" type="checkbox" />
          <span>启用当天日期提示</span>
        </label>
      </section>
    </details>
  </ConfigPanelShell>
</template>

<style scoped>
.context-settings__advanced {
  padding-top: 8px;
}

.context-settings__advanced > summary {
  cursor: pointer;
}

.settings-section__nested {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--color-border);
}
</style>
