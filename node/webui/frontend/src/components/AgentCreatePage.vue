<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";
import AgentSettingsForm from "./AgentSettingsForm.vue";
import AgentWorkspacePicker from "./AgentWorkspacePicker.vue";
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
  initialTemplateId: { type: String, default: "" },
});

const emit = defineEmits(["cancel", "created"]);

/** @type {import('vue').Ref<'start' | 'details' | 'workspace' | 'capabilities'>} */
const step = ref("details");
const loading = ref(false);
const saving = ref(false);
const error = ref("");
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
const startPointLabel = computed(() => {
  if (isBlankDraft.value) return "空白开始";
  const tpl = selectedTemplate.value;
  return tpl ? String(tpl.display_name || tpl.id) : "已选模板";
});

const stepTitle = computed(() => {
  if (loading.value) return "创建智能体";
  if (step.value === "start") return "想从哪里开始？";
  if (step.value === "workspace") return "选择工作目录";
  if (step.value === "capabilities") return "它能做什么？";
  return "给它起个名字";
});

const stepLead = computed(() => {
  if (loading.value) return "稍等，正在准备…";
  if (step.value === "start") return "选择一个模板，或者从空白开始配置。";
  if (step.value === "workspace") {
    return "文件、命令行和本机终端会默认在这里运行，创建后不可修改。";
  }
  if (step.value === "capabilities") return "选择智能体可以使用的工具。";
  if (isBlankDraft.value) return "填好名字和模型就可以创建。也可以继续选择它能做什么。";
  return `以「${startPointLabel.value}」为起点，确认名字和模型即可。`;
});

const stepTotal = computed(() => {
  // 含「选起点」时固定 4 步：起点 → 命名 → 工作目录 → 功能；否则 3 步。
  if (canGoBackToStart.value || step.value === "start") return 4;
  return 3;
});
const stepIndex = computed(() => {
  if (step.value === "start") return 1;
  if (step.value === "details") return stepTotal.value === 4 ? 2 : 1;
  if (step.value === "workspace") return stepTotal.value === 4 ? 3 : 2;
  return stepTotal.value;
});

const canContinueStart = computed(() => !loading.value && (!!draft.templateId || isBlankDraft.value));
const canContinueDetails = computed(
  () =>
    !saving.value &&
    !loading.value &&
    !!draft.displayName?.trim() &&
    !!draft.llmProfileId,
);
const canSubmit = computed(
  () =>
    !saving.value &&
    !loading.value &&
    !!draft.displayName?.trim() &&
    !!draft.llmProfileId,
);
const backLabel = computed(() => {
  if (step.value === "start") return "取消";
  if (step.value === "details") return canGoBackToStart.value ? "上一步" : "取消";
  return "上一步";
});
const primaryLabel = computed(() => {
  if (saving.value) return "创建中…";
  if (step.value === "start") return "继续";
  if (step.value === "details" || step.value === "workspace") return "下一步";
  return "创建";
});
const primaryDisabled = computed(() => {
  if (step.value === "start") return !canContinueStart.value;
  if (step.value === "details") return !canContinueDetails.value;
  if (step.value === "workspace") {
    return draft.workspaceMode === "custom" && !String(draft.workspacePath || "").trim();
  }
  return !canSubmit.value;
});

function onBack() {
  if (step.value === "start") {
    emit("cancel");
    return;
  }
  if (step.value === "details") {
    goBackFromDetails();
    return;
  }
  if (step.value === "workspace") {
    goBackFromWorkspace();
    return;
  }
  goBackFromCapabilities();
}

function onPrimary() {
  if (step.value === "start") {
    goDetails();
    return;
  }
  if (step.value === "details") {
    goWorkspace();
    return;
  }
  if (step.value === "workspace") {
    goCapabilities();
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
  if (key === "workspace") fieldErrors.workspace = "";
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
  return true;
}

/** @returns {boolean} */
function validateWorkspace() {
  fieldErrors.workspace = "";
  if (draft.workspaceMode === "custom" && !String(draft.workspacePath || "").trim()) {
    fieldErrors.workspace = "请选择一个本机目录";
    return false;
  }
  return true;
}

function goDetails() {
  error.value = "";
  clearFieldErrors();
  step.value = "details";
}

function goWorkspace() {
  if (!validateBasics()) {
    step.value = "details";
    return;
  }
  error.value = "";
  clearFieldErrors();
  step.value = "workspace";
}

function goCapabilities() {
  if (!validateWorkspace()) {
    step.value = "workspace";
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
  else emit("cancel");
}

function goBackFromWorkspace() {
  error.value = "";
  clearFieldErrors();
  step.value = "details";
}

function goBackFromCapabilities() {
  error.value = "";
  clearFieldErrors();
  step.value = "workspace";
}

async function submit() {
  if (!validateBasics()) {
    step.value = "details";
    return;
  }
  if (!validateWorkspace()) {
    step.value = "workspace";
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    const created = await api.createAgent(buildCreateAgentPayload(draft));
    emit("created", created);
  } catch (e) {
    error.value = e.message || "创建失败";
  } finally {
    saving.value = false;
  }
}

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

onMounted(() => {
  void loadTemplates();
});
</script>

<template>
  <section class="agent-create-page" aria-labelledby="agent-create-page-title">
    <div class="agent-create-page__shell">
      <header class="agent-create-page__header">
        <p class="agent-create-page__eyebrow">新建智能体</p>
        <h1 id="agent-create-page-title" class="agent-create-page__title">{{ stepTitle }}</h1>
        <p class="agent-create-page__subtitle">{{ stepLead }}</p>
      </header>

      <main class="agent-create-page__body">
        <div v-if="loading" class="agent-create-page__loading">稍等，正在准备…</div>

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

        <div v-else class="agent-create-page__form">
          <AgentSettingsForm
            v-if="step === 'details'"
            v-model:draft="draft"
            :llm-profiles="llmProfiles"
            :field-errors="fieldErrors"
            :show-workspace="false"
            mode="create-basics"
            @clear-field-error="clearFieldError"
          />

          <AgentWorkspacePicker
            v-else-if="step === 'workspace'"
            v-model:draft="draft"
            :field-error="fieldErrors.workspace"
            @clear-error="clearFieldError('workspace')"
          />

          <AgentSettingsForm
            v-else
            v-model:draft="draft"
            :llm-profiles="llmProfiles"
            :available-tool-groups="availableToolGroups"
            mode="create-capabilities"
          />
        </div>
      </main>

      <footer v-if="!loading" class="agent-create-page__footer">
        <div class="agent-create-page__footer-left">
          <button type="button" class="agent-create-page__back" :disabled="saving" @click="onBack">
            {{ backLabel }}
          </button>
          <p v-if="error" class="agent-create-page__error">{{ error }}</p>
        </div>
        <div class="agent-create-page__footer-right">
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

      <div v-if="!loading" class="agent-create-page__progress" aria-hidden="true">
        <span
          v-for="i in stepTotal"
          :key="i"
          :class="{ 'agent-create-page__progress-segment--active': i <= stepIndex }"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.agent-create-page {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  display: grid;
  place-items: center;
  padding: clamp(28px, 7vh, 72px) 32px 40px;
  background: var(--app-background);
  color: var(--color-text);
}

.agent-create-page__shell {
  width: min(620px, 100%);
  display: flex;
  flex-direction: column;
  gap: 24px;
  margin: auto;
}

.agent-create-page__header {
  text-align: center;
}

.agent-create-page__eyebrow {
  margin: 0 0 10px;
  color: var(--color-text-subtle);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.agent-create-page__title {
  margin: 0;
  color: var(--color-text);
  font-size: 24px;
  font-weight: 650;
  letter-spacing: -0.025em;
  line-height: 1.2;
}

.agent-create-page__subtitle {
  min-height: 2.9em;
  max-width: 44em;
  margin: 8px auto 0;
  color: var(--color-text-muted);
  font-size: 14px;
  line-height: 1.5;
}

.agent-create-page__body {
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.agent-create-page__form {
  width: 100%;
}

.agent-create-page__form :deep(.agent-settings-form--create) {
  height: auto;
  min-height: 0;
}

.agent-create-page__loading {
  min-height: 160px;
  display: grid;
  place-items: center;
  color: var(--color-text-subtle);
  font-size: 13px;
}

.agent-create-start {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.agent-create-choice {
  min-height: 96px;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  gap: 5px;
  width: 100%;
  padding: 16px;
  border: 1px solid var(--color-border);
  border-radius: 12px;
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
  color: var(--color-text);
  font-size: 14px;
  font-weight: 600;
}

.agent-create-choice__desc {
  display: -webkit-box;
  overflow: hidden;
  color: var(--color-text-muted);
  font-size: 12px;
  line-height: 1.45;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.agent-create-page__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.agent-create-page__footer-left,
.agent-create-page__footer-right {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 14px;
}

.agent-create-page__footer-left {
  flex: 1 1 auto;
}

.agent-create-page__footer-right {
  flex: 0 0 auto;
  margin-left: auto;
}

.agent-create-page__back,
.agent-create-page__link {
  flex: 0 0 auto;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
  cursor: pointer;
  white-space: nowrap;
}

.agent-create-page__back:hover:not(:disabled),
.agent-create-page__link:hover:not(:disabled) {
  color: var(--color-text);
}

.agent-create-page__back:disabled,
.agent-create-page__link:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.agent-create-page__error {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--color-danger);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-create-page__progress {
  display: flex;
  gap: 5px;
  width: 100%;
  height: 3px;
}

.agent-create-page__progress span {
  flex: 1 1 0;
  border-radius: 999px;
  background: color-mix(in srgb, var(--color-text) 10%, transparent);
}

.agent-create-page__progress-segment--active {
  background: var(--color-primary) !important;
}

@media (max-width: 700px) {
  .agent-create-page {
    place-items: start center;
    padding: 28px 20px 32px;
  }

  .agent-create-page__shell {
    margin: 0 auto;
  }

  .agent-create-start {
    grid-template-columns: 1fr;
  }

  .agent-create-page__footer {
    align-items: flex-end;
  }

  .agent-create-page__error {
    display: none;
  }
}
</style>
