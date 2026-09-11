<script setup>
import { onMounted, ref } from "vue";
import * as api from "../../api/node.js";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
import { feedbackDelivery, feedbackDraft, feedbackProcessing, hasDestinationChanged, unwrapFeedback } from "../../utils/feedback.js";

const DRAFT_KEY = "dagents.feedback.draft";
const form = ref({ type: "problem", title: "", body: "", client_feedback_id: "" });
const items = ref([]);
const selected = ref(null);
const loading = ref(true);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const submissionConflict = ref(false);
const target = ref({ configured: false, enabled: false, url: "", node_id: "" });

function readDraft() {
  try {
    const raw = localStorage.getItem(DRAFT_KEY);
    if (!raw) return;
    const draft = JSON.parse(raw);
    if (draft && typeof draft === "object") {
      form.value = { ...feedbackDraft(draft), client_feedback_id: String(draft.client_feedback_id || "") };
    }
  } catch { /* ignore malformed local draft */ }
}

function persistDraft() {
  try {
    if (form.value.title || form.value.body) localStorage.setItem(DRAFT_KEY, JSON.stringify(form.value));
    else localStorage.removeItem(DRAFT_KEY);
    return true;
  } catch { /* storage may be unavailable */ }
  return false;
}

function stableFeedbackId() {
  if (form.value.client_feedback_id) return form.value.client_feedback_id;
  const id = globalThis.crypto?.randomUUID?.() || `feedback-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  form.value.client_feedback_id = id;
  return id;
}

function clearDraftSafely() {
  try { localStorage.removeItem(DRAFT_KEY); } catch { /* submitted state must remain authoritative */ }
}

function delivery(item) {
  return feedbackDelivery(item);
}

function processing(item) {
  return feedbackProcessing(item);
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [result, feedbackTarget] = await Promise.all([api.listFeedback(), api.getFeedbackTarget()]);
    items.value = Array.isArray(result) ? result : (result?.items || result?.feedback || []);
    target.value = feedbackTarget || { configured: false, enabled: false, url: "", node_id: "" };
    if (selected.value?.client_feedback_id) {
      const fresh = await api.getFeedback(selected.value.client_feedback_id);
      selected.value = unwrapFeedback(fresh);
    }
  } catch (e) {
    error.value = e.message || "无法加载反馈记录";
  } finally {
    loading.value = false;
  }
}

async function submit() {
  const title = form.value.title.trim();
  const body = form.value.body.trim();
  if (!title || !body) {
    error.value = "请填写标题和正文";
    return;
  }
  saving.value = true;
  error.value = "";
  notice.value = "";
  submissionConflict.value = false;
  const destination = target.value?.configured && target.value?.enabled ? String(target.value.url || "").trim() : "";
  if (!destination) {
    const saved = persistDraft();
    error.value = saved
      ? "当前没有可用的 Manage，反馈仅保存在本机草稿中，连接恢复后再提交。"
      : "当前没有可用的 Manage，且浏览器无法保存草稿；请暂时不要关闭页面。";
    saving.value = false;
    return;
  }
  const clientFeedbackId = stableFeedbackId();
  // Persist the generated id before the request so a lost response can be retried idempotently.
  const draftPersisted = persistDraft();
  try {
    const result = await api.createFeedback({
      client_feedback_id: clientFeedbackId,
      category: form.value.type === "problem" ? "bug" : "suggestion",
      title,
      body,
      ...(destination ? { expected_destination: destination } : {}),
    });
    const item = unwrapFeedback(result);
    if (item?.client_feedback_id) selected.value = item;
    form.value = { type: "problem", title: "", body: "", client_feedback_id: "" };
    clearDraftSafely();
    notice.value = item?.delivery_status === "delivered" ? "已送达当前 Manage" : "已保存，等待发送";
    await load();
  } catch (e) {
    // A timeout can happen after Node has accepted the row. Recover that authoritative state
    // before telling the user to retry, so a retry uses the same durable client id.
    try {
      const recovered = unwrapFeedback(await api.getFeedback(clientFeedbackId));
      if (recovered?.client_feedback_id) {
        selected.value = recovered;
        notice.value = "请求响应中断，但反馈已保存；已加载当前送达状态。";
        return;
      }
    } catch { /* the original error remains actionable */ }
    if (e?.status === 409 || e?.response?.status === 409) {
      submissionConflict.value = true;
      error.value = "该反馈编号已经存在，可能是上次提交已成功。请先同步状态；若要提交修改后的内容，请新建一条反馈。";
    } else {
      error.value = e.message || (draftPersisted ? "提交失败，草稿已保留" : "提交失败；浏览器无法保存草稿，请暂时不要关闭页面");
    }
  } finally {
    saving.value = false;
  }
}

function startNewFeedback() {
  form.value = { type: form.value.type, title: "", body: "", client_feedback_id: "" };
  submissionConflict.value = false;
  error.value = "";
  persistDraft();
}

async function openItem(item) {
  const id = unwrapFeedback(item)?.client_feedback_id;
  if (!id) return;
  error.value = "";
  try {
    selected.value = unwrapFeedback(await api.getFeedback(id));
  } catch (e) {
    error.value = e.message || "无法加载反馈详情";
  }
}

async function refreshDetail() {
  const id = selected.value?.client_feedback_id;
  if (!id) return load();
  try {
    selected.value = unwrapFeedback(await api.syncFeedback(id));
  } catch (e) {
    error.value = e.message || "同步失败";
  }
}

onMounted(() => {
  readDraft();
  void load();
});
</script>

<template>
  <div class="settings-page settings-embedded feedback-settings">
    <SettingsPageHeader
      eyebrow="本机反馈"
      title="帮助与反馈"
      description="向当前连接的 Manage 管理员报告问题或提出建议。日志、对话、文件和截图不会自动附带。"
    />

    <div v-if="target.configured && target.enabled" class="feedback-target" role="note">
      发送给当前连接的 Manage：<strong>{{ target.url }}</strong>
    </div>
    <div v-else class="feedback-target feedback-target--offline" role="note">
      当前没有可用的 Manage。你仍可保存草稿，连接恢复后再提交。
    </div>
    <p v-if="error" class="feedback-message feedback-message--error" role="alert">{{ error }}</p>
    <p v-if="notice" class="feedback-message feedback-message--success" role="status">{{ notice }}</p>

    <section class="feedback-card" aria-labelledby="feedback-compose-title">
      <div class="feedback-card__head">
        <div><h2 id="feedback-compose-title">提交反馈</h2><p>正文仅按纯文本发送。</p></div>
        <span class="feedback-draft" v-if="form.title || form.body">草稿自动保留</span>
      </div>
      <div class="feedback-form">
        <label>类型<select v-model="form.type"><option value="problem">问题</option><option value="suggestion">建议</option></select></label>
        <label>标题<input v-model="form.title" maxlength="160" placeholder="用一句话概括" @input="persistDraft" /></label>
        <label class="feedback-form__wide">正文<textarea v-model="form.body" maxlength="10000" rows="7" placeholder="请描述发生了什么、期望结果是什么" @input="persistDraft" /></label>
      </div>
      <div class="feedback-actions"><button class="btn btn--primary" type="button" :disabled="saving" @click="submit">{{ saving ? "保存中…" : "提交反馈" }}</button><button v-if="submissionConflict" class="btn btn--ghost" type="button" @click="startNewFeedback">新建一条反馈</button></div>
    </section>

    <section class="feedback-card" aria-labelledby="feedback-history-title">
      <div class="feedback-card__head"><div><h2 id="feedback-history-title">我的反馈</h2><p>送达状态与管理员处理状态分开显示。</p></div><button class="btn btn--ghost" type="button" @click="load">刷新</button></div>
      <div v-if="loading" class="feedback-empty">正在加载…</div>
      <div v-else-if="!items.length" class="feedback-empty">还没有反馈记录。</div>
      <div v-else class="feedback-list">
        <button v-for="raw in items" :key="unwrapFeedback(raw)?.client_feedback_id" type="button" class="feedback-row" :class="{ 'is-selected': selected?.client_feedback_id === unwrapFeedback(raw)?.client_feedback_id }" @click="openItem(raw)">
          <span><strong>{{ unwrapFeedback(raw)?.title || "无标题" }}</strong><small>{{ unwrapFeedback(raw)?.category === 'suggestion' ? '建议' : '问题' }} · {{ unwrapFeedback(raw)?.client_feedback_id }}</small></span>
          <span class="feedback-row__statuses"><em>{{ delivery(raw) }}</em><em>{{ processing(raw) }}</em></span>
        </button>
      </div>
    </section>

    <section v-if="selected" class="feedback-card feedback-detail" aria-labelledby="feedback-detail-title">
      <div class="feedback-card__head"><div><h2 id="feedback-detail-title">反馈详情</h2><p class="feedback-id">{{ selected.client_feedback_id }}</p></div><div class="feedback-actions"><button class="btn btn--ghost" type="button" @click="refreshDetail">同步状态</button></div></div>
      <h3>{{ selected.title }}</h3>
      <pre class="feedback-body">{{ selected.body }}</pre>
      <div class="feedback-detail__meta"><span>送达：{{ delivery(selected) }}</span><span>处理：{{ processing(selected) }}</span><span v-if="selected.manage_target || selected.destination">目的：{{ selected.manage_target || selected.destination }}</span></div>
      <div v-if="selected.reply" class="feedback-reply"><strong>管理员回复</strong><p>{{ selected.reply }}</p></div>
      <p v-if="selected.last_error || selected.delivery_error" class="feedback-message feedback-message--error">发送失败：{{ selected.last_error || selected.delivery_error }}</p>
      <p v-if="hasDestinationChanged(selected)" class="feedback-message feedback-message--error">Manage 目的地已变化。旧反馈仍归属于原管理方，已阻止自动转发；请检查连接后再处理。</p>
    </section>
  </div>
</template>

<style scoped>
.feedback-settings { width: 100%; box-sizing: border-box; }
.feedback-target { margin: 4px 0 18px; padding: 10px 12px; border: 1px solid var(--color-border); border-radius: 6px; background: var(--color-surface-alt); color: var(--color-text-subtle); font-size: 13px; }
.feedback-target--offline { border-color: var(--color-warning, #b7791f); }
.feedback-card { margin-top: 20px; padding: 0 0 24px; border: 0; border-radius: 0; background: transparent; }
.feedback-card + .feedback-card { padding-top: 24px; border-top: 1px solid var(--color-border); }
.feedback-card__head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.feedback-card h2 { margin: 0; font-size: 16px; }.feedback-card h3 { margin: 20px 0 8px; font-size: 15px; }.feedback-card p { color: var(--color-text-subtle); }
.feedback-card__head p { margin: 6px 0 0; font-size: 13px; }.feedback-draft,.feedback-id { font-size: 12px; color: var(--color-text-subtle); }
.feedback-form { display: flex; flex-direction: column; gap: 0; margin-top: 20px; }
.feedback-form label { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(260px, 1.7fr); align-items: center; gap: 24px; padding: 14px 0; border-bottom: 1px solid var(--color-border); font-size: 13px; color: var(--color-text-subtle); }
.feedback-form__wide { display: block !important; padding-top: 18px !important; }
.feedback-form input,.feedback-form select,.feedback-form textarea { width: 100%; box-sizing: border-box; border: 1px solid var(--color-border); border-radius: 6px; padding: 9px 10px; background: var(--color-surface-alt); color: var(--color-text); font: inherit; }.feedback-form textarea { resize: vertical; min-height: 180px; }
.feedback-actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; margin-top: 16px; }.feedback-message { margin: 12px 0; font-size: 13px; }.feedback-message--error { color: var(--color-danger, #c53030); }.feedback-message--success { color: var(--color-success, #2f855a); }
.feedback-list { margin-top: 16px; border-top: 1px solid var(--color-border); }.feedback-row { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 4px; border: 0; border-bottom: 1px solid var(--color-border); background: transparent; color: var(--color-text); text-align: left; cursor: pointer; }.feedback-row:hover,.feedback-row.is-selected { background: var(--color-surface-alt); }.feedback-row span:first-child { display: grid; gap: 5px; }.feedback-row small { color: var(--color-text-subtle); }.feedback-row__statuses { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }.feedback-row em { padding: 3px 7px; border-radius: 999px; background: var(--color-surface-alt); color: var(--color-text-subtle); font-size: 12px; font-style: normal; white-space: nowrap; }.feedback-empty { padding: 24px 0; color: var(--color-text-subtle); text-align: center; }.feedback-body { white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; padding: 14px; border-radius: 6px; background: var(--color-surface-alt); color: var(--color-text); font: inherit; line-height: 1.55; }.feedback-detail__meta { display: flex; flex-wrap: wrap; gap: 14px; margin-top: 14px; color: var(--color-text-subtle); font-size: 13px; }.feedback-reply { margin-top: 18px; padding: 14px; border-left: 3px solid var(--color-action, #4f46e5); background: var(--color-surface-alt); }.feedback-reply p { margin: 6px 0 0; white-space: pre-wrap; }
@media (max-width: 640px) { .feedback-form label { grid-template-columns: 1fr; gap: 8px; }.feedback-form__wide { display: grid !important; }.feedback-row { align-items: flex-start; flex-direction: column; }.feedback-row__statuses { justify-content: flex-start; } }
</style>
