<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import * as api from "../api/node.js";
import TriggerEditor from "./settings/TriggerEditor.vue";
import { formatTriggerCondition, formatUnixTime, shortId, truncateText, triggerFireStatusMessage } from "../utils/panelFormat.js";
import {
  buildCreatePayload,
  buildTriggerPatch,
  defaultTriggerForm,
  triggerToForm,
  validateTriggerForm,
} from "../utils/triggerForm.js";

defineProps({
  embedded: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const busyKey = ref("");
const error = ref("");
const statusMessage = ref("");
const data = ref(null);
const agents = ref([]);
const agentsLoading = ref(false);
const historyById = ref({});
const historyOpenId = ref("");
const editingId = ref(null);
const form = reactive(defaultTriggerForm());
const formSnapshot = ref("");
const formError = ref("");

const triggers = computed(() => {
  const rows = data.value?.triggers;
  return Array.isArray(rows) ? rows : [];
});

function rowBusy(key) {
  return busyKey.value === key;
}

function isManagedGoal(item) {
  return Boolean(String(item?.managed_goal_id || "").trim());
}

function displayName(item) {
  return isManagedGoal(item) ? "自主任务调度" : (item?.name || "(未命名)");
}

function resetForm() {
  Object.assign(form, defaultTriggerForm());
  formError.value = "";
}

async function load() {
  loading.value = true;
  agentsLoading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const [triggerResult, agentResult] = await Promise.allSettled([api.listTriggers(), api.listAgents()]);
    if (triggerResult.status === "rejected") throw triggerResult.reason;
    data.value = triggerResult.value;
    if (agentResult.status === "fulfilled") {
      const agentData = agentResult.value;
      agents.value = Array.isArray(agentData?.agents) ? agentData.agents : Array.isArray(agentData) ? agentData : [];
    } else {
      agents.value = [];
      error.value = "无法加载智能体列表，请重试后再创建任务";
    }
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
    agentsLoading.value = false;
  }
}

async function fireNow(item) {
  const id = item?.trigger_id;
  if (!id || rowBusy(`fire:${id}`)) return;
  busyKey.value = `fire:${id}`;
  error.value = "";
  statusMessage.value = "";
  try {
    const record = await api.fireTrigger(id);
    statusMessage.value = triggerFireStatusMessage(record, item.name || id);
    await loadHistory(item);
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

async function loadHistory(item) {
  const id = item?.trigger_id;
  if (!id) return;
  historyOpenId.value = id;
  try {
    const result = await api.getTriggerHistory(id);
    historyById.value = { ...historyById.value, [id]: result?.records || [] };
  } catch (e) {
    error.value = `无法加载触发历史：${e.message}`;
  }
}

async function recoverPending(item) {
  const id = item?.trigger_id;
  const deliveryId = String(item?.pending_delivery_id || "").trim();
  if (!id || !deliveryId || rowBusy(`recover:${id}`)) return;
  if (!window.confirm("确认并丢弃这次未确认的旧投递？\n\n不会重放该投递；恢复后任务会保持禁用，需要你手动启用。")) return;
  busyKey.value = `recover:${id}`;
  error.value = "";
  try {
    const updated = await api.recoverTrigger(id, deliveryId);
    replaceTrigger(updated);
    statusMessage.value = `已丢弃旧投递「${item.name || id}」，任务保持禁用；请核对后手动启用。`;
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

function cancelEdit() {
  editingId.value = null;
  resetForm();
}

function startCreate() {
  editingId.value = "new";
  resetForm();
  form.enabled = true;
  formSnapshot.value = "";
}

function startEdit(item) {
  editingId.value = item.trigger_id;
  Object.assign(form, triggerToForm(item));
  formSnapshot.value = JSON.stringify(form);
  formError.value = "";
}

function replaceTrigger(updated) {
  if (!data.value || !updated?.trigger_id) return;
  const rows = Array.isArray(data.value.triggers) ? [...data.value.triggers] : [];
  const idx = rows.findIndex((row) => row.trigger_id === updated.trigger_id);
  if (idx >= 0) rows[idx] = updated;
  else rows.unshift(updated);
  data.value = { ...data.value, triggers: rows };
}

function removeTrigger(triggerId) {
  if (!data.value) return;
  data.value = {
    ...data.value,
    triggers: triggers.value.filter((row) => row.trigger_id !== triggerId),
  };
}

async function saveForm() {
  const validationError = validateTriggerForm(form);
  if (validationError) {
    formError.value = validationError;
    return;
  }
  formError.value = "";
  if (!String(form.targetAgentId || "").trim()) {
    formError.value = "请选择目标智能体";
    return;
  }
  const isCreate = editingId.value === "new";
  busyKey.value = isCreate ? "save:new" : `save:${editingId.value}`;
  error.value = "";
  statusMessage.value = "";
  try {
    if (isCreate) {
      const created = await api.createTrigger(buildCreatePayload(form));
      replaceTrigger(created);
      statusMessage.value = `已创建「${created.name || form.name}」`;
    } else {
      const previous = formSnapshot.value ? JSON.parse(formSnapshot.value) : null;
      const changed = buildTriggerPatch(previous, form);
      if (!Object.keys(changed).length) {
        cancelEdit();
        return;
      }
      const updated = await api.updateTrigger(editingId.value, changed);
      replaceTrigger(updated);
      statusMessage.value = `已保存「${updated.name || form.name}」`;
    }
    cancelEdit();
  } catch (e) {
    formError.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

async function toggleEnabled(item) {
  const id = item?.trigger_id;
  if (!id || rowBusy(`toggle:${id}`)) return;
  const next = !item.enabled;
  busyKey.value = `toggle:${id}`;
  error.value = "";
  statusMessage.value = "";
  try {
    const updated = await api.updateTrigger(id, { enabled: next });
    replaceTrigger(updated);
    statusMessage.value = next ? `已启用「${updated.name || id}」` : `已禁用「${updated.name || id}」`;
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

async function removeTriggerConfirmed(item) {
  const id = item?.trigger_id;
  const label = item?.name || id;
  if (!id || rowBusy(`delete:${id}`)) return;
  if (!window.confirm(`确定删除定时任务「${label}」？\n\n删除后不可恢复。`)) return;
  busyKey.value = `delete:${id}`;
  error.value = "";
  statusMessage.value = "";
  try {
    await api.deleteTrigger(id);
    if (editingId.value === id) cancelEdit();
    removeTrigger(id);
    statusMessage.value = `已删除「${label}」`;
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

function targetHint(item) {
  const mode = String(item.session_target_mode || "").trim();
  const modeLabel =
    {
      latest_active: "最近活跃会话",
      bound_agent: "绑定智能体",
      new_session: "每次新建会话",
      fixed: "固定会话",
    }[mode] || (mode || "未指定");
  const bound = item.target_session_id;
  const targetId = String(item.target_agent_id || "").trim();
  const target = agents.value.find((agent) => String(agent.agent_id || agent.id || "") === targetId);
  const targetLabel = target
    ? `${target.display_name || target.name || targetId}${targetId ? ` · ${shortId(targetId, 12)}` : ""}`
    : targetId ? `未找到智能体 · ${shortId(targetId, 12)}` : "未指定智能体";
  if (bound) return `${targetLabel} · ${modeLabel} · ${shortId(bound, 12)}`;
  return `${targetLabel} · ${modeLabel}`;
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel triggers-panel" :class="{ 'settings-embedded-panel': embedded }">
    <header class="panel__header command-panel__header">
      <div>
        <div v-if="!embedded" class="panel__title">定时任务</div>
      </div>
      <div class="command-panel__header-actions">
        <button v-if="!embedded" type="button" class="btn btn--primary btn--sm" :disabled="loading || editingId === 'new'" @click="startCreate">
          新建
        </button>
        <button v-if="!embedded" type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <p v-if="statusMessage" class="command-panel__status">{{ statusMessage }}</p>
      <template v-if="data">
        <section v-if="editingId === 'new'" class="command-section trigger-editor-section">
          <h3 class="command-section__title">新建定时任务</h3>
          <TriggerEditor
            v-model:form="form"
            :form-error="formError"
            :busy="rowBusy('save:new')"
            :agents="agents"
            :agents-loading="agentsLoading"
            submit-label="创建"
            @submit="saveForm"
            @cancel="cancelEdit"
          />
        </section>

        <section class="command-section">
          <div class="command-section__intro">
              <div>
                <h3 class="command-section__title">已配置任务</h3>
                <p class="command-section__hint">任务会按调度条件自动投递；禁用后保留配置但不会触发。</p>
              </div>
            <div class="command-section__actions">
              <span class="command-section__count">{{ triggers.length }}</span>
              <button type="button" class="btn btn--primary btn--sm" :disabled="loading || editingId === 'new'" @click="startCreate">新建</button>
            </div>
          </div>
          <ul v-if="triggers.length" class="command-card-list">
            <li v-for="item in triggers" :key="item.trigger_id" class="command-card">
              <div class="command-card__main">
                <template v-if="editingId === item.trigger_id">
                  <h4 class="trigger-form__heading">编辑 · {{ displayName(item) }}</h4>
                  <TriggerEditor
                    v-model:form="form"
                    :form-error="formError"
                    :busy="rowBusy(`save:${item.trigger_id}`)"
                    :agents="agents"
                    :agents-loading="agentsLoading"
                    enabled-label="启用"
                    @submit="saveForm"
                    @cancel="cancelEdit"
                  />
                </template>
                <template v-else>
                  <div class="command-card__title">
                    {{ displayName(item) }}
                    <span
                      class="command-card__badge"
                      :class="item.enabled ? 'command-card__badge--active' : 'command-card__badge--muted'"
                    >
                      {{ item.enabled ? "已启用" : "已禁用" }}
                    </span>
                    <span v-if="isManagedGoal(item)" class="command-card__badge command-card__badge--muted">自主任务托管</span>
                    <span v-if="item.recovery_required" class="command-card__badge command-card__badge--muted">需要核对</span>
                  </div>
                  <div class="command-card__meta command-card__meta--mono">{{ isManagedGoal(item) ? "由自主任务管理" : shortId(item.trigger_id, 12) }}</div>
                  <dl class="command-kv-list command-kv-list--compact">
                    <div class="command-kv">
                      <dt>调度</dt>
                      <dd>{{ formatTriggerCondition(item.condition) }}</dd>
                    </div>
                    <div class="command-kv">
                      <dt>下次</dt>
                      <dd>{{ formatUnixTime(item.next_fire_at) }}</dd>
                    </div>
                    <div class="command-kv">
                      <dt>触发</dt>
                      <dd>{{ item.fire_count ?? 0 }} 次 · 上次 {{ formatUnixTime(item.last_fired_at) }}</dd>
                    </div>
                    <div class="command-kv">
                      <dt>目标</dt>
                      <dd>{{ targetHint(item) }}</dd>
                    </div>
                  </dl>
                  <div v-if="item.task_template" class="command-card__preview">
                    任务: {{ truncateText(item.task_template, 120) }}
                  </div>
                  <p v-if="isManagedGoal(item)" class="command-panel__hint">此触发器由 Auto 运行时管理，请在 Auto 设置中查看配置。</p>
                  <div v-if="item.recovery_required" class="command-panel__error">
                    {{ item.recovery_reason || "上次投递结果未知，请核对后恢复。" }}
                    <button type="button" class="btn btn--danger btn--sm" :disabled="rowBusy(`recover:${item.trigger_id}`)" @click="recoverPending(item)">确认并丢弃旧投递</button>
                  </div>
                  <div v-if="isManagedGoal(item)" class="command-card__actions">
                    <button type="button" class="btn btn--ghost btn--sm" :disabled="!!editingId" @click="loadHistory(item)">
                      {{ historyOpenId === item.trigger_id ? "刷新历史" : "触发历史" }}
                    </button>
                  </div>
                  <div v-else class="command-card__actions">
                    <label class="settings-toggle command-card__toggle">
                      <input
                        type="checkbox"
                        :checked="!!item.enabled"
                        :disabled="item.recovery_required || rowBusy(`toggle:${item.trigger_id}`)"
                        @click.prevent="toggleEnabled(item)"
                      />
                      <span>{{ item.enabled ? "已启用" : "已禁用" }}</span>
                    </label>
                    <button
                      type="button"
                      class="btn btn--ghost btn--sm"
                      :disabled="!!editingId || rowBusy(`save:${item.trigger_id}`)"
                      @click="startEdit(item)"
                    >
                      编辑
                    </button>
                    <button type="button" class="btn btn--secondary btn--sm" :disabled="!!editingId || item.recovery_required || rowBusy(`fire:${item.trigger_id}`)" @click="fireNow(item)">
                      {{ rowBusy(`fire:${item.trigger_id}`) ? "投递中…" : "立即运行" }}
                    </button>
                    <button type="button" class="btn btn--ghost btn--sm" :disabled="!!editingId" @click="loadHistory(item)">
                      {{ historyOpenId === item.trigger_id ? "刷新历史" : "触发历史" }}
                    </button>
                    <button
                      type="button"
                      class="btn btn--danger btn--sm"
                      :disabled="!!editingId || rowBusy(`delete:${item.trigger_id}`)"
                      @click="removeTriggerConfirmed(item)"
                    >
                      删除
                    </button>
                  </div>
                  <div v-if="historyOpenId === item.trigger_id" class="command-card__history">
                    <strong>触发历史</strong>
                    <p v-if="!historyById[item.trigger_id]?.length">暂无记录</p>
                    <div v-for="record in historyById[item.trigger_id] || []" :key="record.fire_id" class="command-card__history-row">
                      <span>{{ record.status === "queued" ? "已排队" : record.status === "skipped" ? "已跳过" : record.status === "error" ? "失败" : record.status }}</span>
                      <span>{{ formatUnixTime(record.fired_at) }}</span>
                      <span>{{ record.message || record.reason || "—" }}</span>
                    </div>
                  </div>
                </template>
              </div>
            </li>
          </ul>
          <div v-else class="command-panel__empty-card">
            <strong>暂无定时任务</strong>
            <span>创建后，任务会按照设定的频率向目标智能体投递消息。</span>
            <button type="button" class="btn btn--secondary btn--sm" @click="startCreate">新建定时任务</button>
          </div>
        </section>
      </template>
    </div>
  </section>
</template>
