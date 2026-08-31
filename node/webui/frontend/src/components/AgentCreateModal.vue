<script setup>
import { computed, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";
import AgentSettingsForm from "./AgentSettingsForm.vue";
import {
  buildCreateAgentPayload,
  BLANK_TEMPLATE_ID,
  draftFromBlank,
  draftFromTemplate,
  emptyAgentDraft,
  pruneDraftToolGroups,
  toolGroupsFromSetup,
} from "../utils/agentTemplateForm.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  initialTemplateId: { type: String, default: "" },
});

const emit = defineEmits(["close", "created"]);

/** @type {import('vue').Ref<'start' | 'details' | 'capabilities'>} */
const step = ref("details");
const loading = ref(false);
const saving = ref(false);
const error = ref("");
/** 字段级校验（替换标签，不进页脚） */
const fieldErrors = reactive({ name: "", llm: "", workspace: "" });
const templates = ref([]);
const llmProfiles = ref([]);
const availableToolGroups = ref([]);
const draft = reactive(emptyAgentDraft());
/** 是否从「选起点」进来；无模板或从空态模板直达时为 false */
const canGoBackToStart = ref(false);

const selectedTemplate = computed(
  () => templates.value.find((t) => t.id === draft.templateId) || null,
);
const isBlankDraft = computed(() => draft.templateId === BLANK_TEMPLATE_ID || !draft.templateId);
const hasTemplates = computed(() => templates.value.length > 0);
const llmProfileIds = computed(() => llmProfiles.value.map((p) => p.id).filter(Boolean));
const selectedToolCount = computed(() => (draft.toolGroups || []).length);

const startPointLabel = computed(() => {
  if (isBlankDraft.value) return "空白开始";
  const tpl = selectedTemplate.value;
  return tpl ? String(tpl.display_name || tpl.id) : "已选模板";
});

const stepTitle = computed(() => {
  if (loading.value) return "创建智能体";
  if (step.value === "start") return "想从哪里开始？";
  if (step.value === "capabilities") return "它能做什么？";
  return "给它起个名字";
});

const stepLead = computed(() => {
  if (loading.value) return "稍等，正在准备…";
  if (step.value === "start") return "选择一个模板？还是你想从空白开始配置。";
  if (step.value === "capabilities") {
    return "选择智能体可以使用的工具";
  }
  if (isBlankDraft.value) return "填好名字和模型就可以创建。想先选它能做什么的话，点「选功能」。";
  return `以「${startPointLabel.value}」为起点。改个名字、确认模型就好。`;
});

const stepTotal = computed(() => {
  // 含「选起点」时固定 3 步：起点 → 命名 → 功能；否则 2 步：命名 → 功能。
  // 进度条不因是否进入「选功能」而变长，避免突兀。
  if (canGoBackToStart.value || step.value === "start") return 3;
  return 2;
});
const stepIndex = computed(() => {
  if (step.value === "start") return 1;
  if (step.value === "details") return stepTotal.value === 3 ? 2 : 1;
  return stepTotal.value;
});

const canContinueStart = computed(() => !loading.value && (!!draft.templateId || isBlankDraft.value));
const canSubmit = computed(
  () =>
    !saving.value &&
    !loading.value &&
    !!draft.displayName?.trim() &&
    !!draft.llmProfileId,
);

const showProgress = computed(() => !loading.value);
const backLabel = computed(() => {
  if (step.value === "start") return "取消";
  if (step.value === "details") return canGoBackToStart.value ? "上一步" : "取消";
  return "上一步";
});
const primaryLabel = computed(() => {
  if (saving.value) return "创建中…";
  if (step.value === "start") return "继续";
  return "创建";
});
const primaryDisabled = computed(() => {
  if (step.value === "start") return !canContinueStart.value;
  return !canSubmit.value;
});

function onBack() {
  if (step.value === "start") {
    emit("close");
    return;
  }
  if (step.value === "details") {
    goBackFromDetails();
    return;
  }
  goBackFromCapabilities();
}

function onPrimary() {
  if (step.value === "start") {
    goDetails();
    return;
  }
  void submit();
}

async function loadTemplates() {
  loading.value = true;
  error.value = "";
  clearFieldErrors();
  canGoBackToStart.value = false;
  try {
    const [tplRes, setup] = await Promise.all([
      api.listAgentTemplates(),
      api.getSetupConfig().catch(() => null),
    ]);
    templates.value = tplRes.templates || [];
    llmProfiles.value = Array.isArray(setup?.llm?.profiles)
      ? setup.llm.profiles
          .map((p) => ({
            id: String(p.id || "").trim(),
            provider: p.provider || "",
            model: p.model || "",
          }))
          .filter((p) => p.id)
      : [];
    availableToolGroups.value = toolGroupsFromSetup(setup);
    pruneDraftToolGroups(draft, availableToolGroups.value);

    const prefer = String(props.initialTemplateId || "").trim();
    const preferred = prefer ? templates.value.find((t) => t.id === prefer) : null;

    if (preferred) {
      applyTemplate(preferred);
      step.value = "details";
      canGoBackToStart.value = templates.value.length > 0;
    } else if (templates.value.length) {
      applyTemplate(templates.value[0]);
      step.value = "start";
      canGoBackToStart.value = true;
    } else {
      applyBlank();
      step.value = "details";
      canGoBackToStart.value = false;
    }
  } catch (e) {
    error.value = e.message || "加载模板失败";
    templates.value = [];
    applyBlank();
    step.value = "details";
    canGoBackToStart.value = false;
  } finally {
    loading.value = false;
  }
}

function applyTemplate(template) {
  Object.assign(draft, draftFromTemplate(template, llmProfileIds.value));
  pruneDraftToolGroups(draft, availableToolGroups.value);
}

function applyBlank() {
  Object.assign(draft, draftFromBlank(llmProfileIds.value));
  draft.templateId = BLANK_TEMPLATE_ID;
  pruneDraftToolGroups(draft, availableToolGroups.value);
}

function pickBlank() {
  applyBlank();
}

function pickTemplate(template) {
  applyTemplate(template);
}

function clearFieldErrors() {
  fieldErrors.name = "";
  fieldErrors.llm = "";
  fieldErrors.workspace = "";
}

function clearFieldError(key) {
  if (key === "name") fieldErrors.name = "";
  if (key === "llm") fieldErrors.llm = "";
}

/** @returns {boolean} */
function validateBasics() {
  clearFieldErrors();
  if (!draft.displayName?.trim()) {
    fieldErrors.name = "先给智能体起个名字吧";
    return false;
  }
  if (!draft.llmProfileId) {
    fieldErrors.llm = "选一个模型配置吧";
    return false;
  }
  if (draft.workspaceMode === "custom" && !String(draft.workspacePath || "").trim()) {
    fieldErrors.workspace = "填写一个本机绝对路径吧";
    return false;
  }
  return true;
}

function goDetails() {
  error.value = "";
  clearFieldErrors();
  step.value = "details";
}

function goCapabilities() {
  if (!validateBasics()) {
    step.value = "details";
    return;
  }
  error.value = "";
  step.value = "capabilities";
}

function goStart() {
  if (!hasTemplates.value) return;
  error.value = "";
  clearFieldErrors();
  step.value = "start";
  canGoBackToStart.value = true;
}

function goBackFromDetails() {
  if (canGoBackToStart.value) goStart();
  else emit("close");
}

function goBackFromCapabilities() {
  error.value = "";
  clearFieldErrors();
  step.value = "details";
}

async function submit() {
  if (!validateBasics()) {
    step.value = "details";
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    const created = await api.createAgent(buildCreateAgentPayload(draft));
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
    if (visible) void loadTemplates();
    else {
      error.value = "";
      clearFieldErrors();
      step.value = "details";
      canGoBackToStart.value = false;
      Object.assign(draft, emptyAgentDraft());
    }
  },
);

watch(
  () => draft.displayName,
  () => {
    if (fieldErrors.name) fieldErrors.name = "";
  },
);

watch(
  () => draft.llmProfileId,
  () => {
    if (fieldErrors.llm) fieldErrors.llm = "";
  },
);

watch(
  () => [draft.workspaceMode, draft.workspacePath],
  () => {
    if (fieldErrors.workspace) fieldErrors.workspace = "";
  },
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="agent-create-overlay" @click="onBackdropClick">
      <section class="agent-create-modal" role="dialog" aria-modal="true" aria-labelledby="agent-create-title">
        <header class="agent-create-modal__header">
          <div>
            <h2 id="agent-create-title" class="agent-create-modal__title">{{ stepTitle }}</h2>
            <p class="agent-create-modal__subtitle">{{ stepLead }}</p>
          </div>
          <button
            type="button"
            class="agent-create-modal__close"
            aria-label="关闭"
            :disabled="saving"
            @click="emit('close')"
          >
            ×
          </button>
        </header>

        <div class="agent-create-modal__body">
          <div v-if="loading" class="agent-create-modal__loading">稍等，正在准备…</div>

          <div v-else-if="step === 'start'" class="agent-create-start">
            <button
              type="button"
              class="agent-create-choice"
              :class="{ 'agent-create-choice--active': isBlankDraft }"
              @click="pickBlank"
            >
              <span class="agent-create-choice__name">空白开始</span>
              <span class="agent-create-choice__desc">自己填写设置，不依赖模板</span>
            </button>
            <button
              v-for="tpl in templates"
              :key="tpl.id"
              type="button"
              class="agent-create-choice"
              :class="{ 'agent-create-choice--active': draft.templateId === tpl.id }"
              @click="pickTemplate(tpl)"
            >
              <span class="agent-create-choice__name">{{ tpl.display_name || tpl.id }}</span>
              <span class="agent-create-choice__desc">{{ tpl.description || "从模板快速创建" }}</span>
            </button>
          </div>

          <AgentSettingsForm
            v-else-if="step === 'details'"
            v-model:draft="draft"
            :llm-profiles="llmProfiles"
            :field-errors="fieldErrors"
            mode="create-basics"
            @clear-field-error="clearFieldError"
          />

          <AgentSettingsForm
            v-else
            v-model:draft="draft"
            :llm-profiles="llmProfiles"
            :available-tool-groups="availableToolGroups"
            mode="create-capabilities"
          />
        </div>

        <footer class="agent-create-modal__footer">
          <div class="agent-create-modal__footer-left">
            <button
              type="button"
              class="agent-create-modal__back"
              :disabled="saving"
              @click="onBack"
            >
              {{ backLabel }}
            </button>
            <div
              v-if="showProgress"
              class="agent-create-modal__dots"
              :aria-label="`第 ${stepIndex} 步，共 ${stepTotal} 步`"
            >
              <span
                v-for="i in stepTotal"
                :key="i"
                class="agent-create-modal__dot"
                :class="{ 'agent-create-modal__dot--active': i === stepIndex }"
              />
            </div>
            <p v-if="error" class="agent-create-modal__error">{{ error }}</p>
          </div>
          <div class="agent-create-modal__footer-right">
            <button
              v-if="step === 'details'"
              type="button"
              class="agent-create-modal__link"
              :disabled="saving || loading"
              @click="goCapabilities"
            >
              选功能{{ selectedToolCount ? ` · ${selectedToolCount}` : "" }}
            </button>
            <button
              type="button"
              class="btn btn--primary"
              :disabled="primaryDisabled || saving"
              @click="onPrimary"
            >
              {{ primaryLabel }}
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
  background: var(--color-overlay);
  backdrop-filter: blur(2px);
}

.agent-create-modal {
  width: min(520px, 96vw);
  height: min(480px, 84vh);
  display: flex;
  flex-direction: column;
  border-radius: 14px;
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
  padding: 12px 20px;
  gap: 16px;
}

.agent-create-modal__footer-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1 1 auto;
}

.agent-create-modal__back {
  flex: 0 0 auto;
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
  cursor: pointer;
}

.agent-create-modal__back:hover:not(:disabled) {
  color: var(--color-text);
}

.agent-create-modal__back:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.agent-create-modal__dots {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
}

.agent-create-modal__dot {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--color-text) 16%, transparent);
  transition: background 0.15s ease, width 0.15s ease;
}

.agent-create-modal__dot--active {
  width: 16px;
  background: var(--color-text-muted);
}

.agent-create-modal__error {
  margin: 0;
  font-size: 12px;
  color: var(--color-danger);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-create-modal__footer-right {
  display: flex;
  align-items: center;
  gap: 14px;
  flex: 0 0 auto;
  margin-left: auto;
}

.agent-create-modal__link {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
  cursor: pointer;
  white-space: nowrap;
}

.agent-create-modal__link:hover:not(:disabled) {
  color: var(--color-text);
}

.agent-create-modal__link:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.agent-create-modal__title {
  margin: 0;
  font-size: 18px;
  font-weight: 650;
  letter-spacing: -0.02em;
  color: var(--color-text);
}

.agent-create-modal__subtitle {
  margin: 4px 0 0;
  font-size: 13px;
  line-height: 1.45;
  color: var(--color-text-muted);
  max-width: 36em;
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
  background: color-mix(in srgb, var(--color-text) 8%, transparent);
  color: var(--color-text);
}

.agent-create-modal__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
  padding: 14px 20px 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.agent-create-modal__body > :deep(.agent-settings-form) {
  flex: 1 1 auto;
  min-height: 0;
}

.agent-create-modal__loading {
  flex: 1 1 auto;
  display: grid;
  place-items: center;
  color: var(--color-text-subtle);
  font-size: 13px;
}

.agent-create-start {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow: auto;
  padding-right: 2px;
}

.agent-create-choice {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  gap: 4px;
  flex: 1 1 0;
  min-height: 64px;
  width: 100%;
  padding: 12px 14px;
  border-radius: 10px;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.agent-create-choice:hover {
  border-color: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 40%, var(--color-border));
}

.agent-create-choice--active {
  border-color: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 55%, var(--color-border));
  background: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 8%, var(--color-surface));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 18%, transparent);
}

.agent-create-choice__name {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
}

.agent-create-choice__desc {
  font-size: 12px;
  line-height: 1.45;
  color: var(--color-text-muted);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
