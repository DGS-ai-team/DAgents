<script setup>
import { nextTick, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";
import AgentSettingsForm from "./AgentSettingsForm.vue";
import UiIcon from "./UiIcon.vue";
import { buildCreateTemplatePayload, draftFromBlank, emptyAgentDraft } from "../utils/agentTemplateForm.js";

const props = defineProps({
  open: { type: Boolean, default: false },
});

const emit = defineEmits(["close", "created"]);

const saving = ref(false);
const error = ref("");
const showAdvanced = ref(false);
const llmProfiles = ref([]);
const dialogRoot = ref(null);
const templateNameInput = ref(null);
let openerElement = null;
const meta = reactive({
  displayName: "",
  description: "",
});
const draft = reactive(emptyAgentDraft());
function updateDraft(value) {
  if (value && typeof value === "object") Object.assign(draft, value);
}

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

function focusableElements() {
  return [...(dialogRoot.value?.querySelectorAll(
    'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])',
  ) || [])].filter((element) => !element.closest("[inert], [aria-hidden='true']"));
}

function onDialogKeydown(event) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    if (!saving.value) emit("close");
    return;
  }
  if (event.key !== "Tab") return;

  const focusables = focusableElements();
  if (!focusables.length) {
    event.preventDefault();
    dialogRoot.value?.focus();
    return;
  }
  const first = focusables[0];
  const last = focusables.at(-1);
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialogRoot.value)) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

watch(
  () => props.open,
  async (visible, wasVisible) => {
    if (visible) {
      openerElement = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      resetForm();
      void loadProfiles();
      await nextTick();
      if (props.open) templateNameInput.value?.focus();
      return;
    }
    if (wasVisible) {
      await nextTick();
      if (openerElement?.isConnected) openerElement.focus();
      openerElement = null;
    }
  },
  { immediate: true },
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="ui-dialog-backdrop agent-create-overlay" @click="onBackdropClick">
      <section ref="dialogRoot" class="ui-dialog agent-create-modal" role="dialog" aria-modal="true" aria-labelledby="agent-template-create-title" tabindex="-1" @keydown="onDialogKeydown">
        <header class="agent-create-modal__header">
          <div>
            <h2 id="agent-template-create-title" class="agent-create-modal__title">新建智能体模板</h2>
            <p class="agent-create-modal__subtitle">
              保存为可复用蓝图，创建智能体时可一键预填
            </p>
          </div>
          <button type="button" class="btn btn--ghost btn--sm desktop-icon-button" aria-label="关闭" :disabled="saving" @click="emit('close')">
            <UiIcon name="close" :size="16" />
          </button>
        </header>

        <div class="agent-create-modal__body">
          <section class="agent-template-meta">
            <label class="settings-field agent-template-meta__field agent-template-meta__field--wide">
                <span class="settings-field__label">模板名称</span>
              <input
                ref="templateNameInput"
                v-model="meta.displayName"
                type="text"
                class="settings-field__input"
                placeholder="例如：代码助手模板"
              />
            </label>
            <label class="settings-field agent-template-meta__field agent-template-meta__field--wide">
              <span class="settings-field__label">描述</span>
              <textarea
                v-model="meta.description"
                class="settings-field__input settings-field__input--multiline"
                rows="2"
                placeholder="可选，说明这个模板适合做什么"
              />
            </label>
          </section>

          <AgentSettingsForm
            :draft="draft"
            @update:draft="updateDraft"
            :llm-profiles="llmProfiles"
            :show-workspace="false"
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
}

.agent-create-modal {
  width: min(920px, 96vw);
  max-height: min(88vh, 900px);
  display: flex;
  flex-direction: column;
  padding: 0;
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
}

.agent-template-meta__field--wide {
  grid-column: 1 / -1;
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
