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
    const id = "default";
    form.llm.profiles.push({
      id,
      provider: form.llm.provider || "openai",
      base_url: form.llm.base_url || "",
      model: form.llm.model || "",
      api_key: "",
      has_api_key: false,
      mock: !!form.llm.mock,
      multimodal_enabled: !!form.features?.multimodal_enabled,
    });
  }
  if (!editingId.value || !form.llm.profiles.some((p) => p.id === editingId.value)) {
    editingId.value = form.llm.profiles[0].id;
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
    error.value = `配置 id「${id}」已存在`;
    return;
  }
  form.llm.profiles.push({
    id,
    provider: "deepseek",
    base_url: PROVIDER_PRESETS.deepseek.base_url,
    model: PROVIDER_PRESETS.deepseek.model,
    api_key: "",
    has_api_key: false,
    mock: false,
    multimodal_enabled: false,
  });
  editingId.value = id;
  newProfileId.value = "";
}

function removeProfile(id) {
  ensureProfiles();
  if (form.llm.profiles.length <= 1) {
    error.value = "至少保留一个 LLM 配置";
    return;
  }
  form.llm.profiles = form.llm.profiles.filter((p) => p.id !== id);
  if (editingId.value === id) {
    editingId.value = form.llm.profiles[0].id;
  }
}

function moveProfile(id, delta) {
  const list = form.llm.profiles;
  const idx = list.findIndex((p) => p.id === id);
  if (idx < 0) return;
  const next = idx + delta;
  if (next < 0 || next >= list.length) return;
  const copy = list.slice();
  const [item] = copy.splice(idx, 1);
  copy.splice(next, 0, item);
  form.llm.profiles = copy;
}

function featuresPayload() {
  const payload = { ...form.features };
  // 多模态跟随列表第一条（默认）配置。
  const first = form.llm.profiles?.[0];
  payload.multimodal_enabled = !!first?.multimodal_enabled;
  return payload;
}

function llmPayload() {
  ensureProfiles();
  return {
    max_tool_loops: form.llm.max_tool_loops,
    profiles: form.llm.profiles.map((p) => ({
      id: p.id,
      provider: p.provider,
      base_url: p.base_url,
      model: p.model,
      api_key: p.api_key || undefined,
      clear_api_key: !!p.clear_api_key,
      mock: p.mock || p.provider === "mock",
      thinking: p.thinking || undefined,
      reasoning_effort: p.reasoning_effort || undefined,
      multimodal_enabled: !!p.multimodal_enabled,
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
    <section class="settings-section settings-section--llm">
      <div class="settings-section__head">
        <h2 class="settings-section__title">LLM 配置</h2>
      </div>
      <p class="settings-section__desc">
        以卡片管理多套连接。列表<strong>第一条</strong>为默认；对话页可临时切换。API Key
        直接填写，加密写入本地 SQLite，不会出现在配置文件中。
      </p>

      <div class="llm-config-cards">
        <article
          v-for="(p, index) in profiles"
          :key="p.id"
          class="llm-config-card"
          :class="{
            'llm-config-card--active': editingId === p.id,
            'llm-config-card--default': index === 0,
          }"
        >
          <button type="button" class="llm-config-card__hit" @click="editingId = p.id">
            <div class="llm-config-card__top">
              <div class="llm-config-card__title-row">
                <span class="llm-config-card__id">{{ p.id }}</span>
                <span v-if="index === 0" class="llm-config-card__badge">默认</span>
                <span v-if="p.mock" class="llm-config-card__badge llm-config-card__badge--muted">Mock</span>
              </div>
              <div class="llm-config-card__meta">
                <span>{{ p.provider || "—" }}</span>
                <span class="llm-config-card__dot">·</span>
                <span>{{ p.model || "未设模型" }}</span>
              </div>
              <div class="llm-config-card__key">
                <span v-if="p.has_api_key || (p.api_key && p.api_key.length)">Key 已配置</span>
                <span v-else-if="p.mock">无需 Key</span>
                <span v-else class="llm-config-card__key--warn">未配置 Key</span>
              </div>
            </div>
          </button>
          <div class="llm-config-card__actions">
            <button type="button" class="btn btn--ghost btn--compact" :disabled="index === 0" title="上移" @click="moveProfile(p.id, -1)">↑</button>
            <button
              type="button"
              class="btn btn--ghost btn--compact"
              :disabled="index === profiles.length - 1"
              title="下移"
              @click="moveProfile(p.id, 1)"
            >
              ↓
            </button>
            <button
              v-if="profiles.length > 1"
              type="button"
              class="btn btn--ghost btn--compact"
              title="删除"
              @click="removeProfile(p.id)"
            >
              删除
            </button>
          </div>
        </article>
      </div>

      <div class="llm-config-add">
        <label class="settings-field llm-config-add__field">
          <span class="settings-field__label">新建配置 id</span>
          <input
            v-model="newProfileId"
            class="settings-field__input"
            type="text"
            placeholder="如 qwen-plus"
            autocomplete="off"
          />
        </label>
        <button type="button" class="btn btn--ghost" @click="addProfile">添加配置</button>
      </div>
    </section>

    <section v-if="editingProfile" class="settings-section settings-section--editor">
      <h2 class="settings-section__title">编辑「{{ editingProfile.id }}」</h2>
      <div class="setup-config-panel__field-grid">
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
          <span class="settings-field__label">Model</span>
          <input v-model="editingProfile.model" class="settings-field__input" type="text" autocomplete="off" />
        </label>
      </div>
      <label class="settings-field">
        <span class="settings-field__label">Base URL</span>
        <input v-model="editingProfile.base_url" class="settings-field__input" type="text" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">API Key</span>
        <input
          v-model="editingProfile.api_key"
          class="settings-field__input"
          type="password"
          autocomplete="new-password"
          :placeholder="editingProfile.has_api_key ? '已保存，留空则保持不变' : '直接输入 API Key'"
        />
      </label>
      <label v-if="editingProfile.has_api_key" class="settings-toggle">
        <input v-model="editingProfile.clear_api_key" type="checkbox" />
        <span>清除已保存的 API Key</span>
      </label>
      <div class="setup-config-panel__toggles setup-config-panel__toggles--row">
        <label class="settings-toggle">
          <input v-model="editingProfile.mock" type="checkbox" />
          <span>Mock 模式</span>
        </label>
        <label class="settings-toggle">
          <input v-model="editingProfile.multimodal_enabled" type="checkbox" />
          <span>多模态 / Vision</span>
        </label>
      </div>
      <label class="settings-field">
        <span class="settings-field__label">单条消息工具步上限（全局）</span>
        <input v-model.number="form.llm.max_tool_loops" class="settings-field__input" type="number" min="1" />
      </label>
    </section>

    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">Manage</h2>
      </div>
      <p class="settings-section__desc">注册到 Manage 以启用团队能力与 A2A。</p>
      <label class="settings-toggle">
        <input v-model="form.manage.enabled" type="checkbox" />
        <span>启用 Manage 注册与通信</span>
      </label>
      <div class="setup-config-panel__field-grid">
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
          <span class="settings-field__label">node_token（可选）</span>
          <input
            v-model="form.manage.node_token"
            class="settings-field__input"
            type="password"
            :disabled="manageFieldsDisabled"
            autocomplete="new-password"
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
          <span class="settings-field__label">Registration base_url</span>
          <input
            v-model="form.manage.registration_base_url"
            class="settings-field__input"
            type="text"
            :disabled="manageFieldsDisabled"
            autocomplete="off"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">心跳间隔（秒）</span>
          <input
            v-model.number="form.manage.registration_interval_seconds"
            class="settings-field__input"
            type="number"
            min="1"
            :disabled="manageFieldsDisabled"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">TTL（秒）</span>
          <input
            v-model.number="form.manage.registration_ttl_seconds"
            class="settings-field__input"
            type="number"
            min="1"
            :disabled="manageFieldsDisabled"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Inbox wait（秒）</span>
          <input
            v-model.number="form.manage.a2a_inbox_wait_seconds"
            class="settings-field__input"
            type="number"
            min="1"
            :disabled="manageFieldsDisabled"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Inbox poll（秒）</span>
          <input
            v-model.number="form.manage.a2a_inbox_poll_seconds"
            class="settings-field__input"
            type="number"
            min="1"
            :disabled="manageFieldsDisabled"
          />
        </label>
      </div>
      <label class="settings-toggle">
        <input v-model="form.manage.a2a_enabled" type="checkbox" :disabled="manageFieldsDisabled" />
        <span>启用 A2A Inbox</span>
      </label>
    </section>

    <section class="settings-section">
      <div class="settings-section__head">
        <h2 class="settings-section__title">功能开关</h2>
      </div>
      <div class="setup-config-panel__toggles setup-config-panel__toggles--grid">
        <label class="settings-toggle"><input v-model="form.features.skills_enabled" type="checkbox" /><span>Skills</span></label>
        <label class="settings-toggle"><input v-model="form.features.triggers_enabled" type="checkbox" /><span>Triggers</span></label>
        <label class="settings-toggle"><input v-model="form.features.child_agents_enabled" type="checkbox" /><span>Child Agents</span></label>
        <label class="settings-toggle"><input v-model="form.features.ui_enabled" type="checkbox" /><span>Web UI</span></label>
        <label class="settings-toggle"
          ><input v-model="form.features.browser_enabled" type="checkbox" />
          <span class="settings-toggle__label">
            Browser 工具
            <span class="badge badge--beta" title="试验功能，接口与稳定性可能变更">Beta</span>
          </span>
        </label>
        <label class="settings-toggle"
          ><input v-model="form.features.raw_message_history_enabled" type="checkbox" /><span>原始消息 JSONL</span></label
        >
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
      <div class="settings-section__head">
        <h2 class="settings-section__title">
          Browser 服务
          <span class="badge badge--beta" title="试验功能，接口与稳定性可能变更">Beta</span>
        </h2>
      </div>
      <p class="settings-section__desc">依赖独立 browser 服务，能力与 schema 可能后续调整。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">Service URL</span>
          <input v-model="form.browser.service_url" class="settings-field__input" type="text" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">默认超时（ms）</span>
          <input v-model.number="form.browser.default_timeout_ms" class="settings-field__input" type="number" min="1000" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">最大并发 session</span>
          <input v-model.number="form.browser.max_sessions" class="settings-field__input" type="number" min="1" />
        </label>
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
      </div>
      <div class="setup-config-panel__toggles setup-config-panel__toggles--row">
        <label class="settings-toggle">
          <input v-model="form.browser.headed" type="checkbox" />
          <span>Headed 模式</span>
        </label>
        <label class="settings-toggle">
          <input v-model="form.browser.ignore_https_errors" type="checkbox" />
          <span>忽略 HTTPS 证书错误</span>
        </label>
      </div>
    </section>
  </ConfigPanelShell>
</template>
