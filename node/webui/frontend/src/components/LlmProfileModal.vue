<script setup>
import { computed, reactive, watch } from "vue";

const PROVIDER_PRESETS = {
  deepseek: { base_url: "https://api.deepseek.com", model: "deepseek-chat" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  vllm: { base_url: "http://127.0.0.1:8000/v1", model: "your-model-name" },
  mock: { base_url: "", model: "mock" },
};

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: "edit" }, // create | edit
  profile: { type: Object, default: null },
  existingIds: { type: Array, default: () => [] },
});

const emit = defineEmits(["close", "confirm"]);

const draft = reactive(emptyDraft());
const localError = reactive({ message: "" });

const title = computed(() => (props.mode === "create" ? "新建 LLM 配置" : "编辑 LLM 配置"));
const idEditable = computed(() => props.mode === "create");

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
  };
}

function resetFromProps() {
  localError.message = "";
  if (props.mode === "create") {
    Object.assign(draft, emptyDraft());
    draft.id = suggestId();
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
  });
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
}

function onBackdropClick(event) {
  if (event.target === event.currentTarget) emit("close");
}

function submit() {
  localError.message = "";
  const id = String(draft.id || "").trim();
  if (!id) {
    localError.message = "请填写配置名称";
    return;
  }
  if (props.mode === "create" && (props.existingIds || []).includes(id)) {
    localError.message = `配置「${id}」已存在`;
    return;
  }
  emit("confirm", {
    id,
    provider: draft.provider,
    base_url: draft.base_url,
    model: draft.model,
    api_key: draft.api_key || "",
    has_api_key: draft.has_api_key,
    clear_api_key: !!draft.clear_api_key,
    mock: draft.mock || draft.provider === "mock",
    multimodal_enabled: !!draft.multimodal_enabled,
  });
}

watch(
  () => props.open,
  (visible) => {
    if (visible) resetFromProps();
  }
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="llm-profile-overlay" @click="onBackdropClick">
      <section class="llm-profile-modal" role="dialog" aria-modal="true" :aria-labelledby="'llm-profile-title'">
        <header class="llm-profile-modal__header">
          <h2 id="llm-profile-title" class="llm-profile-modal__title">{{ title }}</h2>
          <button type="button" class="llm-profile-modal__close" aria-label="关闭" @click="emit('close')">×</button>
        </header>

        <div class="llm-profile-modal__body">
          <div class="llm-profile-modal__grid">
            <label class="settings-field">
              <span class="settings-field__label">名称</span>
              <input
                v-model="draft.id"
                class="settings-field__input"
                type="text"
                :readonly="!idEditable"
                :disabled="!idEditable"
                placeholder="如 default"
                autocomplete="off"
              />
            </label>
            <label class="settings-field">
              <span class="settings-field__label">Provider</span>
              <select v-model="draft.provider" class="settings-field__input" @change="applyProviderPreset">
                <option value="deepseek">DeepSeek</option>
                <option value="openai">OpenAI</option>
                <option value="qwen">Qwen</option>
                <option value="vllm">vLLM</option>
                <option value="mock">Mock（测试）</option>
              </select>
            </label>
            <label class="settings-field">
              <span class="settings-field__label">Model</span>
              <input v-model="draft.model" class="settings-field__input" type="text" autocomplete="off" />
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
          </div>

          <label v-if="draft.has_api_key" class="settings-toggle">
            <input v-model="draft.clear_api_key" type="checkbox" />
            <span>清除已保存的 API Key</span>
          </label>

          <div class="setup-config-panel__toggles setup-config-panel__toggles--row">
            <label class="settings-toggle">
              <input v-model="draft.mock" type="checkbox" />
              <span>Mock 模式</span>
            </label>
            <label class="settings-toggle">
              <input v-model="draft.multimodal_enabled" type="checkbox" />
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
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(2px);
}

.llm-profile-modal {
  width: min(560px, 96vw);
  max-height: min(88vh, 720px);
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  box-shadow: var(--shadow-lg, 0 16px 48px rgba(0, 0, 0, 0.28));
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

.llm-profile-modal__close {
  width: 32px;
  height: 32px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 22px;
  line-height: 1;
  cursor: pointer;
}

.llm-profile-modal__close:hover {
  background: rgba(148, 163, 184, 0.16);
  color: var(--color-text);
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

.llm-profile-modal__grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px 14px;
}

.llm-profile-modal__span {
  grid-column: 1 / -1;
}

.llm-profile-modal__error {
  margin: 0;
  flex: 1;
  font-size: 12.5px;
  color: #c2410c;
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
