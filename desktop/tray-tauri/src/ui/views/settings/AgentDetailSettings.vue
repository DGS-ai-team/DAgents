<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../../api/node.js";
import UiIcon from "../../components/UiIcon.vue";
import AgentSettingsForm from "../../components/AgentSettingsForm.vue";
import PolicyPanel from "../../components/PolicyPanel.vue";
import McpAgentPanel from "../../components/McpAgentPanel.vue";
import LinuxAgentPanel from "../../components/LinuxAgentPanel.vue";
import MemoryPanel from "../../components/MemoryPanel.vue";
import SimplifiedAutoPanel from "../../components/SimplifiedAutoPanel.vue";
import AgentHandbookPanel from "../../components/AgentHandbookPanel.vue";
import {
  buildPatchAgentPayload,
  draftFromAgentView,
  emptyAgentDraft,
  pruneDraftToolGroups,
  toolGroupsFromSetup,
} from "../../utils/agentTemplateForm.js";
import { notifyConfigurationChanged, onConfigurationChanged } from "../../utils/configurationEvents.js";
import { saveAgentDetailStages } from "../../utils/agentDetailSave.js";

const route = useRoute();
const router = useRouter();

const loading = ref(true);
const saving = ref(false);
const reloading = ref(false);
const setupConfigReady = ref(false);
const promptContextReady = ref(false);
const error = ref("");
const statusMessage = ref("");
const showAdvanced = ref(false);
const activeSection = ref("behavior");
const policyRefreshKey = ref(0);
const llmProfiles = ref([]);
const availableToolGroups = ref([]);
const agentMeta = ref(null);
const savedMemoryScope = ref("agent");
const draft = reactive(emptyAgentDraft());
let loadEpoch = 0;

const agentId = computed(() => String(route.params.agentId || "").trim());
const detailSections = [
  { id: "behavior", label: "基本设置" },
  { id: "autonomy", label: "Auto 设置" },
  { id: "memory", label: "记忆" },
  { id: "resources", label: "连接与资源" },
  { id: "policy", label: "工具审批" },
];

function normalizedSection(value) {
  const section = String(value || "").trim();
  return detailSections.some((item) => item.id === section) ? section : "behavior";
}

function setActiveSection(section) {
  const next = normalizedSection(section);
  activeSection.value = next;
  if (String(route.query.section || "") === next) return;
  void router.replace({ query: { ...route.query, section: next } });
}

async function load() {
  const requestedAgentId = agentId.value;
  const epoch = ++loadEpoch;
  setupConfigReady.value = false;
  promptContextReady.value = false;
  agentMeta.value = null;
  if (!requestedAgentId) {
    error.value = "缺少智能体标识";
    loading.value = false;
    return;
  }
  loading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const [agent, setupResult, promptResult] = await Promise.all([
      api.getAgent(requestedAgentId),
      api.getSetupConfig().then((data) => ({ ok: true, data }), (cause) => ({ ok: false, cause })),
      api.getAgentPromptContext(requestedAgentId).then((data) => ({ ok: true, data }), (cause) => ({ ok: false, cause })),
    ]);
    if (epoch !== loadEpoch || agentId.value !== requestedAgentId) return;
    agentMeta.value = agent;
    const setup = setupResult.ok ? setupResult.data : null;
    setupConfigReady.value = Boolean(setup && typeof setup === "object");
    llmProfiles.value = Array.isArray(setup?.llm?.profiles)
      ? setup.llm.profiles
          .map((p) => ({
            id: String(p.id || "").trim(),
            provider: p.provider || "",
            model: p.model || "",
          }))
          .filter((p) => p.id)
      : [];
    availableToolGroups.value = setupConfigReady.value ? toolGroupsFromSetup(setup) : [];
    Object.assign(
      draft,
      draftFromAgentView(
        agent,
        llmProfiles.value.map((p) => p.id),
      ),
    );
    draft.agentType = agent?.agent_type === "auto" ? "auto" : "normal";
    if (setupConfigReady.value) pruneDraftToolGroups(draft, availableToolGroups.value);

    const promptCtx = promptResult.ok ? promptResult.data : null;
    promptContextReady.value = Boolean(promptCtx && typeof promptCtx === "object");
    if (promptContextReady.value) {
      draft.promptSoulMd = String(promptCtx.soul_md || "");
      draft.promptCustomMd = String(promptCtx.custom_md || "");
      savedMemoryScope.value =
        String(promptCtx.memory_scope || draft.promptMemoryScope || "agent").trim() === "global"
          ? "global"
          : "agent";
      draft.promptMemoryScope = savedMemoryScope.value;
    }
    const loadErrors = [];
    if (!setupConfigReady.value) {
      loadErrors.push(`无法读取模型与能力目录，已保留 Agent 现有工具组并阻止保存：${setupResult.cause?.message || "配置响应为空"}`);
    }
    if (!promptContextReady.value) {
      loadErrors.push(`无法读取提示词设置，已阻止保存以免覆盖现有内容：${promptResult.cause?.message || "提示词设置响应为空"}`);
    }
    error.value = loadErrors.join(" ");
  } catch (e) {
    if (epoch === loadEpoch && agentId.value === requestedAgentId) {
      error.value = e.message || "加载失败";
    }
  } finally {
    if (epoch === loadEpoch) loading.value = false;
  }
}

async function save() {
  if (loading.value || !setupConfigReady.value || !promptContextReady.value) {
    error.value = "模型/能力目录或提示词设置尚未成功读取，当前不能保存；请重新加载后再试。";
    return;
  }
  if (!draft.displayName?.trim()) {
    error.value = "显示名称不能为空";
    return;
  }
  if (!draft.llmProfileId) {
    error.value = "请选择 LLM 配置";
    return;
  }
  const maxToolCalls = Number(draft.maxToolCalls);
  if (!Number.isInteger(maxToolCalls) || maxToolCalls < 1 || maxToolCalls > 4096) {
    error.value = "最大工具调用数需为 1–4096 的整数。";
    return;
  }
  saving.value = true;
  error.value = "";
  statusMessage.value = "";
  const targetAgentId = agentId.value;
  const nextMemoryScope = draft.promptMemoryScope === "global" ? "global" : "agent";
  try {
    const result = await saveAgentDetailStages({
      agentId: targetAgentId,
      agentPayload: buildPatchAgentPayload(draft),
      promptPayload: {
        soul_md: draft.promptSoulMd || "",
        custom_md: draft.promptCustomMd || "",
        memory_scope: nextMemoryScope,
      },
      patchAgent: api.patchAgent,
      putPromptContext: api.putAgentPromptContext,
      reloadAgentRuntime: api.reloadAgentRuntime,
    });
    if (agentId.value !== targetAgentId) return;
    if (result.updatedAgent) agentMeta.value = result.updatedAgent;
    if (result.promptSaved) savedMemoryScope.value = nextMemoryScope;
    if (!result.ok) {
      const detail = result.error?.message || String(result.error || "未知错误");
      if (result.stage === "agent") {
        error.value = `智能体配置保存请求未确认，请刷新核对后再重试：${detail}`;
      } else if (result.stage === "prompt") {
        error.value = `智能体配置已保存，但提示词保存结果未确认；编辑内容仍保留，请核对后重试：${detail}`;
      } else {
        error.value = `智能体配置和提示词已保存，但运行时重载失败；当前会话可能仍使用旧配置：${detail}`;
      }
      return;
    }
    policyRefreshKey.value += 1;
    notifyConfigurationChanged("tools");
    statusMessage.value = "已保存并应用到该智能体。";
  } catch (e) {
    error.value = e.message || "保存失败";
  } finally {
    saving.value = false;
  }
}

async function reloadRuntime() {
  reloading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    await api.reloadAgentRuntime(agentId.value);
    policyRefreshKey.value += 1;
    statusMessage.value = "已重新加载运行中的配置。";
  } catch (e) {
    error.value = e.message || "刷新失败";
  } finally {
    reloading.value = false;
  }
}

function backToList() {
  router.push({ name: "settings-agents" });
}

function refreshPolicy() {
  policyRefreshKey.value += 1;
}

watch(agentId, () => { void load(); });

watch(
  () => route.query.section,
  (section) => {
    activeSection.value = normalizedSection(section);
  },
  { immediate: true },
);

let stopConfigurationEvents = () => {};
onMounted(() => {
  void load();
  stopConfigurationEvents = onConfigurationChanged((change) => {
    if (change?.kind === "mcp" || change?.kind === "mcp-catalog" || change?.kind === "tools") {
      policyRefreshKey.value += 1;
    }
  });
});
onUnmounted(() => stopConfigurationEvents());
</script>

<template>
  <div class="settings-page settings-embedded">
    <header class="settings-page__header">
      <div class="settings-page__header-main">
        <h1 class="settings-page__title">
          {{ agentMeta?.display_name || "智能体配置" }}
        </h1>
        <p class="agent-detail__intro">管理行为、工具权限和运行连接。</p>
      </div>
      <div class="settings-page__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" @click="backToList">
          <UiIcon name="arrow-left" :size="15" />
          返回列表
        </button>
      </div>
    </header>

    <p v-if="loading" class="agent-detail__status">加载中…</p>
    <template v-else>
      <nav class="agent-detail__subnav" aria-label="智能体配置区段">
        <template v-for="item in detailSections" :key="item.id">
          <button
            v-if="item.id !== 'autonomy' || agentMeta?.agent_type === 'auto'"
            type="button"
            class="agent-detail__subnav-item"
            :class="{ 'agent-detail__subnav-item--active': activeSection === item.id }"
            :aria-current="activeSection === item.id ? 'page' : undefined"
            @click="setActiveSection(item.id)"
          >
            {{ item.label }}
          </button>
        </template>
      </nav>

      <p v-if="error && !agentMeta" class="agent-detail__error" role="alert">{{ error }}</p>

      <section v-if="activeSection === 'autonomy' && agentMeta?.agent_type === 'auto'" class="agent-detail__section agent-detail__section--first">
        <div class="agent-detail__section-heading"><h2>自动检查与手册</h2><span>管理职责、检查频率和可编辑手册</span></div>
        <SimplifiedAutoPanel :key="agentId" :agent-id="agentId" />
        <AgentHandbookPanel :key="`handbook-${agentId}`" :agent-id="agentId" />
      </section>

      <section v-else-if="activeSection === 'behavior'" class="agent-detail__section agent-detail__section--first">
        <h2 class="agent-detail__section-title">基本设置</h2>
        <AgentSettingsForm
          v-model:draft="draft"
          :llm-profiles="llmProfiles"
          :available-tool-groups="availableToolGroups"
          v-model:show-advanced="showAdvanced"
        />
        <div class="agent-detail__actions">
          <div class="agent-detail__actions-copy">
            <strong>应用基本设置</strong>
            <span>保存后重新加载当前智能体运行配置</span>
          </div>
          <div class="agent-detail__action-buttons">
            <button
              type="button"
              class="btn btn--ghost"
              :disabled="reloading || saving"
              title="不改配置，仅重新加载当前已保存的设置"
              @click="reloadRuntime"
            >
              {{ reloading ? "加载中…" : "重新加载" }}
            </button>
          <button
            type="button"
            class="btn btn--primary"
            :disabled="saving || loading || !setupConfigReady || !promptContextReady"
            @click="save"
          >
              {{ saving ? "保存中…" : "保存并应用" }}
            </button>
          </div>
        </div>
        <div class="agent-detail__feedback" aria-live="polite">
          <p v-if="statusMessage" class="agent-detail__ok">{{ statusMessage }}</p>
          <p v-if="error" class="agent-detail__error" role="alert">{{ error }}</p>
        </div>
      </section>

      <section v-else-if="activeSection === 'memory'" class="agent-detail__section agent-detail__section--first">
        <MemoryPanel :agent-id="agentId" :scope="savedMemoryScope" />
      </section>

      <section v-else-if="activeSection === 'resources'" class="agent-detail__section agent-detail__section--first">
        <div class="agent-detail__section-heading">
          <div>
            <span class="agent-detail__section-kicker">运行连接</span>
            <h2>连接与资源</h2>
          </div>
          <span>限定该智能体可以发现和使用的外部能力</span>
        </div>
        <div class="agent-detail__resource-stack">
          <McpAgentPanel :agent-id="agentId" @changed="refreshPolicy" />
          <LinuxAgentPanel :agent-id="agentId" @changed="refreshPolicy" />
        </div>
      </section>

      <section v-else class="agent-detail__section agent-detail__section--first agent-detail__policy">
        <div class="agent-detail__policy-title">工具审批</div>
        <div class="agent-detail__policy-body">
          <PolicyPanel embedded :agent-id="agentId" :refresh-key="policyRefreshKey" @close="() => {}" />
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.agent-detail__eyebrow,
.agent-detail__section-kicker {
  display: block;
  margin-bottom: 5px;
  color: var(--color-primary-strong);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.agent-detail__intro {
  margin: 10px 0 0;
  color: var(--color-text-muted);
  font-size: 13px;
  line-height: 1.5;
}

.agent-detail__subnav {
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 38px;
  margin: -4px 0 28px;
  padding: 4px;
  overflow-x: auto;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface-alt, var(--color-surface));
}

.agent-detail__subnav-item {
  flex: 0 0 auto;
  min-height: 28px;
  padding: 0 11px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 12px;
  white-space: nowrap;
  cursor: pointer;
}

.agent-detail__subnav-item:hover,
.agent-detail__subnav-item:focus-visible {
  color: var(--color-text);
  background: var(--color-surface-hover);
}

.agent-detail__subnav-item--active {
  background: var(--color-surface);
  color: var(--color-text);
  box-shadow: 0 1px 2px rgb(15 23 42 / 8%);
  font-weight: 600;
}

.agent-detail__section {
  min-width: 0;
  padding-top: 24px;
  scroll-margin-top: 64px;
  border-top: 1px solid color-mix(in srgb, var(--color-border) 72%, transparent);
}

.agent-detail__section--first {
  padding-top: 0;
  border-top: 0;
}

.agent-detail__section-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.agent-detail__section-heading h2 {
  margin: 0;
  color: var(--color-text);
  font-size: 17px;
  font-weight: 600;
}

.agent-detail__section-title { margin: 0 0 10px; color: var(--color-text); font-size: 15px; font-weight: 600; }

.agent-detail__section-heading > span {
  padding-bottom: 2px;
  color: var(--color-text-subtle);
  font-size: 11px;
}

.agent-detail__actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin: 24px 0 0;
  padding: 14px 0 0;
  border-top: 1px solid var(--color-border);
}

.agent-detail__actions-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.agent-detail__actions-copy strong {
  color: var(--color-text);
  font-size: 12px;
}

.agent-detail__actions-copy span {
  color: var(--color-text-subtle);
  font-size: 11px;
}

.agent-detail__action-buttons {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
}

.agent-detail__feedback {
  min-height: 0;
}

.agent-detail__feedback p {
  margin: 10px 0 0;
}

.agent-detail__ok {
  font-size: 12.5px;
  color: var(--color-success, #3d9a5f);
}

.agent-detail__error,
.agent-detail__status {
  font-size: 12.5px;
}

.agent-detail__error {
  color: var(--color-danger);
}

.agent-detail__status {
  color: var(--color-text-subtle);
}

.agent-detail__policy {
  padding-top: 0;
}

.agent-detail__resource-stack { display: grid; gap: 22px; }

.agent-detail__policy-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  margin: 0 0 10px;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.agent-detail__policy-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-text);
}

.agent-detail__policy-chevron {
  font-size: 12px;
  color: var(--color-text-muted);
  font-weight: 400;
}

.agent-detail__policy-body {
  margin-top: 12px;
}

@media (max-width: 760px) {
  .agent-detail__section-heading {
    align-items: flex-start;
    flex-direction: column;
    gap: 4px;
  }

  .agent-detail__actions {
    align-items: flex-start;
    flex-direction: column;
  }

  .agent-detail__action-buttons {
    width: 100%;
  }

  .agent-detail__action-buttons .btn {
    flex: 1 1 auto;
  }
}
</style>
