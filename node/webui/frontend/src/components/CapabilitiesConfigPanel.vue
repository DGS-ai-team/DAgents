<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveCapabilities() {
  // 工具组能力由各 Agent 决定；此处只保存进程级服务与配额。技能/子 Agent/定时任务总闸固定为开启。
  const wecom = {
    webhook_url: form.wecom.webhook_url || "",
    api_base: form.wecom.api_base || "",
    clear_webhook_key: !!form.wecom.clear_webhook_key,
  };
  if (form.wecom.webhook_key) {
    wecom.webhook_key = form.wecom.webhook_key;
  }
  await save({
    features: {
      ...form.features,
      skills_enabled: true,
      triggers_enabled: true,
      child_agents_enabled: true,
    },
    browser: { ...form.browser },
    wecom,
    child_agents: { ...form.child_agents },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="Node 能力（进程级）"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveCapabilities"
  >
    <p class="capabilities-intro">
      Agent 是否具备技能、子 Agent、定时任务等能力，由各 Agent 的工具组决定。此处配置进程级共享参数与服务（浏览器、企业微信等）。
      单个 Agent 请到
      <router-link class="capabilities-intro__link" to="/settings/agents">设置 › Agents</router-link>
      配置工具组。
    </p>

    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">进程选项</h2>
      </div>
      <p class="settings-section__desc">与工具组无关的 Node 进程开关与配额参数。</p>
      <div class="setup-config-panel__toggles setup-config-panel__toggles--grid">
        <label class="settings-toggle"><input v-model="form.features.ui_enabled" type="checkbox" /><span>Web 界面</span></label>
        <label class="settings-toggle">
          <input v-model="form.features.browser_enabled" type="checkbox" />
          <span class="settings-toggle__label">
            浏览器服务
            <span class="badge badge--beta" title="试验功能">Beta</span>
          </span>
        </label>
        <label class="settings-toggle">
          <input v-model="form.features.wecom_enabled" type="checkbox" />
          <span>企业微信推送</span>
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
      <p class="settings-section__desc">需另行启动浏览器服务后使用；各 Agent 仍需在自身工具组中启用 browser。</p>
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

    <section v-if="form.features.wecom_enabled" class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">企业微信消息推送</h2>
      </div>
      <p class="settings-section__desc">
        使用群「消息推送」Webhook（非应用消息 API）。在企业微信创建消息推送后粘贴 Webhook 地址或 key；
        各 Agent 仍需在工具组中启用 wecom。保存后通常需重启 Node。
      </p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">Webhook 地址或 Key</span>
          <input
            v-model="form.wecom.webhook_url"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">更新 Key（可选）</span>
          <input
            v-model="form.wecom.webhook_key"
            class="settings-field__input"
            type="password"
            autocomplete="new-password"
            :placeholder="form.wecom.has_webhook_key ? '已配置，留空则保留' : '也可只填此项'"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">API 基址</span>
          <input
            v-model="form.wecom.api_base"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            placeholder="https://qyapi.weixin.qq.com"
          />
        </label>
      </div>
      <label v-if="form.wecom.has_webhook_key" class="settings-toggle">
        <input v-model="form.wecom.clear_webhook_key" type="checkbox" />
        <span>清除已保存的 Webhook Key</span>
      </label>
    </section>

    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">子 Agent 配额</h2>
      </div>
      <p class="settings-section__desc">进程级并发与存活上限；是否启用由各 Agent 工具组「子 Agent」决定。</p>
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

<style scoped>
.capabilities-intro {
  margin: 0 0 14px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--color-text-muted);
}

.capabilities-intro__link {
  color: var(--color-primary-strong, #5d5d5d);
  text-decoration: none;
}

.capabilities-intro__link:hover {
  text-decoration: underline;
}
</style>
