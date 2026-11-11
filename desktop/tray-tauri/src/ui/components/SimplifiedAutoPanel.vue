<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import * as api from "../api/node.js";
import SettingsSaveBar from "./SettingsSaveBar.vue";
import UiSelect from "./UiSelect.vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({ agentId: { type: String, required: true } });
const router = useRouter();
const loading = ref(true); const saving = ref(false); const error = ref(""); const notice = ref(""); const needsReconcile = ref(false);
const loaded = ref(false); const loadedAgentId = ref("");
const dreamingLoading = ref(false); const dreamingError = ref(""); const experienceError = ref(""); const dreamingStatus = ref(null);
const triggerLoading = ref(false); const triggerError = ref(""); const triggerRecovery = ref(null);
const config = reactive({ revision: 0, experience: null });
const draft = reactive({ responsibility: "", wake_interval_seconds: 0, dreaming_enabled: false, dreaming_time: "03:00", timezone: "Asia/Shanghai" });
const savedDraft = ref("");
const draftValue = () => JSON.stringify({ ...draft, wake_interval_seconds: customWake.value ? Number(draft.wake_interval_seconds) : Number(wakeValue.value) });
const dirty = computed(() => !!savedDraft.value && draftValue() !== savedDraft.value);
let epoch = 0; let saveEpoch = 0; let disposed = false;
const wakeOptions = [{ value: 0, label: "关闭自主运行" }, { value: 1800, label: "每 30 分钟" }, { value: 3600, label: "每 1 小时" }, { value: -1, label: "自定义间隔" }];
const customWake = ref(false);
const wakeValue = computed({
  get: () => (customWake.value ? -1 : Number(draft.wake_interval_seconds)),
  set: (value) => {
    customWake.value = Number(value) === -1;
    if (!customWake.value) draft.wake_interval_seconds = Number(value);
    else if (Number(draft.wake_interval_seconds) <= 0) draft.wake_interval_seconds = 1800;
  },
});
const defaults = () => ({ responsibility: "", wake_interval_seconds: 0, dreaming_enabled: false, dreaming_time: "03:00", timezone: "Asia/Shanghai" });
function apply(data) { const value = data || {}; Object.assign(config, { revision: Number(value.revision || 0), experience: value.experience || null }); Object.assign(draft, { responsibility: String(value.responsibility || ""), wake_interval_seconds: Number(value.wake_interval_seconds || 0), dreaming_enabled: !!value.dreaming_enabled, dreaming_time: String(value.dreaming_time || "03:00"), timezone: String(value.timezone || "Asia/Shanghai") }); customWake.value = ![0, 1800, 3600].includes(draft.wake_interval_seconds); savedDraft.value = draftValue(); }
function updateConfigSnapshot(data) { if (!data || typeof data !== "object") return; if (Object.hasOwn(data, "revision")) config.revision = Number(data.revision || 0); if (Object.hasOwn(data, "experience")) config.experience = data.experience || null; }
const dreamingStateLabels = { disabled: "已关闭", waiting: "等待执行", running: "整理中", succeeded: "最近成功", failed: "上次失败", recovery_pending: "等待恢复" };
function dreamingStateLabel(state) { return dreamingStateLabels[String(state || "").trim()] || "未知状态"; }
function formatDreamingDate(value) { if (!value) return "暂无"; const date = new Date(value); return Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1 ? "暂无" : date.toLocaleString(); }
function openTriggerSettings() { void router?.push?.("/settings/triggers"); }
function applyAggregateState(data) { dreamingStatus.value = data?.dreaming_status || null; dreamingError.value = data?.dreaming_available === false ? "每日经验整理状态服务不可用，已禁用编辑" : ""; experienceError.value = data?.experience_available === false ? "经验摘要暂不可用" : ""; triggerRecovery.value = data?.trigger_recovery || null; triggerError.value = data?.trigger_available === false ? "自主运行状态服务不可用" : (Number(data?.wake_interval_seconds) > 0 && !data?.trigger && !triggerRecovery.value ? "自主运行尚未同步" : ""); needsReconcile.value = !triggerRecovery.value && Boolean(data?.needs_reconcile); }
async function loadDreamingStatus(id, token = epoch) { dreamingLoading.value = true; triggerLoading.value = true; try { const data = await api.getAutoSettings(id); if (disposed || token !== epoch || id !== props.agentId) return; applyAggregateState(data); updateConfigSnapshot({ experience: data?.experience || null }); } catch (e) { if (!disposed && token === epoch && id === props.agentId) { dreamingStatus.value = null; dreamingError.value = e?.message || "无法读取每日经验整理状态，已禁用编辑"; triggerError.value = e?.message || "无法读取自主运行状态"; needsReconcile.value = true; } } finally { if (!disposed && token === epoch) { dreamingLoading.value = false; triggerLoading.value = false; } } }
async function load() { const token = ++epoch; const id = props.agentId; loaded.value = false; loadedAgentId.value = ""; Object.assign(draft, defaults()); config.revision = 0; config.experience = null; dreamingStatus.value = null; triggerRecovery.value = null; triggerError.value = ""; dreamingError.value = ""; experienceError.value = ""; dreamingLoading.value = true; triggerLoading.value = true; loading.value = true; error.value = ""; try { const data = await api.getAutoSettings(id); if (disposed || token !== epoch || id !== props.agentId) return; apply(data); applyAggregateState(data); loaded.value = true; loadedAgentId.value = id; } catch (e) { if (!disposed && token === epoch && id === props.agentId) error.value = e?.message || "加载 Auto 设置失败"; } finally { if (!disposed && token === epoch) { dreamingLoading.value = false; triggerLoading.value = false; loading.value = false; } } }
async function save() { if (saving.value || !props.agentId || !loaded.value || loadedAgentId.value !== props.agentId) return; const id = props.agentId; const token = ++saveEpoch; if (customWake.value && Number(draft.wake_interval_seconds) <= 0) { error.value = "自定义间隔必须大于 0 秒"; return; } saving.value = true; error.value = ""; notice.value = ""; try { const payload = { ...draft, wake_interval_seconds: customWake.value ? Number(draft.wake_interval_seconds) : Number(wakeValue.value), expected_revision: config.revision }; const savedValue = JSON.stringify({ ...payload, expected_revision: undefined }); const data = await api.putAutoConfig(id, payload); if (disposed || token !== saveEpoch || id !== props.agentId) return; updateConfigSnapshot(data); applyAggregateState(data); savedDraft.value = savedValue; notice.value = "Auto 设置已保存"; } catch (e) { if (!disposed && token === saveEpoch && id === props.agentId) { const details = e?.data?.error?.details; if (e?.status === 503 && details?.saved_profile) { updateConfigSnapshot({ ...details.saved_profile, experience: config.experience }); needsReconcile.value = true; notice.value = "配置已保存、唤醒未同步"; } else error.value = e?.message || "保存失败"; } } finally { if (token === saveEpoch) saving.value = false; } }
async function reconcile() { if (saving.value || !loaded.value || loadedAgentId.value !== props.agentId) return; const id = props.agentId; const token = ++saveEpoch; saving.value = true; error.value = ""; try { const data = await api.reconcileAutoConfig(id); if (!disposed && token === saveEpoch && id === props.agentId) { updateConfigSnapshot(data); applyAggregateState(data); notice.value = "唤醒已同步"; } } catch (e) { if (!disposed && token === saveEpoch && id === props.agentId) error.value = e?.message || "唤醒同步失败"; } finally { if (token === saveEpoch) saving.value = false; } }
async function refreshDreaming() { if (dreamingLoading.value || !loaded.value || loadedAgentId.value !== props.agentId) return; await loadDreamingStatus(props.agentId, epoch); }
watch(() => props.agentId, () => { epoch += 1; saveEpoch += 1; saving.value = false; dreamingLoading.value = false; needsReconcile.value = false; notice.value = ""; void load(); });
watch(draft, () => { if (dirty.value && notice.value === "Auto 设置已保存") notice.value = ""; }, { deep: true });
onMounted(load); onUnmounted(() => { disposed = true; epoch += 1; saveEpoch += 1; saving.value = false; });
</script>
<template>
  <section class="simplified-auto-panel" aria-label="Auto 设置">
    <div v-if="loading" role="status">加载中…</div>
    <template v-else>
      <fieldset class="simplified-auto-panel__fields" :disabled="saving">
      <label class="settings-row settings-row--field">
        <span class="settings-row__copy"><span class="settings-row__title">职责</span><span class="settings-row__description">定义这个 Auto Agent 的长期工作责任</span></span>
        <textarea v-model="draft.responsibility" class="settings-field__input settings-field__input--multiline" rows="2" aria-label="职责" />
      </label>
      <div class="settings-row settings-row--field">
        <span class="settings-row__copy"><span class="settings-row__title">自主运行频率</span><span class="settings-row__description">设置 Auto Agent 自动工作的间隔</span></span>
        <div class="simplified-auto-panel__control">
          <UiSelect
            v-model="wakeValue"
            :options="wakeOptions"
            aria-label="自主运行频率"
            :disabled="loading || saving"
          />
          <input
            v-if="customWake"
            v-model.number="draft.wake_interval_seconds"
            class="settings-field__input"
            type="number"
            min="1"
            aria-label="自定义间隔秒数"
          />
          <span v-if="customWake" class="simplified-auto-panel__unit">秒</span>
        </div>
      </div>
      <label class="settings-row"><span class="settings-row__copy"><span class="settings-row__title">每日经验整理</span><span class="settings-row__description">定时整理近期经验，帮助保持长期工作脉络</span></span><input v-model="draft.dreaming_enabled" class="settings-switch-input" type="checkbox" aria-label="启用每日经验整理" :disabled="dreamingLoading || !!dreamingError || !dreamingStatus" /></label>
      <div
        v-if="triggerRecovery || triggerError || needsReconcile"
        class="settings-row"
        role="status"
      >
        <span class="settings-row__copy"><span class="settings-row__title">自主运行状态</span><span class="settings-row__description">{{ triggerError || (triggerRecovery ? "上次投递需要核对" : (needsReconcile ? "自主运行配置尚未同步" : "")) }}</span></span>
        <div class="simplified-auto-panel__control">
          <button
            v-if="triggerRecovery"
            type="button"
            class="btn btn--ghost btn--sm desktop-icon-button"
            aria-label="打开自主运行设置"
            title="打开自主运行设置"
            @click="openTriggerSettings"
          >
            <UiIcon name="clock" :size="16" />
          </button>
          <button
            type="button"
            class="btn btn--ghost btn--sm desktop-icon-button"
            :disabled="triggerLoading"
            :aria-label="triggerLoading ? '正在刷新状态' : '刷新状态'"
            :title="triggerLoading ? '正在刷新状态' : '刷新状态'"
            @click="refreshDreaming"
          >
            <UiIcon name="refresh-cw" :size="16" />
          </button>
        </div>
      </div>
      <label v-if="draft.dreaming_enabled" class="settings-row settings-row--field"><span class="settings-row__copy"><span class="settings-row__title">整理时间</span></span><input v-model="draft.dreaming_time" class="settings-field__input" type="time" aria-label="经验整理时间" :disabled="dreamingLoading || !!dreamingError || !dreamingStatus" /></label>
      <label v-if="draft.dreaming_enabled" class="settings-row settings-row--field"><span class="settings-row__copy"><span class="settings-row__title">时区</span></span><input v-model="draft.timezone" class="settings-field__input" aria-label="时区" :disabled="dreamingLoading || !!dreamingError || !dreamingStatus" /></label>
      <div class="settings-row">
        <span class="settings-row__copy"><span class="settings-row__title">每日经验整理状态</span><span class="settings-row__description">{{ dreamingError || "每日经验整理运行情况" }}</span></span>
        <div class="simplified-auto-panel__control">
          <span>{{ dreamingLoading ? "读取中…" : (!draft.dreaming_enabled ? "已关闭" : dreamingStateLabel(dreamingStatus?.state)) }}</span>
          <button
            type="button"
            class="btn btn--ghost btn--sm desktop-icon-button"
            :disabled="dreamingLoading || !loaded"
            aria-label="刷新状态"
            title="刷新状态"
            @click="refreshDreaming"
          >
            <UiIcon name="refresh-cw" :size="16" />
          </button>
        </div>
      </div>
      <div v-if="draft.dreaming_enabled && dreamingStatus" class="settings-row"><span class="settings-row__copy"><span class="settings-row__title">执行时间</span></span><span>下次 {{ formatDreamingDate(dreamingStatus.next_at) }} · 上次成功 {{ formatDreamingDate(dreamingStatus.last_success) }}<template v-if="dreamingStatus.last_error"> · {{ dreamingStatus.last_error }}</template></span></div>
      <div class="settings-row settings-row--field"><span class="settings-row__copy"><span class="settings-row__title">经验摘要</span><span class="settings-row__description">每日经验整理成功后更新</span></span><p class="simplified-auto-panel__experience">{{ experienceError || config.experience?.content || "暂无经验摘要" }}<span v-if="!experienceError && config.experience?.updated_at"> · {{ new Date(config.experience.updated_at).toLocaleString() }}</span></p></div>
      </fieldset>
      <SettingsSaveBar :status="dirty ? (needsReconcile ? '有未保存的更改 · 自主运行配置已保存、尚未同步' : '有未保存的更改') : (notice || '当前智能体设置。')" :error="error" :saving="saving" :disabled="!loaded || !dirty" @save="save">
        <button v-if="needsReconcile" type="button" class="btn btn--ghost btn--sm" :disabled="saving" @click="reconcile">同步自主运行</button>
      </SettingsSaveBar>
    </template>
  </section>
</template>
<style scoped>
.simplified-auto-panel {
  display: grid;
  gap: 0;
}

.simplified-auto-panel__fields {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}

.simplified-auto-panel__control {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
}

.simplified-auto-panel__control :deep(.ui-select) {
  width: 100%;
  max-width: none;
}

.simplified-auto-panel__control input:not([type="checkbox"]) {
  width: 100%;
  max-width: 180px;
}

.simplified-auto-panel__unit {
  flex: 0 0 auto;
  color: var(--color-text-subtle);
  font: 400 11px/1.4 var(--font-ui);
}

.simplified-auto-panel__experience {
  max-height: 200px;
  margin: 0;
  padding: 10px 12px;
  overflow: auto;
  border: 1px solid var(--border-subtle);
  border-radius: 8px;
  background: var(--color-surface-alt);
  color: var(--color-text-subtle);
  font: 400 12px/1.5 var(--font-ui);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

</style>
