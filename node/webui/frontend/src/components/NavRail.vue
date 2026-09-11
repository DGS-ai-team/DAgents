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
import { hasWorkgroupUnread, noteWorkgroupTimeline } from "../stores/unread.js";
import AutoBadge from "./AutoBadge.vue";
import UiIcon from "./UiIcon.vue";
import { readNodePreference, writeNodePreference } from "../utils/nodePreference.js";

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

const emit = defineEmits([
  "switch",
  "create",
  "delete",
  "agents-updated",
  "create-member",
  "configure-member",
]);

const props = defineProps({
  // Agent chat and Workgroup pages own different EventSource connections.
  // The parent may provide the authoritative status for its current stream;
  // otherwise keep the existing Agent chat store as the fallback.
  realtimeStatus: {
    type: String,
    default: "",
  },
});

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
const workgroupsEnabled = ref(true);
const manualRefreshingAgents = ref(false);
const deletingId = ref("");
const renamingId = ref("");
const renameDraft = ref("");
const agentFilter = ref("all");
const agentGroupMode = ref("type");
const agentSearch = ref("");
const collapsedAgentGroups = ref(new Set());
const preferenceNodeId = ref("");
watch([agentFilter, agentGroupMode, collapsedAgentGroups], () => {
  if (preferenceNodeId.value) writeNodePreference(preferenceNodeId.value, "agent-view", { filter: agentFilter.value, group: agentGroupMode.value, collapsed: [...collapsedAgentGroups.value] });
}, { deep: true });
let preferenceRequest = 0;
async function loadNodePreferences() {
  const request = ++preferenceRequest;
  try {
    const boot = await api.getUIBootstrap();
    const id = String(boot?.info?.node_id || boot?.health?.node_id || boot?.info?.NodeID || "").trim();
    if (request !== preferenceRequest || !id) return;
    preferenceNodeId.value = id;
    agentFilter.value = "all";
    agentGroupMode.value = "type";
    agentSearch.value = "";
    collapsedAgentGroups.value = new Set();
    const saved = readNodePreference(id, "agent-view", null);
    if (saved) {
      if (["all", "auto", "normal"].includes(saved.filter)) agentFilter.value = saved.filter;
      if (["type", "workspace"].includes(saved.group)) agentGroupMode.value = saved.group;
      if (Array.isArray(saved.collapsed)) collapsedAgentGroups.value = new Set(saved.collapsed);
    }
  } catch { /* unknown Node: keep in-memory defaults */ }
}

/** 分区展开：智能体 / 工作组 */
const sectionOpen = ref({
  agents: true,
  workgroups: true,
  autonomous: true,
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

const activeWorkgroupId = computed(() =>
  route.name === "workgroups" ? String(route.params.workgroupId || "").trim() : "",
);
const effectiveRealtimeStatus = computed(() => props.realtimeStatus || chromeStore.sseStatus);
const online = computed(() => effectiveRealtimeStatus.value === "connected");
const statusClass = computed(() => {
  if (effectiveRealtimeStatus.value === "unselected") return "nav-rail__dot--unselected";
  if (online.value) return "nav-rail__dot--online";
  if (effectiveRealtimeStatus.value === "connecting") return "nav-rail__dot--connecting";
  return "nav-rail__dot--offline";
});
const statusLabel = computed(() => {
  if (effectiveRealtimeStatus.value === "unselected") return "未选择工作组";
  if (online.value) return "在线";
  if (effectiveRealtimeStatus.value === "connecting") return "连接中";
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
  const ts = Date.parse(agent?.last_active_at || agent?.LastActiveAt || agent?.updated_at || agent?.UpdatedAt || "");
  return Number.isFinite(ts) ? ts : 0;
}

const sortedAgents = computed(() => {
  return [...agents.value].sort((a, b) => agentSortTime(b) - agentSortTime(a) || agentRecordId(a).localeCompare(agentRecordId(b)));
});
const normalAgents = computed(() => sortedAgents.value.filter((agent) => String(agent?.agent_type || agent?.AgentType || "").toLowerCase() !== "auto"));
const autonomousAgents = computed(() => sortedAgents.value.filter((agent) => String(agent?.agent_type || agent?.AgentType || "").toLowerCase() === "auto"));

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
  } catch (error) {
    // Keep the last successful list during transient refresh failures and
    // expose the stale state in the section header instead of replacing rows.
    const message = String(error?.message || "");
    const disabled = /manage is not enabled|workgroup.*disabled|manage_disabled|workgroup_disabled/i.test(message);
    if (disabled) {
      workgroupsEnabled.value = false;
      // Do not let a cached successful response make a disabled feature
      // reappear when the rail is remounted within the cache window.
      railCache.workgroups = [];
      railCache.workgroupsFetchedAt = Date.now();
      workgroups.value = [];
      workgroupsLoaded.value = true;
      workgroupsLoadError.value = "";
    } else {
      workgroupsEnabled.value = true;
      workgroupsLoadError.value = "工作组列表暂时不可用";
    }
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

function onAgentKeydown(event, id) {
  if (event.target !== event.currentTarget) return;
  if (event.key !== "Enter" && event.key !== " ") return;
  event.preventDefault();
  selectAgent(id);
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

function onVisibilityChange() {
  if (document.visibilityState === "visible") {
    void refresh({ force: true });
  }
}

onMounted(() => {
  void loadNodePreferences();
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
  openCreateWg,
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
            <UiIcon name="bot" :size="16" />
          </span>
          <span class="nav-rail__section-title">智能体</span>
          <span v-if="normalAgents.length" class="nav-rail__section-count">{{ normalAgents.length }}</span>
          <span
            v-if="agentsLoadError && agentsLoaded"
            class="nav-rail__section-state nav-rail__section-state--error"
            title="智能体列表刷新失败，当前显示上次成功结果"
          >
            <UiIcon name="alert" :size="15" />
          </span>
        </button>
        <div class="nav-rail__section-actions">
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-action"
          title="新建智能体"
          aria-label="新建智能体"
          @click.stop="mobileActionOpen = ''; openCreateAgent()"
        >
          <UiIcon name="plus" :size="16" />
        </button>
        </div>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-more"
          title="更多操作"
          aria-label="更多操作"
          :aria-expanded="mobileActionOpen === 'agents'"
          @click.stop="toggleSectionActions('agents')"
        >
          <UiIcon name="more-horizontal" :size="16" />
        </button>
      </header>

      <div v-if="sectionOpen.agents">
      <ul class="nav-rail__list" :aria-busy="loadingAgents">
        <li
          v-for="a in normalAgents"
          :key="agentRecordId(a)"
          class="nav-rail__item nav-rail__agent-item"
          :class="{ 'nav-rail__item--active': agentRecordId(a) === agentStore.agentId }"
          tabindex="0"
          @keydown="onAgentKeydown($event, agentRecordId(a))"
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
              <AutoBadge :agent="a" />
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
              v-if="a.last_active_at"
              class="nav-rail__time"
              :title="a.last_active_at"
            >{{ formatCompactRelativeTime(a.last_active_at) }}</span>
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
              <UiIcon name="settings" :size="15" />
            </button>
            <button
              type="button"
              class="nav-rail__icon-btn nav-rail__icon-btn--sm"
              title="重命名"
              @click="startRename(a)"
            >
              <UiIcon name="pencil" :size="14" />
            </button>
            <button
              type="button"
              class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__icon-btn--danger"
              title="删除 Agent"
              :disabled="deletingId === agentRecordId(a)"
              @click="onDeleteAgent(a)"
            >
              <UiIcon v-if="deletingId !== agentRecordId(a)" name="trash" :size="14" />
              <span v-else>…</span>
            </button>
          </div>
        </li>
        <li v-if="!normalAgents.length && !agentsLoaded && loadingAgents && !agentsLoadError" class="nav-rail__hint">加载中…</li>
        <li v-else-if="!normalAgents.length && agentsLoadError" class="nav-rail__hint nav-rail__hint--error">
          <span>暂时无法加载智能体</span>
          <button
            type="button"
            class="nav-rail__retry"
            :class="{ 'nav-rail__icon-btn--spinning': manualRefreshingAgents }"
            :disabled="manualRefreshingAgents"
            @click="refreshAgents({ force: true, manual: true })"
          >重试</button>
        </li>
        <li v-else-if="!normalAgents.length" class="nav-rail__empty">暂无普通智能体</li>
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
            <UiIcon name="users" :size="16" />
          </span>
          <span class="nav-rail__section-title">工作组</span>
          <span v-if="workgroups.length" class="nav-rail__section-count">{{ workgroups.length }}</span>
          <span
            v-if="workgroupsLoadError && workgroupsLoaded"
            class="nav-rail__section-state nav-rail__section-state--error"
            title="工作组列表刷新失败，当前显示上次成功结果"
          >
            <UiIcon name="alert" :size="15" />
          </span>
        </button>
        <div class="nav-rail__section-actions">
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-action"
          title="新建工作组"
          aria-label="新建工作组"
          @click.stop="mobileActionOpen = ''; openCreateWg()"
        >
          <UiIcon name="plus" :size="16" />
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
          <UiIcon name="refresh-cw" :size="16" />
        </button>
        </div>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-more"
          title="更多操作"
          aria-label="更多操作"
          :aria-expanded="mobileActionOpen === 'workgroups'"
          @click.stop="toggleSectionActions('workgroups')"
        >
          <UiIcon name="more-horizontal" :size="16" />
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
                <UiIcon name="plus" :size="14" />
              </button>
              <button
                type="button"
                class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__icon-btn--danger"
                title="删除工作组"
                aria-label="删除工作组"
                @click="removeWorkgroup(wg)"
              >
                <UiIcon name="trash" :size="14" />
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
                  <UiIcon name="bot" :size="15" />
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
                    <UiIcon name="settings" :size="15" />
                  </button>
                  <button
                    type="button"
                    class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__icon-btn--danger"
                    title="删除成员"
                    aria-label="删除成员"
                    @click="removeMember(wg.workgroup_id, m)"
                  >
                    <UiIcon name="trash" :size="14" />
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

    <section class="nav-rail__section nav-rail__section--autonomous">
      <header
        class="nav-rail__section-head"
        :class="{ 'nav-rail__section-head--actions-open': mobileActionOpen === 'autonomous' }"
      >
        <button type="button" class="nav-rail__section-toggle" :aria-expanded="sectionOpen.autonomous" @click="toggleSection('autonomous')">
          <span class="nav-rail__section-icon nav-rail__section-icon--auto" aria-hidden="true">
            <UiIcon name="sparkles" :size="16" />
          </span>
          <span class="nav-rail__section-title">自主智能体</span>
          <span v-if="autonomousAgents.length" class="nav-rail__section-count">{{ autonomousAgents.length }}</span>
        </button>
        <div class="nav-rail__section-actions">
          <router-link
            :to="{ name: 'auto-overview' }"
            class="nav-rail__icon-btn nav-rail__section-action"
            title="Auto 总览"
            aria-label="Auto 总览"
            @click.stop="mobileActionOpen = ''"
          >
            <UiIcon name="receipt-text" :size="16" />
          </router-link>
        </div>
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__section-more"
          title="更多操作"
          aria-label="更多操作"
          :aria-expanded="mobileActionOpen === 'autonomous'"
          @click.stop="toggleSectionActions('autonomous')"
        >
          <UiIcon name="more-horizontal" :size="16" />
        </button>
      </header>
      <ul v-if="sectionOpen.autonomous" class="nav-rail__list" :aria-busy="loadingAgents">
        <li v-for="a in autonomousAgents" :key="agentRecordId(a)" class="nav-rail__item nav-rail__agent-item" :class="{ 'nav-rail__item--active': agentRecordId(a) === agentStore.agentId }" tabindex="0" @keydown="onAgentKeydown($event, agentRecordId(a))" @click="selectAgent(agentRecordId(a))">
          <div class="nav-rail__item-main">
            <div class="nav-rail__item-title-row">
              <input v-if="renamingId === agentRecordId(a)" v-model="renameDraft" class="nav-rail__rename" @click.stop @keydown.enter.prevent="commitRename(a)" @keydown.esc.prevent="renamingId = ''" @blur="commitRename(a)" />
              <span v-else class="nav-rail__item-title" :title="agentDisplayTitle(a)" @dblclick.stop="startRename(a)">{{ agentDisplayTitle(a) }}</span>
              <span v-if="a.has_unread" class="nav-rail__unread-dot" title="有未读消息" aria-label="有未读消息"></span>
            </div>
          </div>
          <div class="nav-rail__item-trail"><span v-if="a.last_active_at" class="nav-rail__time" :title="a.last_active_at">{{ formatCompactRelativeTime(a.last_active_at) }}</span></div>
          <div class="nav-rail__item-actions" @click.stop>
            <button type="button" class="nav-rail__icon-btn nav-rail__icon-btn--sm" title="智能体配置" aria-label="智能体配置" @click="openAgentSettings(a)">
              <UiIcon name="settings" :size="15" />
            </button>
            <button type="button" class="nav-rail__icon-btn nav-rail__icon-btn--sm" title="重命名" aria-label="重命名" @click="startRename(a)">
              <UiIcon name="pencil" :size="14" />
            </button>
            <button type="button" class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__icon-btn--danger" title="删除 Agent" aria-label="删除 Agent" :disabled="deletingId === agentRecordId(a)" @click="onDeleteAgent(a)">
              <UiIcon v-if="deletingId !== agentRecordId(a)" name="trash" :size="14" />
              <span v-else aria-hidden="true">…</span>
            </button>
          </div>
        </li>
        <li v-if="!autonomousAgents.length" class="nav-rail__empty">暂无自主智能体</li>
      </ul>
    </section>

    </div>

    <footer class="nav-rail__footer">
      <div class="nav-rail__brand" :title="`DAgents · 本机智能助手 · 实时事件：${statusLabel}`">
        <img class="nav-rail__brand-mark" :src="brandIcon" width="18" height="18" alt="" aria-hidden="true" />
        <span class="nav-rail__brand-name">DAgents</span>
        <span class="nav-rail__dot" :class="statusClass" :aria-label="`实时事件：${statusLabel}`" />
      </div>
      <div class="nav-rail__footer-actions">
        <button
          type="button"
          class="nav-rail__icon-btn nav-rail__icon-btn--sm"
          :title="themeLabel"
          :aria-label="themeLabel"
          @click="onToggleTheme"
        >
          <UiIcon v-if="themeStore.mode === 'system'" name="monitor" :size="16" aria-hidden="true" />
          <UiIcon v-else-if="themeStore.resolved === 'dark'" name="moon" :size="16" aria-hidden="true" />
          <UiIcon v-else name="sun" :size="16" aria-hidden="true" />
        </button>
        <router-link
          to="/settings/general"
          class="nav-rail__icon-btn nav-rail__icon-btn--sm"
          title="设置"
          aria-label="设置"
        >
          <UiIcon name="settings" :size="16" aria-hidden="true" />
        </router-link>
      </div>
    </footer>
  </nav>
</template>

<style scoped>
.nav-rail__agent-filters {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
  padding: 6px 10px 4px;
}
.nav-rail__agent-filter:first-child { grid-column: 1 / -1; }
.nav-rail__agent-filter {
  min-width: 0;
  border: 1px solid var(--color-border);
  border-radius: 5px;
  padding: 4px 5px;
  background: var(--color-surface);
  color: var(--color-text);
  font-size: 11px;
}
.nav-rail__agent-group { list-style: none; padding: 5px 10px 2px; }
.nav-rail__agent-group-toggle {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 6px;
  border: 0;
  padding: 3px 2px;
  background: transparent;
  color: var(--text-secondary, var(--color-text-muted));
  cursor: pointer;
  font-size: 11px;
  text-align: left;
}
.nav-rail__agent-group-count { margin-left: auto; }
.nav-rail__item-title-row { min-width: 0; }
@media (max-width: 720px) {
  .nav-rail__agent-filters { grid-template-columns: 1fr; }
}
</style>
