<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";
import { useRouter } from "vue-router";
import { formatGoalDate, goalNextWakeLabel, goalReasonLabel } from "../utils/goalPresentation.js";

const props = defineProps({ agentId: { type: String, required: true } });
const router = useRouter();
const loading = ref(true); const saving = ref(false); const actionSaving = ref(false); const error = ref(""); const notice = ref("");
const scheduleEdited = ref(false);
const scheduleDays = new Set(["mon", "tue", "wed", "thu", "fri", "sat", "sun"]);
const legacyScheduleVisible = computed(() => !!String(form.work_schedule || "").trim() && !parseStructuredSchedule(form.work_schedule));
const state = reactive({ goal_id: "", status: "disabled", limits: {}, profile: {}, progress: null, runs: [] });
const form = reactive({ title: "自主任务", role_objective: "", role_boundaries: "", plan_mode: "recurring", timezone: "Asia/Shanghai", work_schedule: "", cycle_duration_seconds: 86400, schedule_kind: "daily", schedule_time: "09:00", schedule_days: [], cycle_duration_value: 1, cycle_duration_unit: "day", objective: "", acceptance: "", enabled: false, max_runs: 12, token_budget: 100000, turn_token_budget: 10000, min_wake_interval_seconds: 300, expires_at: "" });
let cycleRequestKey = "";
let cycleRequestPayload = "";
let loadSequence = 0;
const cycleDraftKeys = ["title", "objective", "acceptance", "enabled", "max_runs", "token_budget", "turn_token_budget", "min_wake_interval_seconds", "expires_at", "schedule_kind", "schedule_time", "schedule_days", "cycle_duration_value", "cycle_duration_unit"];
const profileDraftKeys = ["role_objective", "role_boundaries", "plan_mode", "timezone", "work_schedule", "cycle_duration_seconds"];
const terminal = () => ["completed", "stopped", "failed"].includes(state.status);
const statusLabel = (s, reason = "") => s === "waiting" && reason === "approval_required" ? "等待审批" : ({ disabled: "未启用", active: "已启用", waiting: "等待下次唤醒", paused: "已暂停", stopped: "已停止", completed: "已完成", failed: "失败" }[s] || s);
function localDateInput(value) { if (!value) return ""; const d = new Date(value); if (Number.isNaN(d.getTime())) return ""; const pad = (n) => String(n).padStart(2, "0"); return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`; }
function validScheduleTime(value) { const match = String(value || "").match(/^(\d{2}):(\d{2})$/); return !!match && Number(match[1]) < 24 && Number(match[2]) < 60; }
function parseStructuredSchedule(value) { const raw = String(value || "").trim(); const daily = raw.match(/^daily\s+(\d{2}:\d{2})$/i); if (daily && validScheduleTime(daily[1])) return { kind: "daily", time: daily[1], days: [] }; const weekly = raw.match(/^weekly\s+([a-z,]+)\s+(\d{2}:\d{2})$/i); if (!weekly || !validScheduleTime(weekly[2])) return null; const days = weekly[1].toLowerCase().split(","); if (!days.length || days.some((day) => !scheduleDays.has(day)) || new Set(days).size !== days.length) return null; return { kind: "weekly", time: weekly[2], days }; }
function parseSchedule(value) { const parsed = parseStructuredSchedule(value); if (!parsed) return false; form.schedule_kind = parsed.kind; form.schedule_time = parsed.time; form.schedule_days = parsed.days; scheduleEdited.value = false; return true; }
function structuredSchedule() { const raw = String(form.work_schedule || "").trim(); const time = form.schedule_time; if (!scheduleEdited.value && raw && parseStructuredSchedule(raw) === null) return raw; if (form.schedule_kind === "daily") return `daily ${time}`; const days = form.schedule_days.filter(Boolean); return days.length ? `weekly ${days.join(",")} ${time}` : ""; }
function cycleDurationSeconds() { const value = Number(form.cycle_duration_value); const unit = { day: 86400, hour: 3600, minute: 60 }[form.cycle_duration_unit] || 86400; const seconds = value * unit; return Number.isFinite(seconds) && value >= 1 && seconds <= 31 * 86400 ? Math.round(seconds) : 0; }
function validateCycleDuration() { if (form.plan_mode === "recurring" && cycleDurationSeconds() <= 0) { error.value = "每周期时长须为 1 分钟至 31 天"; return false; } return true; }
function validateRecurring() { if (form.plan_mode !== "recurring") return true; if (!validScheduleTime(form.schedule_time)) { error.value = "工作安排时间无效，请填写 00:00 至 23:59"; return false; } if (!validateCycleDuration()) return false; if (form.schedule_kind === "weekly" && !form.schedule_days.length) { error.value = "每周安排至少选择一天"; return false; } return true; }
function markScheduleEdited() { scheduleEdited.value = true; }
function apply(data) { Object.assign(state, data || {}); const l = data?.limits || {}; const p = data?.profile || {}; scheduleEdited.value = false; form.schedule_kind = "daily"; form.schedule_time = "09:00"; form.schedule_days = []; for (const k of Object.keys(form)) if (k in l) form[k] = k === "expires_at" ? localDateInput(l[k]) : l[k]; for (const k of ["role_objective", "role_boundaries", "plan_mode", "timezone", "work_schedule", "cycle_duration_seconds"]) if (k in p) form[k] = p[k]; if (p.work_schedule) parseSchedule(p.work_schedule); if (p.cycle_duration_seconds) { const seconds = Number(p.cycle_duration_seconds); form.cycle_duration_unit = seconds % 86400 === 0 ? "day" : seconds % 3600 === 0 ? "hour" : "minute"; form.cycle_duration_value = seconds / ({ day: 86400, hour: 3600, minute: 60 }[form.cycle_duration_unit]); } form.enabled = p.enabled ?? ["active", "waiting"].includes(data?.status); }
async function load() {
  const sequence = ++loadSequence;
  const agentId = props.agentId;
  loading.value = true;
  error.value = "";
  try {
    const data = await api.getAgentAutonomy(agentId);
    if (sequence !== loadSequence || props.agentId !== agentId) return false;
    apply(data);
    if (state.goal_id) {
      const r = await api.getGoalRuns(state.goal_id);
      if (sequence !== loadSequence || props.agentId !== agentId) return false;
      state.runs = r?.runs || [];
    }
    return true;
  } catch (e) {
    if (sequence === loadSequence && props.agentId === agentId) error.value = e.message || "加载自主任务失败";
    return false;
  } finally {
    if (sequence === loadSequence) loading.value = false;
  }
}
async function saveProfile() {
  saving.value = true;
  error.value = "";
  notice.value = "";
  if (!validateRecurring()) { saving.value = false; return; }
  try {
    const cycleDraft = Object.fromEntries(cycleDraftKeys.map((key) => [key, form[key]]));
    const scheduleWasEdited = scheduleEdited.value;
    const profile = { agent_id: props.agentId };
    for (const key of profileDraftKeys) profile[key] = key === "work_schedule" ? structuredSchedule() : key === "cycle_duration_seconds" ? cycleDurationSeconds() : form[key];
    const data = await api.putAgentAutonomy(props.agentId, { profile, expected_revision: state.profile?.revision });
    apply(data);
    Object.assign(form, cycleDraft);
    scheduleEdited.value = scheduleWasEdited;
    notice.value = "岗位设置已保存";
  } catch (e) {
    error.value = e.message || "保存岗位设置失败";
  } finally {
    saving.value = false;
  }
}

async function saveCycle() {
  saving.value = true;
  error.value = "";
  notice.value = "";
  if (!validateCycleDuration()) { saving.value = false; return; }
  try {
    const cycle = { title: form.title, objective: form.objective, acceptance: form.acceptance, max_runs: form.max_runs, token_budget: form.token_budget, turn_token_budget: form.turn_token_budget, min_wake_interval_seconds: form.min_wake_interval_seconds, cycle_duration_seconds: cycleDurationSeconds(), expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : undefined };
    const scheduleWasEdited = scheduleEdited.value;
    const profileDraft = Object.fromEntries([...profileDraftKeys, "schedule_kind", "schedule_time", "schedule_days", "cycle_duration_value", "cycle_duration_unit"].map((key) => [key, key === "work_schedule" ? structuredSchedule() : form[key]]));
    const data = await api.putAgentAutonomy(props.agentId, { ...cycle, expected_revision: state.profile?.revision, expected_goal_revision: state.current_cycle?.revision ?? state.limits?.revision });
    apply(data);
    Object.assign(form, profileDraft);
    scheduleEdited.value = scheduleWasEdited;
    notice.value = "周期设置已保存";
  } catch (e) {
    error.value = e.message || "保存周期设置失败";
  } finally {
    saving.value = false;
  }
}
async function action(name) { actionSaving.value = true; error.value = ""; notice.value = ""; try { const data = await api.agentAutonomyAction(props.agentId, { action: name, expected_profile_revision: state.profile?.revision ?? 0, expected_goal_revision: state.limits?.revision ?? undefined }); apply(data); notice.value = "操作已生效"; } catch (e) { error.value = e.message || "操作失败"; } finally { actionSaving.value = false; } }
async function createCycle() {
  actionSaving.value = true;
  error.value = "";
  notice.value = "";
  const payload = { expected_profile_revision: state.profile?.revision ?? 0, title: form.title, objective: form.objective, acceptance: form.acceptance, max_runs: form.max_runs, token_budget: form.token_budget, turn_token_budget: form.turn_token_budget, min_wake_interval_seconds: form.min_wake_interval_seconds, expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : undefined, enabled: false };
  const serialized = JSON.stringify(payload);
  if (!cycleRequestKey || cycleRequestPayload !== serialized) {
    cycleRequestKey = typeof globalThis.crypto?.randomUUID === "function" ? globalThis.crypto.randomUUID() : `ui-${Date.now()}`;
    cycleRequestPayload = serialized;
  }
  try {
    await api.createAgentAutonomyCycle(props.agentId, { ...payload, idempotency_key: cycleRequestKey });
    const loaded = await load();
    if (!loaded) {
      error.value = "周期已创建，但读取最新状态失败，请点击刷新状态重试";
      return;
    }
    cycleRequestKey = "";
    cycleRequestPayload = "";
    notice.value = "已创建新的自主任务周期";
  } catch (e) { error.value = e.message || "创建周期失败"; } finally { actionSaving.value = false; }
}
function openSession() { if (state.limits?.session_id) router.push({ name: "agents", params: { agentId: props.agentId }, query: { session_id: state.limits.session_id } }); }
onMounted(load);
watch(() => props.agentId, () => void load());
</script>
<template>
  <section class="agent-autonomy-panel">
    <div v-if="loading">加载中…</div>
    <template v-else>
      <p v-if="error" class="agent-detail__error" role="alert">{{ error }}</p>
      <p class="agent-autonomy-panel__intro">Auto Agent 会按计划分批推进目标；普通聊天保持在主聊天中进行。</p>
      <label class="settings-field agent-autonomy-panel__wide"><span><span class="settings-field__label">岗位职责</span><small class="settings-field__hint">说明这个 Auto Agent 长期负责的工作</small></span><textarea v-model="form.role_objective" class="settings-field__input" rows="2" /></label>
      <label class="settings-field agent-autonomy-panel__wide"><span><span class="settings-field__label">岗位边界</span><small class="settings-field__hint">约束自主任务的范围和停止条件</small></span><textarea v-model="form.role_boundaries" class="settings-field__input" rows="2" /></label>
      <label class="settings-field"><span><span class="settings-field__label">计划模式</span><small v-if="form.plan_mode === 'recurring'" class="settings-field__hint">持续运行使用当前工具权限；权限变化后需重新保存，不会自动扩大权限</small></span><select v-model="form.plan_mode" class="settings-field__input"><option value="recurring">持续运行</option><option value="one_shot">单次任务</option></select></label>
      <label class="settings-field"><span class="settings-field__label">时区</span><input v-model="form.timezone" class="settings-field__input" /></label>
      <div v-if="form.plan_mode === 'recurring'" class="settings-field agent-autonomy-panel__wide"><span><span class="settings-field__label">工作安排</span><small class="settings-field__hint">选择每天或每周的运行时间</small></span><div class="agent-autonomy-panel__schedule"><select v-model="form.schedule_kind" aria-label="工作安排频率" class="settings-field__input" @change="markScheduleEdited"><option value="daily">每天</option><option value="weekly">每周</option></select><input v-model="form.schedule_time" aria-label="工作安排时间" class="settings-field__input" type="time" @change="markScheduleEdited" /></div></div>
      <div v-if="form.plan_mode === 'recurring' && form.schedule_kind === 'weekly'" class="settings-field agent-autonomy-panel__wide"><span class="settings-field__label">每周日期</span><div class="agent-autonomy-panel__days"><label v-for="day in [['mon','一'],['tue','二'],['wed','三'],['thu','四'],['fri','五'],['sat','六'],['sun','日']]" :key="day[0]"><input v-model="form.schedule_days" :aria-label="`周${day[1]}`" type="checkbox" :value="day[0]" @change="markScheduleEdited" />周{{ day[1] }}</label></div></div>
      <div v-if="legacyScheduleVisible" class="settings-field agent-autonomy-panel__wide"><span><span class="settings-field__label">旧工作安排</span><small class="settings-field__hint">当前格式无法解析，请使用上方的每天/每周控件转为结构化设置后保存</small></span><input v-model="form.work_schedule" aria-label="旧工作安排文本" class="settings-field__input" placeholder="例如：工作日 09:00" /></div>
      <div v-if="form.plan_mode === 'recurring'" class="settings-field"><span class="settings-field__label">每周期时长</span><div class="agent-autonomy-panel__duration"><input v-model.number="form.cycle_duration_value" aria-label="每周期时长数值" class="settings-field__input" type="number" min="1" :max="form.cycle_duration_unit === 'day' ? 31 : form.cycle_duration_unit === 'hour' ? 744 : 44640" /><select v-model="form.cycle_duration_unit" aria-label="每周期时长单位" class="settings-field__input"><option value="minute">分钟</option><option value="hour">小时</option><option value="day">天</option></select></div></div>
      <div class="settings-field"><span><span class="settings-field__label">Auto 状态</span><small class="settings-field__hint">启用与停用请使用下方动作，保存岗位不会改变运行状态</small></span><strong>{{ state.profile?.enabled ? "已启用" : "已停用" }}</strong></div>
      <label class="settings-field"><span class="settings-field__label">目标名称</span><input v-model="form.title" class="settings-field__input" /></label>
      <label class="settings-field agent-autonomy-panel__wide"><span class="settings-field__label">目标</span><textarea v-model="form.objective" class="settings-field__input" rows="3" /></label>
      <label class="settings-field agent-autonomy-panel__wide"><span class="settings-field__label">完成条件</span><textarea v-model="form.acceptance" class="settings-field__input" rows="3" /></label>
      <label class="settings-field"><span class="settings-field__label">运行次数上限</span><input v-model.number="form.max_runs" class="settings-field__input" type="number" min="1" /></label>
      <label class="settings-field"><span class="settings-field__label">累计 Token 预算</span><input v-model.number="form.token_budget" class="settings-field__input" type="number" min="1" /></label>
      <label class="settings-field"><span class="settings-field__label">单轮 Token 预算</span><input v-model.number="form.turn_token_budget" class="settings-field__input" type="number" min="1" /></label>
      <label class="settings-field"><span class="settings-field__label">最小唤醒间隔（秒）</span><input v-model.number="form.min_wake_interval_seconds" class="settings-field__input" type="number" min="60" /></label>
      <label class="settings-field"><span class="settings-field__label">截止时间</span><input v-model="form.expires_at" class="settings-field__input" type="datetime-local" /></label>
      <div class="agent-autonomy-panel__actions">
        <button class="btn btn--primary" :disabled="saving" @click="saveProfile">{{ saving ? "保存中…" : "保存岗位设置" }}</button>
        <button class="btn btn--ghost" :disabled="saving || terminal()" @click="saveCycle">保存周期设置</button>
        <button class="btn btn--ghost" :disabled="actionSaving" @click="load">刷新状态</button>
        <button v-if="state.status === 'active' || state.status === 'waiting'" class="btn btn--ghost" :disabled="actionSaving" @click="action('pause_goal')">暂停当前周期</button>
        <button v-if="state.status === 'paused'" class="btn btn--ghost" :disabled="actionSaving || terminal()" @click="action('resume_goal')">继续当前周期</button>
        <button v-if="state.profile?.enabled" class="btn btn--ghost" :disabled="actionSaving" @click="action('disable_auto')">停用 Auto</button>
        <button v-else class="btn btn--ghost" :disabled="actionSaving" @click="action('enable_auto')">启用 Auto</button>
        <button v-if="state.limits?.session_id" class="btn btn--ghost" :class="{ 'agent-autonomy-panel__approval': state.status === 'waiting' && state.limits.status_reason === 'approval_required' }" @click="openSession">{{ state.status === 'waiting' && state.limits.status_reason === 'approval_required' ? '处理审批' : '查看执行会话 / 审批' }}</button>
        <button v-if="terminal()" class="btn btn--ghost" :disabled="actionSaving" @click="createCycle">创建新的自主任务周期</button>
      </div>
      <p v-if="terminal()" class="agent-autonomy-panel__hint">当前周期已结束；可在此创建新的自主任务周期。</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <div v-if="state.goal_id" class="agent-autonomy-panel__status"><h3>执行状态</h3><p>{{ statusLabel(state.status, state.limits.status_reason) }} · 已运行 {{ state.limits.runs || 0 }}/{{ state.limits.max_runs || 0 }} 次 · 剩余 {{ Math.max(0, Number(state.limits.token_budget || 0) - Number(state.limits.tokens_used || 0)).toLocaleString() }} tokens</p><p>下次唤醒：{{ goalNextWakeLabel({ status: state.status, next_wake_at: state.limits.next_wake_at }) }}</p><p v-if="state.limits.status_reason">状态原因：{{ goalReasonLabel({ status_reason: state.limits.status_reason }) }}</p><p v-if="state.progress?.summary">{{ state.progress.summary }}</p><h3>运行历史</h3><ul><li v-for="run in state.runs" :key="run.id">{{ statusLabel(run.status, run.reason) }} · {{ goalReasonLabel({ status_reason: run.reason }) || '—' }} · {{ formatGoalDate(run.started_at) }}</li></ul></div>
    </template>
  </section>
</template>
<style scoped>
.agent-autonomy-panel__intro { color: var(--text-secondary); font-size: 13px; margin: 0 0 14px; }
.agent-autonomy-panel__actions { display:flex; flex-wrap:wrap; gap:8px; margin-top:14px; }
.agent-autonomy-panel .settings-field { display:grid; grid-template-columns:minmax(180px,1fr) minmax(220px,1.35fr); align-items:center; gap:20px; margin:0; padding:13px 0; border-bottom:1px solid var(--border-subtle); }
.agent-autonomy-panel .settings-field > input[type="checkbox"] { justify-self:end; }
.agent-autonomy-panel .settings-field__input { max-width:100%; }
.agent-autonomy-panel__wide { align-items:start !important; }
.agent-autonomy-panel__wide textarea { resize:vertical; }
.agent-autonomy-panel__status { margin-top:24px; border-top:1px solid var(--border-subtle); padding-top:16px; }
.agent-autonomy-panel__schedule, .agent-autonomy-panel__duration { display:flex; flex-wrap:wrap; align-items:center; gap:8px; }
.agent-autonomy-panel__schedule > .settings-field__input { min-width:120px; }
.agent-autonomy-panel__days { display:flex; flex-wrap:wrap; gap:10px; }
.agent-autonomy-panel__days label { display:inline-flex; align-items:center; gap:4px; white-space:nowrap; }
.agent-autonomy-panel__days input { width:auto; }
@media (max-width:640px) { .agent-autonomy-panel .settings-field { grid-template-columns:1fr; gap:8px; } }
</style>
