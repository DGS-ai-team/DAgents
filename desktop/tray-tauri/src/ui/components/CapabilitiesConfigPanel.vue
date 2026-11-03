<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, dirty, configPath, configWritable, form, load, save } =
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
    title="共享服务与运行限制"
    subtitle="配置本机服务及其可用集成；智能体仍需单独授予对应工具权限。"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    :dirty="dirty"
    @save="saveCapabilities"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">共享服务</h2>
      <p class="settings-section__desc">
        关闭后，对应能力不会出现在智能体创建与设置中。详情随开关展开。
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
      </div>
    </section>

    <section v-if="form.features.browser_enabled" class="settings-section">
      <h2 class="settings-section__title">
        浏览器服务
        <span class="badge badge--beta" title="试验功能">Beta</span>
      </h2>
      <p class="settings-section__desc">
        启用后，智能体可在获准的工具权限范围内浏览网页和截取页面；当前应用会自动启动并管理浏览器服务。
      </p>
      <details class="settings-section__advanced browser-settings__advanced">
        <summary>
          <span>高级浏览器设置</span>
          <small>证书校验例外：{{ form.browser.ignore_https_errors ? "已开启" : "关闭" }}</small>
        </summary>
        <div class="setup-config-panel__field-grid">
          <label class="settings-field">
            <span class="settings-field__label">默认超时（ms）</span>
            <input v-model.number="form.browser.default_timeout_ms" class="settings-field__input" type="number" min="1000" step="1000" />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">最大并发会话</span>
            <input v-model.number="form.browser.max_sessions" class="settings-field__input" type="number" min="1" />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">截图目录</span>
            <input v-model="form.browser.output_dir" class="settings-field__input" type="text" autocomplete="off" placeholder="browser" />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">Chrome 路径（可选）</span>
            <input v-model="form.browser.chrome_path" class="settings-field__input" type="text" autocomplete="off" placeholder="留空则自动查找" />
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
        <p v-if="form.browser.ignore_https_errors" class="settings-section__warning" role="status">
          已启用证书校验例外：浏览器会继续访问证书无效的网站，仅建议用于受控测试环境。
        </p>
        <p v-else class="settings-section__desc">关闭时会正常校验证书；仅在受控测试环境中开启证书例外。</p>
      </details>
    </section>

    <section v-if="form.features.wecom_enabled" class="settings-section">
      <h2 class="settings-section__title">企业微信推送</h2>
      <p class="settings-section__desc">
        使用群「消息推送」Webhook。Agent 仍需勾选「企业微信」工具组；保存后通常需重启本机服务。
      </p>
      <details class="settings-section__advanced wecom-settings__advanced">
        <summary>配置企业微信 Webhook</summary>
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
      </details>
      <div v-if="form.wecom.has_webhook_key" class="setup-config-panel__toggles setup-config-panel__toggles--row">
        <label class="settings-toggle">
          <input v-model="form.wecom.clear_webhook_key" type="checkbox" />
          <span>清除已保存的 Webhook Key</span>
        </label>
      </div>
    </section>

    <details class="settings-section__advanced">
      <summary>
        <span>诊断：原始消息记录</span>
        <small>{{ form.features.raw_message_history_enabled ? "已启用" : "关闭" }}</small>
      </summary>
      <p class="settings-section__desc">用于排查消息处理问题；记录可能包含对话内容，请按需开启。</p>
      <label class="settings-toggle">
        <input v-model="form.features.raw_message_history_enabled" type="checkbox" />
        <span>保存原始消息记录</span>
      </label>
    </details>

    <details class="settings-section">
      <summary class="settings-section__title">高级设置</summary>
      <p class="settings-section__desc">调整当前应用的运行限制。通常无需修改。</p>
      <h3 class="settings-section__title">运行限制</h3>
      <p class="settings-section__desc">同时启用的技能数量与定时任务检查间隔。</p>
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
          <span class="settings-field__label">定时任务检查间隔（秒）</span>
          <input
            v-model.number="form.features.triggers_poll_seconds"
            class="settings-field__input"
            type="number"
            min="1"
          />
        </label>
      </div>
    </details>

    <details class="settings-section">
      <summary class="settings-section__title">子智能体运行限制</summary>
      <p class="settings-section__desc">设置当前应用中子智能体的存活时间、轮次和并发上限。</p>
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
    </details>
  </ConfigPanelShell>
</template>

<style scoped>
.settings-section__advanced {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border-subtle);
}

.settings-section__advanced > summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  cursor: pointer;
  color: var(--color-text-muted);
  font-size: 13px;
}

.settings-section__advanced > summary small {
  color: var(--color-text-subtle);
}

.settings-section__advanced .setup-config-panel__field-grid,
.settings-section__advanced .setup-config-panel__toggles {
  margin-top: 12px;
}

.settings-section__warning {
  margin: 12px 0 0;
  color: var(--color-warning-strong, #a54c00);
  font-size: 12px;
}
</style>
