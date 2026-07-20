<script setup>
import { computed, onMounted, ref, watch } from "vue";
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

const editingId = ref("");
const newProfileId = ref("");

const manageFieldsDisabled = computed(() => !form.manage.enabled);

const profiles = computed(() => (Array.isArray(form.llm.profiles) ? form.llm.profiles : []));

const editingProfile = computed(() => profiles.value.find((p) => p.id === editingId.value) || null);

function ensureProfiles() {
  if (!Array.isArray(form.llm.profiles)) form.llm.profiles = [];
  if (form.llm.profiles.length === 0) {
    const id = form.llm.active || "default";
    form.llm.profiles.push({
      id,
      provider: form.llm.provider || "openai",
      base_url: form.llm.base_url || "",
      model: form.llm.model || "",
      api_key_env: form.llm.api_key_env || "OPENAI_API_KEY",
      mock: !!form.llm.mock,
    });
    form.llm.active = id;
  }
  if (!editingId.value || !form.llm.profiles.some((p) => p.id === editingId.value)) {
    editingId.value = form.llm.active || form.llm.profiles[0].id;
  }
}

function applyProviderPreset() {
  const profile = editingProfile.value;
  if (!profile) return;
  const preset = PROVIDER_PRESETS[profile.provider];
  if (!preset) return;
  profile.base_url = preset.base_url;
  profile.model = preset.model;
  profile.mock = profile.provider === "mock";
}

function addProfile() {
  ensureProfiles();
  let id = String(newProfileId.value || "").trim();
  if (!id) {
    id = `llm-${form.llm.profiles.length + 1}`;
  }
  if (form.llm.profiles.some((p) => p.id === id)) {
    error.value = `档案 id「${id}」已存在`;
    return;
  }
  form.llm.profiles.push({
    id,
    provider: "deepseek",
    base_url: PROVIDER_PRESETS.deepseek.base_url,
    model: PROVIDER_PRESETS.deepseek.model,
    api_key_env: "OPENAI_API_KEY",
    mock: false,
  });
  editingId.value = id;
  newProfileId.value = "";
}

function removeProfile(id) {
  ensureProfiles();
  if (form.llm.profiles.length <= 1) {
    error.value = "至少保留一个 LLM 档案";
    return;
  }
  form.llm.profiles = form.llm.profiles.filter((p) => p.id !== id);
  if (form.llm.active === id) {
    form.llm.active = form.llm.profiles[0].id;
  }
  if (editingId.value === id) {
    editingId.value = form.llm.active;
  }
}

function featuresPayload() {
  return { ...form.features };
}

function llmPayload() {
  ensureProfiles();
  const active = form.llm.active || form.llm.profiles[0]?.id || "default";
  return {
    active,
    max_tool_loops: form.llm.max_tool_loops,
    profiles: form.llm.profiles.map((p) => ({
      id: p.id,
      provider: p.provider,
      base_url: p.base_url,
      model: p.model,
      api_key_env: p.api_key_env,
      mock: p.mock || p.provider === "mock",
      thinking: p.thinking || undefined,
      reasoning_effort: p.reasoning_effort || undefined,
    })),
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
  ensureProfiles();
}

watch(
  () => form.llm.profiles,
  () => ensureProfiles(),
  { deep: true }
);

onMounted(async () => {
  await load();
  ensureProfiles();
});
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
    @refresh="load().then(ensureProfiles)"
    @save="saveConnection"
  >
    <p class="setup-config-panel__hint">
      可配置多个 LLM 档案并在对话页切换。API Key 不写入配置文件，请在系统环境变量中设置（如
      <code>OPENAI_API_KEY</code>）。
    </p>

    <section class="settings-section">
      <h2 class="settings-section__title">LLM 档案</h2>
      <label class="settings-field">
        <span class="settings-field__label">当前启用</span>
        <select v-model="form.llm.active" class="settings-field__input">
          <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.id }} ({{ p.provider }} / {{ p.model }})</option>
        </select>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">编辑档案</span>
        <select v-model="editingId" class="settings-field__input">
          <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.id }}</option>
        </select>
      </label>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">新建档案 id</span>
          <input v-model="newProfileId" class="settings-field__input" type="text" placeholder="如 qwen-plus" autocomplete="off" />
        </label>
        <div class="settings-field">
          <span class="settings-field__label">&nbsp;</span>
          <button type="button" class="btn btn--ghost" @click="addProfile">添加档案</button>
        </div>
      </div>
      <button
        v-if="editingProfile && profiles.length > 1"
        type="button"
        class="btn btn--ghost"
        @click="removeProfile(editingProfile.id)"
      >
        删除当前编辑档案
      </button>
    </section>

    <section v-if="editingProfile" class="settings-section">
      <h2 class="settings-section__title">档案「{{ editingProfile.id }}」</h2>
      <label class="settings-field">
        <span class="settings-field__label">Provider</span>
        <select v-model="editingProfile.provider" class="settings-field__input" @change="applyProviderPreset">
          <option value="deepseek">DeepSeek</option>
          <option value="openai">OpenAI</option>
          <option value="qwen">Qwen</option>
          <option value="vllm">vLLM</option>
          <option value="mock">Mock（测试）</option>
        </select>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Base URL</span>
        <input v-model="editingProfile.base_url" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">Model</span>
        <input v-model="editingProfile.model" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">API Key 环境变量名</span>
        <input v-model="editingProfile.api_key_env" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-toggle">
        <input v-model="editingProfile.mock" type="checkbox" />
        <span>Mock 模式（不调用真实 LLM）</span>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">单条消息工具步上限（全局）</span>
        <input v-model.number="form.llm.max_tool_loops" class="settings-field__input" type="number" min="1" />
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
        <label class="settings-toggle">
          <input v-model="form.features.browser_enabled" type="checkbox" />
          <span class="settings-toggle__label">
            Browser 工具
            <span class="badge badge--beta" title="试验功能，接口与稳定性可能变更">Beta</span>
          </span>
        </label>
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
      <h2 class="settings-section__title">
        Browser 服务
        <span class="badge badge--beta" title="试验功能，接口与稳定性可能变更">Beta</span>
      </h2>
      <p class="setup-config-panel__hint">Browser 工具组目前为 Beta：依赖独立 browser 服务，能力与 schema 可能后续调整。</p>
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
