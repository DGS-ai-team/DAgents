<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({ agentId: { type: String, required: true } });
const loading = ref(true); const saving = ref(false); const error = ref(""); const notice = ref(""); const needsReconcile = ref(false);
const loaded = ref(false); const loadedAgentId = ref("");
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
async function load() { const token = ++epoch; const id = props.agentId; loaded.value = false; loadedAgentId.value = ""; Object.assign(draft, defaults()); config.revision = 0; config.experience = null; loading.value = true; error.value = ""; try { const [data, experienceResponse] = await Promise.all([api.getAutoConfig(id), api.getAutoExperience(id)]); if (disposed || token !== epoch || id !== props.agentId) return; const experience = experienceResponse && Object.prototype.hasOwnProperty.call(experienceResponse, "experience") ? experienceResponse.experience : experienceResponse; apply({ ...data, experience: experience || null }); loaded.value = true; loadedAgentId.value = id; } catch (e) { if (!disposed && token === epoch && id === props.agentId) error.value = e?.message || "加载 Auto 设置失败"; } finally { if (!disposed && token === epoch) loading.value = false; } }
async function save() { if (saving.value || !props.agentId || !loaded.value || loadedAgentId.value !== props.agentId) return; const id = props.agentId; const token = ++saveEpoch; if (customWake.value && Number(draft.wake_interval_seconds) <= 0) { error.value = "自定义间隔必须大于 0 秒"; return; } saving.value = true; error.value = ""; notice.value = ""; try { const payload = { ...draft, wake_interval_seconds: customWake.value ? Number(draft.wake_interval_seconds) : Number(wakeValue.value), expected_revision: config.revision }; const data = await api.putAutoConfig(id, payload); if (disposed || token !== saveEpoch || id !== props.agentId) return; updateConfigSnapshot(data); needsReconcile.value = false; notice.value = "Auto 设置已保存"; } catch (e) { if (!disposed && token === saveEpoch && id === props.agentId) { const details = e?.data?.error?.details; if (e?.status === 503 && details?.saved_profile) { updateConfigSnapshot({ ...details.saved_profile, experience: config.experience }); needsReconcile.value = true; notice.value = "配置已保存、唤醒未同步"; } else error.value = e?.message || "保存 Auto 设置失败"; } } finally { if (token === saveEpoch) saving.value = false; } }
async function reconcile() { if (saving.value || !loaded.value || loadedAgentId.value !== props.agentId) return; const id = props.agentId; const token = ++saveEpoch; saving.value = true; error.value = ""; try { const data = await api.reconcileAutoConfig(id); if (!disposed && token === saveEpoch && id === props.agentId) { if (data?.profile) updateConfigSnapshot({ ...data.profile, experience: config.experience }); needsReconcile.value = false; notice.value = "唤醒已同步"; } } catch (e) { if (!disposed && token === saveEpoch && id === props.agentId) error.value = e?.message || "唤醒同步失败"; } finally { if (token === saveEpoch) saving.value = false; } }
watch(() => props.agentId, () => { epoch += 1; saveEpoch += 1; saving.value = false; needsReconcile.value = false; notice.value = ""; void load(); });
onMounted(load); onUnmounted(() => { disposed = true; epoch += 1; saveEpoch += 1; saving.value = false; });
</script>
<template>
  <section class="simplified-auto-panel" aria-label="Auto 设置">
    <div v-if="loading" role="status">加载中…</div>
    <template v-else>
      <p v-if="error" class="agent-detail__error" role="alert">{{ error }}</p>
      <div class="simplified-auto-panel__row"><div><strong>职责</strong><small>定义这个 Auto Agent 的长期工作责任</small></div><textarea v-model="draft.responsibility" rows="2" aria-label="职责" /></div>
      <div class="simplified-auto-panel__row"><div><strong>自主激活频率</strong><small>设置自动工作的唤醒间隔</small></div><div class="simplified-auto-panel__control"><select v-model.number="wakeValue" aria-label="自主激活频率"><option v-for="option in wakeOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select><input v-if="customWake" v-model.number="draft.wake_interval_seconds" type="number" min="1" aria-label="自定义间隔秒数" /><span v-if="customWake">秒</span></div></div>
      <div class="simplified-auto-panel__row"><div><strong>最大工具轮次</strong><small>仅限制自主激活，普通聊天保持原规则</small></div><input v-model.number="draft.max_tool_rounds" type="number" min="1" aria-label="最大工具轮次" /></div>
      <div class="simplified-auto-panel__row"><div><strong>每日 dreaming</strong><small>经验整理尚在开发中</small></div><div class="simplified-auto-panel__control"><input v-model="draft.dreaming_enabled" type="checkbox" aria-label="启用每日 dreaming" disabled /><input v-if="draft.dreaming_enabled" v-model="draft.dreaming_time" type="time" aria-label="dreaming 时间" disabled /></div></div>
      <div class="simplified-auto-panel__row"><div><strong>时区</strong></div><input v-model="draft.timezone" aria-label="时区" /></div>
      <div class="simplified-auto-panel__row"><div><strong>经验</strong><small>仅在 dreaming 成功后更新</small></div><p class="simplified-auto-panel__experience">{{ config.experience?.content || "暂无经验摘要" }}<span v-if="config.experience?.updated_at"> · {{ new Date(config.experience.updated_at).toLocaleString() }}</span></p></div>
      <div class="simplified-auto-panel__actions"><button class="btn btn--primary" :disabled="saving || !loaded" @click="save">{{ saving ? "保存中…" : "保存 Auto 设置" }}</button><button v-if="needsReconcile" class="btn btn--ghost" :disabled="saving" @click="reconcile">同步唤醒</button></div><p v-if="notice" role="status">{{ notice }}</p>
    </template>
  </section>
</template>
<style scoped>
.simplified-auto-panel { display: grid; gap: 0; }
.simplified-auto-panel__row { display: grid; grid-template-columns: minmax(180px, .8fr) minmax(220px, 1.4fr); gap: 20px; align-items: center; padding: 13px 0; border-bottom: 1px solid var(--border-subtle); }
.simplified-auto-panel__row > div:first-child { display: grid; gap: 4px; }.simplified-auto-panel small { color: var(--text-secondary); font-size: 12px; }.simplified-auto-panel textarea,.simplified-auto-panel input,.simplified-auto-panel select { max-width: 100%; box-sizing: border-box; padding: 8px; font: inherit; }.simplified-auto-panel__control { display: flex; align-items: center; gap: 8px; }.simplified-auto-panel__experience { margin: 0; color: var(--text-secondary); white-space: pre-wrap; overflow-wrap: anywhere; }.simplified-auto-panel > .btn { margin-top: 16px; justify-self: start; }
@media (max-width: 640px) { .simplified-auto-panel__row { grid-template-columns: 1fr; gap: 8px; align-items: start; } }
</style>
