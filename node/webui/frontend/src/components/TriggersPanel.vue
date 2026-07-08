<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import * as api from "../api/node.js";
import TriggerEditor from "./settings/TriggerEditor.vue";
import { formatTriggerCondition, formatUnixTime, shortId, truncateText } from "../utils/panelFormat.js";
import {
  buildCreatePayload,
  buildUpdatePayload,
  defaultTriggerForm,
  triggerToForm,
  validateTriggerForm,
} from "../utils/triggerForm.js";

const emit = defineEmits(["close"]);

const loading = ref(false);
const busyKey = ref("");
const error = ref("");
const statusMessage = ref("");
const showRaw = ref(false);
const data = ref(null);
const editingId = ref(null);
const form = reactive(defaultTriggerForm());
const formError = ref("");

const triggers = computed(() => {
  const rows = data.value?.triggers;
  return Array.isArray(rows) ? rows : [];
});

function rowBusy(key) {
  return busyKey.value === key;
}

function resetForm() {
  Object.assign(form, defaultTriggerForm());
  formError.value = "";
}

async function load() {
  loading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    data.value = await api.listTriggers();
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
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
}

function startEdit(item) {
  editingId.value = item.trigger_id;
  Object.assign(form, triggerToForm(item));
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
  const isCreate = editingId.value === "new";
  busyKey.value = isCreate ? "save:new" : `save:${editingId.value}`;
  error.value = "";
  statusMessage.value = "";
  try {
    if (isCreate) {
      const created = await api.createTrigger(buildCreatePayload(form));
      if (form.enabled === false && created?.trigger_id) {
        const updated = await api.updateTrigger(created.trigger_id, { enabled: false });
        replaceTrigger(updated);
      } else {
        replaceTrigger(created);
      }
      statusMessage.value = `已创建「${created.name || form.name}」`;
    } else {
      const updated = await api.updateTrigger(editingId.value, buildUpdatePayload(form));
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

function sessionHint(item) {
  let hint = String(item.session_target_mode || "—");
  if (item.target_session_id) hint += ` · ${shortId(item.target_session_id, 20)}`;
  return hint;
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel triggers-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">定时任务</div>
        <div class="command-panel__subtitle">自动触发与调度</div>
      </div>
      <div class="command-panel__header-actions">
        <button type="button" class="btn btn--primary btn--sm" :disabled="loading || editingId === 'new'" @click="startCreate">
          新建
        </button>
        <button type="button" class="btn btn--ghost btn--sm" @click="showRaw = !showRaw">
          {{ showRaw ? "友好视图" : "JSON" }}
        </button>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
        <button type="button" class="btn btn--ghost btn--sm" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <p v-if="statusMessage" class="command-panel__status">{{ statusMessage }}</p>
      <pre v-else-if="showRaw && data" class="command-panel__raw">{{ JSON.stringify(data, null, 2) }}</pre>
      <template v-else-if="data">
        <section v-if="editingId === 'new'" class="command-section trigger-editor-section">
          <h3 class="command-section__title">新建定时任务</h3>
          <TriggerEditor
            :form="form"
            :form-error="formError"
            :busy="rowBusy('save:new')"
            submit-label="创建"
            @submit="saveForm"
            @cancel="cancelEdit"
          />
        </section>

        <section class="command-section">
          <h3 class="command-section__title">已配置 ({{ triggers.length }})</h3>
          <ul v-if="triggers.length" class="command-card-list">
            <li v-for="item in triggers" :key="item.trigger_id" class="command-card">
              <div class="command-card__main">
                <template v-if="editingId === item.trigger_id">
                  <h4 class="trigger-form__heading">编辑 · {{ item.name || "(未命名)" }}</h4>
                  <TriggerEditor
                    :form="form"
                    :form-error="formError"
                    :busy="rowBusy(`save:${item.trigger_id}`)"
                    enabled-label="启用"
                    @submit="saveForm"
                    @cancel="cancelEdit"
                  />
                </template>
                <template v-else>
                  <div class="command-card__title">
                    {{ item.name || "(未命名)" }}
                    <span
                      class="command-card__badge"
                      :class="item.enabled ? 'command-card__badge--active' : 'command-card__badge--muted'"
                    >
                      {{ item.enabled ? "已启用" : "已禁用" }}
                    </span>
                  </div>
                  <div class="command-card__meta command-card__meta--mono">{{ shortId(item.trigger_id, 36) }}</div>
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
                      <dt>会话</dt>
                      <dd>{{ sessionHint(item) }}</dd>
                    </div>
                  </dl>
                  <div v-if="item.task_template" class="command-card__preview">
                    任务: {{ truncateText(item.task_template, 120) }}
                  </div>
                  <div class="command-card__actions">
                    <label class="settings-toggle command-card__toggle">
                      <input
                        type="checkbox"
                        :checked="!!item.enabled"
                        :disabled="rowBusy(`toggle:${item.trigger_id}`)"
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
                    <button
                      type="button"
                      class="btn btn--danger btn--sm"
                      :disabled="!!editingId || rowBusy(`delete:${item.trigger_id}`)"
                      @click="removeTriggerConfirmed(item)"
                    >
                      删除
                    </button>
                  </div>
                </template>
              </div>
            </li>
          </ul>
          <p v-else class="command-panel__empty">暂无定时任务，点击「新建」添加。</p>
        </section>
      </template>
    </div>
  </section>
</template>
