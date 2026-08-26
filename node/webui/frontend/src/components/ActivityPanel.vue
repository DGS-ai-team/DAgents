<script setup>
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import * as api from "../api/node.js";
import { chromeStore } from "../stores/chrome.js";
import { agentStore } from "../stores/agent.js";
import { transcriptStore } from "../stores/transcript.js";
import { remoteWorkerStore } from "../stores/remoteWorkers.js";
import { activityStore } from "../stores/activity.js";
import { deriveActivityFromTranscript } from "../utils/workspaceActivity.js";
import {
  useChildAgents,
  formatChildAgentStatus,
  isChildAgentActive,
} from "../composables/useChildAgents.js";
import { shortId } from "../utils/panelFormat.js";

defineProps({
  /** 当前 Agent 列表行（含 origin / host） */
  agent: { type: Object, default: null },
  /** 嵌入左侧 NavRail：无关闭钮、紧凑样式 */
  embedded: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);
const route = useRoute();

const loading = ref(false);
const error = ref("");
const remote = ref(null);
const expandedCmd = ref({});
const sectionOpen = ref({
  terminal: false,
  files: false,
  children: false,
});

/** 工作组主栏：不展示上一智能体的活动残留 */
const inWorkgroupContext = computed(() => route.name === "workgroups");
const workgroupContextKey = computed(() =>
  inWorkgroupContext.value ? String(route.params.workgroupId || "").trim() : "",
);

function toggleSection(key) {
  sectionOpen.value = { ...sectionOpen.value, [key]: !sectionOpen.value[key] };
}

function toggleCmd(key) {
  const k = String(key);
  expandedCmd.value = { ...expandedCmd.value, [k]: !expandedCmd.value[k] };
}

const live = computed(() => {
  if (inWorkgroupContext.value) {
    return { files: [], commands: [], file_count: 0, command_count: 0 };
  }
  return deriveActivityFromTranscript(transcriptStore.entries);
});

const files = computed(() => (remote.value?.files?.length ? remote.value.files : live.value.files) || []);
const commands = computed(() => (remote.value?.commands?.length ? remote.value.commands : live.value.commands) || []);
const fileCount = computed(() => files.value.length);
const cmdCount = computed(() => commands.value.length);

const agentId = computed(() => String(agentStore.agentId || "").trim());
const {
  loading: childrenLoading,
  error: childrenError,
  items: childItems,
  cancellingId,
  load: loadChildren,
  cancelChild,
} = useChildAgents(agentId);

const activeChildCount = computed(() => {
  void remoteWorkerStore.tick;
  return childItems.value.filter((item) => isChildAgentActive(item.status)).length;
});

const summaryLine = computed(() => {
  if (inWorkgroupContext.value && !fileCount.value && !cmdCount.value) {
    return workgroupContextKey.value ? "工作组暂无本地活动" : "选择工作组查看活动";
  }
  const parts = [];
  if (fileCount.value) parts.push(`改动 ${fileCount.value} 个文件`);
  if (cmdCount.value) parts.push(`执行 ${cmdCount.value} 条命令`);
  if (!inWorkgroupContext.value && activeChildCount.value) {
    parts.push(`子 Agent ${activeChildCount.value}`);
  }
  return parts.join(" · ") || "暂无活动";
});

function isTerminalCap(id) {
  return id === "shell" || id === "terminal";
}

function isFilesCap(id) {
  return id === "filesystem" || id === "files";
}

/** 是否展示 Terminal / Files 行（无 capabilities 时默认都展示） */
const showTerminal = computed(() => {
  const caps = chromeStore.agentInfo?.capabilities;
  if (!Array.isArray(caps) || !caps.length) return true;
  return caps.some((c) => isTerminalCap(String(c || "").trim()));
});

const showFiles = computed(() => {
  const caps = chromeStore.agentInfo?.capabilities;
  if (!Array.isArray(caps) || !caps.length) return true;
  return caps.some((c) => isFilesCap(String(c || "").trim()));
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

function opClass(ops, rejected, failed) {
  if (rejected || failed) return "activity-op--danger";
  const list = Array.isArray(ops) ? ops : [];
  if (list.includes("write")) return "activity-op--add";
  return "activity-op--mod";
}

function statusClass(status) {
  if (status === "ok") return "activity-status--ok";
  if (status === "error" || status === "rejected" || status === "cancelled") return "activity-status--bad";
  if (status === "running") return "activity-status--muted";
  return "activity-status--muted";
}

function truncateCmd(cmd, max = 64) {
  const s = String(cmd || "").replace(/\s+/g, " ").trim();
  if (s.length <= max) return s;
  return `${s.slice(0, max)}…`;
}

function isAgentMissingError(err) {
  const msg = String(err?.message || err || "");
  return (
    msg.includes("agent 不存在") ||
    msg.includes("尚未激活") ||
    msg.includes("agent_not_found")
  );
}

async function refresh() {
  if (inWorkgroupContext.value) {
    // 工作组会话与智能体 transcript / workspace-activity 解耦；切换工作组时清空残留
    remote.value = null;
    error.value = "";
    loading.value = false;
    expandedCmd.value = {};
    return;
  }
  const id = agentStore.agentId?.trim();
  if (!id) {
    remote.value = null;
    error.value = "";
    return;
  }
  // /clear 后先丢掉缓存，避免空 transcript 时仍显示旧 remote 命令/文件
  remote.value = null;
  expandedCmd.value = {};
  loading.value = true;
  error.value = "";
  try {
    remote.value = await api.getWorkspaceActivity(id);
  } catch (e) {
    remote.value = null;
    // 首配/尚未激活等：按空活动处理，避免吓人的错误条
    if (!isAgentMissingError(e)) {
      error.value = e.message || "加载失败";
    }
  } finally {
    loading.value = false;
  }
  void loadChildren();
}

watch(
  () => [agentStore.agentId, route.name, workgroupContextKey.value],
  () => {
    void refresh();
  },
  { immediate: true },
);

watch(
  () => remoteWorkerStore.tick,
  () => {
    if (!inWorkgroupContext.value && agentId.value) void loadChildren();
  },
);

watch(
  () => activityStore.tick,
  () => {
    void refresh();
  },
);

defineExpose({ refresh });
</script>

<template>
  <aside
    class="activity-rail"
    :class="{ 'activity-rail--embedded': embedded }"
    aria-label="变更与上下文"
  >
    <header v-if="!embedded" class="activity-rail__top">
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
      <ul class="activity-rows">
        <!-- 执行命令记录 -->
        <li v-if="showTerminal" class="activity-cap">
          <button
            type="button"
            class="activity-row activity-row--toggle"
            title="执行命令记录"
            :aria-expanded="sectionOpen.terminal"
            @click="toggleSection('terminal')"
          >
            <span class="activity-row__icon" aria-hidden="true">
              <svg viewBox="0 0 16 16" fill="none">
                <path d="M3.5 4.5 6.5 8 3.5 11.5" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" stroke-linejoin="round" />
                <path d="M8 11.5h4.5" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" />
              </svg>
            </span>
            <span class="activity-row__body">
              <span class="activity-row__name">执行命令记录</span>
            </span>
            <span v-if="cmdCount" class="activity-cap__count">{{ cmdCount }}</span>
            <span class="activity-row__chevron" aria-hidden="true">{{ sectionOpen.terminal ? "▾" : "▸" }}</span>
          </button>
          <ul v-if="sectionOpen.terminal" class="activity-rows activity-rows--nested">
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
        </li>

        <!-- 文件变更记录 -->
        <li v-if="showFiles" class="activity-cap">
          <button
            type="button"
            class="activity-row activity-row--toggle"
            title="文件变更记录"
            :aria-expanded="sectionOpen.files"
            @click="toggleSection('files')"
          >
            <span class="activity-row__icon" aria-hidden="true">
              <svg viewBox="0 0 16 16" fill="none">
                <path d="M2.75 4.25h4.1l1.2 1.35h5.2v6.15H2.75V4.25Z" stroke="currentColor" stroke-width="1.15" stroke-linejoin="round" />
              </svg>
            </span>
            <span class="activity-row__body">
              <span class="activity-row__name">文件变更记录</span>
            </span>
            <span v-if="fileCount" class="activity-cap__count activity-cap__count--add">+{{ fileCount }}</span>
            <span class="activity-row__chevron" aria-hidden="true">{{ sectionOpen.files ? "▾" : "▸" }}</span>
          </button>
          <ul v-if="sectionOpen.files" class="activity-rows activity-rows--nested">
            <li v-if="!files.length" class="activity-rows__empty">尚无文件改动</li>
            <li v-for="f in files" :key="f.path" class="activity-row" :title="f.path">
              <span class="activity-op" :class="opClass(f.ops, f.rejected, f.failed)">{{ primaryOp(f.ops) }}</span>
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
                <span v-else-if="f.failed" class="activity-row__meta activity-row__meta--danger">执行失败</span>
              </span>
            </li>
          </ul>
        </li>

        <!-- 临时子 Agent -->
        <li v-if="!inWorkgroupContext" class="activity-cap">
          <button
            type="button"
            class="activity-row activity-row--toggle"
            title="临时子 Agent"
            :aria-expanded="sectionOpen.children"
            @click="toggleSection('children')"
          >
            <span class="activity-row__icon" aria-hidden="true">
              <svg viewBox="0 0 16 16" fill="none">
                <circle cx="8" cy="5.5" r="2" stroke="currentColor" stroke-width="1.15" />
                <path
                  d="M3.5 12.5c.6-2 2.2-3 4.5-3s3.9 1 4.5 3"
                  stroke="currentColor"
                  stroke-width="1.15"
                  stroke-linecap="round"
                />
              </svg>
            </span>
            <span class="activity-row__body">
              <span class="activity-row__name">临时子 Agent</span>
            </span>
            <span v-if="activeChildCount" class="activity-cap__count">{{ activeChildCount }}</span>
            <span class="activity-row__chevron" aria-hidden="true">{{ sectionOpen.children ? "▾" : "▸" }}</span>
          </button>
          <ul v-if="sectionOpen.children" class="activity-rows activity-rows--nested">
            <li v-if="!agentId" class="activity-rows__empty">先打开一个智能体</li>
            <li v-else-if="childrenLoading && !childItems.length" class="activity-rows__empty">加载中…</li>
            <li v-else-if="childrenError" class="activity-rows__empty">{{ childrenError }}</li>
            <li v-else-if="!childItems.length" class="activity-rows__empty">暂无运行中的子 Agent</li>
            <li
              v-for="item in childItems"
              :key="item.child_agent_id"
              class="activity-row activity-row--child"
            >
              <span
                class="activity-status"
                :class="isChildAgentActive(item.status) ? 'activity-status--ok' : 'activity-status--muted'"
                aria-hidden="true"
              />
              <span class="activity-row__body">
                <span class="activity-row__name">{{
                  item.purpose?.trim() || shortId(item.child_agent_id, 20)
                }}</span>
                <span class="activity-row__meta">
                  {{ formatChildAgentStatus(item.status) }}
                  · {{ item.turn_count ?? 0 }}/{{ item.max_turns ?? "—" }}
                </span>
              </span>
              <button
                v-if="isChildAgentActive(item.status)"
                type="button"
                class="activity-row__action"
                :disabled="cancellingId === item.child_agent_id"
                @click.stop="cancelChild(item.child_agent_id)"
              >
                {{ cancellingId === item.child_agent_id ? "…" : "取消" }}
              </button>
            </li>
          </ul>
        </li>
      </ul>
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

.activity-rail--embedded {
  height: auto;
  max-height: none;
  min-height: 0;
  flex: 0 0 auto;
  overflow: visible;
  border-left: 0;
  background: transparent;
}

.activity-rail--embedded .activity-rail__scroll {
  flex: none;
  min-height: 0;
  overflow: visible;
  padding: 0 4px 8px;
}

.activity-rail--embedded .activity-section__title {
  font-size: 11px;
}

.activity-rail--embedded .activity-row__name {
  font-size: 12px;
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
  background: var(--color-surface-hover);
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

.activity-rows--nested {
  padding: 0 0 4px 12px;
  margin: 0 0 2px;
}

.activity-rows__empty {
  padding: 8px 10px 10px 28px;
  font-size: 12px;
  color: var(--color-text-subtle);
}

.activity-cap {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.activity-cap__count {
  flex: 0 0 auto;
  margin-left: 4px;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--color-text-subtle);
}

.activity-cap__count--add {
  color: var(--color-success);
}

.activity-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 6px;
  min-width: 0;
}

.activity-row--toggle {
  width: 100%;
  margin: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  align-items: center;
}

.activity-row--toggle:hover {
  background: var(--color-surface-hover);
}

.activity-row:hover {
  background: var(--color-surface-hover);
}

.activity-row--muted {
  opacity: 0.72;
}

.activity-row--cmd {
  flex-direction: column;
  gap: 0;
  padding: 2px 4px;
}

.activity-row--child {
  align-items: center;
}

.activity-row__action {
  flex-shrink: 0;
  margin-left: auto;
  padding: 2px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 11.5px;
  cursor: pointer;
}

.activity-row__action:hover:not(:disabled) {
  color: var(--color-danger, #b42318);
  background: var(--color-surface-hover);
}

.activity-row__action:disabled {
  opacity: 0.55;
  cursor: default;
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
  background: var(--color-surface-hover);
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
  background: var(--color-code-bg);
  color: var(--color-code-fg);
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
