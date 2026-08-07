<script setup>
import { reactive, ref, watch } from "vue";
import * as api from "../api/node.js";
import AgentSettingsForm from "./AgentSettingsForm.vue";
import { buildCreateTemplatePayload, draftFromBlank, emptyAgentDraft } from "../utils/agentTemplateForm.js";

const props = defineProps({
  open: { type: Boolean, default: false },
});

const emit = defineEmits(["close", "created"]);

const saving = ref(false);
const error = ref("");
const showAdvanced = ref(false);
const llmProfiles = ref([]);
const meta = reactive({
  displayName: "",
  description: "",
});
const draft = reactive(emptyAgentDraft());

/** 生成符合后端 ValidateID 的唯一模板 id（用户无需填写）。 */
function generateTemplateId(displayName) {
  const slug = String(displayName || "")
    .trim()
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .replace(/^[^a-z]+/, "")
    .slice(0, 40);
  const stem = slug || "tpl";
  const suffix = Math.random().toString(36).slice(2, 10);
  return `${stem}-${suffix}`.slice(0, 64);
}

async function loadProfiles() {
  try {
    const setup = await api.getSetupConfig().catch(() => null);
    llmProfiles.value = Array.isArray(setup?.llm?.profiles)
      ? setup.llm.profiles.map((p) => ({
          id: String(p.id || "").trim(),
          provider: p.provider || "",
          model: p.model || "",
        })).filter((p) => p.id)
      : [];
  } catch {
    llmProfiles.value = [];
  }
  Object.assign(draft, draftFromBlank(llmProfiles.value.map((p) => p.id)));
}

function resetForm() {
  meta.displayName = "";
  meta.description = "";
  showAdvanced.value = false;
  error.value = "";
  Object.assign(draft, draftFromBlank(llmProfiles.value.map((p) => p.id)));
}

async function submit() {
  const displayName = String(meta.displayName || "").trim();
  if (!displayName) {
    error.value = "请填写显示名称";
    return;
  }
  const id = generateTemplateId(displayName);
  saving.value = true;
  error.value = "";
  try {
    const created = await api.createAgentTemplate(
      buildCreateTemplatePayload(
        { id, displayName, description: meta.description },
        draft,
      ),
    );
    emit("created", created);
    emit("close");
  } catch (e) {
    error.value = e.message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function onBackdropClick(event) {
  if (event.target === event.currentTarget && !saving.value) emit("close");
}

watch(
  () => props.open,
  (visible) => {
    if (visible) {
      resetForm();
      void loadProfiles();
    }
  },
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="agent-create-overlay" @click="onBackdropClick">
      <section class="agent-create-modal" role="dialog" aria-modal="true" aria-labelledby="agent-template-create-title">
        <header class="agent-create-modal__header">
          <div>
            <h2 id="agent-template-create-title" class="agent-create-modal__title">新建智能体模板</h2>
            <p class="agent-create-modal__subtitle">
              保存为可复用蓝图，创建智能体时可一键预填
            </p>
          </div>
          <button type="button" class="agent-create-modal__close" aria-label="关闭" :disabled="saving" @click="emit('close')">
            ×
          </button>
        </header>

        <div class="agent-create-modal__body">
          <section class="agent-template-meta">
            <label class="agent-template-meta__field agent-template-meta__field--wide">
              <span>显示名称</span>
              <input
                v-model="meta.displayName"
                type="text"
                class="agent-template-meta__input"
                placeholder="例如：代码助手模板"
                autofocus
              />
            </label>
            <label class="agent-template-meta__field agent-template-meta__field--wide">
              <span>描述</span>
              <textarea
                v-model="meta.description"
                class="agent-template-meta__input agent-template-meta__input--area"
                rows="2"
                placeholder="可选，说明这个模板适合做什么"
              />
            </label>
          </section>

          <AgentSettingsForm
            :draft="draft"
            :llm-profiles="llmProfiles"
            v-model:show-advanced="showAdvanced"
          />
        </div>

        <footer class="agent-create-modal__footer">
          <p v-if="error" class="agent-create-modal__error">{{ error }}</p>
          <div class="agent-create-modal__actions">
            <button type="button" class="btn btn--ghost" :disabled="saving" @click="emit('close')">取消</button>
            <button
              type="button"
              class="btn btn--primary"
              :disabled="saving || !meta.displayName?.trim()"
              @click="submit"
            >
              {{ saving ? "保存中…" : "保存模板" }}
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.agent-create-overlay {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(2px);
}

.agent-create-modal {
  width: min(920px, 96vw);
  max-height: min(88vh, 900px);
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  border: 1px solid var(--color-border-strong);
  background: var(--color-surface);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}

.agent-create-modal__header,
.agent-create-modal__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--color-border);
}

.agent-create-modal__footer {
  border-bottom: 0;
  border-top: 1px solid var(--color-border);
  align-items: center;
}

.agent-create-modal__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.agent-create-modal__subtitle {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--color-text-subtle);
}

.agent-create-modal__close {
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

.agent-create-modal__close:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.06);
  color: var(--color-text);
}

.agent-create-modal__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 16px 20px 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.agent-template-meta {
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
}

.agent-template-meta__field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--color-text-muted);
}

.agent-template-meta__field--wide {
  grid-column: 1 / -1;
}

.agent-template-meta__input {
  width: 100%;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  background: var(--color-surface-muted);
  color: var(--color-text);
  font-size: 13px;
}

.agent-template-meta__input--area {
  resize: vertical;
  min-height: 56px;
}

.agent-template-meta__hint {
  grid-column: 1 / -1;
  margin: 0;
  font-size: 11.5px;
  color: var(--color-text-subtle);
}

.agent-template-meta__hint--warn {
  color: var(--color-danger);
}

.agent-create-modal__error {
  flex: 1 1 auto;
  margin: 0;
  font-size: 12px;
  color: var(--color-danger);
}

.agent-create-modal__actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}

@media (max-width: 640px) {
  .agent-template-meta {
    grid-template-columns: 1fr;
  }
}
</style>
