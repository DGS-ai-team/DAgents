<script setup>
import { computed, onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-chat" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
  mock: { base_url: "", model: "mock" },
};

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

const manageFieldsDisabled = computed(() => !form.manage.enabled);

function applyProviderPreset() {
  const preset = PROVIDER_PRESETS[form.llm.provider];
  if (!preset) return;
  form.llm.base_url = preset.base_url;
  form.llm.model = preset.model;
  form.llm.mock = form.llm.provider === "mock";
}

function featuresPayload() {
  return { ...form.features };
}

function llmPayload() {
  return {
    ...form.llm,
    mock: form.llm.mock || form.llm.provider === "mock",
  };
}

function managePayload() {
  return {
    ...form.manage,
    a2a_enabled: form.manage.a2a_enabled ?? false,
  };
}

async function saveConnection() {
  await save({
    llm: llmPayload(),
    manage: managePayload(),
    features: featuresPayload(),
    browser: { ...form.browser },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="连接与功能"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveConnection"
  >
    <p class="setup-config-panel__hint">
      API Key 不写入配置文件，请在系统环境变量中设置（如 <code>OPENAI_API_KEY</code>）。
    </p>

    <section class="settings-section">
      <h2 class="settings-section__title">LLM</h2>
      <label class="settings-field">
        <span class="settings-field__label">Provider</span>
        <select v-model="form.llm.provider" class="settings-field__input" @change="applyProviderPreset">
          <option value="deepseek">DeepSeek</option>
          <option value="openai">OpenAI</option>
          <option value="qwen">Qwen</option>
          <option value="vllm">vLLM</option>
          <option value="mock">Mock（测试）</option>
        </select>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Base URL</span>
        <input v-model="form.llm.base_url" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Model</span>
        <input v-model="form.llm.model" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">API Key 环境变量名</span>
        <input v-model="form.llm.api_key_env" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">单条消息工具步上限</span>
        <input v-model.number="form.llm.max_tool_loops" class="settings-field__input" type="number" min="1" />
      </label>
      <label class="settings-toggle">
        <input v-model="form.llm.mock" type="checkbox" />
        <span>Mock 模式（不调用真实 LLM）</span>
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">Manage</h2>
      <label class="settings-toggle">
        <input v-model="form.manage.enabled" type="checkbox" />
        <span>启用 Manage 注册与通信</span>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Manage URL</span>
        <input v-model="form.manage.url" class="settings-field__input" type="text" :disabled="manageFieldsDisabled" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">node_token（可选）</span>
        <input v-model="form.manage.node_token" class="settings-field__input" type="text" :disabled="manageFieldsDisabled" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Console 分组 (team)</span>
        <input v-model="form.manage.team" class="settings-field__input" type="text" :disabled="manageFieldsDisabled" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Registration base_url（可选）</span>
        <input v-model="form.manage.registration_base_url" class="settings-field__input" type="text" :disabled="manageFieldsDisabled" autocomplete="off" />
      </label>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">心跳间隔（秒）</span>
          <input v-model.number="form.manage.registration_interval_seconds" class="settings-field__input" type="number" min="1" :disabled="manageFieldsDisabled" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">TTL（秒）</span>
          <input v-model.number="form.manage.registration_ttl_seconds" class="settings-field__input" type="number" min="1" :disabled="manageFieldsDisabled" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Inbox wait（秒）</span>
          <input v-model.number="form.manage.a2a_inbox_wait_seconds" class="settings-field__input" type="number" min="1" :disabled="manageFieldsDisabled" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Inbox poll（秒）</span>
          <input v-model.number="form.manage.a2a_inbox_poll_seconds" class="settings-field__input" type="number" min="1" :disabled="manageFieldsDisabled" />
        </label>
      </div>
      <label class="settings-toggle">
        <input v-model="form.manage.a2a_enabled" type="checkbox" :disabled="manageFieldsDisabled" />
        <span>启用 A2A Inbox</span>
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">功能开关</h2>
      <div class="setup-config-panel__toggles">
        <label class="settings-toggle"><input v-model="form.features.skills_enabled" type="checkbox" /><span>Skills</span></label>
        <label class="settings-toggle"><input v-model="form.features.triggers_enabled" type="checkbox" /><span>Triggers</span></label>
        <label class="settings-toggle"><input v-model="form.features.child_agents_enabled" type="checkbox" /><span>Child Agents</span></label>
        <label class="settings-toggle"><input v-model="form.features.ui_enabled" type="checkbox" /><span>Web UI</span></label>
        <label class="settings-toggle"><input v-model="form.features.browser_enabled" type="checkbox" /><span>Browser 工具</span></label>
        <label class="settings-toggle"><input v-model="form.features.multimodal_enabled" type="checkbox" /><span>多模态 / Vision</span></label>
        <label class="settings-toggle"><input v-model="form.features.raw_message_history_enabled" type="checkbox" /><span>原始消息 JSONL</span></label>
      </div>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">Skills 注入上限</span>
          <input v-model.number="form.features.skills_max_in_prompt" class="settings-field__input" type="number" min="1" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Triggers 轮询（秒）</span>
          <input v-model.number="form.features.triggers_poll_seconds" class="settings-field__input" type="number" min="1" />
        </label>
      </div>
    </section>

    <section v-if="form.features.browser_enabled" class="settings-section">
      <h2 class="settings-section__title">Browser 服务</h2>
      <label class="settings-field">
        <span class="settings-field__label">Service URL</span>
        <input v-model="form.browser.service_url" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-toggle">
        <input v-model="form.browser.headed" type="checkbox" />
        <span>Headed 模式</span>
      </label>
      <label class="settings-toggle">
        <input v-model="form.browser.ignore_https_errors" type="checkbox" />
        <span>忽略 HTTPS 证书错误</span>
      </label>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">默认超时（ms）</span>
          <input v-model.number="form.browser.default_timeout_ms" class="settings-field__input" type="number" min="1000" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大并发 session</span>
          <input v-model.number="form.browser.max_sessions" class="settings-field__input" type="number" min="1" />
        </label>
      </div>
      <label class="settings-field">
        <span class="settings-field__label">截图目录（相对 fs_root）</span>
        <input v-model="form.browser.output_dir" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Chrome 路径（可选）</span>
        <input v-model="form.browser.chrome_path" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">CDP URL（可选）</span>
        <input v-model="form.browser.cdp_url" class="settings-field__input" type="text" autocomplete="off" />
      </label>
    </section>
  </ConfigPanelShell>
</template>
