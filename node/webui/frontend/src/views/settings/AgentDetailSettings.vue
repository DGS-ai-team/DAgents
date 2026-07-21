<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../../api/node.js";
import AgentSettingsForm from "../../components/AgentSettingsForm.vue";
import {
  buildPatchAgentPayload,
  draftFromAgentView,
  emptyAgentDraft,
} from "../../utils/agentTemplateForm.js";

const route = useRoute();
const router = useRouter();

const loading = ref(true);
const saving = ref(false);
const reloading = ref(false);
const error = ref("");
const statusMessage = ref("");
const showAdvanced = ref(true);
const llmProfiles = ref([]);
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
    const [agent, setup] = await Promise.all([
      api.getAgent(agentId.value),
      api.getSetupConfig().catch(() => null),
    ]);
    agentMeta.value = agent;
    llmProfiles.value = Array.isArray(setup?.llm?.profiles)
      ? setup.llm.profiles.map((p) => ({
          id: String(p.id || "").trim(),
          provider: p.provider || "",
          model: p.model || "",
        })).filter((p) => p.id)
      : [];
    Object.assign(
      draft,
      draftFromAgentView(
        agent,
        llmProfiles.value.map((p) => p.id),
      ),
    );
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
    statusMessage.value = "已保存，运行时已按新配置重建。";
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
    statusMessage.value = "已刷新运行时配置。";
  } catch (e) {
    error.value = e.message || "刷新失败";
  } finally {
    reloading.value = false;
  }
}

function backToList() {
  router.push({ name: "settings-agents" });
}

watch(agentId, () => {
  void load();
});

onMounted(load);
</script>

<template>
  <div class="settings-page settings-embedded">
    <div class="agent-detail__head">
      <button type="button" class="btn btn--ghost btn--sm" @click="backToList">← Agents 列表</button>
      <h1 class="settings-page__title">Agent 配置</h1>
      <p v-if="agentMeta" class="agent-detail__id">{{ agentMeta.agent_id }}</p>
    </div>

    <p v-if="loading" class="agent-detail__status">加载中…</p>
    <template v-else>
      <AgentSettingsForm
        :draft="draft"
        :llm-profiles="llmProfiles"
        v-model:show-advanced="showAdvanced"
      />

      <div class="agent-detail__actions">
        <button type="button" class="btn btn--primary" :disabled="saving" @click="save">
          {{ saving ? "保存中…" : "保存并应用" }}
        </button>
        <button type="button" class="btn btn--ghost" :disabled="reloading || saving" @click="reloadRuntime">
          {{ reloading ? "刷新中…" : "刷新运行时" }}
        </button>
      </div>
      <p v-if="statusMessage" class="agent-detail__ok">{{ statusMessage }}</p>
      <p v-if="error" class="agent-detail__error">{{ error }}</p>
      <p class="agent-detail__hint">
        保存后会立即按快照重建该 Agent 的工具表与回合选项；发送消息前也会检测配置版本并自动重载。
      </p>
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
</style>
