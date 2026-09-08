<script setup>
import { onMounted, reactive, ref } from "vue";
import * as api from "../api/node.js";
import { useRouter } from "vue-router";
import { formatGoalDate, goalNextWakeLabel, goalReasonLabel } from "../utils/goalPresentation.js";

const props = defineProps({ agentId: { type: String, required: true } });
const router = useRouter();
const loading = ref(true); const saving = ref(false); const error = ref(""); const notice = ref("");
const state = reactive({ goal_id: "", status: "disabled", limits: {}, progress: null, runs: [] });
const form = reactive({ title: "自主任务", objective: "", acceptance: "", enabled: false, max_runs: 12, token_budget: 100000, turn_token_budget: 10000, min_wake_interval_seconds: 300, expires_at: "" });
const terminal = () => ["completed", "stopped", "failed"].includes(state.status);
const statusLabel = (s, reason = "") => s === "waiting" && reason === "approval_required" ? "等待审批" : ({ disabled: "未启用", active: "已启用", waiting: "等待下次唤醒", paused: "已暂停", stopped: "已停止", completed: "已完成", failed: "失败" }[s] || s);
function localDateInput(value) { if (!value) return ""; const d = new Date(value); if (Number.isNaN(d.getTime())) return ""; const pad = (n) => String(n).padStart(2, "0"); return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`; }
function apply(data) { Object.assign(state, data || {}); const l = data?.limits || {}; for (const k of Object.keys(form)) if (k in l) form[k] = k === "expires_at" ? localDateInput(l[k]) : l[k]; form.enabled = ["active", "waiting"].includes(data?.status); }
async function load() { loading.value = true; error.value = ""; try { const data = await api.getAgentAutonomy(props.agentId); apply(data); if (state.goal_id) { const r = await api.getGoalRuns(state.goal_id); state.runs = r?.runs || []; } } catch (e) { error.value = e.message || "加载自主任务失败"; } finally { loading.value = false; } }
async function save() { saving.value = true; error.value = ""; notice.value = ""; try { const data = await api.putAgentAutonomy(props.agentId, { ...form, expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : undefined }); apply(data); notice.value = "自主任务设置已保存"; } catch (e) { error.value = e.message || "保存失败"; } finally { saving.value = false; } }
function openSession() { if (state.limits?.session_id) router.push({ name: "agents", params: { agentId: props.agentId }, query: { session_id: state.limits.session_id } }); }
onMounted(load);
</script>
<template>
  <section class="agent-autonomy-panel">
    <div v-if="loading">加载中…</div>
    <template v-else>
      <p v-if="error" class="agent-detail__error" role="alert">{{ error }}</p>
      <p class="agent-autonomy-panel__intro">Auto Agent 会按计划分批推进目标；普通聊天保持在主聊天中进行。</p>
      <label class="settings-field"><span><span class="settings-field__label">启用自主任务</span><small class="settings-field__hint">启用后由后台按间隔唤醒</small></span><input v-model="form.enabled" type="checkbox" :disabled="terminal()" /></label>
      <label class="settings-field"><span class="settings-field__label">目标名称</span><input v-model="form.title" class="settings-field__input" /></label>
      <label class="settings-field agent-autonomy-panel__wide"><span class="settings-field__label">目标</span><textarea v-model="form.objective" class="settings-field__input" rows="3" /></label>
      <label class="settings-field agent-autonomy-panel__wide"><span class="settings-field__label">完成条件</span><textarea v-model="form.acceptance" class="settings-field__input" rows="3" /></label>
      <label class="settings-field"><span class="settings-field__label">运行次数上限</span><input v-model.number="form.max_runs" class="settings-field__input" type="number" min="1" /></label>
      <label class="settings-field"><span class="settings-field__label">累计 Token 预算</span><input v-model.number="form.token_budget" class="settings-field__input" type="number" min="1" /></label>
      <label class="settings-field"><span class="settings-field__label">单轮 Token 预算</span><input v-model.number="form.turn_token_budget" class="settings-field__input" type="number" min="1" /></label>
      <label class="settings-field"><span class="settings-field__label">最小唤醒间隔（秒）</span><input v-model.number="form.min_wake_interval_seconds" class="settings-field__input" type="number" min="60" /></label>
      <label class="settings-field"><span class="settings-field__label">截止时间</span><input v-model="form.expires_at" class="settings-field__input" type="datetime-local" /></label>
      <button class="btn btn--primary" :disabled="saving || terminal()" @click="save">{{ saving ? "保存中…" : "保存自主任务" }}</button><button class="btn btn--ghost" @click="load">刷新状态</button><button v-if="state.limits?.session_id" class="btn btn--ghost" :class="{ 'agent-autonomy-panel__approval': state.status === 'waiting' && state.limits.status_reason === 'approval_required' }" @click="openSession">{{ state.status === 'waiting' && state.limits.status_reason === 'approval_required' ? '处理审批' : '查看执行会话 / 审批' }}</button><p v-if="terminal()" class="agent-autonomy-panel__hint">任务已结束；如需执行新的任务，请创建新的自主 Agent。</p><p v-if="notice" role="status">{{ notice }}</p>
      <div v-if="state.goal_id" class="agent-autonomy-panel__status"><h3>执行状态</h3><p>{{ statusLabel(state.status, state.limits.status_reason) }} · 已运行 {{ state.limits.runs || 0 }}/{{ state.limits.max_runs || 0 }} 次 · 剩余 {{ Math.max(0, Number(state.limits.token_budget || 0) - Number(state.limits.tokens_used || 0)).toLocaleString() }} tokens</p><p>下次唤醒：{{ goalNextWakeLabel({ status: state.status, next_wake_at: state.limits.next_wake_at }) }}</p><p v-if="state.limits.status_reason">状态原因：{{ goalReasonLabel({ status_reason: state.limits.status_reason }) }}</p><p v-if="state.progress?.summary">{{ state.progress.summary }}</p><h3>运行历史</h3><ul><li v-for="run in state.runs" :key="run.id">{{ statusLabel(run.status, run.reason) }} · {{ goalReasonLabel({ status_reason: run.reason }) || '—' }} · {{ formatGoalDate(run.started_at) }}</li></ul></div>
    </template>
  </section>
</template>
<style scoped>
.agent-autonomy-panel__intro { color: var(--text-secondary); font-size: 13px; margin: 0 0 14px; }
.agent-autonomy-panel .settings-field { display:grid; grid-template-columns:minmax(180px,1fr) minmax(220px,1.35fr); align-items:center; gap:20px; margin:0; padding:13px 0; border-bottom:1px solid var(--border-subtle); }
.agent-autonomy-panel .settings-field > input[type="checkbox"] { justify-self:end; }
.agent-autonomy-panel > .btn { margin:14px 8px 0 0; }
.agent-autonomy-panel .settings-field__input { max-width:100%; }
.agent-autonomy-panel__wide { align-items:start !important; }
.agent-autonomy-panel__wide textarea { resize:vertical; }
.agent-autonomy-panel__status { margin-top:24px; border-top:1px solid var(--border-subtle); padding-top:16px; }
@media (max-width:640px) { .agent-autonomy-panel .settings-field { grid-template-columns:1fr; gap:8px; } }
</style>
