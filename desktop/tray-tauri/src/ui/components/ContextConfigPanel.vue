<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";
import { chromeStore } from "../stores/chrome.js";
import * as api from "../api/node.js";

const { loading, saving, error, statusMessage, dirty, configPath, configWritable, form, load, save } =
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
    subtitle="管理对话长度、工具结果与日期提示"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    :dirty="dirty"
    @save="saveContext"
  >
    <section class="settings-section settings-group">
      <h2 class="settings-section__title">工具结果</h2>
      <label class="settings-row">
        <span class="settings-row__copy"><span class="settings-row__title">自动摘要</span><span class="settings-row__description">达到摘要阈值后缩短过长的工具输出。</span></span>
        <input v-model="form.hooks.tool_result_enabled" class="settings-switch-input" type="checkbox" aria-label="自动摘要工具结果" />
      </label>
      <label class="settings-field settings-row settings-row--field">
        <span class="settings-row__copy"><span class="settings-field__label">摘要阈值（tokens）</span><span class="settings-row__description">至少为 1。</span></span>
        <input v-model.number="form.hooks.tool_result_spill_threshold_tokens" class="settings-field__input" type="number" min="1" step="500" />
      </label>
    </section>

    <section class="settings-section settings-group">
      <h2 class="settings-section__title">对话管理</h2>
      <p class="settings-section__desc">上下文过长时自动压缩，阻塞阈值应不低于静默阈值。压缩阈值设为 0 可关闭。</p>
      <label class="settings-field settings-row settings-row--field">
        <span class="settings-row__copy"><span class="settings-field__label">静默压缩阈值（tokens）</span><span class="settings-row__description">后台提前压缩的长度。</span></span>
        <input v-model.number="form.compression.silent_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
      </label>
      <label class="settings-field settings-row settings-row--field">
        <span class="settings-row__copy"><span class="settings-field__label">阻塞压缩阈值（tokens）</span><span class="settings-row__description">请求继续前执行压缩的长度。</span></span>
        <input v-model.number="form.compression.blocking_trigger_tokens" class="settings-field__input" type="number" min="0" step="1000" />
      </label>
      <label class="settings-field settings-row settings-row--field">
        <span class="settings-row__copy"><span class="settings-field__label">新对话空闲时长（秒）</span><span class="settings-row__description">普通 Agent 达到此时长后，下条消息开启新对话并归档旧上下文；历史消息保留。设为 0 可关闭。</span></span>
        <input v-model.number="form.compression.idle_new_conversation_seconds" class="settings-field__input" type="number" min="0" step="60" />
      </label>
    </section>

    <section class="settings-section settings-group">
      <h2 class="settings-section__title">提示内容</h2>
      <label class="settings-row">
        <span class="settings-row__copy"><span class="settings-row__title">当天日期</span><span class="settings-row__description">将当前日期加入对话，帮助理解相对日期。</span></span>
        <input v-model="form.hooks.inject_today_date_enabled" class="settings-switch-input" type="checkbox" aria-label="当天日期提示" />
      </label>
    </section>
  </ConfigPanelShell>
</template>
