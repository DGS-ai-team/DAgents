<script setup>
import { computed, reactive, ref, watch } from "vue";
import { getLLMCapabilities, probeLLMModels } from "../api/node.js";
import UiSelect from "./UiSelect.vue";
import UiIcon from "./UiIcon.vue";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-flash" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
  glm: { base_url: "https://open.bigmodel.cn/api/paas/v4", model: "glm-5.2" },
  minimax: { base_url: "https://api.minimaxi.com/v1", model: "MiniMax-M3" },
  mimo: { base_url: "https://api.xiaomimimo.com/v1", model: "mimo-v2.5" },
  mock: { base_url: "", model: "mock" },
};

const PROVIDER_OPTIONS = [
  { value: "deepseek", label: "DeepSeek" },
  { value: "openai", label: "OpenAI" },
  { value: "qwen", label: "Qwen" },
  { value: "vllm", label: "vLLM" },
  { value: "glm", label: "GLM" },
  { value: "minimax", label: "MiniMax" },
  { value: "mimo", label: "MiMo" },
  { value: "mock", label: "Mock（测试）" },
];

const ALLOWED_PROVIDERS = new Set([
  "deepseek",
  "openai",
  "qwen",
  "vllm",
  "glm",
  "minimax",
  "mimo",
  "mock",
]);

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: "edit" }, // create | edit
  profile: { type: Object, default: null },
  existingIds: { type: Array, default: () => [] },
});

const emit = defineEmits(["close", "confirm"]);

const draft = reactive(emptyDraft());
const localError = reactive({ message: "" });
const probeState = reactive({
  loading: false,
  message: "",
  ok: false,
});
const probedModels = ref(/** @type {string[]} */ ([]));
const modelManual = ref(false);
const capability = ref(null);
const capabilityError = ref("");
const capabilityLoading = ref(false);
let capabilityRequest = 0;
let lastCapabilityKey = "";

const title = computed(() => (props.mode === "create" ? "新建模型配置" : "编辑模型配置"));
const currentId = computed(() => String(props.profile?.id || "").trim());
const originalId = computed(() => String(props.profile?.original_id || props.profile?.id || "").trim());
const canProbe = computed(() => {
  if (draft.mock || draft.provider === "mock") return false;
  return String(draft.base_url || "").trim().length > 0;
});
const useModelSelect = computed(() => probedModels.value.length > 0 && !modelManual.value);
const EFFORT_LABELS = {
  none: "无",
  minimal: "最低",
  low: "低",
  medium: "中",
  high: "高",
  xhigh: "极高",
  max: "最高",
};
const effortOptions = computed(() =>
  (capability.value?.allowed_efforts || []).map((value) => ({
    value,
    label: `${EFFORT_LABELS[value] || value}（${value}）`,
  })),
);
const modelOptions = computed(() =>
  probedModels.value.map((m) => ({ value: m, label: m })),
);

function emptyDraft() {
  return {
    id: "",
    provider: "deepseek",
    base_url: PROVIDER_PRESETS.deepseek.base_url,
    model: PROVIDER_PRESETS.deepseek.model,
    api_key: "",
    has_api_key: false,
    clear_api_key: false,
    mock: false,
    multimodal_enabled: false,
    thinking: "enabled",
    reasoning_effort: "",
    thinking_budget: null,
  };
}

function resetProbeUI() {
  probedModels.value = [];
  modelManual.value = false;
  probeState.loading = false;
  probeState.message = "";
  probeState.ok = false;
}

function resetFromProps() {
  localError.message = "";
  resetProbeUI();
  if (props.mode === "create") {
    Object.assign(draft, emptyDraft());
    draft.id = suggestId();
    lastCapabilityKey = `${draft.provider}\0${draft.model}`;
    return;
  }
  const src = props.profile || {};
  Object.assign(draft, emptyDraft(), {
    id: src.id || "",
    provider: src.provider || "deepseek",
    base_url: src.base_url || "",
    model: src.model || "",
    api_key: "",
    has_api_key: !!src.has_api_key,
    clear_api_key: false,
    mock: !!src.mock,
    multimodal_enabled: !!src.multimodal_enabled,
    thinking: src.thinking || "enabled",
    reasoning_effort: src.reasoning_effort || "",
    thinking_budget: src.thinking_budget ?? null,
  });
  lastCapabilityKey = `${draft.provider}\0${draft.model}`;
}

async function loadModelCapabilities() {
  const provider = String(draft.provider || "").trim();
  const model = String(draft.model || "").trim();
  const key = `${provider}\0${model}`;
  const request = ++capabilityRequest;
  capability.value = null;
  capabilityError.value = "";
  capabilityLoading.value = false;
  if (!props.open || !provider || !model) return;
  capabilityLoading.value = true;

  const modelChanged = lastCapabilityKey && lastCapabilityKey !== key;
  lastCapabilityKey = key;
  if (modelChanged) {
    draft.thinking = "enabled";
    draft.reasoning_effort = "";
    draft.thinking_budget = null;
  }
  try {
    const result = await getLLMCapabilities(provider, model);
    if (request !== capabilityRequest) return;
    capability.value = result;
    if (!result?.supported) {
      draft.thinking = "";
      draft.reasoning_effort = "";
      draft.thinking_budget = null;
    } else {
      if (!result.can_disable_thinking) draft.thinking = "enabled";
      if (result.thinking_control !== "effort") draft.reasoning_effort = "";
      if (result.thinking_control !== "budget") draft.thinking_budget = null;
    }
    if (result?.thinking_control === "effort" && (modelChanged || !draft.reasoning_effort)) {
      draft.reasoning_effort = result?.default_effort || draft.reasoning_effort;
    }
  } catch (error) {
    if (request === capabilityRequest) capabilityError.value = error?.message || "暂时无法读取模型能力";
  } finally {
    if (request === capabilityRequest) capabilityLoading.value = false;
  }
}

function suggestId() {
  const used = new Set((props.existingIds || []).map((x) => String(x)));
  if (!used.has("default")) return "default";
  let n = 2;
  while (used.has(`llm-${n}`)) n += 1;
  return `llm-${n}`;
}

function applyProviderPreset() {
  const preset = PROVIDER_PRESETS[draft.provider];
  if (!preset) return;
  draft.base_url = preset.base_url;
  draft.model = preset.model;
  draft.mock = draft.provider === "mock";
  resetProbeUI();
}

/** 新建时可用模型名建议配置名称；编辑时保留用户当前 ID。 */
function syncNameFromModel(model) {
  if (props.mode !== "create") return;
  const m = String(model || "").trim();
  if (m) draft.id = m;
}

function onModelSelect() {
  syncNameFromModel(draft.model);
}

function onBackdropClick(event) {
  if (event.target === event.currentTarget) emit("close");
}

async function runProbe() {
  localError.message = "";
  probeState.message = "";
  probeState.ok = false;
  if (!canProbe.value) {
    probeState.message = "请先填写 Base URL（测试模式（Mock）无需测试）";
    return;
  }
  probeState.loading = true;
  try {
    const payload = {
      base_url: String(draft.base_url || "").trim(),
      api_key: String(draft.api_key || "").trim(),
      provider: draft.provider,
    };
    if (!payload.api_key && draft.has_api_key && !draft.clear_api_key && originalId.value) {
      payload.profile_id = originalId.value;
    }
    const data = await probeLLMModels(payload);
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
      draft.provider = suggested;
      draft.mock = false;
    }
    if (!models.includes(draft.model)) {
      draft.model = models[0];
    }
    syncNameFromModel(draft.model);
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

function submit() {
  localError.message = "";
  const id = String(draft.id || "").trim();
  if (!id) {
    localError.message = "请填写配置名称（将显示在输入栏）";
    return;
  }
  const others = (props.existingIds || []).filter((x) => x !== currentId.value);
  if (others.includes(id)) {
    localError.message = `配置「${id}」已存在`;
    return;
  }
  if (!String(draft.model || "").trim() && !(draft.mock || draft.provider === "mock")) {
    localError.message = "请填写或选择模型名称";
    return;
  }
  emit("confirm", {
    id,
    original_id: originalId.value || undefined,
    provider: draft.provider,
    base_url: draft.base_url,
    model: draft.model,
    api_key: draft.api_key || "",
    has_api_key: draft.has_api_key,
    clear_api_key: !!draft.clear_api_key,
    mock: draft.mock || draft.provider === "mock",
    multimodal_enabled: !!draft.multimodal_enabled,
    thinking: draft.thinking,
    reasoning_effort: draft.reasoning_effort,
    thinking_budget: draft.thinking_budget === "" || draft.thinking_budget == null ? null : Number(draft.thinking_budget),
    clear_reasoning_effort: !draft.reasoning_effort,
    clear_thinking_budget: draft.thinking_budget === "" || draft.thinking_budget == null,
  });
}

watch(
  () => props.open,
  (visible) => {
    if (visible) {
      resetFromProps();
      loadModelCapabilities();
    } else {
      capabilityRequest += 1;
      capability.value = null;
    }
  },
  { immediate: true }
);

watch(() => [draft.provider, draft.model], () => {
  if (props.open) loadModelCapabilities();
});

watch(
  () => [draft.base_url, draft.api_key, draft.clear_api_key],
  () => {
    // URL / Key 变更后清空探测结果，避免选到过期列表。
    if (probedModels.value.length || probeState.message) {
      probedModels.value = [];
      modelManual.value = false;
      probeState.ok = false;
      probeState.message = "";
    }
  }
);

</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="ui-dialog-backdrop llm-profile-overlay" @click="onBackdropClick">
      <section class="ui-dialog llm-profile-modal" role="dialog" aria-modal="true" :aria-labelledby="'llm-profile-title'">
        <header class="llm-profile-modal__header">
          <h2 id="llm-profile-title" class="llm-profile-modal__title">{{ title }}</h2>
          <button type="button" class="btn btn--ghost btn--sm desktop-icon-button" aria-label="关闭" @click="emit('close')"><UiIcon name="close" :size="18" /></button>
        </header>

        <div class="llm-profile-modal__body">
          <div class="llm-profile-modal__grid">
            <label class="settings-field llm-profile-modal__span">
              <span class="settings-field__label">名称（输入栏展示）</span>
              <input
                v-model="draft.id"
                class="settings-field__input"
                type="text"
                placeholder="如 DeepSeek / 默认"
                autocomplete="off"
              />
            </label>

            <label class="settings-field llm-profile-modal__span">
              <span class="settings-field__label">Base URL</span>
              <input v-model="draft.base_url" class="settings-field__input" type="text" autocomplete="off" />
            </label>
            <label class="settings-field llm-profile-modal__span">
              <span class="settings-field__label">API Key（可留空）</span>
              <input
                v-model="draft.api_key"
                class="settings-field__input"
                type="password"
                autocomplete="new-password"
                :placeholder="draft.has_api_key ? '已保存，留空则保持不变' : '可选'"
              />
            </label>

            <div class="llm-profile-modal__span llm-profile-modal__probe">
              <button
                type="button"
                class="btn btn--ghost"
                :disabled="probeState.loading || !canProbe"
                @click="runProbe"
              >
                {{ probeState.loading ? "测试中…" : "测试并拉取模型" }}
              </button>
              <p
                v-if="probeState.message"
                class="llm-profile-modal__probe-msg"
                :class="{ 'llm-profile-modal__probe-msg--ok': probeState.ok }"
              >
                {{ probeState.message }}
              </p>
              <p v-else class="settings-field__hint">
                先填 URL 与 Key，点击测试将请求兼容接口 <code>/models</code>；失败时可手动填写模型。
              </p>
            </div>

            <label class="settings-field">
              <span class="settings-field__label">服务商</span>
              <UiSelect
                v-model="draft.provider"
                :options="PROVIDER_OPTIONS"
                aria-label="服务商"
                @change="applyProviderPreset"
              />
            </label>
            <label class="settings-field">
              <span class="settings-field__label">模型</span>
              <UiSelect
                v-if="useModelSelect"
                v-model="draft.model"
                :options="modelOptions"
                aria-label="模型"
                @change="onModelSelect"
              />
              <input
                v-else
                v-model="draft.model"
                class="settings-field__input"
                type="text"
                autocomplete="off"
                placeholder="模型名称"
                aria-label="模型"
              />
              <button
                v-if="probedModels.length"
                type="button"
                class="llm-profile-modal__model-toggle"
                @click="modelManual = !modelManual"
              >
                {{ modelManual ? "改用下拉选择" : "改用手动输入" }}
              </button>
            </label>
          </div>

          <section v-if="capability?.supported" class="llm-profile-modal__reasoning">
            <h3 class="llm-profile-modal__section-title">思考与推理</h3>
            <p class="settings-field__hint">此配置的参数会应用于引用它的智能体。</p>

            <label v-if="capability.thinking_control === 'effort'" class="settings-field">
              <span class="settings-field__label">{{ capability.thinking_secondary_label || "推理强度" }}</span>
              <UiSelect
                v-model="draft.reasoning_effort"
                :options="effortOptions"
                :aria-label="capability.thinking_secondary_label || '推理强度'"
              />
            </label>

            <label v-else-if="capability.thinking_control === 'budget'" class="settings-field">
              <span class="settings-field__label">{{ capability.thinking_secondary_label || "思考预算（Token）" }}</span>
              <input
                v-model.number="draft.thinking_budget"
                class="settings-field__input"
                type="number"
                :min="capability.thinking_budget_min || 1"
                :max="capability.thinking_budget_max || undefined"
                placeholder="留空跟随服务默认"
              />
            </label>

            <label v-if="capability.can_disable_thinking" class="settings-toggle llm-profile-modal__toggle-row">
              <input v-model="draft.thinking" class="settings-switch-input" type="checkbox" :true-value="'enabled'" :false-value="'disabled'" />
              <span>{{ capability.thinking_label || "思考" }}</span>
            </label>
            <p v-else-if="capability.allowed_efforts?.includes('none')" class="settings-field__hint">
              可在推理强度中选择关闭。
            </p>
            <p v-else class="settings-field__hint">此模型始终启用思考。</p>
          </section>
          <p v-else-if="capabilityError" class="settings-field__hint">{{ capabilityError }}</p>
          <p v-else-if="capabilityLoading" class="settings-field__hint">正在读取模型能力…</p>
          <p v-else-if="draft.model" class="settings-field__hint">此模型暂无可配置的思考参数。</p>

          <label v-if="draft.has_api_key" class="settings-toggle llm-profile-modal__toggle-row">
            <input v-model="draft.clear_api_key" class="settings-switch-input" type="checkbox" />
            <span>清除已保存的 API Key</span>
          </label>

          <div class="llm-profile-modal__toggles">
            <label class="settings-toggle llm-profile-modal__toggle-row">
              <input v-model="draft.mock" class="settings-switch-input" type="checkbox" />
              <span>测试模式（Mock）</span>
            </label>
            <label class="settings-toggle llm-profile-modal__toggle-row">
              <input v-model="draft.multimodal_enabled" class="settings-switch-input" type="checkbox" />
              <span>多模态 / Vision</span>
            </label>
          </div>
        </div>

        <footer class="llm-profile-modal__footer">
          <p v-if="localError.message" class="llm-profile-modal__error">{{ localError.message }}</p>
          <div class="llm-profile-modal__actions">
            <button type="button" class="btn btn--ghost" @click="emit('close')">取消</button>
            <button type="button" class="btn btn--primary" @click="submit">
              {{ mode === "create" ? "添加" : "确定" }}
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.llm-profile-overlay {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: grid;
  place-items: center;
  padding: 24px;
}

.llm-profile-modal {
  width: min(560px, 96vw);
  max-height: min(88vh, 720px);
  display: flex;
  flex-direction: column;
  padding: 0;
  overflow: hidden;
}

.llm-profile-modal__header,
.llm-profile-modal__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--color-border);
}

.llm-profile-modal__footer {
  border-bottom: 0;
  border-top: 1px solid var(--color-border);
}

.llm-profile-modal__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.llm-profile-modal__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 16px 18px 18px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.llm-profile-modal__body :deep(.ui-select) {
  max-width: none;
}


.llm-profile-modal__grid {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.llm-profile-modal__grid .settings-field {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(220px, 1.35fr);
  align-items: center;
  gap: 20px;
  margin: 0;
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border);
}

.llm-profile-modal__grid .settings-field__input {
  max-width: 100%;
}

@media (max-width: 640px) {
  .llm-profile-modal__grid .settings-field {
    grid-template-columns: 1fr;
    gap: 8px;
  }
}

.llm-profile-modal__span {
  grid-column: 1 / -1;
}

.llm-profile-modal__probe {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}

.llm-profile-modal__probe-msg {
  margin: 0;
  font-size: 12px;
  color: var(--color-warning);
  line-height: 1.4;
}

.llm-profile-modal__probe-msg--ok {
  color: var(--color-text-muted);
}

.llm-profile-modal__reasoning {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--color-border);
}

.llm-profile-modal__section-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
}

.llm-profile-modal__model-toggle {
  margin-top: 6px;
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--color-primary);
  font-size: 12px;
  cursor: pointer;
  text-align: left;
}

.llm-profile-modal__toggles {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.llm-profile-modal__toggle-row {
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: center;
  gap: 16px;
  min-height: 42px;
  margin: 0;
  padding: 8px 0;
  border-bottom: 1px solid var(--color-border);
}

.llm-profile-modal__toggle-row input {
  grid-column: 2;
  grid-row: 1;
}

.settings-field__hint {
  margin: 0;
}


.llm-profile-modal__error {
  margin: 0;
  flex: 1;
  font-size: 12.5px;
  color: var(--color-warning);
}

.llm-profile-modal__actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}

@media (max-width: 560px) {
  .llm-profile-modal__grid {
    grid-template-columns: 1fr;
  }
}
</style>
