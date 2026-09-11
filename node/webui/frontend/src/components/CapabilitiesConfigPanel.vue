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
    title="能力设置"
    subtitle="进程级服务与配额；各 Agent 的工具组在 Agents 中配置。运行中的子 Agent 见侧栏。"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @save="saveCapabilities"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">共享服务</h2>
      <p class="settings-section__desc">
        关闭后，对应工具组不会出现在 Agent 创建与设置中。详情随开关展开。
      </p>
      <div class="setup-config-panel__toggles">
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
    </section>

    <section v-if="form.features.browser_enabled" class="settings-section">
      <h2 class="settings-section__title">
        浏览器服务
        <span class="badge badge--beta" title="试验功能">Beta</span>
      </h2>
      <p class="settings-section__desc">
        需同机运行 dagents-browser。Agent 勾选「浏览器」工具组后自动创建伴生；任务闭环需要非 mock 模型。
      </p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">服务地址</span>
          <input
            v-model="form.browser.service_url"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            placeholder="http://127.0.0.1:18766"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">默认超时（ms）</span>
          <input
            v-model.number="form.browser.default_timeout_ms"
            class="settings-field__input"
            type="number"
            min="1000"
            step="1000"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大并发会话</span>
          <input
            v-model.number="form.browser.max_sessions"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">截图目录</span>
          <input
            v-model="form.browser.output_dir"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            placeholder="browser"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Chrome 路径（可选）</span>
          <input
            v-model="form.browser.chrome_path"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            placeholder="留空则自动查找"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">CDP 地址（可选）</span>
          <input
            v-model="form.browser.cdp_url"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            placeholder="attach 已打开的 Chrome"
          />
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
      <h2 class="settings-section__title">企业微信推送</h2>
      <p class="settings-section__desc">
        使用群「消息推送」Webhook。Agent 仍需勾选「企业微信」工具组；保存后通常需重启 Node。
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
      <div v-if="form.wecom.has_webhook_key" class="setup-config-panel__toggles setup-config-panel__toggles--row">
        <label class="settings-toggle">
          <input v-model="form.wecom.clear_webhook_key" type="checkbox" />
          <span>清除已保存的 Webhook Key</span>
        </label>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">运行配额</h2>
      <p class="settings-section__desc">同时启用的技能数量与定时任务轮询间隔（进程级）。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">同时启用技能数上限</span>
          <input
            v-model.number="form.features.skills_max_in_prompt"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">定时任务轮询（秒）</span>
          <input
            v-model.number="form.features.triggers_poll_seconds"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">子 Agent 配额</h2>
      <p class="settings-section__desc">
        进程级并发与存活上限。是否启用由各 Agent 的「子智能体」工具组决定。
      </p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">默认存活时间（秒）</span>
          <input
            v-model.number="form.child_agents.default_ttl_seconds"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大存活时间（秒）</span>
          <input
            v-model.number="form.child_agents.max_ttl_seconds"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">默认轮次上限</span>
          <input
            v-model.number="form.child_agents.default_max_turns"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大轮次上限</span>
          <input
            v-model.number="form.child_agents.max_max_turns"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">每会话并发上限</span>
          <input
            v-model.number="form.child_agents.max_active_per_parent"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
      </div>
    </section>
  </ConfigPanelShell>
</template>
