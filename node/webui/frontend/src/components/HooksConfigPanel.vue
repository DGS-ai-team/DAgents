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
    },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="Tool 结果摘要"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveHooks"
  >
    <p class="setup-config-panel__hint">
      超长 tool 结果落盘并对 history 做头尾摘要（tool_result Hook）。
    </p>
    <label class="settings-toggle">
      <input v-model="form.hooks.tool_result_enabled" type="checkbox" />
      <span>启用 tool_result Hook</span>
    </label>
    <label class="settings-field">
      <span class="settings-field__label">落盘阈值（tokens）</span>
      <input v-model.number="form.hooks.tool_result_spill_threshold_tokens" class="settings-field__input" type="number" min="1" step="500" />
    </label>
  </ConfigPanelShell>
</template>
