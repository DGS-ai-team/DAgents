<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({
  agentId: { type: String, required: true },
  enabled: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  unsupported: { type: Boolean, default: false },
  confirmed: { type: Boolean, default: true },
  saveError: { type: String, default: "" },
});
const emit = defineEmits(["update:enabled", "unsupported"]);
const loading = ref(false);
const error = ref("");
const observations = ref([]);
let controller = null;
let epoch = 0;
const legacyNode = computed(() => error.value === "__unsupported__");
function levelLabel(value) {
  return ({ low: "低风险", medium: "中风险", high: "高风险", critical: "严重风险" })[String(value || "").toLowerCase()] || "未知级别";
}
function formatDate(value) {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}

function load() {
  const id = props.agentId;
  const token = ++epoch;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  observations.value = [];
  api.getAgentRiskObservations(id, { signal: controller.signal }).then((data) => {
    if (token !== epoch || id !== props.agentId) return;
    observations.value = Array.isArray(data?.observations) ? data.observations : [];
    emit("unsupported", false);
  }).catch((cause) => {
    if (token !== epoch || id !== props.agentId || cause?.name === "AbortError") return;
    if (cause?.status === 404) { error.value = "__unsupported__"; emit("unsupported", true); }
    else error.value = cause?.message || "风险观察加载失败";
  }).finally(() => {
    if (token === epoch && id === props.agentId) loading.value = false;
  });
}

function toggle(event) {
  emit("update:enabled", Boolean(event.target.checked));
}

watch(() => props.agentId, load, { immediate: true });
onBeforeUnmount(() => { epoch += 1; controller?.abort(); });
</script>

<template>
  <section class="risk-observation-panel">
    <div class="risk-observation-panel__heading">
      <div><span class="settings-kicker">Auto 专属</span><h3>风险观察</h3></div>
    </div>
    <p class="risk-observation-panel__description">
      只记录风险建议，不改变现有审批；评估用量计入智能体总预算，并单独记录。
    </p>
    <p v-if="!confirmed" class="hint" role="status">设置尚未得到 Node 确认。</p>
    <p v-if="saveError" class="error" role="alert">{{ saveError }}</p>
    <label class="settings-field risk-observation-panel__toggle">
      <span class="settings-field__label">启用风险观察</span>
      <input type="checkbox" :checked="enabled" :disabled="saving || unsupported" @change="toggle" />
    </label>
    <p v-if="loading" class="hint">加载中…</p>
    <p v-else-if="legacyNode" class="hint">当前 Node 版本尚不支持风险观察。</p>
    <p v-else-if="error" class="error" role="alert">{{ error }}</p>
    <p v-else-if="!observations.length" class="hint">暂无风险观察记录。</p>
    <ul v-else class="risk-observation-list">
      <li v-for="item in observations" :key="`${item.request_id}-${item.created_at}`" class="risk-observation-list__item">
        <div class="risk-observation-list__meta">
          <strong>{{ levelLabel(item.level) }}</strong>
          <span>{{ item.tool_name || "未知工具" }}</span>
          <time>{{ formatDate(item.created_at) }}</time>
        </div>
        <p>{{ item.reason || "未提供原因" }}</p>
        <p v-if="item.recommendation" class="hint">建议：{{ item.recommendation }}</p>
        <span v-if="item.risk_unknown" class="hint">风险判断未知</span>
        <span v-if="item.usage_unknown" class="hint">用量判断未知</span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.risk-observation-panel { display: grid; gap: 10px; padding-top: 20px; border-top: 1px solid var(--color-border); }
.risk-observation-panel__heading h3 { margin: 0; color: var(--color-text); font-size: 15px; }
.risk-observation-panel__description, .risk-observation-list p { margin: 0; color: var(--color-text-muted); font-size: 12px; line-height: 1.5; }
.risk-observation-panel__toggle { margin: 0; }
.risk-observation-list { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
.risk-observation-list__item { padding: 10px; border: 1px solid var(--color-border); border-radius: 7px; }
.risk-observation-list__meta { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; color: var(--color-text-subtle); font-size: 11px; }
.risk-observation-list__meta strong { color: var(--color-text); }
.risk-observation-list__meta time { margin-left: auto; }
.hint { color: var(--color-text-subtle); font-size: 12px; }
.error { color: var(--color-danger); font-size: 12px; }
</style>
