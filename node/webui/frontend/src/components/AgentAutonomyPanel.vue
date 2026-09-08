<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";
import { useRouter } from "vue-router";
import { formatGoalDate, goalNextWakeLabel, goalReasonLabel } from "../utils/goalPresentation.js";

const props = defineProps({ agentId: { type: String, required: true } });
const router = useRouter();
const loading = ref(true); const saving = ref(false); const actionSaving = ref(false); const error = ref(""); const notice = ref("");
const state = reactive({ goal_id: "", status: "disabled", limits: {}, profile: {}, progress: null, runs: [] });
const form = reactive({ title: "自主任务", role_objective: "", role_boundaries: "", plan_mode: "recurring", timezone: "Asia/Shanghai", work_schedule: "", objective: "", acceptance: "", enabled: false, max_runs: 12, token_budget: 100000, turn_token_budget: 10000, min_wake_interval_seconds: 300, expires_at: "" });
let cycleRequestKey = "";
let cycleRequestPayload = "";
let loadSequence = 0;
const cycleDraftKeys = ["title", "objective", "acceptance", "enabled", "max_runs", "token_budget", "turn_token_budget", "min_wake_interval_seconds", "expires_at"];
const profileDraftKeys = ["role_objective", "role_boundaries", "plan_mode", "timezone", "work_schedule"];
const terminal = () => ["completed", "stopped", "failed"].includes(state.status);
const statusLabel = (s, reason = "") => s === "waiting" && reason === "approval_required" ? "等待审批" : ({ disabled: "未启用", active: "已启用", waiting: "等待下次唤醒", paused: "已暂停", stopped: "已停止", completed: "已完成", failed: "失败" }[s] || s);
function localDateInput(value) { if (!value) return ""; const d = new Date(value); if (Number.isNaN(d.getTime())) return ""; const pad = (n) => String(n).padStart(2, "0"); return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`; }
function apply(data) { Object.assign(state, data || {}); const l = data?.limits || {}; const p = data?.profile || {}; for (const k of Object.keys(form)) if (k in l) form[k] = k === "expires_at" ? localDateInput(l[k]) : l[k]; for (const k of ["role_objective", "role_boundaries", "plan_mode", "timezone", "work_schedule"]) if (k in p) form[k] = p[k]; form.enabled = p.enabled ?? ["active", "waiting"].includes(data?.status); }
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
  try {
    const cycleDraft = Object.fromEntries(cycleDraftKeys.map((key) => [key, form[key]]));
    const profile = { agent_id: props.agentId };
    for (const key of profileDraftKeys) profile[key] = form[key];
    const data = await api.putAgentAutonomy(props.agentId, { profile, expected_revision: state.profile?.revision });
    apply(data);
    Object.assign(form, cycleDraft);
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
  try {
    const cycle = { title: form.title, objective: form.objective, acceptance: form.acceptance, max_runs: form.max_runs, token_budget: form.token_budget, turn_token_budget: form.turn_token_budget, min_wake_interval_seconds: form.min_wake_interval_seconds, expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : undefined };
    const profileDraft = Object.fromEntries(profileDraftKeys.map((key) => [key, form[key]]));
    const data = await api.putAgentAutonomy(props.agentId, { ...cycle, expected_revision: state.profile?.revision, expected_goal_revision: state.current_cycle?.revision ?? state.limits?.revision });
    apply(data);
    Object.assign(form, profileDraft);
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
      <label class="settings-field"><span class="settings-field__label">计划模式</span><select v-model="form.plan_mode" class="settings-field__input"><option value="recurring">持续运行</option><option value="one_shot">单次任务</option></select></label>
      <label class="settings-field"><span class="settings-field__label">时区</span><input v-model="form.timezone" class="settings-field__input" /></label>
      <label class="settings-field"><span class="settings-field__label">工作安排</span><input v-model="form.work_schedule" class="settings-field__input" placeholder="例如：工作日 09:00" /></label>
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
@media (max-width:640px) { .agent-autonomy-panel .settings-field { grid-template-columns:1fr; gap:8px; } }
</style>
