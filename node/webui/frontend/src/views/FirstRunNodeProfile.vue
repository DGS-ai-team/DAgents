<script setup>
import { computed, reactive, ref, onMounted } from "vue";
import * as api from "../api/node.js";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-chat" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
  mock: { base_url: "", model: "mock" },
};

const ALLOWED_PROVIDERS = new Set(["deepseek", "openai", "qwen", "vllm", "mock"]);

const emit = defineEmits(["completed"]);

const loading = ref(true);
const saving = ref(false);
const error = ref("");
/** @type {import('vue').Ref<'identity' | 'llm'>} */
const step = ref("identity");

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
    <div class="first-run__glow" aria-hidden="true" />
    <div class="first-run__panel" :class="{ 'first-run__panel--wide': step === 'llm' }">
      <p class="first-run__brand">DAgents</p>
      <p class="first-run__step">{{ step === "identity" ? "1 / 2" : "2 / 2" }}</p>
      <h1 class="first-run__title">{{ step === "identity" ? "开始使用" : "配置模型" }}</h1>
      <p class="first-run__lead">
        <template v-if="step === 'identity'">先告诉我们怎么称呼你，以及这台 Node 的名称。</template>
        <template v-else>再添加一条 LLM 配置，完成后即可使用本机功能。</template>
      </p>

      <div v-if="loading" class="first-run__hint">加载中…</div>

      <form v-else-if="step === 'identity'" class="first-run__form" @submit.prevent="goLLM">
        <label class="first-run__field">
          <span class="first-run__label">怎么称呼你</span>
          <input
            v-model="preferredName"
            class="first-run__input"
            type="text"
            maxlength="64"
            placeholder="例如：小明"
            autocomplete="nickname"
            autofocus
          />
        </label>
        <label class="first-run__field">
          <span class="first-run__label">Node 名称</span>
          <input
            v-model="nodeName"
            class="first-run__input"
            type="text"
            maxlength="64"
            placeholder="注册到 Manage 后的展示名"
            autocomplete="off"
          />
        </label>
        <p v-if="error" class="first-run__error">{{ error }}</p>
        <button class="first-run__cta" type="submit" :disabled="!canNextIdentity">下一步</button>
      </form>

      <form v-else class="first-run__form" @submit.prevent="submit">
        <label class="first-run__field">
          <span class="first-run__label">配置名称</span>
          <input v-model="llm.id" class="first-run__input" type="text" placeholder="如 DeepSeek / 默认" autocomplete="off" />
        </label>
        <label class="first-run__field">
          <span class="first-run__label">Provider</span>
          <select v-model="llm.provider" class="first-run__input" @change="applyProviderPreset">
            <option value="deepseek">DeepSeek</option>
            <option value="openai">OpenAI</option>
            <option value="qwen">Qwen</option>
            <option value="vllm">vLLM</option>
            <option value="mock">Mock（测试）</option>
          </select>
        </label>
        <label class="first-run__field">
          <span class="first-run__label">Base URL</span>
          <input
            v-model="llm.base_url"
            class="first-run__input"
            type="text"
            autocomplete="off"
            :disabled="llm.mock || llm.provider === 'mock'"
          />
        </label>
        <label class="first-run__field">
          <span class="first-run__label">API Key（可留空）</span>
          <input
            v-model="llm.api_key"
            class="first-run__input"
            type="password"
            autocomplete="new-password"
            placeholder="可选"
            :disabled="llm.mock || llm.provider === 'mock'"
          />
        </label>

        <div class="first-run__probe">
          <button
            type="button"
            class="first-run__secondary"
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

        <label class="first-run__field">
          <span class="first-run__label">Model</span>
          <select v-if="useModelSelect" v-model="llm.model" class="first-run__input">
            <option v-for="m in probedModels" :key="m" :value="m">{{ m }}</option>
          </select>
          <input
            v-else
            v-model="llm.model"
            class="first-run__input"
            type="text"
            autocomplete="off"
            placeholder="模型名称"
            :disabled="llm.mock || llm.provider === 'mock'"
          />
          <button
            v-if="probedModels.length"
            type="button"
            class="first-run__link"
            @click="modelManual = !modelManual"
          >
            {{ modelManual ? "改用下拉选择" : "改用手动输入" }}
          </button>
        </label>

        <label class="first-run__toggle">
          <input v-model="llm.mock" type="checkbox" @change="onMockToggle" />
          <span>Mock 模式（无 Key 冒烟）</span>
        </label>

        <p v-if="error" class="first-run__error">{{ error }}</p>
        <div class="first-run__actions">
          <button type="button" class="first-run__secondary" :disabled="saving" @click="goIdentity">上一步</button>
          <button class="first-run__cta first-run__cta--grow" type="submit" :disabled="!canSubmitLLM">
            {{ saving ? "保存中…" : "开始使用" }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.first-run {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: var(--space-6);
  position: relative;
  overflow: hidden;
  background:
    radial-gradient(1200px 600px at 12% -10%, rgba(0, 120, 212, 0.22), transparent 55%),
    radial-gradient(900px 500px at 90% 110%, rgba(96, 205, 255, 0.12), transparent 50%),
    linear-gradient(160deg, #1a1a1a 0%, #202020 45%, #181818 100%);
}

.first-run__glow {
  position: absolute;
  inset: auto auto 18% 50%;
  width: 42rem;
  height: 42rem;
  transform: translateX(-50%);
  background: radial-gradient(circle, rgba(0, 120, 212, 0.14), transparent 65%);
  pointer-events: none;
  animation: first-run-pulse 6s ease-in-out infinite;
}

.first-run__panel {
  width: min(420px, 100%);
  position: relative;
  z-index: 1;
  animation: first-run-rise 0.55s ease-out both;
}

.first-run__panel--wide {
  width: min(520px, 100%);
}

.first-run__brand {
  margin: 0 0 var(--space-2);
  font-size: 1.75rem;
  font-weight: 650;
  letter-spacing: 0.04em;
  color: var(--color-text);
}

.first-run__step {
  margin: 0 0 var(--space-2);
  font-size: 0.75rem;
  letter-spacing: 0.08em;
  color: var(--color-text-subtle);
}

.first-run__title {
  margin: 0 0 var(--space-2);
  font-size: 1.125rem;
  font-weight: 600;
  color: var(--color-text-muted);
}

.first-run__lead {
  margin: 0 0 var(--space-6);
  color: var(--color-text-subtle);
  line-height: 1.55;
}

.first-run__form {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.first-run__field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.first-run__label {
  font-size: 0.8125rem;
  color: var(--color-text-muted);
}

.first-run__input {
  height: 40px;
  padding: 0 var(--space-3);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: var(--color-input);
  color: var(--color-text);
  font: inherit;
  outline: none;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}

.first-run__input:focus {
  border-color: var(--color-primary);
  box-shadow: 0 0 0 1px var(--color-primary-soft);
}

.first-run__input:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.first-run__probe {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}

.first-run__probe-msg {
  margin: 0;
  font-size: 0.75rem;
  color: var(--color-warning);
  line-height: 1.4;
}

.first-run__probe-msg--ok {
  color: var(--color-text-muted);
}

.first-run__link {
  align-self: flex-start;
  margin-top: 2px;
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--color-primary);
  font-size: 0.75rem;
  cursor: pointer;
}

.first-run__toggle {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: 0.875rem;
  color: var(--color-text-muted);
}

.first-run__actions {
  display: flex;
  gap: var(--space-3);
  margin-top: var(--space-2);
}

.first-run__cta {
  margin-top: var(--space-2);
  height: 40px;
  border: none;
  border-radius: var(--radius-md);
  background: var(--color-primary);
  color: #fff;
  font: inherit;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s ease, transform 0.15s ease, opacity 0.15s ease;
}

.first-run__actions .first-run__cta {
  margin-top: 0;
}

.first-run__cta--grow {
  flex: 1;
}

.first-run__cta:hover:not(:disabled) {
  background: var(--color-primary-strong);
  transform: translateY(-1px);
}

.first-run__cta:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.first-run__secondary {
  height: 40px;
  padding: 0 var(--space-4);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--color-text-muted);
  font: inherit;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease, opacity 0.15s ease;
}

.first-run__secondary:hover:not(:disabled) {
  border-color: var(--color-border-strong);
  color: var(--color-text);
}

.first-run__secondary:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.first-run__error {
  margin: 0;
  color: var(--color-danger);
  font-size: 0.875rem;
}

.first-run__hint {
  color: var(--color-text-subtle);
}

@keyframes first-run-rise {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes first-run-pulse {
  0%,
  100% {
    opacity: 0.7;
    transform: translateX(-50%) scale(1);
  }
  50% {
    opacity: 1;
    transform: translateX(-50%) scale(1.06);
  }
}

@media (max-width: 520px) {
  .first-run {
    place-items: start center;
    padding-top: 12vh;
  }
}
</style>
