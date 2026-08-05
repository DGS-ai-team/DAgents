<script setup>
import { ref, onMounted, computed, watch } from "vue";
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
import brandIcon from "../assets/brand-icon.png";
import ActivityPanel from "./ActivityPanel.vue";
import { transcriptStore } from "../stores/transcript.js";
import { deriveActivityFromTranscript } from "../utils/workspaceActivity.js";

const emit = defineEmits([
  "switch",
  "create",
  "delete",
  "agents-updated",
]);

const route = useRoute();
const router = useRouter();

const agents = ref([]);
const workgroups = ref([]);
const loadingAgents = ref(false);
const loadingWgs = ref(false);
const deletingId = ref("");
const renamingId = ref("");
const renameDraft = ref("");

/** 分区展开：智能体 / 工作组 / 活动 */
const sectionOpen = ref({
  agents: true,
  workgroups: true,
  activity: true,
});

function toggleSection(key) {
  sectionOpen.value = {
    ...sectionOpen.value,
    [key]: !sectionOpen.value[key],
  };
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

const activitySnap = computed(() => deriveActivityFromTranscript(transcriptStore.entries));
const activityBadge = computed(() => {
  const n = (activitySnap.value.file_count || 0) + (activitySnap.value.command_count || 0);
  return n > 0 ? n : 0;
});

const activeAgentId = computed(() => String(agentStore.agentId || "").trim());
const activeWorkgroupId = computed(() =>
  route.name === "workgroups" ? String(route.params.workgroupId || "").trim() : "",
);
const activeMemberId = computed(() => String(route.query.member || "").trim());

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

async function refreshAgents() {
  loadingAgents.value = true;
  try {
    const res = await api.listAgents();
    agents.value = res.agents || [];
  } catch {
    agents.value = [];
  } finally {
    loadingAgents.value = false;
    emit("agents-updated", agents.value.slice());
  }
}

async function refreshWorkgroups() {
  loadingWgs.value = true;
  try {
    const res = await api.listWorkgroups({ scope: "subscribed" });
    workgroups.value = res.workgroups || [];
  } catch {
    workgroups.value = [];
  } finally {
    loadingWgs.value = false;
  }
}

async function refresh() {
  await Promise.all([refreshAgents(), refreshWorkgroups()]);
  // 已展开的工作组刷新成员
  await Promise.all(
    [...expanded.value].map((id) => loadMembers(id, true)),
  );
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
    await refreshAgents();
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
    const id = out?.workgroup_id || out?.id || "";
    createWgOpen.value = false;
    await refreshWorkgroups();
    if (id) await openWorkgroup(id);
  } catch (e) {
    createWgError.value = e?.message || "创建失败";
  } finally {
    createWgBusy.value = false;
  }
}

function addMember(wgId) {
  openWorkgroup(wgId, { addMember: "1" });
}

function editMember(wgId, memberId) {
  openWorkgroup(wgId, { member: memberId });
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

onMounted(refresh);

defineExpose({
  refresh,
  setDeleting,
  openCreate,
  refreshAgents,
  refreshWorkgroups,
  expandSection,
  toggleSection,
});
</script>

<template>
  <nav class="nav-rail" aria-label="智能体与工作组">
    <div class="nav-rail__scroll">
    <!-- Agents -->
    <section class="nav-rail__section">
      <header class="nav-rail__section-head">
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
        </button>
        <button
          type="button"
          class="nav-rail__icon-btn"
          title="新建智能体"
          aria-label="新建智能体"
          @click.stop="openCreateAgent"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <path
              d="M3.2 4.2h7.2a1.2 1.2 0 0 1 1.2 1.2v6.2a1.2 1.2 0 0 1-1.2 1.2H3.2A1.2 1.2 0 0 1 2 11.6V5.4a1.2 1.2 0 0 1 1.2-1.2Z"
              fill="none"
              stroke="currentColor"
              stroke-width="1.2"
            />
            <path
              d="M5 4.2V3.4A1.4 1.4 0 0 1 6.4 2h1.8A1.4 1.4 0 0 1 9.6 3.4v.8"
              fill="none"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
            />
            <path
              d="M12.6 9.2v3.2M11 10.8h3.2"
              fill="none"
              stroke="currentColor"
              stroke-width="1.25"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </header>

      <div v-if="sectionOpen.agents">
      <div v-if="loadingAgents" class="nav-rail__hint">加载中…</div>
      <ul v-else class="nav-rail__list">
        <li
          v-for="a in sortedAgents"
          :key="agentRecordId(a)"
          class="nav-rail__item"
          :class="{ 'nav-rail__item--active': agentRecordId(a) === activeAgentId }"
          @click="selectAgent(agentRecordId(a))"
        >
          <div class="nav-rail__item-main">
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
        <li v-if="!sortedAgents.length" class="nav-rail__empty">暂无智能体</li>
      </ul>
      </div>
    </section>

    <!-- Workgroups -->
    <section class="nav-rail__section">
      <header class="nav-rail__section-head">
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
        </button>
        <button
          type="button"
          class="nav-rail__icon-btn"
          title="新建工作组"
          aria-label="新建工作组"
          @click.stop="openCreateWg"
        >
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <path
              d="M3.2 4.2h7.2a1.2 1.2 0 0 1 1.2 1.2v6.2a1.2 1.2 0 0 1-1.2 1.2H3.2A1.2 1.2 0 0 1 2 11.6V5.4a1.2 1.2 0 0 1 1.2-1.2Z"
              fill="none"
              stroke="currentColor"
              stroke-width="1.2"
            />
            <path
              d="M5 4.2V3.4A1.4 1.4 0 0 1 6.4 2h1.8A1.4 1.4 0 0 1 9.6 3.4v.8"
              fill="none"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
            />
            <path
              d="M12.6 9.2v3.2M11 10.8h3.2"
              fill="none"
              stroke="currentColor"
              stroke-width="1.25"
              stroke-linecap="round"
            />
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

      <div v-if="loadingWgs" class="nav-rail__hint">加载中…</div>
      <ul v-else class="nav-rail__list">
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
            <button
              type="button"
              class="nav-rail__fold-toggle"
              :title="isExpanded(wg.workgroup_id) ? '折叠成员' : '展开成员'"
              :aria-expanded="isExpanded(wg.workgroup_id)"
              @click.stop="toggleWorkgroup(wg.workgroup_id)"
            >
              <svg
                v-if="isExpanded(wg.workgroup_id)"
                viewBox="0 0 16 16"
                width="14"
                height="14"
                aria-hidden="true"
              >
                <path
                  d="M2.5 5.2h4.2l1.2 1.4H13.5a.9.9 0 0 1 .9.9v4.4a.9.9 0 0 1-.9.9H2.5a.9.9 0 0 1-.9-.9V6.1a.9.9 0 0 1 .9-.9Z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.2"
                  stroke-linejoin="round"
                />
              </svg>
              <svg v-else viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <path
                  d="M2.5 4.5h4l1.3 1.5H13.5a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1H2.5a1 1 0 0 1-1-1V5.5a1 1 0 0 1 1-1Z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.2"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <span class="nav-rail__item-title" :title="wg.display_name || wg.workgroup_id">
              {{ wg.display_name || wg.workgroup_id }}
            </span>
            <button
              type="button"
              class="nav-rail__icon-btn nav-rail__icon-btn--sm nav-rail__folder-add"
              title="新增成员 Agent"
              aria-label="新增成员 Agent"
              @click.stop="addMember(wg.workgroup_id)"
            >
              <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                <path d="M8 3.2v9.6M3.2 8h9.6" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" />
              </svg>
            </button>
          </div>

          <ul v-if="isExpanded(wg.workgroup_id)" class="nav-rail__children">
            <li v-if="membersLoading[wg.workgroup_id]" class="nav-rail__empty">加载成员…</li>
            <template v-else>
              <li
                v-for="m in membersByWg[wg.workgroup_id] || []"
                :key="m.member_id"
                class="nav-rail__item nav-rail__item--child"
                :class="{
                  'nav-rail__item--active':
                    wg.workgroup_id === activeWorkgroupId && m.member_id === activeMemberId,
                }"
                :title="`${memberLabel(m)} · ${m.status || ''}`"
                @click="editMember(wg.workgroup_id, m.member_id)"
              >
                <span class="nav-rail__item-title">{{ memberLabel(m) }}</span>
                <span class="nav-rail__meta">{{ m.status }}</span>
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
        <li v-if="!workgroups.length" class="nav-rail__empty">暂无工作组</li>
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
