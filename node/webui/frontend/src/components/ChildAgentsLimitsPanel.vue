<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveLimits() {
  await save({
    child_agents: { ...form.child_agents },
    features: { ...form.features },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="子 Agent 配额"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveLimits"
  >
    <label class="settings-toggle">
      <input v-model="form.features.child_agents_enabled" type="checkbox" />
      <span>启用 Child Agents</span>
    </label>
    <div class="setup-config-panel__field-grid">
      <label class="settings-field">
        <span class="settings-field__label">默认 TTL（秒）</span>
        <input v-model.number="form.child_agents.default_ttl_seconds" class="settings-field__input" type="number" min="1" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">最大 TTL（秒）</span>
        <input v-model.number="form.child_agents.max_ttl_seconds" class="settings-field__input" type="number" min="1" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">默认 max_turns</span>
        <input v-model.number="form.child_agents.default_max_turns" class="settings-field__input" type="number" min="1" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">最大 max_turns</span>
        <input v-model.number="form.child_agents.max_max_turns" class="settings-field__input" type="number" min="1" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">每父 session 并发上限</span>
        <input v-model.number="form.child_agents.max_active_per_parent" class="settings-field__input" type="number" min="1" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">默认 wait 超时（秒）</span>
        <input v-model.number="form.child_agents.default_wait_timeout_seconds" class="settings-field__input" type="number" min="1" />
      </label>
    </div>
  </ConfigPanelShell>
</template>
