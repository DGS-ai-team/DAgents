<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import * as api from "../api/node.js";

const props = defineProps({ agentId: { type: String, required: true } });
const router = useRouter();
const loading = ref(true); const saving = ref(false); const error = ref(""); const notice = ref(""); const needsReconcile = ref(false);
const loaded = ref(false); const loadedAgentId = ref("");
const dreamingLoading = ref(false); const dreamingError = ref(""); const experienceError = ref(""); const dreamingStatus = ref(null);
const triggerLoading = ref(false); const triggerError = ref(""); const triggerRecovery = ref(null);
const config = reactive({ revision: 0, responsibility: "", wake_interval_seconds: 0, max_tool_rounds: 32, dreaming_enabled: false, dreaming_time: "03:00", timezone: "Asia/Shanghai", experience: null });
const draft = reactive({ responsibility: "", wake_interval_seconds: 0, max_tool_rounds: 32, dreaming_enabled: false, dreaming_time: "03:00", timezone: "Asia/Shanghai" });
let epoch = 0; let saveEpoch = 0; let disposed = false;
const wakeOptions = [{ value: 0, label: "关闭自主激活" }, { value: 1800, label: "每 30 分钟" }, { value: 3600, label: "每 1 小时" }, { value: -1, label: "自定义间隔" }];
const customWake = ref(false);
const wakeValue = computed({
  get: () => (customWake.value ? -1 : Number(draft.wake_interval_seconds)),
  set: (value) => {
    customWake.value = Number(value) === -1;
    if (!customWake.value) draft.wake_interval_seconds = Number(value);
    else if (Number(draft.wake_interval_seconds) <= 0) draft.wake_interval_seconds = 1800;
  },
});
const defaults = () => ({ responsibility: "", wake_interval_seconds: 0, max_tool_rounds: 32, dreaming_enabled: false, dreaming_time: "03:00", timezone: "Asia/Shanghai" });
function apply(data) { const value = data || {}; Object.assign(config, value); Object.assign(draft, { responsibility: String(value.responsibility || ""), wake_interval_seconds: Number(value.wake_interval_seconds || 0), max_tool_rounds: Number(value.max_tool_rounds || 32), dreaming_enabled: !!value.dreaming_enabled, dreaming_time: String(value.dreaming_time || "03:00"), timezone: String(value.timezone || "Asia/Shanghai") }); customWake.value = ![0, 1800, 3600].includes(draft.wake_interval_seconds); }
function updateConfigSnapshot(data) { if (data && typeof data === "object") Object.assign(config, data); }
const dreamingStateLabels = { disabled: "已关闭", waiting: "等待执行", running: "整理中", succeeded: "最近成功", failed: "上次失败", recovery_pending: "等待恢复" };
function dreamingStateLabel(state) { return dreamingStateLabels[String(state || "").trim()] || "未知状态"; }
function formatDreamingDate(value) { if (!value) return "暂无"; const date = new Date(value); return Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1 ? "暂无" : date.toLocaleString(); }
function openTriggerSettings() { void router?.push?.("/settings/triggers"); }
function applyAggregateState(data) { dreamingStatus.value = data?.dreaming_status || null; dreamingError.value = data?.dreaming_available === false ? "Dreaming 状态服务不可用，已禁用编辑" : ""; experienceError.value = data?.experience_available === false ? "经验摘要暂不可用" : ""; triggerRecovery.value = data?.trigger_recovery || null; triggerError.value = data?.trigger_available === false ? "默认唤醒状态服务不可用" : (Number(data?.wake_interval_seconds) > 0 && !data?.trigger && !triggerRecovery.value ? "默认唤醒尚未同步" : ""); needsReconcile.value = !triggerRecovery.value && Boolean(data?.needs_reconcile); }
async function loadDreamingStatus(id, token = epoch) { dreamingLoading.value = true; triggerLoading.value = true; try { const data = await api.getAutoSettings(id); if (disposed || token !== epoch || id !== props.agentId) return; applyAggregateState(data); updateConfigSnapshot({ experience: data?.experience || null }); } catch (e) { if (!disposed && token === epoch && id === props.agentId) { dreamingStatus.value = null; dreamingError.value = e?.message || "无法读取 Auto 状态，已禁用编辑"; triggerError.value = e?.message || "无法读取默认唤醒状态"; needsReconcile.value = true; } } finally { if (!disposed && token === epoch) { dreamingLoading.value = false; triggerLoading.value = false; } } }
async function load() { const token = ++epoch; const id = props.agentId; loaded.value = false; loadedAgentId.value = ""; Object.assign(draft, defaults()); config.revision = 0; config.experience = null; dreamingStatus.value = null; triggerRecovery.value = null; triggerError.value = ""; dreamingError.value = ""; experienceError.value = ""; dreamingLoading.value = true; triggerLoading.value = true; loading.value = true; error.value = ""; try { const data = await api.getAutoSettings(id); if (disposed || token !== epoch || id !== props.agentId) return; apply(data); applyAggregateState(data); loaded.value = true; loadedAgentId.value = id; } catch (e) { if (!disposed && token === epoch && id === props.agentId) error.value = e?.message || "加载 Auto 设置失败"; } finally { if (!disposed && token === epoch) { dreamingLoading.value = false; triggerLoading.value = false; loading.value = false; } } }
async function save() { if (saving.value || !props.agentId || !loaded.value || loadedAgentId.value !== props.agentId) return; const id = props.agentId; const token = ++saveEpoch; if (customWake.value && Number(draft.wake_interval_seconds) <= 0) { error.value = "自定义间隔必须大于 0 秒"; return; } saving.value = true; error.value = ""; notice.value = ""; try { const payload = { ...draft, wake_interval_seconds: customWake.value ? Number(draft.wake_interval_seconds) : Number(wakeValue.value), expected_revision: config.revision }; const data = await api.putAutoConfig(id, payload); if (disposed || token !== saveEpoch || id !== props.agentId) return; updateConfigSnapshot(data); applyAggregateState(data); notice.value = "Auto 设置已保存"; } catch (e) { if (!disposed && token === saveEpoch && id === props.agentId) { const details = e?.data?.error?.details; if (e?.status === 503 && details?.saved_profile) { updateConfigSnapshot({ ...details.saved_profile, experience: config.experience }); needsReconcile.value = true; notice.value = "配置已保存、唤醒未同步"; } else error.value = e?.message || "保存 Auto 设置失败"; } } finally { if (token === saveEpoch) saving.value = false; } }
async function reconcile() { if (saving.value || !loaded.value || loadedAgentId.value !== props.agentId) return; const id = props.agentId; const token = ++saveEpoch; saving.value = true; error.value = ""; try { const data = await api.reconcileAutoConfig(id); if (!disposed && token === saveEpoch && id === props.agentId) { updateConfigSnapshot(data); applyAggregateState(data); notice.value = "唤醒已同步"; } } catch (e) { if (!disposed && token === saveEpoch && id === props.agentId) error.value = e?.message || "唤醒同步失败"; } finally { if (token === saveEpoch) saving.value = false; } }
async function refreshDreaming() { if (dreamingLoading.value || !loaded.value || loadedAgentId.value !== props.agentId) return; await loadDreamingStatus(props.agentId, epoch); }
watch(() => props.agentId, () => { epoch += 1; saveEpoch += 1; saving.value = false; dreamingLoading.value = false; needsReconcile.value = false; notice.value = ""; void load(); });
onMounted(load); onUnmounted(() => { disposed = true; epoch += 1; saveEpoch += 1; saving.value = false; });
</script>
<template>
  <section class="simplified-auto-panel" aria-label="Auto 设置">
    <div v-if="loading" role="status">加载中…</div>
    <template v-else>
      <p v-if="error" class="agent-detail__error" role="alert">{{ error }}</p>
      <div class="simplified-auto-panel__row"><div><strong>职责</strong><small>定义这个 Auto Agent 的长期工作责任</small></div><textarea v-model="draft.responsibility" rows="2" aria-label="职责" /></div>
      <div class="simplified-auto-panel__row"><div><strong>自主激活频率</strong><small>设置自动工作的唤醒间隔</small></div><div class="simplified-auto-panel__control"><select v-model.number="wakeValue" aria-label="自主激活频率"><option v-for="option in wakeOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select><input v-if="customWake" v-model.number="draft.wake_interval_seconds" type="number" min="1" aria-label="自定义间隔秒数" /><span v-if="customWake">秒</span></div></div>
      <div class="simplified-auto-panel__row"><div><strong>最大工具轮次</strong><small>自主激活与 dreaming 都受此限制，普通聊天保持原规则</small></div><input v-model.number="draft.max_tool_rounds" type="number" min="1" aria-label="最大工具轮次" /></div>
      <div class="simplified-auto-panel__row"><div><strong>每日 dreaming</strong><small>定时整理近期经验，帮助保持长期工作脉络</small></div><div class="simplified-auto-panel__control"><input v-model="draft.dreaming_enabled" class="settings-switch-input" type="checkbox" aria-label="启用每日 dreaming" :disabled="dreamingLoading || !!dreamingError || !dreamingStatus" /></div></div>
      <div v-if="triggerRecovery || triggerError || needsReconcile" class="simplified-auto-panel__row" role="status"><div><strong>默认唤醒</strong><small>{{ triggerError || (triggerRecovery ? "上次投递需要核对" : (needsReconcile ? "唤醒配置尚未同步" : "")) }}</small></div><div class="simplified-auto-panel__control"><button v-if="triggerRecovery" type="button" class="btn btn--ghost" aria-label="打开定时任务设置" @click="openTriggerSettings">管理定时任务</button><button type="button" class="btn btn--ghost" :disabled="triggerLoading" @click="refreshDreaming">{{ triggerLoading ? "读取中…" : "刷新状态" }}</button></div></div>
      <div v-if="draft.dreaming_enabled" class="simplified-auto-panel__row"><div><strong>整理时间</strong></div><input v-model="draft.dreaming_time" type="time" aria-label="dreaming 时间" :disabled="dreamingLoading || !!dreamingError || !dreamingStatus" /></div>
      <div v-if="draft.dreaming_enabled" class="simplified-auto-panel__row"><div><strong>时区</strong></div><input v-model="draft.timezone" aria-label="时区" :disabled="dreamingLoading || !!dreamingError || !dreamingStatus" /></div>
      <div class="simplified-auto-panel__row"><div><strong>dreaming 状态</strong><small v-if="dreamingError">{{ dreamingError }}</small><small v-else>经验整理状态</small></div><div class="simplified-auto-panel__control"><span>{{ dreamingLoading ? "读取中…" : dreamingStateLabel(dreamingStatus?.state) }}</span><button type="button" class="btn btn--ghost" :disabled="dreamingLoading || !loaded" @click="refreshDreaming">刷新状态</button></div></div>
      <div v-if="dreamingStatus" class="simplified-auto-panel__row"><div><strong>执行时间</strong></div><span>下次 {{ formatDreamingDate(dreamingStatus.next_at) }} · 上次成功 {{ formatDreamingDate(dreamingStatus.last_success) }}<template v-if="dreamingStatus.last_error"> · {{ dreamingStatus.last_error }}</template></span></div>
      <div class="simplified-auto-panel__row"><div><strong>经验</strong><small>仅在 dreaming 成功后更新</small></div><p class="simplified-auto-panel__experience">{{ experienceError || config.experience?.content || "暂无经验摘要" }}<span v-if="!experienceError && config.experience?.updated_at"> · {{ new Date(config.experience.updated_at).toLocaleString() }}</span></p></div>
      <p v-if="notice" class="simplified-auto-panel__notice" role="status">{{ notice }}</p>
      <div class="simplified-auto-panel__actions"><button class="btn btn--primary" :disabled="saving || !loaded" @click="save">{{ saving ? "保存中…" : "保存 Auto 设置" }}</button><button v-if="needsReconcile" class="btn btn--ghost" :disabled="saving" @click="reconcile">同步唤醒</button></div>
    </template>
  </section>
</template>
<style scoped>
.simplified-auto-panel { display: grid; gap: 0; }
.simplified-auto-panel__row { display: grid; grid-template-columns: minmax(200px, .8fr) minmax(0, 1.2fr); gap: 20px; align-items: center; padding: 10px 0; border-bottom: 1px solid var(--border-subtle); }
.simplified-auto-panel__row > div:first-child { display: grid; gap: 3px; }.simplified-auto-panel small { color: var(--text-secondary); font-size: 12px; line-height: 1.4; }.simplified-auto-panel textarea,.simplified-auto-panel input:not([type="checkbox"]),.simplified-auto-panel select { width: 100%; max-width: 100%; box-sizing: border-box; border: 1px solid var(--border-subtle); border-radius: 6px; padding: 8px 10px; background: var(--color-surface-alt); color: var(--color-text); font: inherit; }.simplified-auto-panel textarea { min-height: 54px; resize: vertical; }.simplified-auto-panel input[type="checkbox"]:not(.settings-switch-input) { width: 17px; height: 17px; margin: 0; accent-color: var(--color-accent); }.simplified-auto-panel__control { display: flex; align-items: center; justify-content: flex-end; gap: 8px; min-width: 0; }.simplified-auto-panel__control select { width: 100%; max-width: 320px; }.simplified-auto-panel__control input:not([type="checkbox"]) { width: 100%; max-width: 180px; }.simplified-auto-panel__experience { max-height: 200px; margin: 0; padding: 10px 12px; overflow: auto; border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--color-surface-alt); color: var(--text-secondary); white-space: pre-wrap; overflow-wrap: anywhere; }
.simplified-auto-panel__actions { display: flex; justify-content: flex-end; gap: 8px; padding-top: 14px; }
.simplified-auto-panel__notice { margin: 10px 0 0; color: var(--color-success, #3d9a5f); font-size: 12px; }
@media (max-width: 640px) { .simplified-auto-panel__row { grid-template-columns: 1fr; gap: 8px; align-items: start; }.simplified-auto-panel__control { justify-content: flex-start; }.simplified-auto-panel__actions { justify-content: flex-start; } }
</style>
