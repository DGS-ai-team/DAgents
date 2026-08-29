<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveGeneral() {
  const { fs_root: _ignored, ...runtime } = form.runtime;
  await save({
    runtime,
    agent: {
      name: form.agent.name || "",
      description: form.agent.description || "",
    },
    user: {
      preferred_name: form.user.preferred_name || "",
    },
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
      <h2 class="settings-section__title">Node 信息</h2>
      <p class="settings-section__desc">注册到 Manage 后的展示名（peers / Console）；与具体 Agent 实例名称无关。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">怎么称呼你</span>
          <input
            v-model="form.user.preferred_name"
            class="settings-field__input"
            type="text"
            placeholder="本机使用者称呼"
            autocomplete="nickname"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Node 名称</span>
          <input v-model="form.agent.name" class="settings-field__input" type="text" placeholder="空则显示 Node ID" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">简介</span>
          <input v-model="form.agent.description" class="settings-field__input" type="text" autocomplete="off" />
        </label>
      </div>
    </section>

  </ConfigPanelShell>
</template>
