<script setup>
import { ref, onMounted, onUnmounted, computed, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import { agentStore } from "../stores/agent.js";
import { chromeStore } from "../stores/chrome.js";
import { cycleTheme, themeStore } from "../stores/theme.js";
import {
  formatCompactRelativeTime,
  agentDisplayTitle,
  agentRecordId,
} from "../utils/format.js";
import brandIcon from "@dagents-brand/brand-icon.png";
import ActivityPanel from "./ActivityPanel.vue";
import { transcriptStore } from "../stores/transcript.js";
import { deriveActivityFromTranscript } from "../utils/workspaceActivity.js";
import { activeChildCount, remoteWorkerStore } from "../stores/remoteWorkers.js";
import { hasWorkgroupUnread, noteWorkgroupTimeline } from "../stores/unread.js";

const RAIL_CACHE_TTL_MS = 30_000;
const UNREAD_REFRESH_INTERVAL_MS = 15_000;
const railCache = {
  agents: [],
  workgroups: [],
  agentsFetchedAt: 0,
  workgroupsFetchedAt: 0,
  agentsInFlight: null,
  workgroupsInFlight: null,
};
let refreshTimer = null;
let unreadRefreshTimer = null;
let railRefreshInFlight = null;
let terminalLoadToken = 0;

const props = defineProps({
  terminalRevision: { type: Number, default: 0 },
  selectedTerminalId: { type: String, default: "" },
});

const emit = defineEmits([
  "switch",
  "terminal-selected",
  "create",
  "delete",
  "agents-updated",
  "create-member",
  "configure-member",
]);

const route = useRoute();
const router = useRouter();

const agents = ref([]);
const workgroups = ref([]);
const loadingAgents = ref(false);
const loadingWgs = ref(false);
const agentsLoaded = ref(false);
const workgroupsLoaded = ref(false);
const agentsLoadError = ref("");
const workgroupsLoadError = ref("");
const manualRefreshingAgents = ref(false);
const terminals = ref([]);
const loadingTerminals = ref(false);
const manualRefreshingTerminals = ref(false);
const terminalError = ref("");
const deletingId = ref("");
const renamingId = ref("");
const renameDraft = ref("");

/** 分区展开：智能体 / 工作组 / 活动 */
const sectionOpen = ref({
  agents: true,
  terminals: true,
  workgroups: true,
  activity: false,
});
const mobileActionOpen = ref("");

function toggleSection(key) {
  mobileActionOpen.value = "";
  sectionOpen.value = {
    ...sectionOpen.value,
    [key]: !sectionOpen.value[key],
  };
}

function toggleSectionActions(key) {
  mobileActionOpen.value = mobileActionOpen.value === key ? "" : key;
}

/** 展开某一分区（供对话区 Changes 等入口调用） */
function expandSection(key) {
  if (!Object.prototype.hasOwnProperty.call(sectionOpen.value, key)) return;
  if (sectionOpen.value[key]) return;
  sectionOpen.value = { ...sectionOpen.value, [key]: true };
}

/** @type {import('vue').Ref<Set<string>>} */
const expanded = ref(new Set());
/** workgroupId -> members[] */
const membersByWg = ref({});
/** workgroupId -> loading */
const membersLoading = ref({});

const createWgOpen = ref(false);
const createWgName = ref("");
const createWgBusy = ref(false);
const createWgError = ref("");
const manualRefreshingWgs = ref(false);

const activitySnap = computed(() => {
  if (route.name === "workgroups") {
    return { files: [], commands: [], file_count: 0, command_count: 0 };
  }
  return deriveActivityFromTranscript(transcriptStore.entries);
});
const activityBadge = computed(() => {
  void remoteWorkerStore.tick;
  const n =
    (activitySnap.value.file_count || 0) +
    (activitySnap.value.command_count || 0) +
    (route.name === "workgroups" ? 0 : activeChildCount());
  return n > 0 ? n : 0;
});

const activeAgentId = computed(() => {
  // 在工作组等其它主栏时不应保留智能体选中高亮
  if (route.name !== "agents") return "";
  return String(agentStore.agentId || "").trim();
});

async function loadTerminals({ manual = false } = {}) {
  if (manual) manualRefreshingTerminals.value = true;
  const agentId = String(activeAgentId.value || "").trim();
  const token = ++terminalLoadToken;
  if (!agentId) {
    terminals.value = [];
    terminalError.value = "";
    if (manual) manualRefreshingTerminals.value = false;
    return;
  }
  loadingTerminals.value = true;
  terminalError.value = "";
  try {
    const result = await api.listAgentTerminals(agentId);
    if (token !== terminalLoadToken || agentId !== activeAgentId.value) return;
    terminals.value = Array.isArray(result?.terminals) ? result.terminals : [];
    const selected = terminals.value.find(
      (item) => String(item?.terminal_id || "") === String(props.selectedTerminalId || ""),
    );
    if (props.selectedTerminalId && !selected) {
      emit("terminal-selected", null);
    } else if (selected) {
      // 刷新后同步最新状态、cwd 和目标信息到主区域。
      emit("terminal-selected", selected);
    }
  } catch (e) {
    if (token !== terminalLoadToken) return;
    terminalError.value = e.message || "加载终端列表失败";
  } finally {
    if (token === terminalLoadToken) loadingTerminals.value = false;
    if (manual) manualRefreshingTerminals.value = false;
  }
}

function terminalLabel(item) {
  const displayName = String(item?.display_name || item?.label || item?.name || "").trim();
  if (displayName) return displayName;
  const shell = String(item?.shell || "终端").trim();
  const target = String(item?.target_kind || "local").trim() === "linux_channel"
    ? `Linux · ${String(item?.target_id || "通道").trim()}`
    : "本机";
  return `${shell} · ${target}`;
}

function terminalMeta(item) {
  const parts = [terminalStatusLabel(item?.status)];
  const username = String(item?.username || item?.user || "").trim();
  const host = String(item?.host || item?.hostname || "").trim();
  if (username && host) parts.push(`${username}@${host}`);
  else if (host) parts.push(host);
  if (item?.config_id) parts.push(String(item.config_id));
  return parts.join(" · ");
}

function terminalStatusLabel(status) {
  switch (String(status || "").trim()) {
    case "running":
      return "运行中";
    case "exited":
      return "已退出";
    case "closed":
      return "已关闭";
    default:
      return String(status || "未知");
  }
}

function selectTerminal(item) {
  if (!item?.terminal_id) return;
  emit("terminal-selected", item);
}
const activeWorkgroupId = computed(() =>
  route.name === "workgroups" ? String(route.params.workgroupId || "").trim() : "",
);
const online = computed(() => chromeStore.sseStatus === "connected");
const statusClass = computed(() => {
  if (online.value) return "nav-rail__dot--online";
  if (chromeStore.sseStatus === "connecting") return "nav-rail__dot--connecting";
  return "nav-rail__dot--offline";
});
const statusLabel = computed(() => {
  if (online.value) return "在线";
  if (chromeStore.sseStatus === "connecting") return "连接中";
  return "离线";
});
const themeLabel = computed(() => {
  if (themeStore.mode === "system") return "主题：跟随系统（点击切换）";
  if (themeStore.mode === "light") return "主题：浅色（点击切换）";
  return "主题：深色（点击切换）";
});

function onToggleTheme() {
  cycleTheme();
}

function agentSortTime(agent) {
  const ts = Date.parse(agent?.updated_at || agent?.UpdatedAt || "");
  return Number.isFinite(ts) ? ts : 0;
}

const sortedAgents = computed(() => {
  return [...agents.value].sort((a, b) => agentSortTime(b) - agentSortTime(a));
});

async function refreshAgents({ force = false, manual = false } = {}) {
  if (manual) manualRefreshingAgents.value = true;
  loadingAgents.value = true;
  try {
    const now = Date.now();
    if (!force && railCache.agentsFetchedAt && now - railCache.agentsFetchedAt < RAIL_CACHE_TTL_MS) {
      agents.value = [...railCache.agents];
      agentsLoaded.value = true;
      agentsLoadError.value = "";
      return;
    }
    if (!railCache.agentsInFlight) {
      railCache.agentsInFlight = api
        .listAgents()
        .then((res) => {
          railCache.agents = res.agents || [];
          railCache.agentsFetchedAt = Date.now();
        })
        .finally(() => {
          railCache.agentsInFlight = null;
        });
    }
    await railCache.agentsInFlight;
    agents.value = [...railCache.agents];
    agentsLoaded.value = true;
    agentsLoadError.value = "";
  } catch {
    // Keep the last successful list during transient refresh failures and
    // expose the stale state in the section header instead of replacing rows.
    agentsLoadError.value = "智能体列表暂时不可用，正在重试…";
  } finally {
    loadingAgents.value = false;
    if (manual) manualRefreshingAgents.value = false;
    emit("agents-updated", agents.value.slice());
  }
}

async function refreshWorkgroups({ force = false, manual = false } = {}) {
  if (manual) manualRefreshingWgs.value = true;
  loadingWgs.value = true;
  try {
    const now = Date.now();
    if (
      !force &&
      railCache.workgroupsFetchedAt &&
      now - railCache.workgroupsFetchedAt < RAIL_CACHE_TTL_MS
    ) {
      workgroups.value = [...railCache.workgroups];
      workgroupsLoaded.value = true;
      workgroupsLoadError.value = "";
      return;
    }
    if (!railCache.workgroupsInFlight) {
      railCache.workgroupsInFlight = api
        .listWorkgroups({ scope: "subscribed" })
        .then((res) => {
          railCache.workgroups = res.workgroups || [];
          railCache.workgroupsFetchedAt = Date.now();
        })
        .finally(() => {
          railCache.workgroupsInFlight = null;
        });
    }
    await railCache.workgroupsInFlight;
    workgroups.value = [...railCache.workgroups];
    workgroupsLoaded.value = true;
    workgroupsLoadError.value = "";
  } catch {
    // Keep the last successful list during transient refresh failures and
    // expose the stale state in the section header instead of replacing rows.
    workgroupsLoadError.value = "工作组列表暂时不可用";
  } finally {
    loadingWgs.value = false;
    if (manual) manualRefreshingWgs.value = false;
  }
}

async function refreshWorkgroupUnread(workgroupList) {
  await Promise.all(
    (Array.isArray(workgroupList) ? workgroupList : []).map(async (wg) => {
      const id = String(wg?.workgroup_id || "").trim();
      if (!id) return;
      try {
        const res = await api.getWorkgroupTimeline(id, { limit: 1 });
        const latestSeq = (Array.isArray(res?.events) ? res.events : []).reduce(
          (max, event) => Math.max(max, Number(event?.seq || 0)),
          0,
        );
        noteWorkgroupTimeline(id, latestSeq);
      } catch {
        // Keep an already-known unread state across transient refresh failures.
      }
    }),
  );
}

async function refresh({ force = true, manual = false } = {}) {
  if (railRefreshInFlight) return railRefreshInFlight;
  const task = (async () => {
    await Promise.all([refreshAgents({ force }), refreshWorkgroups({ force, manual })]);
    await refreshWorkgroupUnread(workgroups.value);
    // 已展开的工作组刷新成员
    await Promise.all(
      workgroups.value
        .filter((wg) => expanded.value.has(wg.workgroup_id))
        .map((wg) => loadMembers(wg.workgroup_id, true)),
    );
  })();
  railRefreshInFlight = task;
  try {
    return await task;
  } finally {
    if (railRefreshInFlight === task) railRefreshInFlight = null;
  }
}

function isExpanded(wgId) {
  return expanded.value.has(wgId);
}

async function loadMembers(wgId, force = false) {
  if (!wgId) return;
  if (!force && membersByWg.value[wgId]) return;
  membersLoading.value = { ...membersLoading.value, [wgId]: true };
  try {
    const res = await api.listWorkgroupMembers(wgId);
    membersByWg.value = {
      ...membersByWg.value,
      [wgId]: res.members || [],
    };
  } catch {
    membersByWg.value = { ...membersByWg.value, [wgId]: [] };
  } finally {
    membersLoading.value = { ...membersLoading.value, [wgId]: false };
  }
}

async function toggleWorkgroup(wgId) {
  const next = new Set(expanded.value);
  if (next.has(wgId)) {
    next.delete(wgId);
    expanded.value = next;
    return;
  }
  next.add(wgId);
  expanded.value = next;
  await loadMembers(wgId);
}

async function openWorkgroup(wgId, query = {}) {
  const next = new Set(expanded.value);
  next.add(wgId);
  expanded.value = next;
  await loadMembers(wgId);
  router.push({
    name: "workgroups",
    params: { workgroupId: wgId },
    query,
  });
}

/** 行点击：已展开且当前选中 → 收起成员清单；否则展开并进入工作组 */
async function onWorkgroupRowClick(wgId) {
  if (isExpanded(wgId) && activeWorkgroupId.value === wgId) {
    await toggleWorkgroup(wgId);
    return;
  }
  await openWorkgroup(wgId);
}

function selectAgent(id) {
  const agentId = String(id || "").trim();
  if (agentId) {
    agents.value = agents.value.map((agent) =>
      agentRecordId(agent) === agentId ? { ...agent, has_unread: false } : agent,
    );
    railCache.agents = railCache.agents.map((agent) =>
      agentRecordId(agent) === agentId ? { ...agent, has_unread: false } : agent,
    );
  }
  emit("switch", id);
  if (route.name !== "agents") {
    router.push({ name: "agents", params: { agentId: id } });
  }
}

function openCreateAgent() {
  emit("create");
}

function openAgentSettings(agent) {
  const id = agentRecordId(agent);
  if (!id) return;
  router.push({ name: "settings-agent-detail", params: { agentId: id } });
}

function onDeleteAgent(agent) {
  const id = agentRecordId(agent);
  if (!id || deletingId.value === id) return;
  emit("delete", { id, agent });
}

function startRename(agent) {
  const id = agentRecordId(agent);
  renamingId.value = id;
  renameDraft.value = agentDisplayTitle(agent);
}

async function commitRename(agent) {
  const id = agentRecordId(agent);
  const name = String(renameDraft.value || "").trim();
  renamingId.value = "";
  if (!id || !name || name === agentDisplayTitle(agent)) return;
  try {
    await api.patchAgent(id, { display_name: name });
    await refreshAgents({ force: true });
  } catch (e) {
    agentStore.error = e.message;
  }
}

function setDeleting(id) {
  deletingId.value = id || "";
}

function openCreate() {
  openCreateAgent();
}

function openCreateWg() {
  createWgName.value = "";
  createWgError.value = "";
  createWgOpen.value = true;
}

async function submitCreateWg() {
  const name = createWgName.value.trim();
  if (!name || createWgBusy.value) return;
  createWgBusy.value = true;
  createWgError.value = "";
  try {
    const out = await api.createWorkgroup(name);
    const wg = out?.workgroup || out;
    const id = String(wg?.workgroup_id || out?.workgroup_id || out?.id || "").trim();
    createWgOpen.value = false;
    await refreshWorkgroups();
    if (id) await openWorkgroup(id);
  } catch (e) {
    createWgError.value = e?.message || "创建失败";
  } finally {
    createWgBusy.value = false;
  }
}

async function removeWorkgroup(wg) {
  const wid = String(wg?.workgroup_id || "").trim();
  if (!wid) return;
  const label = String(wg?.display_name || wid).trim();
  if (!window.confirm(`确定删除工作组「${label}」？\n将取消本机订阅，工作组数据仍保留在 Manage。`)) {
    return;
  }
  try {
    await api.unsubscribeWorkgroup(wid);
    const next = new Set(expanded.value);
    next.delete(wid);
    expanded.value = next;
    await refreshWorkgroups({ force: true });
    if (activeWorkgroupId.value === wid) {
      router.push({ name: "workgroups" });
    }
  } catch (e) {
    agentStore.error = e?.message || "删除工作组失败";
  }
}

function openCreateMember(wgId) {
  const wid = String(wgId || "").trim();
  if (!wid) return;
  emit("create-member", wid);
}

function openConfigureMember(wgId, memberId) {
  const wid = String(wgId || "").trim();
  const mid = String(memberId || "").trim();
  if (!wid || !mid) return;
  emit("configure-member", { workgroupId: wid, memberId: mid });
}

async function removeMember(wgId, member) {
  const wid = String(wgId || "").trim();
  const mid = String(member?.member_id || "").trim();
  if (!wid || !mid) return;
  const label = memberLabel(member);
  if (!window.confirm(`确定删除成员「${label}」？`)) return;
  try {
    await api.archiveWorkgroupMember(wid, mid);
    await loadMembers(wid, true);
  } catch (e) {
    agentStore.error = e?.message || "删除成员失败";
  }
}

function memberLabel(m) {
  return String(m?.display_name || m?.member_id || "成员").trim();
}

watch(
  () => route.params.workgroupId,
  (id) => {
    const wid = String(id || "").trim();
    if (!wid) return;
    const next = new Set(expanded.value);
    next.add(wid);
    expanded.value = next;
    void loadMembers(wid);
  },
  { immediate: true },
);

watch(
  () => [activeAgentId.value, props.terminalRevision],
  () => {
    void loadTerminals();
  },
  { immediate: true },
);

function onVisibilityChange() {
  if (document.visibilityState === "visible") {
    void refresh({ force: true });
  }
}

onMounted(() => {
  void refresh({ force: false });
  refreshTimer = window.setInterval(() => {
    void refresh({ force: true });
  }, RAIL_CACHE_TTL_MS);
  unreadRefreshTimer = window.setInterval(() => {
    void refreshWorkgroupUnread(workgroups.value);
  }, UNREAD_REFRESH_INTERVAL_MS);
  document.addEventListener("visibilitychange", onVisibilityChange);
});

onUnmounted(() => {
  if (refreshTimer !== null) {
    window.clearInterval(refreshTimer);
    refreshTimer = null;
  }
  if (unreadRefreshTimer !== null) {
    window.clearInterval(unreadRefreshTimer);
    unreadRefreshTimer = null;
  }
  document.removeEventListener("visibilitychange", onVisibilityChange);
});

defineExpose({
  refresh,
  setDeleting,
  openCreate,
  refreshAgents,
  refreshWorkgroups,
  loadMembers,
  expandSection,
  toggleSection,
  loadTerminals,
});
</script>

<template>
  <nav class="nav-rail" aria-label="智能体与工作组">
    <div class="nav-rail__scroll">
    <!-- Agents -->
    <section class="nav-rail__section">
      <header
        class="nav-rail__section-head"
        :class="{ 'nav-rail__section-head--actions-open': mobileActionOpen === 'agents' }"
      >
        <button
          type="button"
          class="nav-rail__section-toggle"
          :aria-expanded="sectionOpen.agents"
          @click="toggleSection('agents')"
        >
          <span class="nav-rail__section-icon" aria-hidden="true">
            <svg viewBox="0 0 16 16" width="14" height="14" fill="none">
              <circle cx="8" cy="5.2" r="2.2" stroke="currentColor" stroke-width="1.2" />
              <path d="M3.2 13.2c.6-2.4 2.4-3.6 4.8-3.6s4.2 1.2 4.8 3.6" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
            </svg>
          </span>
          <span class="nav-rail__section-title">智能体</span>
          <span v-if="sortedAgents.length" class="nav-rail__section-count">{{ sortedAgents.length }}</span>
          <span
            v-if="agentsLoadError && agentsLoaded"
            class="nav-rail__section-state nav-rail__section-state--error"
            title="智能体列表刷新失败，当前显示上次成功结果"
          >!</span>
          <span class="nav-rail__section-chevron" aria-hidden="true">{{ sectionOpen.agents ? "⌄" : "›" }}</span>
        </button>
        <div class="nav-rail__section-actions">
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-action"
          title="新建智能体"
          aria-label="新建智能体"
          @click.stop="mobileActionOpen = ''; openCreateAgent()"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <path
              d="M8 3.2v9.6M3.2 8h9.6"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            />
          </svg>
        </button>
        </div>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-collapse"
          :title="sectionOpen.agents ? '收起智能体' : '展开智能体'"
          :aria-label="sectionOpen.agents ? '收起智能体' : '展开智能体'"
          :aria-expanded="sectionOpen.agents"
          @click.stop="toggleSection('agents')"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" fill="none" aria-hidden="true">
            <path class="nav-rail__section-chevron-path" d="m5 6.5 3 3 3-3" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-more"
          title="更多操作"
          aria-label="更多操作"
          :aria-expanded="mobileActionOpen === 'agents'"
          @click.stop="toggleSectionActions('agents')"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <circle cx="4" cy="8" r="1" fill="currentColor" /><circle cx="8" cy="8" r="1" fill="currentColor" /><circle cx="12" cy="8" r="1" fill="currentColor" />
          </svg>
        </button>
      </header>

      <div v-if="sectionOpen.agents">
      <ul class="nav-rail__list" :aria-busy="loadingAgents">
        <li
          v-for="a in sortedAgents"
          :key="agentRecordId(a)"
          class="nav-rail__item"
          :class="{ 'nav-rail__item--active': agentRecordId(a) === activeAgentId }"
          @click="selectAgent(agentRecordId(a))"
        >
          <div class="nav-rail__item-main">
            <div class="nav-rail__item-title-row">
              <input
                v-if="renamingId === agentRecordId(a)"
                v-model="renameDraft"
                class="nav-rail__rename"
                @click.stop
                @keydown.enter.prevent="commitRename(a)"
                @keydown.esc.prevent="renamingId = ''"
                @blur="commitRename(a)"
              />
              <span
                v-else
                class="nav-rail__item-title"
                :title="agentDisplayTitle(a)"
                @dblclick.stop="startRename(a)"
              >{{ agentDisplayTitle(a) }}</span>
              <span
                v-if="a.has_unread"
                class="nav-rail__unread-dot"
                title="有未读消息"
                aria-label="有未读消息"
              ></span>
            </div>
          </div>
          <div class="nav-rail__item-trail">
            <span
              v-if="a.updated_at"
              class="nav-rail__time"
              :title="a.updated_at"
            >{{ formatCompactRelativeTime(a.updated_at) }}</span>
          </div>
          <div class="nav-rail__item-actions" @click.stop>
            <button
              type="button"
              class="nav-rail__icon-btn nav-rail__icon-btn--sm"
              title="智能体配置"
              aria-label="智能体配置"
              @click="openAgentSettings(a)"
            >
              <!-- 齿轮：与主题（显示器/日月）区分，贴近「配置」语义 -->
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" aria-hidden="true">
                <path
                  d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"
                  stroke="currentColor"
                  stroke-width="1.75"
                />
                <path
                  d="M19.4 13.5a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V20a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H4a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V4a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.91 1 1.51 1H20a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
                  stroke="currentColor"
                  stroke-width="1.75"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button
              type="button"
              class="nav-rail__icon-btn nav-rail__icon-btn--sm"
              title="重命名"
              @click="startRename(a)"
            >
              <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                <path
                  d="M3.5 12.5 6 12l6.2-6.2a1.4 1.4 0 0 0-2-2L4 10l-.5 2.5Z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.2"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button
              type="button"
              class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__icon-btn--danger"
              title="删除 Agent"
              :disabled="deletingId === agentRecordId(a)"
              @click="onDeleteAgent(a)"
            >
              <svg v-if="deletingId !== agentRecordId(a)" viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
              </svg>
              <span v-else>…</span>
            </button>
          </div>
        </li>
        <li v-if="!sortedAgents.length && !agentsLoaded && loadingAgents && !agentsLoadError" class="nav-rail__hint">加载中…</li>
        <li v-else-if="!sortedAgents.length && agentsLoadError" class="nav-rail__hint nav-rail__hint--error">
          <span>暂时无法加载智能体</span>
          <button
            type="button"
            class="nav-rail__retry"
            :class="{ 'nav-rail__icon-btn--spinning': manualRefreshingAgents }"
            :disabled="manualRefreshingAgents"
            @click="refreshAgents({ force: true, manual: true })"
          >重试</button>
        </li>
        <li v-else-if="!sortedAgents.length" class="nav-rail__empty">暂无智能体</li>
      </ul>
      </div>
    </section>

    <!-- Terminals -->
    <section v-if="activeAgentId" class="nav-rail__section">
      <header
        class="nav-rail__section-head"
        :class="{ 'nav-rail__section-head--actions-open': mobileActionOpen === 'terminals' }"
      >
        <button
          type="button"
          class="nav-rail__section-toggle"
          :aria-expanded="sectionOpen.terminals"
          @click="toggleSection('terminals')"
        >
          <span class="nav-rail__section-icon" aria-hidden="true">
            <svg viewBox="0 0 16 16" width="14" height="14" fill="none">
              <rect x="2.25" y="3" width="11.5" height="10" rx="1.5" stroke="currentColor" stroke-width="1.15" />
              <path d="m5 6 2 2-2 2M8.5 10h2.5" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </span>
          <span class="nav-rail__section-title">终端</span>
          <span v-if="terminals.length" class="nav-rail__section-count">{{ terminals.length }}</span>
          <span class="nav-rail__section-chevron" aria-hidden="true">{{ sectionOpen.terminals ? "⌄" : "›" }}</span>
        </button>
        <div class="nav-rail__section-actions">
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-action"
          :class="{ 'nav-rail__icon-btn--spinning': manualRefreshingTerminals }"
          title="刷新终端清单"
          aria-label="刷新终端清单"
          :disabled="manualRefreshingTerminals"
          @click.stop="mobileActionOpen = ''; loadTerminals({ manual: true })"
        >
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" aria-hidden="true">
            <path
              d="M17.65 6.35A7.98 7.98 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h7V4l-2.35 2.35Z"
              fill="currentColor"
            />
          </svg>
        </button>
        </div>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-collapse"
          :title="sectionOpen.terminals ? '收起终端' : '展开终端'"
          :aria-label="sectionOpen.terminals ? '收起终端' : '展开终端'"
          :aria-expanded="sectionOpen.terminals"
          @click.stop="toggleSection('terminals')"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" fill="none" aria-hidden="true">
            <path class="nav-rail__section-chevron-path" d="m5 6.5 3 3 3-3" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-more"
          title="更多操作"
          aria-label="更多操作"
          :aria-expanded="mobileActionOpen === 'terminals'"
          @click.stop="toggleSectionActions('terminals')"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <circle cx="4" cy="8" r="1" fill="currentColor" /><circle cx="8" cy="8" r="1" fill="currentColor" /><circle cx="12" cy="8" r="1" fill="currentColor" />
          </svg>
        </button>
      </header>
      <div v-if="sectionOpen.terminals">
        <p v-if="loadingTerminals && !terminals.length" class="nav-rail__hint">加载终端…</p>
        <p v-else-if="terminalError" class="nav-rail__error">{{ terminalError }}</p>
        <ul v-else class="nav-rail__list nav-rail__terminal-list">
          <li
            v-for="item in terminals"
            :key="item.terminal_id"
            class="nav-rail__item nav-rail__terminal-item"
            :class="{ 'nav-rail__item--active': item.terminal_id === props.selectedTerminalId }"
            :title="`${terminalLabel(item)} · ${item.terminal_id}`"
            @click="selectTerminal(item)"
          >
            <span class="nav-rail__terminal-mark" aria-hidden="true">
              <svg viewBox="0 0 16 16" width="14" height="14" fill="none">
                <path d="m3.5 4.5 2.5 2-2.5 2M7.5 9h4" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            <span class="nav-rail__terminal-main">
              <span class="nav-rail__terminal-title">{{ terminalLabel(item) }}</span>
              <span class="nav-rail__terminal-meta">
                <span class="nav-rail__terminal-status-dot" :class="`nav-rail__terminal-status-dot--${item.status}`"></span>
                <span class="nav-rail__terminal-meta-text">{{ terminalMeta(item) }}</span>
              </span>
            </span>
          </li>
          <li v-if="!terminals.length" class="nav-rail__empty">暂无打开的终端</li>
        </ul>
      </div>
    </section>

    <!-- Workgroups -->
    <section class="nav-rail__section">
      <header
        class="nav-rail__section-head"
        :class="{ 'nav-rail__section-head--actions-open': mobileActionOpen === 'workgroups' }"
      >
        <button
          type="button"
          class="nav-rail__section-toggle"
          :aria-expanded="sectionOpen.workgroups"
          @click="toggleSection('workgroups')"
        >
          <span class="nav-rail__section-icon" aria-hidden="true">
            <svg viewBox="0 0 16 16" width="14" height="14" fill="none">
              <circle cx="5.5" cy="5.5" r="2" stroke="currentColor" stroke-width="1.2" />
              <circle cx="10.5" cy="5.5" r="2" stroke="currentColor" stroke-width="1.2" />
              <path d="M2.4 13c.5-2 1.9-3 3.1-3h.4c.7 0 1.4.3 1.9.8M8.2 10.8c.5-.5 1.2-.8 1.9-.8h.4c1.2 0 2.6 1 3.1 3" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
            </svg>
          </span>
          <span class="nav-rail__section-title">工作组</span>
          <span v-if="workgroups.length" class="nav-rail__section-count">{{ workgroups.length }}</span>
          <span
            v-if="workgroupsLoadError && workgroupsLoaded"
            class="nav-rail__section-state nav-rail__section-state--error"
            title="工作组列表刷新失败，当前显示上次成功结果"
          >!</span>
          <span class="nav-rail__section-chevron" aria-hidden="true">{{ sectionOpen.workgroups ? "⌄" : "›" }}</span>
        </button>
        <div class="nav-rail__section-actions">
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-action"
          title="新建工作组"
          aria-label="新建工作组"
          @click.stop="mobileActionOpen = ''; openCreateWg()"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <path
              d="M8 3.2v9.6M3.2 8h9.6"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            />
          </svg>
        </button>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-action"
          :class="{ 'nav-rail__icon-btn--spinning': manualRefreshingWgs }"
          title="刷新工作组"
          aria-label="刷新工作组"
          :disabled="manualRefreshingWgs"
          @click.stop="mobileActionOpen = ''; refresh({ force: true, manual: true })"
        >
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" aria-hidden="true">
            <path
              d="M17.65 6.35A7.98 7.98 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h7V4l-2.35 2.35Z"
              fill="currentColor"
            />
          </svg>
        </button>
        </div>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-collapse"
          :title="sectionOpen.workgroups ? '收起工作组' : '展开工作组'"
          :aria-label="sectionOpen.workgroups ? '收起工作组' : '展开工作组'"
          :aria-expanded="sectionOpen.workgroups"
          @click.stop="toggleSection('workgroups')"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" fill="none" aria-hidden="true">
            <path class="nav-rail__section-chevron-path" d="m5 6.5 3 3 3-3" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-more"
          title="更多操作"
          aria-label="更多操作"
          :aria-expanded="mobileActionOpen === 'workgroups'"
          @click.stop="toggleSectionActions('workgroups')"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <circle cx="4" cy="8" r="1" fill="currentColor" /><circle cx="8" cy="8" r="1" fill="currentColor" /><circle cx="12" cy="8" r="1" fill="currentColor" />
          </svg>
        </button>
      </header>

      <div v-if="sectionOpen.workgroups">
      <form
        v-if="createWgOpen"
        class="nav-rail__popover"
        @submit.prevent="submitCreateWg"
      >
        <label class="nav-rail__popover-label" for="nav-rail-wg-name">新建工作组</label>
        <input
          id="nav-rail-wg-name"
          v-model="createWgName"
          class="nav-rail__popover-input"
          type="text"
          placeholder="显示名称"
          autofocus
          :disabled="createWgBusy"
          @keydown.esc.prevent="createWgOpen = false"
        />
        <p v-if="createWgError" class="nav-rail__popover-error">{{ createWgError }}</p>
        <div class="nav-rail__popover-actions">
          <button type="button" class="nav-rail__popover-btn" :disabled="createWgBusy" @click="createWgOpen = false">
            取消
          </button>
          <button
            type="submit"
            class="nav-rail__popover-btn nav-rail__popover-btn--primary"
            :disabled="createWgBusy || !createWgName.trim()"
          >
            创建
          </button>
        </div>
      </form>

      <ul class="nav-rail__list" :aria-busy="loadingWgs">
        <li
          v-for="wg in workgroups"
          :key="wg.workgroup_id"
          class="nav-rail__folder"
        >
          <div
            class="nav-rail__folder-row"
            :class="{ 'nav-rail__folder-row--active': wg.workgroup_id === activeWorkgroupId }"
            @click="onWorkgroupRowClick(wg.workgroup_id)"
          >
            <span class="nav-rail__item-title-row">
              <span class="nav-rail__item-title" :title="wg.display_name || wg.workgroup_id">
                {{ wg.display_name || wg.workgroup_id }}
              </span>
              <span
                v-if="hasWorkgroupUnread(wg.workgroup_id)"
                class="nav-rail__unread-dot"
                title="有未读消息"
                aria-label="有未读消息"
              ></span>
            </span>
            <span
              v-if="wg.status && wg.status !== 'active'"
              class="nav-rail__meta"
              :title="wg.status"
            >{{ wg.status === "configuring" ? "配置中" : wg.status }}</span>
            <span
              v-if="membersByWg[wg.workgroup_id]"
              class="nav-rail__member-count"
              :title="`成员数：${membersByWg[wg.workgroup_id].length}`"
            >{{ membersByWg[wg.workgroup_id].length }}</span>
            <div class="nav-rail__item-actions" @click.stop>
              <button
                type="button"
                class="nav-rail__icon-btn nav-rail__icon-btn--sm"
                title="添加成员"
                aria-label="添加成员"
                @click="openCreateMember(wg.workgroup_id)"
              >
                <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                  <path
                    d="M8 3.2v9.6M3.2 8h9.6"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.25"
                    stroke-linecap="round"
                  />
                </svg>
              </button>
              <button
                type="button"
                class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__icon-btn--danger"
                title="删除工作组"
                aria-label="删除工作组"
                @click="removeWorkgroup(wg)"
              >
                <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                  <path
                    d="M4.5 4.5l7 7M11.5 4.5l-7 7"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.3"
                    stroke-linecap="round"
                  />
                </svg>
              </button>
            </div>
          </div>

          <ul v-if="isExpanded(wg.workgroup_id)" class="nav-rail__children">
            <li v-if="membersLoading[wg.workgroup_id]" class="nav-rail__empty">加载成员…</li>
            <template v-else>
              <li
                v-for="m in membersByWg[wg.workgroup_id] || []"
                :key="m.member_id"
                class="nav-rail__item nav-rail__item--child"
                :title="`${memberLabel(m)} · ${m.status || ''}`"
                @click="openWorkgroup(wg.workgroup_id)"
              >
                <span class="nav-rail__member-mark" aria-hidden="true">
                  <svg viewBox="0 0 16 16" width="14" height="14" fill="none">
                    <rect x="4.25" y="4.25" width="7.5" height="7.5" rx="1.5" stroke="currentColor" stroke-width="1.15" />
                    <circle cx="8" cy="8" r="1.25" fill="currentColor" />
                    <path
                      d="M8 1.75v1.5M8 12.75v1.5M1.75 8h1.5M12.75 8h1.5"
                      stroke="currentColor"
                      stroke-width="1.15"
                      stroke-linecap="round"
                    />
                  </svg>
                </span>
                <span class="nav-rail__item-title">{{ memberLabel(m) }}</span>
                <span class="nav-rail__meta">{{ m.status }}</span>
                <div class="nav-rail__item-actions" @click.stop>
                  <button
                    type="button"
                    class="nav-rail__icon-btn nav-rail__icon-btn--sm"
                    title="配置成员"
                    aria-label="配置成员"
                    @click="openConfigureMember(wg.workgroup_id, m.member_id)"
                  >
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" aria-hidden="true">
                      <path
                        d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"
                        stroke="currentColor"
                        stroke-width="1.75"
                      />
                      <path
                        d="M19.4 13.5a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V20a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H4a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V4a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.91 1 1.51 1H20a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
                        stroke="currentColor"
                        stroke-width="1.75"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                    </svg>
                  </button>
                  <button
                    type="button"
                    class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__icon-btn--danger"
                    title="删除成员"
                    aria-label="删除成员"
                    @click="removeMember(wg.workgroup_id, m)"
                  >
                    <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                      <path
                        d="M4.5 4.5l7 7M11.5 4.5l-7 7"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.3"
                        stroke-linecap="round"
                      />
                    </svg>
                  </button>
                </div>
              </li>
              <li
                v-if="!(membersByWg[wg.workgroup_id] || []).length"
                class="nav-rail__empty"
              >
                暂无成员
              </li>
            </template>
          </ul>
        </li>
        <li v-if="!workgroups.length && !workgroupsLoaded && loadingWgs && !workgroupsLoadError" class="nav-rail__hint">加载中…</li>
        <li v-else-if="!workgroups.length && workgroupsLoadError" class="nav-rail__hint nav-rail__hint--error">
          <span>暂时无法加载工作组</span>
        </li>
        <li v-else-if="!workgroups.length" class="nav-rail__empty">暂无工作组</li>
      </ul>
      </div>
    </section>

    <!-- Activity -->
    <section class="nav-rail__section">
      <header class="nav-rail__section-head">
        <button
          type="button"
          class="nav-rail__section-toggle"
          :aria-expanded="sectionOpen.activity"
          @click="toggleSection('activity')"
        >
          <span class="nav-rail__section-icon" aria-hidden="true">
            <svg viewBox="0 0 16 16" width="14" height="14" fill="none">
              <path
                d="M3.5 3.5h9v2h-9v-2Zm0 3.5h9v2h-9V7Zm0 3.5h6V13h-6v-2.5Z"
                stroke="currentColor"
                stroke-width="1.15"
                stroke-linejoin="round"
              />
            </svg>
          </span>
          <span class="nav-rail__section-title">活动</span>
          <span v-if="activityBadge" class="nav-rail__section-count">{{ activityBadge }}</span>
          <span class="nav-rail__section-chevron" aria-hidden="true">{{ sectionOpen.activity ? "⌄" : "›" }}</span>
        </button>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-collapse"
          :title="sectionOpen.activity ? '收起活动' : '展开活动'"
          :aria-label="sectionOpen.activity ? '收起活动' : '展开活动'"
          :aria-expanded="sectionOpen.activity"
          @click.stop="toggleSection('activity')"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" fill="none" aria-hidden="true">
            <path class="nav-rail__section-chevron-path" d="m5 6.5 3 3 3-3" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </header>
      <ActivityPanel v-if="sectionOpen.activity" embedded />
    </section>
    </div>

    <footer class="nav-rail__footer">
      <div class="nav-rail__brand" :title="`DAgents · 本机智能助手 · 实时连接：${statusLabel}`">
        <img class="nav-rail__brand-mark" :src="brandIcon" width="18" height="18" alt="" aria-hidden="true" />
        <span class="nav-rail__brand-name">DAgents</span>
        <span class="nav-rail__dot" :class="statusClass" :aria-label="`实时连接：${statusLabel}`" />
      </div>
      <div class="nav-rail__footer-actions">
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__icon-btn--sm"
          :title="themeLabel"
          :aria-label="themeLabel"
          @click="onToggleTheme"
        >
          <svg v-if="themeStore.mode === 'system'" viewBox="0 0 16 16" width="14" height="14" fill="none" aria-hidden="true">
            <rect x="2.5" y="3.5" width="11" height="8" rx="1.2" stroke="currentColor" stroke-width="1.2" />
            <path d="M5.5 13.5h5" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
            <path d="M8 11.5v2" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
          </svg>
          <svg v-else-if="themeStore.resolved === 'dark'" viewBox="0 0 16 16" width="14" height="14" fill="none" aria-hidden="true">
            <path d="M10.9 2.3a5.8 5.8 0 1 0 2.8 10 5.9 5.9 0 0 1-2.8-10Z" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" />
          </svg>
          <svg v-else viewBox="0 0 16 16" width="14" height="14" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="2.1" stroke="currentColor" stroke-width="1.2" />
            <path d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.2 3.2l1.1 1.1M11.7 11.7l1.1 1.1M3.2 12.8l1.1-1.1M11.7 4.3l1.1-1.1" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
          </svg>
        </button>
        <router-link
          to="/settings/general"
          class="nav-rail__icon-btn nav-rail__icon-btn--sm"
          title="设置"
          aria-label="设置"
        >
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" aria-hidden="true">
            <path
              d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"
              stroke="currentColor"
              stroke-width="1.75"
            />
            <path
              d="M19.4 13.5a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V20a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H4a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V4a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.91 1 1.51 1H20a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
              stroke="currentColor"
              stroke-width="1.75"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </router-link>
      </div>
    </footer>
  </nav>
</template>
