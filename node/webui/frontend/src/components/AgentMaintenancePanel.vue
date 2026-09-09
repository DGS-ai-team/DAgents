<script setup>
import { computed, onMounted, ref, watch } from "vue";

const props = defineProps({ agentId: { type: String, required: true } });
const unsupportedMessage = "当前 Node 版本尚不支持此功能，请更新 Node 后重试";
const defaultConfig = () => ({
  maintenance_enabled: false,
  maintenance_schedule: "daily 09:00",
  timezone: "Asia/Shanghai",
  profile_revision: 0,
});
const config = ref(defaultConfig());
const loading = ref(false);
const saving = ref(false);
const running = ref(false);
const error = ref("");
const conflict = ref(false);
const loadError = ref("");
const result = ref(null);
let loadEpoch = 0;
let mutationEpoch = 0;

const endpoint = computed(
  () => `/v1/agents/${encodeURIComponent(props.agentId)}/maintenance`,
);
const scheduleTime = computed({
  get: () =>
    String(config.value.maintenance_schedule || "").match(
      /(\d{2}:\d{2})$/,
    )?.[1] || "",
  set: (value) => {
    config.value.maintenance_schedule = `daily ${value}`;
  },
});
const canOperate = computed(
  () =>
    !loading.value &&
    !saving.value &&
    !running.value &&
    Number(config.value.profile_revision) > 0,
);

const statusLabels = {
  pending: "等待维护",
  queued: "等待维护",
  running: "维护中",
  succeeded: "已完成",
  success: "已完成",
  completed: "已完成",
  failed: "失败",
  recovery_required: "需要恢复",
};

function statusLabel(status) {
  if (!status) return "暂无记录";
  return statusLabels[status] || "未知状态";
}

function resultStatusLabel(status) {
  return status ? statusLabel(status) : "状态未知";
}

function usage(value) {
  const raw = typeof value === "number" ? value : value?.maintenance_tokens;
  const tokens = typeof raw === "number" ? raw : Number(raw);
  if (
    value == null ||
    value.unknown ||
    raw == null ||
    !Number.isFinite(tokens) ||
    tokens < 0
  )
    return "累计维护用量：待对账";
  return `累计维护用量：${tokens} tokens`;
}

function message(body, fallback) {
  try {
    const parsed = JSON.parse(body);
    return String(parsed?.message || parsed?.error || fallback);
  } catch {
    return String(body || fallback);
  }
}

async function responseJSON(response, fallback) {
  if (response.ok) return response.json();
  const cause = new Error(message(await response.text(), fallback));
  cause.status = response.status;
  if (response.status === 404) cause.message = unsupportedMessage;
  throw cause;
}

function isCurrentLoad(id, token) {
  return id === props.agentId && token === loadEpoch;
}

function isCurrentMutation(id, token) {
  return id === props.agentId && token === mutationEpoch;
}

function reset() {
  loadEpoch += 1;
  mutationEpoch += 1;
  config.value = defaultConfig();
  loading.value = false;
  saving.value = false;
  running.value = false;
  error.value = "";
  conflict.value = false;
  loadError.value = "";
  result.value = null;
}

async function load() {
  const id = props.agentId;
  const token = ++loadEpoch;
  loading.value = true;
  loadError.value = "";
  error.value = "";
  result.value = null;
  try {
    const response = await fetch(endpoint.value);
    const data = await responseJSON(response, "维护配置加载失败");
    if (isCurrentLoad(id, token))
      config.value = { ...defaultConfig(), ...data };
  } catch (cause) {
    if (isCurrentLoad(id, token))
      loadError.value = cause.message || "维护配置加载失败";
  } finally {
    if (isCurrentLoad(id, token)) loading.value = false;
  }
}

async function save() {
  if (!canOperate.value) return;
  const id = props.agentId;
  const token = ++mutationEpoch;
  saving.value = true;
  error.value = "";
  conflict.value = false;
  try {
    const response = await fetch(endpoint.value, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        maintenance_enabled: config.value.maintenance_enabled,
        maintenance_schedule: config.value.maintenance_schedule,
        timezone: config.value.timezone,
        expected_revision: config.value.profile_revision,
      }),
    });
    const data = await responseJSON(response, "维护配置保存失败");
    if (isCurrentMutation(id, token))
      config.value = { ...config.value, ...data };
  } catch (cause) {
    if (isCurrentMutation(id, token)) {
      conflict.value = cause.status === 409;
      error.value = conflict.value
        ? "维护配置已被修改，请刷新后重试"
        : cause.message || "维护配置保存失败";
    }
  } finally {
    if (isCurrentMutation(id, token)) saving.value = false;
  }
}

async function run() {
  if (!canOperate.value) return;
  const id = props.agentId;
  const token = ++mutationEpoch;
  running.value = true;
  error.value = "";
  result.value = null;
  try {
    const response = await fetch(`${endpoint.value}/run`, { method: "POST" });
    const data = await responseJSON(response, "维护运行失败");
    if (isCurrentMutation(id, token)) result.value = data;
  } catch (cause) {
    if (isCurrentMutation(id, token)) {
      error.value = cause.message || "维护运行失败";
      result.value = { error: error.value };
    }
  } finally {
    if (isCurrentMutation(id, token)) running.value = false;
  }
}

onMounted(load);
watch(
  () => props.agentId,
  () => {
    reset();
    void load();
  },
);
</script>

<template>
  <section class="agent-maintenance-panel">
    <div class="maintenance-heading">
      <div>
        <span class="settings-kicker">维护</span>
        <h3>自动维护</h3>
      </div>
    </div>
    <p v-if="loading" class="hint">加载中…</p>
    <div v-else-if="loadError" class="maintenance-load-error">
      <p class="error" role="alert">{{ loadError }}</p>
      <button class="btn btn--ghost" @click="load">重新加载</button>
    </div>
    <template v-else>
      <div class="maintenance-summary">
        <p>
          下次维护：{{
            config.next_at
              ? new Date(config.next_at).toLocaleString()
              : "未安排"
          }}
        </p>
        <p>最近结果：{{ statusLabel(config.last?.status) }}</p>
        <p>{{ usage(config.usage) }}</p>
      </div>
      <p
        v-if="config.last?.status === 'recovery_required'"
        class="maintenance-explanation"
      >
        上次维护中断，需要核对执行结果和用量；重新运行不会自动解除待恢复状态。
      </p>
      <label class="settings-field"
        ><span class="settings-field__label">启用维护</span
        ><input v-model="config.maintenance_enabled" type="checkbox"
      /></label>
      <label class="settings-field"
        ><span class="settings-field__label">每天运行时间</span
        ><input v-model="scheduleTime" type="time"
      /></label>
      <label class="settings-field"
        ><span class="settings-field__label">时区</span
        ><input v-model="config.timezone" type="text"
      /></label>
      <div class="maintenance-actions">
        <button class="btn btn--ghost" :disabled="!canOperate" @click="save">
          {{ saving ? "保存中…" : "保存维护设置" }}</button
        ><button class="btn btn--primary" :disabled="!canOperate" @click="run">
          {{ running ? "运行中…" : "立即运行维护" }}
        </button>
      </div>
      <div v-if="error" class="maintenance-error">
        <p class="error" role="alert">{{ error }}</p>
        <button
          v-if="conflict"
          class="maintenance-reload btn btn--ghost btn--sm"
          @click="load"
        >
          重新加载配置
        </button>
      </div>
      <p v-if="result && !result.error" class="maintenance-result">
        本次维护状态：{{ resultStatusLabel(result.status) }}。 {{ usage(result.usage) }}
      </p>
    </template>
  </section>
</template>

<style scoped>
.agent-maintenance-panel {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--border-subtle);
}
.maintenance-heading {
  display: flex;
  justify-content: space-between;
}
.maintenance-heading h3 {
  margin: 3px 0;
}
.settings-kicker,
.hint {
  color: var(--text-secondary);
  font-size: 12px;
}
.maintenance-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  color: var(--text-secondary);
  font-size: 13px;
}
.maintenance-summary p {
  margin: 0;
}
.maintenance-explanation {
  color: var(--text-secondary);
  font-size: 13px;
}
.settings-field {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(220px, 1.35fr);
  align-items: center;
  gap: 20px;
  padding: 13px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.maintenance-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 14px;
}
.maintenance-load-error {
  display: flex;
  align-items: center;
  gap: 12px;
}
.maintenance-error {
  display: flex;
  align-items: center;
  gap: 12px;
}
.error {
  color: var(--danger, #c44);
}
.maintenance-result {
  padding: 12px;
  background: var(--surface-subtle, rgba(127, 127, 127, 0.08));
}
@media (max-width: 640px) {
  .maintenance-summary {
    grid-template-columns: 1fr;
    gap: 4px;
  }
  .settings-field {
    grid-template-columns: 1fr;
    gap: 8px;
  }
  .maintenance-load-error {
    align-items: stretch;
    flex-direction: column;
  }
  .maintenance-error {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
