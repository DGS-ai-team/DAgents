<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveCapabilities() {
  await save({
    features: { ...form.features },
    browser: { ...form.browser },
    child_agents: { ...form.child_agents },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="能力设置"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveCapabilities"
  >
    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">功能开关</h2>
      </div>
      <p class="settings-section__desc">关闭后对应能力不可用；技能与定时任务的具体管理仍在各自页面。</p>
      <div class="setup-config-panel__toggles setup-config-panel__toggles--grid">
        <label class="settings-toggle"><input v-model="form.features.skills_enabled" type="checkbox" /><span>技能</span></label>
        <label class="settings-toggle"><input v-model="form.features.triggers_enabled" type="checkbox" /><span>定时任务</span></label>
        <label class="settings-toggle"><input v-model="form.features.child_agents_enabled" type="checkbox" /><span>子 Agent</span></label>
        <label class="settings-toggle"><input v-model="form.features.ui_enabled" type="checkbox" /><span>Web 界面</span></label>
        <label class="settings-toggle">
          <input v-model="form.features.browser_enabled" type="checkbox" />
          <span class="settings-toggle__label">
            浏览器工具
            <span class="badge badge--beta" title="试验功能">Beta</span>
          </span>
        </label>
        <label class="settings-toggle">
          <input v-model="form.features.raw_message_history_enabled" type="checkbox" />
          <span>原始消息记录</span>
        </label>
      </div>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">技能注入上限</span>
          <input v-model.number="form.features.skills_max_in_prompt" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">定时任务轮询（秒）</span>
          <input v-model.number="form.features.triggers_poll_seconds" class="settings-field__input" type="number" min="1" />
        </label>
      </div>
    </section>

    <section v-if="form.features.browser_enabled" class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">
          浏览器服务
          <span class="badge badge--beta" title="试验功能">Beta</span>
        </h2>
      </div>
      <p class="settings-section__desc">需另行启动浏览器服务后使用。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">服务地址</span>
          <input v-model="form.browser.service_url" class="settings-field__input" type="text" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">默认超时（ms）</span>
          <input v-model.number="form.browser.default_timeout_ms" class="settings-field__input" type="number" min="1000" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大并发会话</span>
          <input v-model.number="form.browser.max_sessions" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">截图目录</span>
          <input v-model="form.browser.output_dir" class="settings-field__input" type="text" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Chrome 路径（可选）</span>
          <input v-model="form.browser.chrome_path" class="settings-field__input" type="text" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">CDP 地址（可选）</span>
          <input v-model="form.browser.cdp_url" class="settings-field__input" type="text" autocomplete="off" />
        </label>
      </div>
      <div class="setup-config-panel__toggles setup-config-panel__toggles--row">
        <label class="settings-toggle">
          <input v-model="form.browser.headed" type="checkbox" />
          <span>显示浏览器窗口</span>
        </label>
        <label class="settings-toggle">
          <input v-model="form.browser.ignore_https_errors" type="checkbox" />
          <span>忽略 HTTPS 证书错误</span>
        </label>
      </div>
    </section>

    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">子 Agent 配额</h2>
      </div>
      <p class="settings-section__desc">控制临时子 Agent 的存活时间与并发上限。</p>
      <label class="settings-toggle">
        <input v-model="form.features.child_agents_enabled" type="checkbox" />
        <span>启用子 Agent</span>
      </label>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">默认存活时间（秒）</span>
          <input v-model.number="form.child_agents.default_ttl_seconds" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大存活时间（秒）</span>
          <input v-model.number="form.child_agents.max_ttl_seconds" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">默认轮次上限</span>
          <input v-model.number="form.child_agents.default_max_turns" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大轮次上限</span>
          <input v-model.number="form.child_agents.max_max_turns" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">每会话并发上限</span>
          <input v-model.number="form.child_agents.max_active_per_parent" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">默认等待超时（秒）</span>
          <input v-model.number="form.child_agents.default_wait_timeout_seconds" class="settings-field__input" type="number" min="1" />
        </label>
      </div>
    </section>
  </ConfigPanelShell>
</template>
