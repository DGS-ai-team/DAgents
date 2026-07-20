<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveHooks() {
  await save({
    hooks: {
      tool_result_enabled: form.hooks.tool_result_enabled,
      tool_result_spill_threshold_tokens: form.hooks.tool_result_spill_threshold_tokens,
      inject_today_date_enabled: form.hooks.inject_today_date_enabled,
    },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="内置 Hook"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveHooks"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">当天日期注入</h2>
      <p class="setup-config-panel__hint">
        每轮 LLM turn 开头检查并插入「当天日期为：YYYYMMDD」human message（inject_today_date Hook）。
      </p>
      <label class="settings-toggle">
        <input v-model="form.hooks.inject_today_date_enabled" type="checkbox" />
        <span>启用 inject_today_date Hook</span>
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">Tool 结果摘要</h2>
      <p class="setup-config-panel__hint">
        超长 tool 结果落盘并对 history 做头尾摘要（tool_result Hook）。
      </p>
      <label class="settings-toggle">
        <input v-model="form.hooks.tool_result_enabled" type="checkbox" />
        <span>启用 tool_result Hook</span>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">落盘阈值（tokens）</span>
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
