<script setup>
import { computed, onMounted, ref, watch } from "vue";
import {
  createLLMConfig,
  deleteLLMConfig,
  fetchLLMConfigs,
  updateLLMConfig,
} from "../api.js";
import LlmConfigModal from "./LlmConfigModal.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const configs = ref([]);
const loading = ref(false);
const error = ref("");
const saving = ref(false);

const modalOpen = ref(false);
const modalMode = ref("create");
const editing = ref(null);

const sortedConfigs = computed(() => {
  const list = [...(configs.value || [])];
  list.sort((a, b) => {
    if (a.is_default !== b.is_default) return a.is_default ? -1 : 1;
    return String(a.name || "").localeCompare(String(b.name || ""), "zh");
  });
  return list;
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    configs.value = await fetchLLMConfigs();
  } catch (err) {
    error.value = err.message;
    emit("toast", { message: err.message, type: "error" });
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  modalMode.value = "create";
  editing.value = null;
  modalOpen.value = true;
}

function openEdit(cfg) {
  modalMode.value = "edit";
  editing.value = cfg;
  modalOpen.value = true;
}

function closeModal() {
  modalOpen.value = false;
  editing.value = null;
}

async function onConfirm(payload) {
  saving.value = true;
  try {
    if (modalMode.value === "create") {
      await createLLMConfig(payload);
      emit("toast", { message: `已创建配置 ${payload.name}`, type: "success" });
    } else if (editing.value?.id) {
      await updateLLMConfig(editing.value.id, payload);
      emit("toast", { message: `已更新 ${payload.name}`, type: "success" });
    }
    closeModal();
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onSetDefault(cfg) {
  if (!cfg?.id || cfg.is_default) return;
  try {
    await updateLLMConfig(cfg.id, {
      name: cfg.name,
      provider: cfg.provider,
      base_url: cfg.base_url,
      model: cfg.model,
      api_key: "",
      is_default: true,
      allowed_groups: Array.isArray(cfg.allowed_groups) ? cfg.allowed_groups : [],
    });
    emit("toast", { message: `已将 ${cfg.name} 设为默认`, type: "success" });
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

async function onDelete(cfg) {
  if (!window.confirm(`删除配置「${cfg.name}」？`)) return;
  try {
    await deleteLLMConfig(cfg.id);
    emit("toast", { message: `已删除 ${cfg.name}`, type: "success" });
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

watch(
  () => props.active,
  (now) => {
    if (now) load();
  },
);
onMounted(() => {
  if (props.active) load();
});
defineExpose({ load });
</script>

<template>
  <section class="llm-view">
    <p v-if="error" class="banner banner-error" role="alert">{{ error }}</p>
    <p v-else-if="loading && !sortedConfigs.length" class="state">加载中…</p>

    <div v-else class="llm-card-grid">
      <article v-for="c in sortedConfigs" :key="c.id" class="llm-card">
        <div class="llm-card__top">
          <strong class="llm-card__name">{{ c.name }}</strong>
          <span v-if="c.is_default" class="pill pill-online">默认</span>
        </div>
        <p class="llm-card__id muted">{{ c.id }}</p>
        <dl class="llm-card__meta">
          <div>
            <dt>Provider</dt>
            <dd>{{ c.provider }}</dd>
          </div>
          <div>
            <dt>Model</dt>
            <dd>{{ c.model }}</dd>
          </div>
          <div class="llm-card__wide">
            <dt>Base URL</dt>
            <dd class="cell-wrap">{{ c.base_url }}</dd>
          </div>
          <div>
            <dt>API Key</dt>
            <dd><code>{{ c.api_key || "—" }}</code></dd>
          </div>
        </dl>
        <div class="llm-card__actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="openEdit(c)">编辑</button>
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            :disabled="c.is_default"
            @click="onSetDefault(c)"
          >
            设为默认
          </button>
          <button type="button" class="btn btn-ghost btn-sm" @click="onDelete(c)">删除</button>
        </div>
      </article>

      <button type="button" class="llm-card llm-card--add" @click="openCreate">
        <span class="wg-card__plus" aria-hidden="true">+</span>
        <strong>{{ sortedConfigs.length ? "新建配置" : "新建第一条配置" }}</strong>
        <span class="muted llm-card--add-hint">
          {{
            sortedConfigs.length
              ? "Key 仅在创建或更新时提交"
              : "填写 URL、Key，测试并拉取模型"
          }}
        </span>
      </button>
    </div>

    <LlmConfigModal
      :open="modalOpen"
      :mode="modalMode"
      :config="editing"
      @close="closeModal"
      @confirm="onConfirm"
    />
  </section>
</template>
