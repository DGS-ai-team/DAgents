<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../../api/node.js";
import AgentSettingsForm from "../../components/AgentSettingsForm.vue";
import PolicyPanel from "../../components/PolicyPanel.vue";
import McpAgentPanel from "../../components/McpAgentPanel.vue";
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
const showAdvanced = ref(true);
const policyRefreshKey = ref(0);
const llmProfiles = ref([]);
const availableToolGroups = ref([]);
const agentMeta = ref(null);
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
      draft.promptLongTermScope =
        String(promptCtx.long_term_scope || draft.promptLongTermScope || "agent").trim() === "global"
          ? "global"
          : "agent";
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
    await api.putAgentPromptContext(agentId.value, {
      soul_md: draft.promptSoulMd || "",
      custom_md: draft.promptCustomMd || "",
      long_term_scope: draft.promptLongTermScope === "global" ? "global" : "agent",
    });
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
    <div class="agent-detail__head">
      <button type="button" class="btn btn--ghost btn--sm" @click="backToList">← 返回列表</button>
      <h1 class="settings-page__title">
        {{ agentMeta?.display_name || "智能体配置" }}
      </h1>
      <p v-if="agentMeta" class="agent-detail__id" :title="agentMeta.agent_id">
        {{ agentMeta.agent_id }}
      </p>
    </div>

    <p v-if="loading" class="agent-detail__status">加载中…</p>
    <template v-else>
      <AgentSettingsForm
        :draft="draft"
        :llm-profiles="llmProfiles"
        :available-tool-groups="availableToolGroups"
        v-model:show-advanced="showAdvanced"
      />

      <div class="agent-detail__actions">
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
      <McpAgentPanel :agent-id="agentId" @changed="refreshPolicy" />
      <p v-if="statusMessage" class="agent-detail__ok">{{ statusMessage }}</p>
      <p v-if="error" class="agent-detail__error">{{ error }}</p>
      <p class="agent-detail__hint">保存后立即生效。工具审批可在下方单独调整。</p>

      <section class="agent-detail__policy">
        <div class="agent-detail__policy-title">工具审批</div>
        <div class="agent-detail__policy-body">
          <PolicyPanel embedded :agent-id="agentId" :refresh-key="policyRefreshKey" @close="() => {}" />
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.agent-detail__head {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  margin-bottom: 16px;
}

.agent-detail__id {
  margin: 0;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--color-text-subtle);
}

.agent-detail__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}

.agent-detail__ok {
  margin: 10px 0 0;
  font-size: 12.5px;
  color: var(--color-success, #3d9a5f);
}

.agent-detail__error,
.agent-detail__status {
  margin: 10px 0 0;
  font-size: 12.5px;
}

.agent-detail__error {
  color: var(--color-danger);
}

.agent-detail__status {
  color: var(--color-text-subtle);
}

.agent-detail__hint {
  margin: 12px 0 0;
  font-size: 11.5px;
  line-height: 1.45;
  color: var(--color-text-subtle);
}

.agent-detail__policy {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid color-mix(in srgb, var(--color-border) 80%, transparent);
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
  padding-left: 12px;
}
</style>
