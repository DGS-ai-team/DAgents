<script setup>
import { computed, reactive, ref, watch } from "vue";
import { probeLLMModels } from "../api.js";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-chat" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: {
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    model: "qwen-plus",
  },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
};

const ALLOWED_PROVIDERS = new Set(["deepseek", "openai", "qwen", "vllm"]);

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: "create" },
  config: { type: Object, default: null },
});

const emit = defineEmits(["close", "confirm"]);

const draft = reactive(emptyDraft());
const localError = ref("");
const probeState = reactive({ loading: false, message: "", ok: false });
const probedModels = ref([]);
const modelManual = ref(false);

const title = computed(() => (props.mode === "create" ? "新建 LLM 配置" : "编辑 LLM 配置"));
const canProbe = computed(() => String(draft.base_url || "").trim().length > 0);
const useModelSelect = computed(() => probedModels.value.length > 0 && !modelManual.value);

function emptyDraft() {
  return {
    name: "",
    provider: "deepseek",
    base_url: PROVIDER_PRESETS.deepseek.base_url,
    model: PROVIDER_PRESETS.deepseek.model,
    api_key: "",
    has_api_key: false,
    clear_api_key: false,
    is_default: false,
    allowed_groups_text: "",
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
  localError.value = "";
  resetProbeUI();
  if (props.mode === "create" || !props.config) {
    Object.assign(draft, emptyDraft());
    return;
  }
  const src = props.config;
  Object.assign(draft, emptyDraft(), {
    name: src.name || "",
    provider: src.provider || "openai",
    base_url: src.base_url || "",
    model: src.model || "",
    api_key: "",
    has_api_key: Boolean(src.has_api_key || src.api_key),
    clear_api_key: false,
    is_default: Boolean(src.is_default),
    allowed_groups_text: Array.isArray(src.allowed_groups)
      ? src.allowed_groups.join(", ")
      : "",
  });
}

function applyProviderPreset() {
  const preset = PROVIDER_PRESETS[draft.provider];
  if (!preset) return;
  draft.base_url = preset.base_url;
  draft.model = preset.model;
  resetProbeUI();
}

/** 探测/下拉选中模型后，用模型名填「名称」，免去手工输入。 */
function syncNameFromModel(model) {
  const m = String(model || "").trim();
  if (m) draft.name = m;
}

function onModelSelect() {
  syncNameFromModel(draft.model);
}

function onBackdropClick(event) {
  if (event.target === event.currentTarget) emit("close");
}

async function runProbe() {
  localError.value = "";
  probeState.message = "";
  probeState.ok = false;
  if (!canProbe.value) {
    probeState.message = "请先填写 Base URL";
    return;
  }
  probeState.loading = true;
  try {
    const payload = {
      base_url: String(draft.base_url || "").trim(),
      api_key: String(draft.api_key || "").trim(),
      provider: draft.provider,
    };
    if (
      !payload.api_key &&
      draft.has_api_key &&
      !draft.clear_api_key &&
      props.config?.id
    ) {
      payload.config_id = props.config.id;
    }
    const data = await probeLLMModels(payload);
    const models = Array.isArray(data?.models)
      ? data.models.map((m) => String(m?.id || m || "").trim()).filter(Boolean)
      : [];
    if (!models.length) throw new Error("未返回模型列表");
    probedModels.value = models;
    modelManual.value = false;
    const suggested = String(data?.suggested_provider || "").trim().toLowerCase();
    if (suggested && ALLOWED_PROVIDERS.has(suggested)) {
      draft.provider = suggested;
    }
    if (!models.includes(draft.model)) {
      draft.model = models[0];
    }
    syncNameFromModel(draft.model);
    probeState.ok = true;
    probeState.message = `已获取 ${models.length} 个模型，可下拉选择`;
  } catch (err) {
    probedModels.value = [];
    modelManual.value = true;
    probeState.ok = false;
    probeState.message = err?.message || "测试失败，请手动填写模型名称";
  } finally {
    probeState.loading = false;
  }
}

function submit() {
  localError.value = "";
  const name = String(draft.name || "").trim();
  if (!name) {
    localError.value = "请填写配置名称";
    return;
  }
  if (!String(draft.base_url || "").trim()) {
    localError.value = "请填写 Base URL";
    return;
  }
  if (!String(draft.model || "").trim()) {
    localError.value = "请填写或选择模型名称";
    return;
  }
  const allowed_groups = String(draft.allowed_groups_text || "")
    .split(/[,，\s]+/)
    .map((s) => s.trim())
    .filter(Boolean);
  emit("confirm", {
    name,
    provider: draft.provider,
    base_url: String(draft.base_url || "").trim(),
    model: String(draft.model || "").trim(),
    api_key: String(draft.api_key || "").trim(),
    clear_api_key: Boolean(draft.clear_api_key),
    is_default: Boolean(draft.is_default),
    allowed_groups,
  });
}

watch(
  () => props.open,
  (visible) => {
    if (visible) resetFromProps();
  },
);

watch(
  () => [draft.base_url, draft.api_key, draft.clear_api_key],
  () => {
    if (probedModels.value.length || probeState.message) {
      probedModels.value = [];
      modelManual.value = false;
      probeState.ok = false;
      probeState.message = "";
    }
  },
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="modal-backdrop" @click="onBackdropClick">
      <section class="modal-panel llm-config-modal" role="dialog" aria-modal="true">
        <header class="drawer-header">
          <div class="drawer-title-block">
            <h2>{{ title }}</h2>
          </div>
          <button type="button" class="btn btn-ghost" aria-label="关闭" @click="emit('close')">
            ×
          </button>
        </header>

        <div class="drawer-body llm-config-modal__body">
          <label class="field">
            <span>名称</span>
            <input v-model="draft.name" type="text" placeholder="如 DeepSeek / 默认" autocomplete="off" />
          </label>
          <label class="field">
            <span>Base URL（含 /v1）</span>
            <input v-model="draft.base_url" type="text" autocomplete="off" />
          </label>
          <label class="field">
            <span>API Key</span>
            <input
              v-model="draft.api_key"
              type="password"
              autocomplete="new-password"
              :placeholder="draft.has_api_key ? '已保存，留空则保持不变' : 'sk-...'"
            />
          </label>
          <label v-if="mode === 'edit' && draft.has_api_key" class="field field-checkbox">
            <input v-model="draft.clear_api_key" type="checkbox" />
            <span>清除已保存的 API Key</span>
          </label>

          <div class="llm-config-modal__probe">
            <button
              type="button"
              class="btn btn-ghost"
              :disabled="probeState.loading || !canProbe"
              @click="runProbe"
            >
              {{ probeState.loading ? "测试中…" : "测试并拉取模型" }}
            </button>
            <p
              v-if="probeState.message"
              class="llm-config-modal__probe-msg"
              :class="{ ok: probeState.ok }"
            >
              {{ probeState.message }}
            </p>
            <p v-else class="muted">先填 URL 与 Key，请求兼容接口 <code>/models</code>。</p>
          </div>

          <div class="form-grid">
            <label class="field">
              <span>Provider</span>
              <select v-model="draft.provider" @change="applyProviderPreset">
                <option value="deepseek">DeepSeek</option>
                <option value="openai">OpenAI</option>
                <option value="qwen">Qwen</option>
                <option value="vllm">vLLM</option>
              </select>
            </label>
            <label class="field">
              <span>Model</span>
              <select v-if="useModelSelect" v-model="draft.model" @change="onModelSelect">
                <option v-for="m in probedModels" :key="m" :value="m">{{ m }}</option>
              </select>
              <input v-else v-model="draft.model" type="text" placeholder="模型名称" />
            </label>
          </div>

          <label class="field">
            <span>可见分组（可选，逗号分隔；空=全部）</span>
            <input
              v-model="draft.allowed_groups_text"
              type="text"
              placeholder="如 ops, research"
              autocomplete="off"
            />
          </label>

          <label class="field field-checkbox">
            <input v-model="draft.is_default" type="checkbox" />
            <span>设为默认配置</span>
          </label>

          <p v-if="localError" class="banner banner-error" role="alert">{{ localError }}</p>
        </div>

        <footer class="drawer-footer">
          <button type="button" class="btn btn-ghost" @click="emit('close')">取消</button>
          <button type="button" class="btn btn-primary" @click="submit">保存</button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>
