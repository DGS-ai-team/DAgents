<script setup>
import { computed, reactive, ref, onMounted } from "vue";
import * as api from "../api/node.js";
import { setTheme, themeStore } from "../stores/theme.js";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-chat" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
  mock: { base_url: "", model: "mock" },
};

const ALLOWED_PROVIDERS = new Set(["deepseek", "openai", "qwen", "vllm", "mock"]);

const THEME_OPTIONS = [
  { id: "light", title: "浅色" },
  { id: "dark", title: "深色" },
  { id: "system", title: "跟随系统" },
];

const emit = defineEmits(["completed"]);

const loading = ref(true);
const saving = ref(false);
const error = ref("");
/** @type {import('vue').Ref<'theme' | 'identity' | 'llm'>} */
const step = ref("theme");

const preferredName = ref("");
const nodeName = ref("");
const description = ref("");

const llm = reactive({
  id: "default",
  provider: "deepseek",
  base_url: PROVIDER_PRESETS.deepseek.base_url,
  model: PROVIDER_PRESETS.deepseek.model,
  api_key: "",
  mock: false,
  multimodal_enabled: false,
});

const probeState = reactive({
  loading: false,
  message: "",
  ok: false,
});
const probedModels = ref(/** @type {string[]} */ ([]));
const modelManual = ref(false);

const stepIndex = computed(() => {
  if (step.value === "theme") return 1;
  if (step.value === "identity") return 2;
  return 3;
});

const stepTitle = computed(() => {
  if (step.value === "theme") return "选择主题";
  if (step.value === "identity") return "Node 身份";
  return "配置模型";
});

const stepLead = computed(() => {
  if (step.value === "theme") return "先选择界面外观，可随时在侧栏再次切换。";
  if (step.value === "identity") return "告诉我们怎么称呼你，以及这台 Node 的名称。";
  return "再添加一条 LLM 配置，完成后即可使用本机功能。";
});

const canNextIdentity = computed(() => {
  return preferredName.value.trim().length > 0 && nodeName.value.trim().length > 0 && !saving.value;
});

const canSubmitLLM = computed(() => {
  if (saving.value) return false;
  const id = String(llm.id || "").trim();
  if (!id) return false;
  if (llm.mock || llm.provider === "mock") return true;
  return String(llm.model || "").trim().length > 0;
});

const useModelSelect = computed(() => probedModels.value.length > 0 && !modelManual.value);
const canProbe = computed(() => {
  if (llm.mock || llm.provider === "mock") return false;
  return String(llm.base_url || "").trim().length > 0;
});

onMounted(async () => {
  loading.value = true;
  error.value = "";
  try {
    const boot = await api.getUIBootstrap();
    preferredName.value = String(boot?.user?.preferred_name || "").trim();
    nodeName.value = String(boot?.agent?.name || "").trim() || "local-assistant";
    const setup = await api.getSetupConfig();
    if (setup?.agent?.description) {
      description.value = String(setup.agent.description);
    }
    if (setup?.user?.preferred_name) {
      preferredName.value = String(setup.user.preferred_name);
    }
    if (setup?.agent?.name) {
      nodeName.value = String(setup.agent.name);
    }
    const profiles = Array.isArray(setup?.llm?.profiles) ? setup.llm.profiles : [];
    const first = profiles[0];
    if (first) {
      llm.id = String(first.id || "default").trim() || "default";
      llm.provider = String(first.provider || "deepseek").trim() || "deepseek";
      llm.base_url = String(first.base_url || "").trim();
      llm.model = String(first.model || "").trim();
      llm.mock = !!first.mock || llm.provider === "mock";
      llm.multimodal_enabled = !!first.multimodal_enabled;
      if (!llm.base_url && PROVIDER_PRESETS[llm.provider]) {
        llm.base_url = PROVIDER_PRESETS[llm.provider].base_url;
      }
      if (!llm.model && PROVIDER_PRESETS[llm.provider]) {
        llm.model = PROVIDER_PRESETS[llm.provider].model;
      }
    } else if (setup?.llm?.mock) {
      llm.provider = "mock";
      llm.model = "mock";
      llm.mock = true;
      llm.base_url = "";
    }
  } catch (e) {
    error.value = e.message || "加载失败";
  } finally {
    loading.value = false;
  }
});

function chooseTheme(mode) {
  setTheme(mode);
}

function applyProviderPreset() {
  const preset = PROVIDER_PRESETS[llm.provider];
  if (!preset) return;
  llm.base_url = preset.base_url;
  llm.model = preset.model;
  llm.mock = llm.provider === "mock";
  probedModels.value = [];
  modelManual.value = false;
  probeState.message = "";
  probeState.ok = false;
}

function onMockToggle() {
  if (llm.mock) {
    llm.provider = "mock";
    llm.base_url = "";
    llm.model = "mock";
    probedModels.value = [];
    modelManual.value = false;
    probeState.message = "";
    probeState.ok = false;
    return;
  }
  if (llm.provider === "mock") {
    llm.provider = "deepseek";
    applyProviderPreset();
  }
}

function goTheme() {
  error.value = "";
  step.value = "theme";
}

function goIdentity() {
  error.value = "";
  step.value = "identity";
}

function goLLM() {
  if (!canNextIdentity.value) return;
  error.value = "";
  step.value = "llm";
}

async function runProbe() {
  error.value = "";
  probeState.message = "";
  probeState.ok = false;
  if (!canProbe.value) {
    probeState.message = "请先填写 Base URL（Mock 模式无需测试）";
    return;
  }
  probeState.loading = true;
  try {
    const data = await api.probeLLMModels({
      base_url: String(llm.base_url || "").trim(),
      api_key: String(llm.api_key || "").trim(),
      provider: llm.provider,
    });
    const models = Array.isArray(data?.models)
      ? data.models.map((m) => String(m?.id || m || "").trim()).filter(Boolean)
      : [];
    if (!models.length) {
      throw new Error("未返回模型列表");
    }
    probedModels.value = models;
    modelManual.value = false;
    const suggested = String(data?.suggested_provider || "").trim().toLowerCase();
    if (suggested && ALLOWED_PROVIDERS.has(suggested) && suggested !== "mock") {
      llm.provider = suggested;
      llm.mock = false;
    }
    if (!models.includes(llm.model)) {
      llm.model = models[0];
    }
    probeState.ok = true;
    probeState.message = `已获取 ${models.length} 个模型，可下拉选择`;
  } catch (e) {
    probedModels.value = [];
    modelManual.value = true;
    probeState.ok = false;
    probeState.message = e?.message || "测试失败，请手动填写模型名称";
  } finally {
    probeState.loading = false;
  }
}

async function submit() {
  if (!canSubmitLLM.value) return;
  saving.value = true;
  error.value = "";
  try {
    const profileId = String(llm.id || "").trim() || "default";
    const mock = !!llm.mock || llm.provider === "mock";
    await api.patchSetupConfig({
      user: { preferred_name: preferredName.value.trim() },
      agent: {
        name: nodeName.value.trim(),
        description: description.value || "",
      },
      llm: {
        active: profileId,
        profiles: [
          {
            id: profileId,
            provider: mock ? "mock" : llm.provider,
            base_url: mock ? "" : String(llm.base_url || "").trim(),
            model: mock ? "mock" : String(llm.model || "").trim(),
            api_key: mock ? undefined : String(llm.api_key || "").trim() || undefined,
            mock,
            multimodal_enabled: !!llm.multimodal_enabled,
          },
        ],
      },
      onboarding: { node_profile_completed: true },
    });
    emit("completed");
  } catch (e) {
    error.value = e.message || "保存失败";
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="first-run">
    <section class="first-run__card panel" :class="{ 'first-run__card--wide': step === 'llm' }">
      <header class="first-run__header">
        <div class="first-run__brand-row">
          <span class="first-run__brand">DAgents</span>
          <span class="pill">{{ stepIndex }} / 3</span>
        </div>
        <h1 class="first-run__title">{{ stepTitle }}</h1>
        <p class="first-run__lead">{{ stepLead }}</p>
      </header>

      <div v-if="loading" class="first-run__body">
        <p class="setup-config-panel__hint">加载中…</p>
      </div>

      <!-- Step 1: theme -->
      <div v-else-if="step === 'theme'" class="first-run__body">
        <div class="first-run__theme-row" role="radiogroup" aria-label="主题">
          <button
            v-for="opt in THEME_OPTIONS"
            :key="opt.id"
            type="button"
            class="first-run__theme-item"
            role="radio"
            :aria-checked="themeStore.mode === opt.id"
            :class="{ 'first-run__theme-item--active': themeStore.mode === opt.id }"
            @click="chooseTheme(opt.id)"
          >
            <span
              class="first-run__theme-dot"
              :data-theme-dot="opt.id"
              aria-hidden="true"
            />
            <span class="first-run__theme-label">{{ opt.title }}</span>
          </button>
        </div>
        <p v-if="error" class="first-run__error">{{ error }}</p>
        <div class="first-run__actions">
          <button type="button" class="btn btn--primary first-run__actions-grow" @click="goIdentity">
            下一步
          </button>
        </div>
      </div>

      <!-- Step 2: identity -->
      <form v-else-if="step === 'identity'" class="first-run__body" @submit.prevent="goLLM">
        <label class="settings-field">
          <span class="settings-field__label">怎么称呼你</span>
          <input
            v-model="preferredName"
            class="settings-field__input"
            type="text"
            maxlength="64"
            placeholder="例如：小明"
            autocomplete="nickname"
            autofocus
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Node 名称</span>
          <input
            v-model="nodeName"
            class="settings-field__input"
            type="text"
            maxlength="64"
            placeholder="注册到 Manage 后的展示名"
            autocomplete="off"
          />
        </label>
        <p v-if="error" class="first-run__error">{{ error }}</p>
        <div class="first-run__actions">
          <button type="button" class="btn btn--ghost" @click="goTheme">上一步</button>
          <button type="submit" class="btn btn--primary first-run__actions-grow" :disabled="!canNextIdentity">
            下一步
          </button>
        </div>
      </form>

      <!-- Step 3: LLM -->
      <form v-else class="first-run__body" @submit.prevent="submit">
        <label class="settings-field">
          <span class="settings-field__label">配置名称</span>
          <input v-model="llm.id" class="settings-field__input" type="text" placeholder="如 DeepSeek / 默认" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Provider</span>
          <select v-model="llm.provider" class="settings-field__input" @change="applyProviderPreset">
            <option value="deepseek">DeepSeek</option>
            <option value="openai">OpenAI</option>
            <option value="qwen">Qwen</option>
            <option value="vllm">vLLM</option>
            <option value="mock">Mock（测试）</option>
          </select>
        </label>
        <label class="settings-field">
          <span class="settings-field__label">Base URL</span>
          <input
            v-model="llm.base_url"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            :disabled="llm.mock || llm.provider === 'mock'"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">API Key（可留空）</span>
          <input
            v-model="llm.api_key"
            class="settings-field__input"
            type="password"
            autocomplete="new-password"
            placeholder="可选"
            :disabled="llm.mock || llm.provider === 'mock'"
          />
        </label>

        <div class="first-run__probe">
          <button
            type="button"
            class="btn btn--ghost btn--sm"
            :disabled="probeState.loading || !canProbe"
            @click="runProbe"
          >
            {{ probeState.loading ? "测试中…" : "测试并拉取模型" }}
          </button>
          <p
            v-if="probeState.message"
            class="first-run__probe-msg"
            :class="{ 'first-run__probe-msg--ok': probeState.ok }"
          >
            {{ probeState.message }}
          </p>
        </div>

        <label class="settings-field">
          <span class="settings-field__label">Model</span>
          <select v-if="useModelSelect" v-model="llm.model" class="settings-field__input">
            <option v-for="m in probedModels" :key="m" :value="m">{{ m }}</option>
          </select>
          <input
            v-else
            v-model="llm.model"
            class="settings-field__input"
            type="text"
            autocomplete="off"
            placeholder="模型名称"
            :disabled="llm.mock || llm.provider === 'mock'"
          />
          <button
            v-if="probedModels.length"
            type="button"
            class="first-run__text-btn"
            @click="modelManual = !modelManual"
          >
            {{ modelManual ? "改用下拉选择" : "改用手动输入" }}
          </button>
        </label>

        <label class="settings-toggle">
          <input v-model="llm.mock" type="checkbox" @change="onMockToggle" />
          <span>Mock 模式（无 Key 冒烟）</span>
        </label>

        <p v-if="error" class="first-run__error">{{ error }}</p>
        <div class="first-run__actions">
          <button type="button" class="btn btn--ghost" :disabled="saving" @click="goIdentity">上一步</button>
          <button type="submit" class="btn btn--primary first-run__actions-grow" :disabled="!canSubmitLLM">
            {{ saving ? "保存中…" : "开始使用" }}
          </button>
        </div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.first-run {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: var(--space-6);
  background: var(--color-bg);
  color: var(--color-text);
  font-family: var(--font-ui);
}

.first-run__card {
  width: min(440px, 100%);
  padding: 0;
}

.first-run__card--wide {
  width: min(520px, 100%);
}

.first-run__header {
  padding: 20px 20px 0;
}

.first-run__brand-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}

.first-run__brand {
  font-size: 18px;
  font-weight: 650;
  letter-spacing: 0.02em;
  color: var(--color-text);
}

.first-run__title {
  margin: 0 0 var(--space-2);
  font-size: 16px;
  font-weight: 600;
  color: var(--color-text);
}

.first-run__lead {
  margin: 0 0 var(--space-4);
  font-size: 13px;
  line-height: 1.5;
  color: var(--color-text-muted);
}

.first-run__body {
  padding: 4px 20px 20px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.first-run__theme-row {
  display: flex;
  justify-content: center;
  align-items: flex-start;
  gap: 28px;
  margin: 8px 0 16px;
  padding: 8px 0;
}

.first-run__theme-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  min-width: 72px;
  padding: 4px;
  border: none;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
}

.first-run__theme-dot {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 2px solid var(--color-border-strong);
  box-sizing: border-box;
  transition: border-color 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease;
}

.first-run__theme-item:hover .first-run__theme-dot {
  border-color: var(--color-primary);
  transform: scale(1.06);
}

.first-run__theme-item--active .first-run__theme-dot {
  border-color: var(--color-primary);
  box-shadow: 0 0 0 3px var(--color-primary-soft);
}

.first-run__theme-dot[data-theme-dot="light"] {
  background: #f3f3f3;
}

.first-run__theme-dot[data-theme-dot="dark"] {
  background: #202020;
}

.first-run__theme-dot[data-theme-dot="system"] {
  background: linear-gradient(135deg, #f3f3f3 50%, #202020 50%);
}

.first-run__theme-label {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--color-text-muted);
  transition: color 0.15s ease;
}

.first-run__theme-item--active .first-run__theme-label {
  color: var(--color-text);
  font-weight: 600;
}

.first-run__probe {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  margin-top: 8px;
}

.first-run__probe-msg {
  margin: 0;
  font-size: 12px;
  color: var(--color-warning);
  line-height: 1.4;
}

.first-run__probe-msg--ok {
  color: var(--color-text-muted);
}

.first-run__text-btn {
  align-self: flex-start;
  margin-top: 4px;
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--color-primary);
  font-size: 12px;
  cursor: pointer;
}

.first-run__actions {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}

.first-run__actions-grow {
  flex: 1;
}

.first-run__error {
  margin: 8px 0 0;
  color: var(--color-danger);
  font-size: 13px;
}

.first-run__body :deep(.settings-field) {
  margin-top: 10px;
}

.first-run__body :deep(.settings-field__input) {
  max-width: none;
  background: var(--color-input);
}

.first-run__body :deep(.settings-toggle) {
  margin-top: 12px;
}

@media (max-width: 520px) {
  .first-run {
    place-items: start center;
    padding-top: 10vh;
  }
}
</style>
