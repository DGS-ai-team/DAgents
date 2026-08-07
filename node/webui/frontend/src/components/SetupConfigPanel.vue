<script setup>
import { computed, onMounted, ref, watch } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import LlmProfileModal from "./LlmProfileModal.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const DEEPSEEK_DEFAULT = {
  id: "deepseek-chat",
  provider: "deepseek",
  base_url: "https://api.deepseek.com",
  model: "deepseek-chat",
  api_key: "",
  has_api_key: false,
  mock: false,
  multimodal_enabled: false,
};

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
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
    });
  } else {
    const fromId = String(payload.original_id || editingId.value || "").trim();
    const target = form.llm.profiles.find((p) => p.id === fromId);
    if (!target) {
      error.value = "配置不存在";
      return;
    }
    const nextId = String(payload.id || "").trim();
    if (nextId !== fromId && form.llm.profiles.some((p) => p.id === nextId)) {
      error.value = `配置「${nextId}」已存在`;
      return;
    }
    target.id = nextId;
    target.provider = payload.provider;
    target.base_url = payload.base_url;
    target.model = payload.model;
    target.api_key = payload.api_key || "";
    target.clear_api_key = !!payload.clear_api_key;
    target.mock = !!payload.mock;
    target.multimodal_enabled = !!payload.multimodal_enabled;
    editingId.value = nextId;
  }
  modalOpen.value = false;
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
    profiles: form.llm.profiles.map((p) => ({
      id: p.id,
      provider: p.provider,
      base_url: p.base_url,
      model: p.model,
      api_key: p.api_key || undefined,
      clear_api_key: !!p.clear_api_key,
      mock: p.mock || p.provider === "mock",
      // thinking / reasoning_effort 由状态栏运行时控制，不在连接配置里重复落盘
      multimodal_enabled: !!p.multimodal_enabled,
    })),
  };
}

function managePayload() {
  return {
    ...form.manage,
    workgroup_enabled: form.manage.workgroup_enabled ?? true,
  };
}

async function saveConnection() {
  if (form.manage.enabled && !String(form.manage.url || "").trim()) {
    error.value = "已启用管理平台时，请填写服务地址";
    return;
  }
  await save({
    llm: llmPayload(),
    manage: managePayload(),
  });
  ensureProfiles();
}

watch(
  () => form.llm.profiles,
  () => ensureProfiles(),
  { deep: true }
);

onMounted(async () => {
  await load();
  ensureProfiles();
});
</script>

<template>
  <ConfigPanelShell
    title="模型与连接"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load().then(ensureProfiles)"
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
            <button type="button" class="btn btn--ghost btn--compact" :disabled="index === 0" title="上移" @click="moveProfile(p.id, -1)">↑</button>
            <button
              type="button"
              class="btn btn--ghost btn--compact"
              :disabled="index === profiles.length - 1"
              title="下移"
              @click="moveProfile(p.id, 1)"
            >
              ↓
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
        连接到 Manage 后可注册本机 Node，并参与工作组协作。
      </p>
      <div class="setup-manage-stack">
        <label class="settings-toggle">
          <input v-model="form.manage.enabled" type="checkbox" />
          <span>启用连接</span>
        </label>
        <label class="settings-field settings-field--stack">
          <span class="settings-field__label">服务地址</span>
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
        <label class="settings-toggle" :class="{ 'settings-toggle--disabled': manageFieldsDisabled }">
          <input
            v-model="form.manage.workgroup_enabled"
            type="checkbox"
            :disabled="manageFieldsDisabled"
          />
          <span>启用工作组通道</span>
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
