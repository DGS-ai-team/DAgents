<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import UiSelect from "./UiSelect.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, dirty, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveGeneral() {
  await save({
    runtime: { log_level: form.runtime.log_level },
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
    subtitle="管理本机服务的显示信息、日志和运行状态。"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    :dirty="dirty"
    @save="saveGeneral"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">访问地址</h2>
      <p class="settings-section__desc">监听地址由启动配置决定，修改后需要重启本机服务。</p>
      <div class="setup-config-panel__readonly-grid">
        <div class="command-stat">
          <span class="command-stat__label">监听</span>
          <span class="command-stat__value">{{ form.node.listen_host || "—" }}:{{ form.node.listen_port || "—" }}</span>
        </div>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">显示信息</h2>
      <p class="settings-section__desc">用于界面和协作场景中的识别，不影响具体智能体的名称。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span><span class="settings-field__label">怎么称呼你</span><small class="settings-field__hint">用于回复和协作场景</small></span>
          <input
            v-model="form.user.preferred_name"
            class="settings-field__input"
            type="text"
            placeholder="当前本机服务使用者称呼"
            autocomplete="nickname"
          />
        </label>
        <label class="settings-field">
          <span><span class="settings-field__label">这台电脑的名称</span><small class="settings-field__hint">帮助区分不同本机服务</small></span>
          <input v-model="form.agent.name" class="settings-field__input" type="text" placeholder="例如：开发机" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span><span class="settings-field__label">简介</span><small class="settings-field__hint">展示在本机服务信息中</small></span>
          <input v-model="form.agent.description" class="settings-field__input" type="text" autocomplete="off" />
        </label>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">诊断</h2>
      <p class="settings-section__desc">一般使用保持 info；排查问题时再临时提高日志详细度。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">日志级别</span>
          <UiSelect
            v-model="form.runtime.log_level"
            :options="['debug', 'info', 'warn', 'error']"
            aria-label="日志级别"
          />
        </label>
      </div>
    </section>

  </ConfigPanelShell>
</template>
