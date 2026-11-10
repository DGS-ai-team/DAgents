<script setup>
import { computed, onMounted, ref, watch } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import LlmProfileModal from "./LlmProfileModal.vue";
import UiIcon from "./UiIcon.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";
import { notifyConfigurationChanged } from "../utils/configurationEvents.js";

const props = defineProps({
  subtitle: { type: String, default: "管理模型配置、默认模型和管理平台连接。" },
  requestedProfileId: { type: String, default: "" },
});
const emit = defineEmits(["profile-request-handled"]);

const DEEPSEEK_DEFAULT = {
  id: "deepseek-v4-flash",
  provider: "deepseek",
  base_url: "https://api.deepseek.com",
  model: "deepseek-v4-flash",
  api_key: "",
  has_api_key: false,
  mock: false,
  multimodal_enabled: false,
};

const { loading, saving, error, statusMessage, dirty, configPath, configWritable, form, load, save } =
  useSetupConfig();

const modalOpen = ref(false);
const modalMode = ref("edit"); // create | edit
const editingId = ref("");

const manageFieldsDisabled = computed(() => !form.manage.enabled);
const profiles = computed(() => (Array.isArray(form.llm.profiles) ? form.llm.profiles : []));
const existingIds = computed(() => profiles.value.map((p) => p.id));
const modalProfile = computed(() => profiles.value.find((p) => p.id === editingId.value) || null);

function ensureProfiles() {
  if (!Array.isArray(form.llm.profiles)) form.llm.profiles = [];
  if (form.llm.profiles.length === 0) {
    form.llm.profiles.push({ ...DEEPSEEK_DEFAULT });
  }
}

function openCreateModal() {
  ensureProfiles();
  modalMode.value = "create";
  editingId.value = "";
  modalOpen.value = true;
}

function openEditModal(id) {
  ensureProfiles();
  modalMode.value = "edit";
  editingId.value = id;
  modalOpen.value = true;
}

function closeModal() {
  modalOpen.value = false;
  emit("profile-request-handled");
}

async function onModalConfirm(payload) {
  ensureProfiles();
  if (modalMode.value === "create") {
    form.llm.profiles.push({
      id: payload.id,
      provider: payload.provider,
      base_url: payload.base_url,
      model: payload.model,
      api_key: payload.api_key || "",
      has_api_key: false,
      clear_api_key: false,
      mock: !!payload.mock,
      multimodal_enabled: !!payload.multimodal_enabled,
      thinking: payload.thinking || "enabled",
      reasoning_effort: payload.reasoning_effort || "",
      thinking_budget: payload.thinking_budget ?? null,
      clear_reasoning_effort: !!payload.clear_reasoning_effort,
      clear_thinking_budget: !!payload.clear_thinking_budget,
    });
  } else {
    const currentId = String(editingId.value || "").trim();
    const sourceId = String(payload.original_id || currentId).trim();
    const target = form.llm.profiles.find((p) => p.id === currentId);
    if (!target) {
      error.value = "配置不存在";
      return;
    }
    const nextId = String(payload.id || "").trim();
    if (nextId !== currentId && form.llm.profiles.some((p) => p.id === nextId)) {
      error.value = `配置「${nextId}」已存在`;
      return;
    }
    target.id = nextId;
    if (nextId !== currentId) target.original_id = target.original_id || sourceId;
    target.provider = payload.provider;
    target.base_url = payload.base_url;
    target.model = payload.model;
    target.api_key = payload.api_key || "";
    target.clear_api_key = !!payload.clear_api_key;
    target.mock = !!payload.mock;
    target.multimodal_enabled = !!payload.multimodal_enabled;
    target.thinking = payload.thinking || "enabled";
    target.reasoning_effort = payload.reasoning_effort || "";
    target.thinking_budget = payload.thinking_budget ?? null;
    target.clear_reasoning_effort = !!payload.clear_reasoning_effort;
    target.clear_thinking_budget = !!payload.clear_thinking_budget;
    if (String(form.llm.active || "") === currentId) form.llm.active = nextId;
    editingId.value = nextId;
  }
  modalOpen.value = false;
  emit("profile-request-handled");
  await saveConnection();
}

function removeProfile(id) {
  ensureProfiles();
  if (form.llm.profiles.length <= 1) {
    error.value = "至少保留一个 LLM 配置";
    return;
  }
  form.llm.profiles = form.llm.profiles.filter((p) => p.id !== id);
  if (editingId.value === id) {
    editingId.value = "";
    modalOpen.value = false;
  }
}

function moveProfile(id, delta) {
  const list = form.llm.profiles;
  const idx = list.findIndex((p) => p.id === id);
  if (idx < 0) return;
  const next = idx + delta;
  if (next < 0 || next >= list.length) return;
  const copy = list.slice();
  const [item] = copy.splice(idx, 1);
  copy.splice(next, 0, item);
  form.llm.profiles = copy;
}

function llmPayload() {
  ensureProfiles();
  return {
    active: form.llm.active || profiles.value[0]?.id || "",
    profiles: form.llm.profiles.map((p) => ({
      id: p.id,
      original_id: p.original_id || undefined,
      provider: p.provider,
      base_url: p.base_url,
      model: p.model,
      api_key: p.api_key || undefined,
      clear_api_key: !!p.clear_api_key,
      mock: p.mock || p.provider === "mock",
      thinking: p.thinking || undefined,
      reasoning_effort: p.reasoning_effort || "",
      thinking_budget: p.thinking_budget ?? undefined,
      clear_reasoning_effort: !!p.clear_reasoning_effort,
      clear_thinking_budget: !!p.clear_thinking_budget,
      multimodal_enabled: !!p.multimodal_enabled,
    })),
  };
}

function managePayload() {
  const payload = {
    enabled: !!form.manage.enabled,
    url: form.manage.url || "",
    workgroup_enabled: form.manage.workgroup_enabled ?? true,
    clear_node_token: !!form.manage.clear_node_token,
  };
  if (form.manage.node_token) payload.node_token = form.manage.node_token;
  return payload;
}

async function saveConnection() {
  if (form.manage.enabled && !String(form.manage.url || "").trim()) {
    error.value = "已启用管理平台时，请填写服务地址";
    return;
  }
  const saved = await save({
    llm: llmPayload(),
    manage: managePayload(),
  });
  ensureProfiles();
  if (saved) notifyConfigurationChanged("llm");
}

watch(
  () => form.llm.profiles,
  () => ensureProfiles(),
  { deep: true }
);

onMounted(async () => {
  await load();
  ensureProfiles();
  handleRequestedProfile();
});

watch(() => props.requestedProfileId, () => {
  if (!loading.value) handleRequestedProfile();
});

function handleRequestedProfile() {
  const id = String(props.requestedProfileId || "").trim();
  if (!id) return;
  if (modalOpen.value) {
    error.value = "请先保存或关闭当前模型配置，再打开其他配置。";
    emit("profile-request-handled");
    return;
  }
  if (!profiles.value.some((profile) => profile.id === id)) {
    error.value = `未找到模型配置「${id}」`;
    emit("profile-request-handled");
    return;
  }
  openEditModal(id);
  emit("profile-request-handled");
}
</script>

<template>
  <ConfigPanelShell
    title="模型与连接"
    :subtitle="props.subtitle"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    :dirty="dirty"
    @save="saveConnection"
  >
    <section class="settings-section settings-section--llm">
      <div class="settings-section__head">
        <h2 class="settings-section__title">模型</h2>
        <button type="button" class="btn btn--ghost btn--sm" @click="openCreateModal">新增</button>
      </div>
      <p class="settings-section__desc">
        第一条为默认配置；点击卡片可编辑。API Key 可留空（本机环境变量亦可）。
      </p>

      <div class="llm-config-cards">
        <article
          v-for="(p, index) in profiles"
          :key="p.id"
          class="llm-config-card"
          :class="{ 'llm-config-card--default': index === 0 }"
        >
          <button type="button" class="llm-config-card__hit" @click="openEditModal(p.id)">
            <div class="llm-config-card__title-row">
              <span class="llm-config-card__id">{{ p.id }}</span>
              <span v-if="index === 0" class="llm-config-card__badge">默认</span>
              <span v-if="p.mock" class="llm-config-card__badge llm-config-card__badge--muted">Mock</span>
            </div>
            <div class="llm-config-card__meta">
              <span>{{ p.provider || "—" }}</span>
              <span class="llm-config-card__dot">·</span>
              <span>{{ p.model || "未设模型" }}</span>
            </div>
            <div class="llm-config-card__key">
              <span v-if="p.has_api_key || (p.api_key && p.api_key.length)">密钥已配置</span>
              <span v-else-if="p.mock">无需密钥</span>
              <span v-else>密钥未填写</span>
            </div>
          </button>
          <div class="llm-config-card__actions">
            <button type="button" class="btn btn--ghost btn--compact" :disabled="index === 0" title="上移" aria-label="上移" @click="moveProfile(p.id, -1)">
              <UiIcon name="arrow-up" :size="14" />
            </button>
            <button
              type="button"
              class="btn btn--ghost btn--compact"
              :disabled="index === profiles.length - 1"
              title="下移"
              @click="moveProfile(p.id, 1)"
            >
              <UiIcon name="arrow-down" :size="14" />
            </button>
            <button
              v-if="profiles.length > 1"
              type="button"
              class="btn btn--ghost btn--compact"
              title="删除"
              @click="removeProfile(p.id)"
            >
              删除
            </button>
          </div>
        </article>
      </div>
    </section>

    <section class="settings-section settings-section--manage">
      <div class="settings-section__head">
        <h2 class="settings-section__title">管理平台</h2>
      </div>
      <p class="settings-section__desc">
        连接到管理平台后可注册本机服务，并参与工作组协作。
      </p>
      <div class="setup-manage-stack">
        <label class="settings-row settings-toggle">
          <input v-model="form.manage.enabled" type="checkbox" />
          <span class="settings-row__copy"><span class="settings-row__title">启用连接</span></span>
        </label>
        <label class="settings-row settings-row--field">
          <span class="settings-row__copy"><span class="settings-row__title">服务地址</span></span>
          <input
            v-model="form.manage.url"
            class="settings-field__input"
            type="url"
            inputmode="url"
            placeholder="http://192.168.1.10:8020"
            :disabled="manageFieldsDisabled"
            autocomplete="off"
          />
        </label>
        <label class="settings-row settings-row--field">
          <span class="settings-row__copy"><span class="settings-row__title">管理平台连接密钥</span><span class="settings-row__description">{{ form.manage.has_node_token ? "已配置凭据；替换时输入新值。" : "尚未配置凭据。" }}</span></span>
          <input
            v-model="form.manage.node_token"
            class="settings-field__input"
            type="password"
            autocomplete="new-password"
            placeholder="留空保持当前凭据"
          />
        </label>
        <label v-if="form.manage.has_node_token" class="settings-row settings-toggle">
          <input v-model="form.manage.clear_node_token" type="checkbox" />
          <span class="settings-row__copy"><span class="settings-row__title">清除已保存的管理平台连接密钥</span><span class="settings-row__description">保存时删除当前已保存的密钥。</span></span>
        </label>
        <label class="settings-row settings-toggle" :class="{ 'settings-toggle--disabled': manageFieldsDisabled }">
          <input
            v-model="form.manage.workgroup_enabled"
            type="checkbox"
            :disabled="manageFieldsDisabled"
          />
          <span class="settings-row__copy"><span class="settings-row__title">启用工作组通道</span><span class="settings-row__description">允许此本机服务加入和使用工作组。</span></span>
        </label>
      </div>
    </section>
  </ConfigPanelShell>

  <LlmProfileModal
    :open="modalOpen"
    :mode="modalMode"
    :profile="modalProfile"
    :existing-ids="existingIds"
    @close="closeModal"
    @confirm="onModalConfirm"
  />
</template>
