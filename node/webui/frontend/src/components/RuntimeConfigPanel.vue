<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveRuntime() {
  await save({
    runtime: { ...form.runtime },
    agent: { ...form.agent },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="运行时"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveRuntime"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">Node 地址（只读）</h2>
      <p class="setup-config-panel__hint">
        修改 <code>listen</code> / <code>local.endpoint</code> 须编辑 config.yaml 并重启 Node。
      </p>
      <div class="setup-config-panel__readonly-grid">
        <div class="command-stat">
          <span class="command-stat__label">监听</span>
          <span class="command-stat__value">{{ form.node.listen_host || "—" }}:{{ form.node.listen_port || "—" }}</span>
        </div>
        <div class="command-stat">
          <span class="command-stat__label">Client endpoint</span>
          <span class="command-stat__value">{{ form.node.local_endpoint || "—" }}</span>
        </div>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">身份与路径</h2>
      <label class="settings-field">
        <span class="settings-field__label">Agent ID</span>
        <input v-model="form.runtime.agent_id" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">工作区根目录 (fs_root)</span>
        <input v-model="form.runtime.fs_root" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">日志级别</span>
        <select v-model="form.runtime.log_level" class="settings-field__input">
          <option value="debug">debug</option>
          <option value="info">info</option>
          <option value="warn">warn</option>
          <option value="error">error</option>
        </select>
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">Agent 元数据</h2>
      <label class="settings-field">
        <span class="settings-field__label">名称</span>
        <input v-model="form.agent.name" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">简介</span>
        <input v-model="form.agent.description" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Role（A2A）</span>
        <input v-model="form.agent.role" class="settings-field__input" type="text" placeholder="compliance / ops" autocomplete="off" />
      </label>
    </section>
  </ConfigPanelShell>
</template>
