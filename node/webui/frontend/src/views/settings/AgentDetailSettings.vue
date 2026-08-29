<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../../api/node.js";
import AgentSettingsForm from "../../components/AgentSettingsForm.vue";
import PolicyPanel from "../../components/PolicyPanel.vue";
import McpAgentPanel from "../../components/McpAgentPanel.vue";
import LinuxAgentPanel from "../../components/LinuxAgentPanel.vue";
import MemoryPanel from "../../components/MemoryPanel.vue";
import {
  buildPatchAgentPayload,
  draftFromAgentView,
  emptyAgentDraft,
  pruneDraftToolGroups,
  toolGroupsFromSetup,
} from "../../utils/agentTemplateForm.js";
import { notifyConfigurationChanged, onConfigurationChanged } from "../../utils/configurationEvents.js";

const route = useRoute();
const router = useRouter();

const loading = ref(true);
const saving = ref(false);
const reloading = ref(false);
const error = ref("");
const statusMessage = ref("");
const showAdvanced = ref(false);
const policyRefreshKey = ref(0);
const llmProfiles = ref([]);
const availableToolGroups = ref([]);
const agentMeta = ref(null);
const savedLongTermScope = ref("agent");
const draft = reactive(emptyAgentDraft());

const agentId = computed(() => String(route.params.agentId || "").trim());

async function load() {
  if (!agentId.value) {
    error.value = "缺少 agent_id";
    loading.value = false;
    return;
  }
  loading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const [agent, setup, promptCtx] = await Promise.all([
      api.getAgent(agentId.value),
      api.getSetupConfig().catch(() => null),
      api.getAgentPromptContext(agentId.value).catch(() => null),
    ]);
    agentMeta.value = agent;
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
    Object.assign(
      draft,
      draftFromAgentView(
        agent,
        llmProfiles.value.map((p) => p.id),
      ),
    );
    pruneDraftToolGroups(draft, availableToolGroups.value);
    if (promptCtx) {
      draft.promptSoulMd = String(promptCtx.soul_md || "");
      draft.promptCustomMd = String(promptCtx.custom_md || "");
      savedLongTermScope.value =
        String(promptCtx.long_term_scope || draft.promptLongTermScope || "agent").trim() === "global"
          ? "global"
          : "agent";
      draft.promptLongTermScope = savedLongTermScope.value;
    }
  } catch (e) {
    error.value = e.message || "加载失败";
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!draft.displayName?.trim()) {
    error.value = "显示名称不能为空";
    return;
  }
  if (!draft.llmProfileId) {
    error.value = "请选择 LLM 配置";
    return;
  }
  saving.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const updated = await api.patchAgent(agentId.value, buildPatchAgentPayload(draft));
    agentMeta.value = updated;
    const nextLongTermScope = draft.promptLongTermScope === "global" ? "global" : "agent";
    await api.putAgentPromptContext(agentId.value, {
      soul_md: draft.promptSoulMd || "",
      custom_md: draft.promptCustomMd || "",
      long_term_scope: nextLongTermScope,
    });
    savedLongTermScope.value = nextLongTermScope;
    await api.reloadAgentRuntime(agentId.value);
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

watch(agentId, () => {
  void load();
});

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
        <span class="agent-detail__eyebrow">智能体配置</span>
        <h1 class="settings-page__title">
          {{ agentMeta?.display_name || "智能体配置" }}
        </h1>
        <p v-if="agentMeta" class="agent-detail__id" :title="agentMeta.agent_id">
          {{ agentMeta.agent_id }}
        </p>
        <p class="agent-detail__intro">管理这个智能体的行为、工具权限和运行连接。</p>
      </div>
      <div class="settings-page__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" @click="backToList">← 返回列表</button>
      </div>
    </header>

    <p v-if="loading" class="agent-detail__status">加载中…</p>
    <template v-else>
      <nav class="agent-detail__subnav" aria-label="智能体配置区段">
        <a href="#agent-behavior">行为与能力</a>
        <a href="#agent-memory">记忆</a>
        <a href="#agent-mcp">MCP</a>
        <a href="#agent-linux">Linux 通道</a>
        <a href="#agent-policy">工具审批</a>
      </nav>

      <section id="agent-behavior" class="agent-detail__section agent-detail__section--first">
        <div class="agent-detail__section-heading">
          <div>
            <span class="agent-detail__section-kicker">核心配置</span>
            <h2>行为与能力</h2>
          </div>
          <span>影响该智能体的每次对话</span>
        </div>
        <AgentSettingsForm
          v-model:draft="draft"
          :llm-profiles="llmProfiles"
          :available-tool-groups="availableToolGroups"
          v-model:show-advanced="showAdvanced"
        />
      </section>

      <div class="agent-detail__actions">
        <div class="agent-detail__actions-copy">
          <strong>应用配置</strong>
          <span>保存后立即应用到当前智能体</span>
        </div>
        <div class="agent-detail__action-buttons">
          <button type="button" class="btn btn--primary" :disabled="saving" @click="save">
            {{ saving ? "保存中…" : "保存并应用" }}
          </button>
          <button
            type="button"
            class="btn btn--ghost"
            :disabled="reloading || saving"
            title="不改配置，仅重新加载当前已保存的设置"
            @click="reloadRuntime"
          >
            {{ reloading ? "加载中…" : "重新加载" }}
          </button>
        </div>
      </div>
      <div class="agent-detail__feedback" aria-live="polite">
        <p v-if="statusMessage" class="agent-detail__ok">{{ statusMessage }}</p>
        <p v-if="error" class="agent-detail__error" role="alert">{{ error }}</p>
      </div>

      <section id="agent-memory" class="agent-detail__section">
        <MemoryPanel :agent-id="agentId" :scope="savedLongTermScope" />
      </section>
      <section id="agent-mcp" class="agent-detail__section">
        <McpAgentPanel :agent-id="agentId" @changed="refreshPolicy" />
      </section>
      <section id="agent-linux" class="agent-detail__section">
        <LinuxAgentPanel :agent-id="agentId" @changed="refreshPolicy" />
      </section>

      <section id="agent-policy" class="agent-detail__section agent-detail__policy">
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

.agent-detail__id {
  margin: 0;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--color-text-subtle);
}

.agent-detail__subnav {
  position: sticky;
  top: 0;
  z-index: 4;
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 40px;
  margin: -4px 0 24px;
  padding: 4px;
  overflow-x: auto;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface);
  box-shadow: var(--shadow-sm);
}

.agent-detail__subnav a {
  flex: 0 0 auto;
  padding: 6px 10px;
  border-radius: 5px;
  color: var(--color-text-muted);
  font-size: 12px;
  text-decoration: none;
  white-space: nowrap;
}

.agent-detail__subnav a:hover,
.agent-detail__subnav a:focus-visible {
  color: var(--color-text);
  background: var(--color-surface-hover);
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
  position: sticky;
  bottom: 14px;
  z-index: 3;
  margin: 20px 0 0;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--color-primary) 24%, var(--color-border));
  border-radius: 9px;
  background: color-mix(in srgb, var(--color-surface) 94%, transparent);
  box-shadow: var(--shadow-md);
  backdrop-filter: blur(10px);
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
  padding-top: 24px;
}

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
