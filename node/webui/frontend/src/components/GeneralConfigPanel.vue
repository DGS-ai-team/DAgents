<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveGeneral() {
  await save({
    runtime: { ...form.runtime },
    agent: { ...form.agent },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="通用设置"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveGeneral"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">Node 地址</h2>
      <p class="settings-section__desc">监听地址需修改本地配置文件后重启生效。</p>
      <div class="setup-config-panel__readonly-grid">
        <div class="command-stat">
          <span class="command-stat__label">监听</span>
          <span class="command-stat__value">{{ form.node.listen_host || "—" }}:{{ form.node.listen_port || "—" }}</span>
        </div>
        <div class="command-stat">
          <span class="command-stat__label">连接地址</span>
          <span class="command-stat__value">{{ form.node.local_endpoint || "—" }}</span>
        </div>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">身份与路径</h2>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">Node ID</span>
          <input v-model="form.runtime.node_id" class="settings-field__input" type="text" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">工作区根目录</span>
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
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">Agent 信息</h2>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">名称</span>
          <input v-model="form.agent.name" class="settings-field__input" type="text" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">简介</span>
          <input v-model="form.agent.description" class="settings-field__input" type="text" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">角色</span>
          <input v-model="form.agent.role" class="settings-field__input" type="text" placeholder="可选" autocomplete="off" />
        </label>
      </div>
    </section>
  </ConfigPanelShell>
</template>
