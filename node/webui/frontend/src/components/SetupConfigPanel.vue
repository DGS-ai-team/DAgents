<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import * as api from "../api/node.js";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-chat" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
  mock: { base_url: "", model: "mock" },
};

const loading = ref(false);
const saving = ref(false);
const error = ref("");
const statusMessage = ref("");
const configPath = ref("");
const configWritable = ref(false);

const form = reactive({
  llm: {
    provider: "deepseek",
    base_url: "",
    model: "",
    api_key_env: "OPENAI_API_KEY",
    mock: false,
  },
  manage: {
    enabled: false,
    url: "http://127.0.0.1:8020",
    team: "platform",
    registration_base_url: "",
    a2a_enabled: false,
  },
  features: {
    skills_enabled: true,
    triggers_enabled: true,
    child_agents_enabled: true,
    ui_enabled: true,
    browser_enabled: false,
    multimodal_enabled: false,
  },
});

const manageFieldsDisabled = computed(() => !form.manage.enabled);

function applyProviderPreset() {
  const preset = PROVIDER_PRESETS[form.llm.provider];
  if (!preset) return;
  form.llm.base_url = preset.base_url;
  form.llm.model = preset.model;
  form.llm.mock = form.llm.provider === "mock";
}

function fillForm(data) {
  if (!data) return;
  configPath.value = data.config_path || "";
  configWritable.value = Boolean(data.config_writable);
  Object.assign(form.llm, data.llm || {});
  Object.assign(form.manage, data.manage || {});
  Object.assign(form.features, data.features || {});
}

async function load() {
  loading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const data = await api.getSetupConfig();
    fillForm(data);
  } catch (e) {
    error.value = e.message;
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!configWritable.value) {
    error.value = "当前环境无法写入 config.yaml";
    return;
  }
  saving.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const payload = {
      llm: { ...form.llm, mock: form.llm.mock || form.llm.provider === "mock" },
      manage: { ...form.manage },
      features: { ...form.features },
    };
    if (payload.manage.enabled && payload.manage.a2a_enabled === undefined) {
      payload.manage.a2a_enabled = false;
    }
    const data = await api.patchSetupConfig(payload);
    fillForm(data);
    statusMessage.value = data.restart_required
      ? "已保存到 config.yaml。请重启 Node（或 Shell 重启 Node）使 Manage / Browser / 功能开关生效。"
      : "已保存。";
  } catch (e) {
    error.value = e.message;
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section class="panel settings-embedded-panel setup-config-panel">
    <header class="panel__header">
      <div>
        <div class="panel__title">连接与功能</div>
        <div class="setup-config-panel__subtitle">
          写入 <code v-if="configPath">{{ configPath }}</code><span v-else>config.yaml</span>
          <span v-if="!configWritable"> · 只读（Node 未记录可写路径）</span>
        </div>
      </div>
      <div class="setup-config-panel__actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading || saving" @click="load">刷新</button>
        <button type="button" class="btn btn--primary btn--sm" :disabled="loading || saving || !configWritable" @click="save">
          {{ saving ? "保存中…" : "保存" }}
        </button>
      </div>
    </header>

    <div class="panel__body setup-config-panel__body">
      <div v-if="loading && !configPath" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <template v-else>
        <p v-if="statusMessage" class="setup-config-panel__status">{{ statusMessage }}</p>
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
            <input
              v-model="form.manage.url"
              class="settings-field__input"
              type="text"
              :disabled="manageFieldsDisabled"
              autocomplete="off"
            />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">Console 分组 (team)</span>
            <input
              v-model="form.manage.team"
              class="settings-field__input"
              type="text"
              :disabled="manageFieldsDisabled"
              autocomplete="off"
            />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">Registration base_url（可选）</span>
            <input
              v-model="form.manage.registration_base_url"
              class="settings-field__input"
              type="text"
              :disabled="manageFieldsDisabled"
              autocomplete="off"
            />
          </label>
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
          </div>
        </section>
      </template>
    </div>
  </section>
</template>
