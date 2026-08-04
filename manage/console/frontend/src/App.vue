<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import {
  fetchAgents,
  fetchAuthMe,
  fetchHealth,
  loginAdmin,
  loginNode,
  logoutAuth,
} from "./api.js";
import AppSidebar from "./components/AppSidebar.vue";
import AskAiButton from "./components/AskAiButton.vue";
import DetailDrawer from "./components/DetailDrawer.vue";
import LoginView from "./components/LoginView.vue";
import NodeAdminView from "./components/NodeAdminView.vue";
import CasesView from "./components/CasesView.vue";
import WorkgroupView from "./components/WorkgroupView.vue";
import PageHeader from "./components/PageHeader.vue";
import RegistryView from "./components/RegistryView.vue";
import StatsRow from "./components/StatsRow.vue";
import ToastHost from "./components/ToastHost.vue";
import { useToast } from "./composables/useToast.js";
import { computeStats, touchLastRefreshedLabel, VIEW_META } from "./utils.js";

const bootstrapping = ref(true);
const authenticated = ref(false);
const authKind = ref(null);
const authSubject = ref("");
const authGroups = ref([]);
const defaultAdminUsername = ref("admin");
const loginBusy = ref(false);
const loginError = ref("");
const loginHint = ref("");

const view = ref("registry");
const refreshing = ref(false);
const lastRefreshed = ref("—");
const drawerAgent = ref(null);

const healthOnline = ref(false);
const healthLabel = ref("连接中…");

const stats = reactive({
  online: "—",
  offline: "—",
  total: "—",
});

const registry = reactive({
  page: 1,
  total: 0,
  pageSize: 50,
  agents: [],
  loading: false,
  error: "",
  filters: {
    group: "",
    team: "",
    status: "all",
    q: "",
    pageSize: 50,
  },
  roleHint: "",
  listSummary: "—",
});

const { toasts, showToast } = useToast();

const viewMeta = computed(() => VIEW_META[view.value] || VIEW_META.registry);

const sessionLabel = computed(() => {
  if (authKind.value === "admin") return `管理员 · ${authSubject.value || "admin"}`;
  if (authKind.value === "node") return `Node · ${authSubject.value || "—"}`;
  return "";
});

function readNodeIdFromUrl() {
  try {
    const params = new URLSearchParams(window.location.search);
    return String(params.get("node_id") || params.get("nodeId") || "").trim();
  } catch {
    return "";
  }
}

function clearNodeIdFromUrl() {
  try {
    const url = new URL(window.location.href);
    if (!url.searchParams.has("node_id") && !url.searchParams.has("nodeId")) return;
    url.searchParams.delete("node_id");
    url.searchParams.delete("nodeId");
    window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
  } catch {
    /* ignore */
  }
}

function applyAuth(me) {
  authenticated.value = Boolean(me?.authenticated);
  authKind.value = me?.kind || null;
  authSubject.value = me?.subject || me?.agent_id || "";
  authGroups.value = Array.isArray(me?.discovery_groups) ? [...me.discovery_groups] : [];
  if (me?.default_admin_username) {
    defaultAdminUsername.value = me.default_admin_username;
  }
  if (authenticated.value && authKind.value === "node") {
    const groups = authGroups.value.filter((g) => g && g !== "*");
    if (groups.length && !registry.filters.group) {
      registry.filters.group = groups[0];
    }
  }
}

async function enterHome() {
  await refreshHealth();
  await loadAgents();
}

async function bootstrapAuth() {
  bootstrapping.value = true;
  loginError.value = "";
  loginHint.value = "";
  try {
    const nodeId = readNodeIdFromUrl();
    if (nodeId) {
      try {
        const me = await loginNode(nodeId);
        applyAuth(me);
        clearNodeIdFromUrl();
        if (authenticated.value) {
          await enterHome();
          return;
        }
      } catch (err) {
        loginHint.value = `Node 身份登录失败（${err.message}）。可使用管理员账号登录。`;
        clearNodeIdFromUrl();
      }
    }

    const me = await fetchAuthMe();
    applyAuth(me);
    if (authenticated.value) {
      await enterHome();
      return;
    }
  } catch (err) {
    loginError.value = err.message || "无法连接 Manage";
  } finally {
    bootstrapping.value = false;
  }
}

async function onLoginSubmit({ username, password }) {
  loginBusy.value = true;
  loginError.value = "";
  try {
    const me = await loginAdmin({ username, password });
    applyAuth(me);
    await enterHome();
  } catch (err) {
    loginError.value = err.message || "登录失败";
  } finally {
    loginBusy.value = false;
  }
}

async function onLogout() {
  try {
    await logoutAuth();
  } catch {
    /* ignore */
  }
  authenticated.value = false;
  authKind.value = null;
  authSubject.value = "";
  authGroups.value = [];
  loginHint.value = "";
  loginError.value = "";
}

async function refreshHealth() {
  try {
    const data = await fetchHealth();
    healthOnline.value = true;
    const who = sessionLabel.value ? ` · ${sessionLabel.value}` : "";
    healthLabel.value = `Manage · ${data.agents} nodes${who}`;
  } catch {
    healthOnline.value = false;
    healthLabel.value = "Manage 不可达";
  }
}

async function loadStatsSnapshot(group) {
  try {
    const params = { status: "all", page: 1, page_size: 200 };
    if (group) params.discovery_group = group;
    const data = await fetchAgents(params);
    Object.assign(stats, computeStats(data.agents || []));
  } catch {
    Object.assign(stats, computeStats(registry.agents));
  }
}

async function loadAgents() {
  registry.loading = true;
  registry.error = "";
  registry.pageSize = registry.filters.pageSize || 50;
  const group = registry.filters.group.trim();
  const params = {
    team: registry.filters.team.trim(),
    status: registry.filters.status,
    q: registry.filters.q.trim(),
    page: registry.page,
    page_size: registry.pageSize,
  };
  if (group) params.discovery_group = group;
  try {
    const data = await fetchAgents(params);
    registry.agents = data.agents || [];
    registry.total = data.total ?? registry.agents.length;
    registry.page = data.page || registry.page;
    registry.roleHint = group ? `分组：${group}` : "全部 Node";
    registry.listSummary = `本页 ${registry.agents.length} 条，合计 ${registry.total} 条`;
    await loadStatsSnapshot(group);
    lastRefreshed.value = touchLastRefreshedLabel();
  } catch (err) {
    registry.error = err.message;
    showToast(err.message, "error");
  } finally {
    registry.loading = false;
  }
}

function navigate(nextView) {
  if (view.value === nextView) return;
  view.value = nextView;
  if (nextView === "registry") {
    loadAgents();
  }
}

async function onRefresh() {
  refreshing.value = true;
  await refreshHealth();
  try {
    if (view.value === "registry") {
      registry.page = 1;
      await loadAgents();
    }
  } finally {
    refreshing.value = false;
  }
}

function onRegistryFilterChange(filters) {
  registry.filters = { ...filters };
  registry.pageSize = filters.pageSize || 50;
  registry.page = 1;
  loadAgents();
}

function registryPrevPage() {
  if (registry.page > 1) {
    registry.page -= 1;
    loadAgents();
  }
}

function registryNextPage() {
  const totalPages = Math.max(1, Math.ceil(registry.total / registry.pageSize));
  if (registry.page < totalPages) {
    registry.page += 1;
    loadAgents();
  }
}

function onDrawerGroupsSaved({ agentId, updated }) {
  showToast(`${agentId} 分组已更新`, "success");
  const idx = registry.agents.findIndex((a) => a.agent_id === agentId);
  if (idx >= 0) {
    registry.agents[idx] = { ...registry.agents[idx], ...updated };
  }
  if (drawerAgent.value?.agent_id === agentId) {
    drawerAgent.value = { ...drawerAgent.value, ...updated };
  }
  loadAgents();
}

onMounted(() => {
  void bootstrapAuth();
});
</script>

<template>
  <div v-if="bootstrapping" class="boot-screen">正在确认登录状态…</div>

  <LoginView
    v-else-if="!authenticated"
    :default-username="defaultAdminUsername"
    :busy="loginBusy"
    :error="loginError"
    :hint="loginHint"
    @submit="onLoginSubmit"
  />

  <template v-else>
    <div class="app-shell">
      <AppSidebar
        :view="view"
        :health-label="healthLabel"
        :health-online="healthOnline"
        :session-label="sessionLabel"
        @navigate="navigate"
        @logout="onLogout"
      />

      <div class="main-area">
        <PageHeader
          :breadcrumb="viewMeta.title"
          :title="viewMeta.title"
          :subtitle="viewMeta.subtitle"
          :last-refreshed="lastRefreshed"
          :refreshing="refreshing"
          @refresh="onRefresh"
        />

        <main class="page-content">
          <StatsRow
            v-show="view === 'registry'"
            :online="stats.online"
            :offline="stats.offline"
            :total="stats.total"
          />

          <RegistryView
            v-show="view === 'registry'"
            :agents="registry.agents"
            :loading="registry.loading"
            :error="registry.error"
            :page="registry.page"
            :total="registry.total"
            :page-size="registry.pageSize"
            :filters="registry.filters"
            :role-hint="registry.roleHint"
            :list-summary="registry.listSummary"
            @open="drawerAgent = $event"
            @filter-change="onRegistryFilterChange"
            @page-prev="registryPrevPage"
            @page-next="registryNextPage"
          />

          <NodeAdminView
            v-if="view === 'nodeadmin'"
            :active="view === 'nodeadmin'"
            @toast="showToast($event.message, $event.type)"
          />

          <CasesView
            v-if="view === 'cases'"
            :active="view === 'cases'"
            @toast="showToast($event.message, $event.type)"
          />

          <WorkgroupView
            v-if="view === 'workgroup'"
            :active="view === 'workgroup'"
            @toast="showToast($event.message, $event.type)"
          />
        </main>
      </div>
    </div>

    <DetailDrawer
      :agent="drawerAgent"
      @close="drawerAgent = null"
      @groups-saved="onDrawerGroupsSaved"
    />

    <AskAiButton @toast="showToast($event.message, $event.type)" />
  </template>

  <ToastHost :toasts="toasts" />
</template>
