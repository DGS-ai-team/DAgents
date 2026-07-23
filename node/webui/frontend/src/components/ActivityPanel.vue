<script setup>
import { computed, ref, watch } from "vue";
import * as api from "../api/node.js";
import { chromeStore } from "../stores/chrome.js";
import { agentStore } from "../stores/agent.js";
import { transcriptStore } from "../stores/transcript.js";
import { deriveActivityFromTranscript } from "../utils/workspaceActivity.js";

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const remote = ref(null);
const expandedCmd = ref({});
const sectionOpen = ref({
  changes: true,
  commands: true,
  context: true,
});

function toggleSection(key) {
  sectionOpen.value = { ...sectionOpen.value, [key]: !sectionOpen.value[key] };
}

function toggleCmd(key) {
  const k = String(key);
  expandedCmd.value = { ...expandedCmd.value, [k]: !expandedCmd.value[k] };
}

const live = computed(() => deriveActivityFromTranscript(transcriptStore.entries));

const files = computed(() => (remote.value?.files?.length ? remote.value.files : live.value.files) || []);
const commands = computed(() => (remote.value?.commands?.length ? remote.value.commands : live.value.commands) || []);
const fileCount = computed(() => files.value.length);
const cmdCount = computed(() => commands.value.length);

const summaryLine = computed(() => {
  const parts = [];
  if (fileCount.value) parts.push(`改动 ${fileCount.value} 个文件`);
  if (cmdCount.value) parts.push(`执行 ${cmdCount.value} 条命令`);
  return parts.join(" · ") || "暂无活动";
});

const agentId = computed(() => String(agentStore.agentId || "").trim());
const agentIdShort = computed(() => {
  const id = agentId.value;
  if (!id) return "—";
  if (id.length <= 18) return id;
  return `${id.slice(0, 10)}…${id.slice(-4)}`;
});

const modelLabel = computed(() => {
  const llm = chromeStore.llmSettings;
  return String(llm?.model || llm?.active_profile || "—").trim() || "—";
});

const providerLabel = computed(() => String(chromeStore.llmSettings?.provider || "").trim());

const capabilityItems = computed(() => {
  const caps = chromeStore.agentInfo?.capabilities;
  if (!Array.isArray(caps) || !caps.length) {
    return [
      { id: "files", label: "Files", hint: "读写工作区文件" },
      { id: "terminal", label: "Terminal", hint: "执行 shell 命令" },
    ];
  }
  return caps.map((c) => {
    const id = String(c || "").trim();
    const map = {
      shell: { label: "Terminal", hint: "执行 shell 命令" },
      filesystem: { label: "Files", hint: "读写工作区文件" },
      triggers: { label: "Triggers", hint: "定时与事件触发" },
      browser: { label: "Browser", hint: "浏览器工具" },
      wecom: { label: "WeCom", hint: "企业微信推送" },
    };
    return { id, ...(map[id] || { label: id, hint: "" }) };
  });
});

const contextTitle = computed(() => {
  const id = agentIdShort.value;
  return id && id !== "—" ? `On ${id}` : "On Agent";
});

function fileParts(path) {
  const raw = String(path || "");
  const i = Math.max(raw.lastIndexOf("/"), raw.lastIndexOf("\\"));
  if (i < 0) return { name: raw || "—", dir: "" };
  return { name: raw.slice(i + 1) || raw, dir: raw.slice(0, i) };
}

function primaryOp(ops) {
  const list = Array.isArray(ops) ? ops : [];
  if (list.includes("write")) return "A";
  if (list.includes("replace")) return "M";
  return list[0] ? String(list[0]).slice(0, 1).toUpperCase() : "M";
}

function opClass(ops, rejected) {
  if (rejected) return "activity-op--danger";
  const list = Array.isArray(ops) ? ops : [];
  if (list.includes("write")) return "activity-op--add";
  return "activity-op--mod";
}

function statusClass(status) {
  if (status === "ok") return "activity-status--ok";
  if (status === "error" || status === "rejected") return "activity-status--bad";
  return "activity-status--muted";
}

function truncateCmd(cmd, max = 64) {
  const s = String(cmd || "").replace(/\s+/g, " ").trim();
  if (s.length <= max) return s;
  return `${s.slice(0, max)}…`;
}

async function refresh() {
  const id = agentStore.agentId?.trim();
  if (!id) {
    remote.value = null;
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    remote.value = await api.getWorkspaceActivity(id);
  } catch (e) {
    error.value = e.message || "加载失败";
  } finally {
    loading.value = false;
  }
}

watch(
  () => agentStore.agentId,
  () => {
    void refresh();
  },
  { immediate: true },
);

defineExpose({ refresh });
</script>

<template>
  <aside class="activity-rail" aria-label="变更与上下文">
    <header class="activity-rail__top">
      <div class="activity-rail__top-text">
        <div class="activity-rail__kicker">Activity</div>
        <div class="activity-rail__summary">{{ summaryLine }}</div>
      </div>
      <div class="activity-rail__top-actions">
        <button
          type="button"
          class="activity-rail__icon-btn"
          title="刷新"
          aria-label="刷新"
          :disabled="loading"
          @click="refresh"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path
              d="M3.2 8a4.8 4.8 0 0 1 8.2-3.3M12.8 8a4.8 4.8 0 0 1-8.2 3.3"
              stroke="currentColor"
              stroke-width="1.25"
              stroke-linecap="round"
            />
            <path d="M11.5 2.5v2.8H8.7M4.5 13.5v-2.8h2.8" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <button type="button" class="activity-rail__icon-btn" title="关闭" aria-label="关闭" @click="emit('close')">
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" />
          </svg>
        </button>
      </div>
    </header>

    <p v-if="error" class="activity-rail__error">{{ error }}</p>
    <div v-if="loading && !files.length && !commands.length" class="activity-rail__empty">加载中…</div>

    <div class="activity-rail__scroll">
      <!-- Changes -->
      <section class="activity-section">
        <button type="button" class="activity-section__head" @click="toggleSection('changes')">
          <span class="activity-section__chevron" aria-hidden="true">{{ sectionOpen.changes ? "▾" : "▸" }}</span>
          <span class="activity-section__title">Changes</span>
          <span v-if="fileCount" class="activity-section__badge">
            <span class="activity-section__plus">+{{ fileCount }}</span>
          </span>
        </button>
        <ul v-if="sectionOpen.changes" class="activity-rows">
          <li v-if="!files.length" class="activity-rows__empty">尚无文件改动</li>
          <li v-for="f in files" :key="f.path" class="activity-row" :title="f.path">
            <span class="activity-op" :class="opClass(f.ops, f.rejected)">{{ primaryOp(f.ops) }}</span>
            <span class="activity-row__icon" aria-hidden="true">
              <svg viewBox="0 0 16 16" fill="none">
                <path d="M4.25 2.75h5.1L11.75 5.2v8.05H4.25V2.75Z" stroke="currentColor" stroke-width="1.15" stroke-linejoin="round" />
                <path d="M9.2 2.75V5.2h2.55" stroke="currentColor" stroke-width="1.15" stroke-linejoin="round" />
              </svg>
            </span>
            <span class="activity-row__body">
              <span class="activity-row__name">{{ fileParts(f.path).name }}</span>
              <span v-if="fileParts(f.path).dir" class="activity-row__meta">{{ fileParts(f.path).dir }}</span>
              <span v-if="f.rejected" class="activity-row__meta activity-row__meta--danger">已拒绝</span>
            </span>
          </li>
        </ul>
      </section>

      <!-- Commands -->
      <section class="activity-section">
        <button type="button" class="activity-section__head" @click="toggleSection('commands')">
          <span class="activity-section__chevron" aria-hidden="true">{{ sectionOpen.commands ? "▾" : "▸" }}</span>
          <span class="activity-section__title">Commands</span>
          <span v-if="cmdCount" class="activity-section__badge">{{ cmdCount }}</span>
        </button>
        <ul v-if="sectionOpen.commands" class="activity-rows">
          <li v-if="!commands.length" class="activity-rows__empty">尚无命令记录</li>
          <li v-for="(c, i) in commands" :key="c.tool_call_id || i" class="activity-row activity-row--cmd">
            <button type="button" class="activity-row__btn" @click="toggleCmd(c.tool_call_id || i)">
              <span class="activity-status" :class="statusClass(c.status)" aria-hidden="true" />
              <span class="activity-row__icon" aria-hidden="true">
                <svg viewBox="0 0 16 16" fill="none">
                  <path d="M3.5 4.5 6.5 8 3.5 11.5" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round" />
                  <path d="M8 11.5h4.5" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" />
                </svg>
              </span>
              <span class="activity-row__body">
                <span class="activity-row__name activity-row__name--mono">{{ truncateCmd(c.command) }}</span>
                <span class="activity-row__meta">{{ c.status }}</span>
              </span>
              <span class="activity-row__chevron" aria-hidden="true">{{ expandedCmd[c.tool_call_id || i] ? "▾" : "▸" }}</span>
            </button>
            <pre
              v-if="expandedCmd[c.tool_call_id || i] && c.content_preview"
              class="activity-row__preview"
            >{{ c.content_preview }}</pre>
          </li>
        </ul>
      </section>

      <!-- Extensible context -->
      <section class="activity-section">
        <button type="button" class="activity-section__head" @click="toggleSection('context')">
          <span class="activity-section__chevron" aria-hidden="true">{{ sectionOpen.context ? "▾" : "▸" }}</span>
          <span class="activity-section__title">{{ contextTitle }}</span>
        </button>
        <ul v-if="sectionOpen.context" class="activity-rows">
          <li class="activity-row" title="当前模型">
            <span class="activity-row__icon" aria-hidden="true">
              <svg viewBox="0 0 16 16" fill="none">
                <circle cx="8" cy="8" r="5.25" stroke="currentColor" stroke-width="1.15" />
                <path d="M8 5.25v3.1l2 1.4" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            <span class="activity-row__body">
              <span class="activity-row__name">{{ modelLabel }}</span>
              <span v-if="providerLabel" class="activity-row__meta">{{ providerLabel }}</span>
            </span>
          </li>
          <li
            v-for="item in capabilityItems"
            :key="item.id"
            class="activity-row"
            :title="item.hint || item.label"
          >
            <span class="activity-row__icon" aria-hidden="true">
              <svg v-if="item.id === 'shell' || item.id === 'terminal'" viewBox="0 0 16 16" fill="none">
                <path d="M3.5 4.5 6.5 8 3.5 11.5" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" stroke-linejoin="round" />
                <path d="M8 11.5h4.5" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" />
              </svg>
              <svg v-else-if="item.id === 'filesystem' || item.id === 'files'" viewBox="0 0 16 16" fill="none">
                <path d="M2.75 4.25h4.1l1.2 1.35h5.2v6.15H2.75V4.25Z" stroke="currentColor" stroke-width="1.15" stroke-linejoin="round" />
              </svg>
              <svg v-else viewBox="0 0 16 16" fill="none">
                <rect x="3" y="3" width="10" height="10" rx="2" stroke="currentColor" stroke-width="1.15" />
              </svg>
            </span>
            <span class="activity-row__body">
              <span class="activity-row__name">{{ item.label }}</span>
              <span v-if="item.hint" class="activity-row__meta">{{ item.hint }}</span>
            </span>
          </li>
          <li class="activity-row activity-row--muted" title="后续可扩展：PR、浏览器会话、桌面环境等">
            <span class="activity-row__icon" aria-hidden="true">
              <svg viewBox="0 0 16 16" fill="none">
                <path d="M8 3.25v9.5M3.25 8h9.5" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
              </svg>
            </span>
            <span class="activity-row__body">
              <span class="activity-row__name">More</span>
              <span class="activity-row__meta">PR / Browser / Desktop…</span>
            </span>
          </li>
        </ul>
      </section>
    </div>
  </aside>
</template>

<style scoped>
.activity-rail {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
  background: var(--color-sidebar);
  border-left: 1px solid var(--color-border);
  color: var(--color-text);
}

.activity-rail__top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 12px 10px;
  border-bottom: 1px solid var(--color-border);
}

.activity-rail__kicker {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--color-text-subtle);
}

.activity-rail__summary {
  margin-top: 4px;
  font-size: 12px;
  color: var(--color-text-muted);
  line-height: 1.4;
}

.activity-rail__top-actions {
  display: flex;
  gap: 2px;
  flex-shrink: 0;
}

.activity-rail__icon-btn {
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--color-text-muted);
  display: inline-grid;
  place-items: center;
  cursor: pointer;
}

.activity-rail__icon-btn svg {
  width: 14px;
  height: 14px;
  display: block;
}

.activity-rail__icon-btn:hover:not(:disabled) {
  color: var(--color-text);
  background: rgba(255, 255, 255, 0.05);
}

.activity-rail__icon-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.activity-rail__error {
  margin: 8px 12px 0;
  font-size: 12px;
  color: var(--color-danger);
}

.activity-rail__empty {
  padding: 20px 12px;
  text-align: center;
  font-size: 12px;
  color: var(--color-text-subtle);
}

.activity-rail__scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 6px 0 16px;
}

.activity-section {
  padding: 6px 0 4px;
}

.activity-section + .activity-section {
  border-top: 1px solid var(--color-border);
  margin-top: 4px;
  padding-top: 8px;
}

.activity-section__head {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: 0;
  background: transparent;
  color: var(--color-text-subtle);
  cursor: pointer;
  text-align: left;
  font: inherit;
}

.activity-section__head:hover {
  color: var(--color-text-muted);
}

.activity-section__chevron {
  width: 10px;
  font-size: 10px;
  line-height: 1;
  flex: 0 0 auto;
}

.activity-section__title {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: none;
}

.activity-section__badge {
  flex: 0 0 auto;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--color-text-subtle);
}

.activity-section__plus {
  color: var(--color-success);
}

.activity-rows {
  list-style: none;
  margin: 0;
  padding: 0 6px 4px;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.activity-rows__empty {
  padding: 8px 10px 10px 28px;
  font-size: 12px;
  color: var(--color-text-subtle);
}

.activity-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 6px;
  min-width: 0;
}

.activity-row:hover {
  background: rgba(255, 255, 255, 0.04);
}

.activity-row--muted {
  opacity: 0.72;
}

.activity-row--cmd {
  flex-direction: column;
  gap: 0;
  padding: 2px 4px;
}

.activity-row__btn {
  width: 100%;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.activity-row__btn:hover {
  background: rgba(255, 255, 255, 0.04);
}

.activity-op {
  flex: 0 0 auto;
  width: 14px;
  margin-top: 1px;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  line-height: 1.2;
  text-align: center;
}

.activity-op--add {
  color: var(--color-success);
}

.activity-op--mod {
  color: #e2a053;
}

.activity-op--danger {
  color: var(--color-danger);
}

.activity-status {
  flex: 0 0 auto;
  width: 7px;
  height: 7px;
  margin-top: 5px;
  border-radius: 999px;
  background: var(--color-text-subtle);
}

.activity-status--ok {
  background: var(--color-success);
}

.activity-status--bad {
  background: var(--color-danger);
}

.activity-status--muted {
  background: var(--color-text-subtle);
}

.activity-row__icon {
  flex: 0 0 auto;
  width: 14px;
  height: 14px;
  margin-top: 2px;
  color: var(--color-text-subtle);
}

.activity-row__icon svg {
  width: 14px;
  height: 14px;
  display: block;
}

.activity-row__body {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.activity-row__name {
  font-size: 12.5px;
  color: var(--color-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.activity-row__name--mono {
  font-family: var(--font-mono);
  font-size: 12px;
}

.activity-row__meta {
  font-size: 11px;
  color: var(--color-text-subtle);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.activity-row__meta--danger {
  color: var(--color-danger);
}

.activity-row__chevron {
  flex: 0 0 auto;
  margin-top: 2px;
  font-size: 10px;
  color: var(--color-text-subtle);
}

.activity-row__preview {
  margin: 0 8px 8px 38px;
  padding: 8px 10px;
  max-height: 10em;
  overflow: auto;
  border-radius: 6px;
  border: 1px solid var(--color-border-strong);
  background: #111111;
  color: #d4d4d4;
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
