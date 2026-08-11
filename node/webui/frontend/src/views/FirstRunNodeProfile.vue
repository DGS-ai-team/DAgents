<script setup>
import { computed, reactive, ref, onMounted } from "vue";
import * as api from "../api/node.js";
import { setTheme, themeStore } from "../stores/theme.js";
import UiSelect from "../components/UiSelect.vue";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-chat" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
};

const PROVIDER_OPTIONS = [
  { value: "deepseek", label: "DeepSeek" },
  { value: "openai", label: "OpenAI" },
  { value: "qwen", label: "Qwen" },
  { value: "vllm", label: "vLLM" },
];

const ALLOWED_PROVIDERS = new Set(["deepseek", "openai", "qwen", "vllm"]);

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
/** 称呼失焦后用于页头寒暄 */
const greetedName = ref("");

const llm = reactive({
  id: PROVIDER_PRESETS.deepseek.model,
  provider: "deepseek",
  base_url: PROVIDER_PRESETS.deepseek.base_url,
  model: PROVIDER_PRESETS.deepseek.model,
  api_key: "",
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
  if (step.value === "theme") return "你喜欢哪种外观？";
  if (step.value === "identity") return "先认识一下";
  return "最后，接上模型";
});

const stepLead = computed(() => {
  if (step.value === "theme") return "选一个顺眼的就行，之后还能在侧栏随时改。";
  if (step.value === "identity") {
    if (greetedName.value) return `你好呀，${greetedName.value}`;
    return "告诉我怎么称呼你，再给这台 Node 起个名字。";
  }
  if (probeState.message) return probeState.message;
  return "配好一条 LLM，我们就可以开始聊天了。";
});

const canNextIdentity = computed(() => {
  return preferredName.value.trim().length > 0 && nodeName.value.trim().length > 0 && !saving.value;
});

const canSubmitLLM = computed(() => {
  if (saving.value) return false;
  return String(llm.model || "").trim().length > 0;
});

const useModelSelect = computed(() => probedModels.value.length > 0 && !modelManual.value);
const canProbe = computed(() => String(llm.base_url || "").trim().length > 0);
const modelOptions = computed(() =>
  probedModels.value.map((m) => ({ value: m, label: m })),
);

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
    const first = profiles.find((p) => !p?.mock && String(p?.provider || "") !== "mock") || profiles[0];
    if (first && !first.mock && String(first.provider || "") !== "mock") {
      const provider = String(first.provider || "deepseek").trim() || "deepseek";
      llm.provider = ALLOWED_PROVIDERS.has(provider) ? provider : "deepseek";
      llm.base_url = String(first.base_url || "").trim();
      llm.model = String(first.model || "").trim();
      llm.multimodal_enabled = !!first.multimodal_enabled;
      if (!llm.base_url && PROVIDER_PRESETS[llm.provider]) {
        llm.base_url = PROVIDER_PRESETS[llm.provider].base_url;
      }
      if (!llm.model && PROVIDER_PRESETS[llm.provider]) {
        llm.model = PROVIDER_PRESETS[llm.provider].model;
      }
      // 首配默认用模型名作配置名；旧的 default 占位不保留
      const prevId = String(first.id || "").trim();
      llm.id =
        (llm.model && (prevId === "default" || !prevId) ? llm.model : prevId || llm.model) ||
        PROVIDER_PRESETS.deepseek.model;
    }
  } catch (e) {
    error.value = e.message || "没加载上来，请稍后再试";
  } finally {
    loading.value = false;
  }
});

function chooseTheme(mode) {
  setTheme(mode);
}

function onPreferredNameBlur() {
  const name = preferredName.value.trim();
  greetedName.value = name;
}

/** 首配：配置名默认跟模型名（与连接页卡片展示一致）。 */
function syncIdFromModel() {
  const m = String(llm.model || "").trim();
  if (m) llm.id = m;
}

function applyProviderPreset() {
  const preset = PROVIDER_PRESETS[llm.provider];
  if (!preset) return;
  llm.base_url = preset.base_url;
  llm.model = preset.model;
  syncIdFromModel();
  probedModels.value = [];
  modelManual.value = false;
  probeState.message = "";
  probeState.ok = false;
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
    probeState.message = "先填一下接口地址，我才能帮你试试连接。";
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
      throw new Error("empty");
    }
    probedModels.value = models;
    modelManual.value = false;
    const suggested = String(data?.suggested_provider || "").trim().toLowerCase();
    if (suggested && ALLOWED_PROVIDERS.has(suggested)) {
      llm.provider = suggested;
    }
    if (!models.includes(llm.model)) {
      llm.model = models[0];
    }
    syncIdFromModel();
    probeState.ok = true;
    probeState.message = `连上了，找到 ${models.length} 个模型。`;
  } catch (e) {
    probedModels.value = [];
    modelManual.value = true;
    probeState.ok = false;
    probeState.message = "看起来没有成功获取到模型呢。";
    void e;
  } finally {
    probeState.loading = false;
  }
}

async function submit() {
  if (!canSubmitLLM.value) return;
  saving.value = true;
  error.value = "";
  try {
    const model = String(llm.model || "").trim();
    const profileId = model || String(llm.id || "").trim() || "default";
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
            provider: llm.provider,
            base_url: String(llm.base_url || "").trim(),
            model,
            api_key: String(llm.api_key || "").trim() || undefined,
            mock: false,
            multimodal_enabled: !!llm.multimodal_enabled,
          },
        ],
      },
      onboarding: { node_profile_completed: true },
    });
    emit("completed");
  } catch (e) {
    error.value = e.message || "没保存成功，请再试一次";
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="first-run">
    <span class="first-run__brand">DAgents</span>

    <section class="first-run__shell" :class="{ 'first-run__shell--wide': step === 'llm' }">
      <header class="first-run__header">
        <h1 class="first-run__title">{{ stepTitle }}</h1>
        <p class="first-run__lead">
          {{ stepLead }}
        </p>
      </header>

      <div v-if="loading" class="first-run__body">
        <p class="first-run__hint">稍等，正在准备…</p>
      </div>

      <!-- Step 1: theme -->
      <div
        v-else-if="step === 'theme'"
        class="first-run__body"
        tabindex="0"
        @keydown.enter.prevent="goIdentity"
      >
        <div class="first-run__segment" role="radiogroup" aria-label="主题">
          <button
            v-for="opt in THEME_OPTIONS"
            :key="opt.id"
            type="button"
            class="first-run__segment-item"
            role="radio"
            :aria-checked="themeStore.mode === opt.id"
            :class="{ 'first-run__segment-item--active': themeStore.mode === opt.id }"
            @click="chooseTheme(opt.id)"
          >
            <span class="first-run__swatch" :data-theme-dot="opt.id" aria-hidden="true" />
            {{ opt.title }}
          </button>
        </div>
        <p v-if="error" class="first-run__error">{{ error }}</p>
      </div>

      <!-- Step 2: identity -->
      <form v-else-if="step === 'identity'" class="first-run__body first-run__body--form" @submit.prevent="goLLM">
        <label class="settings-field">
          <span class="settings-field__label">我该怎么称呼你？</span>
          <input
            v-model="preferredName"
            class="settings-field__input"
            type="text"
            maxlength="64"
            placeholder="例如：小明"
            autocomplete="nickname"
            autofocus
            @blur="onPreferredNameBlur"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">这台 Node 叫什么？</span>
          <input
            v-model="nodeName"
            class="settings-field__input"
            type="text"
            maxlength="64"
            placeholder="例如：我的工作站"
            autocomplete="off"
          />
        </label>
        <p v-if="error" class="first-run__error">{{ error }}</p>
      </form>

      <!-- Step 3: LLM -->
      <form v-else id="first-run-llm" class="first-run__body first-run__body--form" @submit.prevent="submit">
        <label class="settings-field">
          <span class="settings-field__label">用哪家模型？</span>
          <UiSelect
            v-model="llm.provider"
            :options="PROVIDER_OPTIONS"
            @change="applyProviderPreset"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">接口地址</span>
          <div class="first-run__row">
            <input
              v-model="llm.base_url"
              class="settings-field__input"
              type="text"
              autocomplete="off"
            />
            <button
              type="button"
              class="btn btn--ghost btn--sm first-run__probe-btn"
              :disabled="probeState.loading || !canProbe"
              @click="runProbe"
            >
              {{ probeState.loading ? "连接中…" : "试连接" }}
            </button>
          </div>
        </label>
        <label class="settings-field">
          <span class="settings-field__label">API Key（没有也可以先跳过）</span>
          <input
            v-model="llm.api_key"
            class="settings-field__input"
            type="password"
            autocomplete="new-password"
            placeholder="粘贴你的 Key"
          />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">具体用哪个模型？</span>
          <UiSelect
            v-if="useModelSelect"
            v-model="llm.model"
            class="first-run__control"
            :options="modelOptions"
            @change="syncIdFromModel"
          />
          <input
            v-else
            v-model="llm.model"
            class="settings-field__input first-run__control"
            type="text"
            autocomplete="off"
            placeholder="例如：deepseek-chat"
            @change="syncIdFromModel"
          />
          <div class="first-run__text-btn-slot">
            <button
              type="button"
              class="first-run__text-btn"
              :hidden="!probedModels.length"
              @click="modelManual = !modelManual"
            >
              {{ modelManual ? "还是用下拉选择吧" : "我想自己输入" }}
            </button>
          </div>
        </label>

        <p v-if="error" class="first-run__error">{{ error }}</p>
      </form>

      <footer v-if="!loading" class="first-run__footer">
        <button
          v-if="step !== 'theme'"
          type="button"
          class="btn btn--ghost"
          :disabled="saving"
          @click="step === 'llm' ? goIdentity() : goTheme()"
        >
          返回上一步
        </button>
        <button
          v-if="step === 'theme'"
          type="button"
          class="btn btn--primary first-run__footer-primary"
          @click="goIdentity"
        >
          继续
        </button>
        <button
          v-else-if="step === 'identity'"
          type="button"
          class="btn btn--primary first-run__footer-primary"
          :disabled="!canNextIdentity"
          @click="goLLM"
        >
          继续
        </button>
        <button
          v-else
          type="submit"
          form="first-run-llm"
          class="btn btn--primary first-run__footer-primary"
          :disabled="!canSubmitLLM"
        >
          {{ saving ? "正在保存…" : "开始使用吧" }}
        </button>
      </footer>

      <div class="first-run__progress-wrap">
        <div
          class="first-run__progress"
          aria-label="进度"
          :aria-valuenow="stepIndex"
          aria-valuemin="1"
          aria-valuemax="3"
          role="progressbar"
        >
          <span
            v-for="n in 3"
            :key="n"
            class="first-run__progress-seg"
            :class="{
              'first-run__progress-seg--done': n < stepIndex,
              'first-run__progress-seg--current': n === stepIndex,
            }"
          />
        </div>
        <span class="first-run__progress-label">{{ stepIndex }} / 3</span>
      </div>
    </section>
  </div>
</template>

<style scoped>
.first-run {
  position: relative;
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: var(--space-6);
  background: var(--app-background);
  color: var(--color-text);
  font-family: var(--font-ui);
}

.first-run__brand {
  position: absolute;
  top: var(--space-5);
  left: var(--space-5);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--color-text-subtle);
}

.first-run__shell {
  width: min(400px, 100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-5);
  text-align: center;
}

.first-run__shell--wide {
  width: min(440px, 100%);
}

.first-run__header {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 0;
  width: 100%;
}

.first-run__title {
  margin: 0 0 8px;
  font-size: 22px;
  font-weight: 600;
  letter-spacing: -0.02em;
  line-height: 1.3;
  color: var(--color-text);
}

.first-run__lead {
  margin: 0;
  width: 100%;
  min-height: calc(14px * 1.5 * 2);
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
  font-size: 14px;
  line-height: 1.5;
  color: var(--color-text-muted);
}

.first-run__body {
  width: 100%;
  padding: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0;
  outline: none;
}

.first-run__body--form {
  align-items: stretch;
  text-align: left;
}

.first-run__hint {
  margin: 0;
  font-size: 13px;
  color: var(--color-text-muted);
}

.first-run__segment {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
  margin: var(--space-1) 0 0;
}

.first-run__segment-item {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 36px;
  padding: 0 14px;
  border: 1px solid transparent;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--color-text-muted);
  font: inherit;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease, border-color 0.12s ease;
}

.first-run__segment-item:hover {
  background: var(--color-surface-hover);
  color: var(--color-text);
}

.first-run__segment-item--active {
  background: var(--color-surface-elevated);
  border-color: var(--color-border-strong);
  color: var(--color-text);
}

.first-run__swatch {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 1px solid var(--color-border-strong);
  flex-shrink: 0;
}

.first-run__swatch[data-theme-dot="light"] {
  background: #f3f3f3;
}

.first-run__swatch[data-theme-dot="dark"] {
  background: #202020;
}

.first-run__swatch[data-theme-dot="system"] {
  background: linear-gradient(135deg, #f3f3f3 50%, #202020 50%);
}

.first-run__row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.first-run__row .settings-field__input {
  flex: 1;
  min-width: 0;
}

.first-run__probe-btn {
  flex-shrink: 0;
  white-space: nowrap;
}

.first-run__text-btn-slot {
  display: flex;
  align-items: center;
  min-height: 20px;
  margin-top: 4px;
}

.first-run__text-btn {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 12px;
  line-height: 20px;
  cursor: pointer;
}

.first-run__text-btn:hover {
  color: var(--color-text);
  text-decoration: underline;
}

.first-run__control {
  height: var(--control-height);
  box-sizing: border-box;
}

.first-run__footer {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 0;
  width: 100%;
}

.first-run__footer-primary {
  min-width: 108px;
}

.first-run__footer :deep(.btn--primary) {
  background: var(--color-primary);
  border-color: var(--color-primary);
  color: #ffffff;
}

.first-run__footer :deep(.btn--primary:hover:not(:disabled)) {
  background: var(--color-primary-strong);
  border-color: var(--color-primary-strong);
  color: #ffffff;
}

.first-run__footer :deep(.btn--ghost) {
  color: var(--color-text-muted);
}

.first-run__footer :deep(.btn--ghost:hover:not(:disabled)) {
  color: var(--color-text);
  background: var(--color-surface-hover);
}

.first-run__progress-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  margin-top: var(--space-1);
}

.first-run__progress {
  display: flex;
  gap: 4px;
  width: 72px;
}

.first-run__progress-seg {
  flex: 1;
  height: 3px;
  border-radius: 2px;
  background: var(--color-border);
  transition: flex-grow 0.15s ease, background 0.15s ease;
}

.first-run__progress-seg--done {
  background: var(--color-text-subtle);
}

.first-run__progress-seg--current {
  flex: 1.6;
  background: var(--color-text);
}

.first-run__progress-label {
  font-size: 11px;
  letter-spacing: 0.04em;
  color: var(--color-text-subtle);
}

.first-run__error {
  margin: var(--space-3) 0 0;
  color: var(--color-danger);
  font-size: 13px;
}

.first-run__body :deep(.settings-field) {
  margin-top: var(--space-3);
}

.first-run__body :deep(.settings-field:first-child) {
  margin-top: 0;
}

.first-run__body :deep(.settings-field__input) {
  max-width: none;
  background-color: var(--color-input);
}

.first-run__body :deep(.ui-select) {
  max-width: none;
}

.first-run__body :deep(.ui-select__trigger) {
  background: var(--color-input);
}

@media (max-width: 520px) {
  .first-run {
    place-items: start center;
    padding: var(--space-4);
    padding-top: 10vh;
  }

  .first-run__brand {
    top: var(--space-4);
    left: var(--space-4);
  }

  .first-run__row {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
