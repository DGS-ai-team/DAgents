<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveCapabilities() {
  // 仅进程级能力；工具组在「设置 › Agents」按实例配置，此处不改 enabled_groups。
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
      此处控制本机 Node 进程总闸与共享服务参数。单个 Agent 的工具组、沙箱、侧车等请到
      <router-link class="capabilities-intro__link" to="/settings/agents">设置 › Agents</router-link>
      配置。
    </p>

    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">功能开关</h2>
      </div>
      <p class="settings-section__desc">关闭后整个 Node 不再提供对应能力；技能与定时任务的具体管理仍在各自页面。</p>
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

    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">子 Agent 配额</h2>
      </div>
      <p class="settings-section__desc">进程级并发与存活上限；单个 Agent 还可在自身配置中关闭子 Agent 能力。</p>
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

<style scoped>
.capabilities-intro {
  margin: 0 0 14px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--color-text-muted);
}

.capabilities-intro__link {
  color: var(--color-primary-strong, #5b9cff);
  text-decoration: none;
}

.capabilities-intro__link:hover {
  text-decoration: underline;
}
</style>
