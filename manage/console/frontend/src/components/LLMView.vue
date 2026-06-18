<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { createLLMConfig, deleteLLMConfig, fetchLLMConfigs } from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const configs = ref([]);
const loading = ref(false);
const error = ref("");
const saving = ref(false);

const form = reactive({
  name: "",
  provider: "openai",
  base_url: "",
  model: "",
  api_key: "",
  is_default: false,
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

function resetForm() {
  form.name = "";
  form.provider = "openai";
  form.base_url = "";
  form.model = "";
  form.api_key = "";
  form.is_default = false;
}

async function onCreate() {
  if (!form.name.trim() || !form.base_url.trim() || !form.model.trim()) {
    emit("toast", { message: "name / base_url / model 必填", type: "error" });
    return;
  }
  saving.value = true;
  try {
    await createLLMConfig({
      name: form.name.trim(),
      provider: form.provider,
      base_url: form.base_url.trim(),
      model: form.model.trim(),
      api_key: form.api_key,
      is_default: form.is_default,
    });
    emit("toast", { message: `已创建配置 ${form.name.trim()}`, type: "success" });
    resetForm();
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onDelete(cfg) {
  if (!window.confirm(`删除配置 ${cfg.id}？`)) return;
  try {
    await deleteLLMConfig(cfg.id);
    emit("toast", { message: `已删除 ${cfg.id}`, type: "success" });
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
  <section class="panel">
    <div class="panel-head">
      <h2 class="panel-title">新建 LLM 配置</h2>
      <span class="panel-meta">key 仅在创建时填写；列表只显示掩码</span>
    </div>
    <div class="filters-grid">
      <label class="field">
        <span>名称</span>
        <input v-model="form.name" placeholder="如 cliproxy-claude" />
      </label>
      <label class="field field-narrow">
        <span>provider</span>
        <select v-model="form.provider">
          <option value="openai">openai</option>
          <option value="deepseek">deepseek</option>
          <option value="qwen">qwen</option>
          <option value="vllm">vllm</option>
        </select>
      </label>
      <label class="field field-grow">
        <span>base_url（含 /v1）</span>
        <input v-model="form.base_url" placeholder="http://host:port/v1" />
      </label>
      <label class="field">
        <span>model</span>
        <input v-model="form.model" placeholder="claude-sonnet-4-6" />
      </label>
      <label class="field field-grow">
        <span>api_key</span>
        <input v-model="form.api_key" type="password" placeholder="sk-..." />
      </label>
      <label class="field field-narrow checkbox-field">
        <span>设为默认</span>
        <input v-model="form.is_default" type="checkbox" />
      </label>
      <div class="field field-narrow">
        <span>&nbsp;</span>
        <button class="btn btn-primary" :disabled="saving" @click="onCreate">
          {{ saving ? "创建中…" : "创建" }}
        </button>
      </div>
    </div>
  </section>

  <section class="table-panel">
    <div class="panel-head">
      <h2 class="panel-title">已注册配置</h2>
      <span class="panel-meta">{{ loading ? "加载中…" : `${configs.length} 条` }}</span>
    </div>
    <p v-if="error" class="banner-error">{{ error }}</p>
    <div class="table-scroll">
      <table class="data-table">
        <thead>
          <tr>
            <th>名称 / ID</th>
            <th>provider</th>
            <th>model</th>
            <th>base_url</th>
            <th>api_key</th>
            <th>默认</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in configs" :key="c.id">
            <td>
              <strong>{{ c.name }}</strong>
              <div class="muted">{{ c.id }}</div>
            </td>
            <td>{{ c.provider }}</td>
            <td>{{ c.model }}</td>
            <td class="cell-wrap">{{ c.base_url }}</td>
            <td><code>{{ c.api_key }}</code></td>
            <td>
              <span v-if="c.is_default" class="pill pill-online">默认</span>
              <span v-else class="pill pill-muted">—</span>
            </td>
            <td>
              <button class="btn btn-ghost" @click="onDelete(c)">删除</button>
            </td>
          </tr>
          <tr v-if="!loading && configs.length === 0">
            <td colspan="7" class="empty">暂无 LLM 配置</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
