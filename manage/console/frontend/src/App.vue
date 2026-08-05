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
import AppTopNav from "./components/AppTopNav.vue";
import AskAiButton from "./components/AskAiButton.vue";
import DetailDrawer from "./components/DetailDrawer.vue";
import LoginView from "./components/LoginView.vue";
import HomeDashboard from "./components/HomeDashboard.vue";
import MarketplaceView from "./components/MarketplaceView.vue";
import TemplatesView from "./components/TemplatesView.vue";
import PermissionsView from "./components/PermissionsView.vue";
import SettingsView from "./components/SettingsView.vue";
import WorkgroupView from "./components/WorkgroupView.vue";
import WorkgroupChatView from "./components/WorkgroupChatView.vue";
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

const view = ref("home");
const homeRef = ref(null);
const workgroupRef = ref(null);
const lastRefreshed = ref("—");
const drawerAgent = ref(null);
/** @type {import('vue').Ref<{ workgroupId: string, displayName: string } | null>} */
const chatSession = ref(null);
/** @type {import('vue').Ref<{ title?: string, trail?: string, subtitle?: string, showBack?: boolean } | null>} */
const workgroupPageMeta = ref(null);

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

const viewMeta = computed(() => {
  if (view.value === "chat" && chatSession.value?.displayName) {
    return {
      title: `对话 · ${chatSession.value.displayName}`,
      trail: "",
      subtitle: chatSession.value.workgroupId || "",
      showBack: false,
    };
  }
  if (view.value === "workgroup" && workgroupPageMeta.value) {
    return {
      title: workgroupPageMeta.value.title || "工作组",
      trail: workgroupPageMeta.value.trail || "",
      subtitle: workgroupPageMeta.value.subtitle || "",
      showBack: Boolean(workgroupPageMeta.value.showBack),
    };
  }
  const base = VIEW_META[view.value] || VIEW_META.home;
  return { title: base.title, trail: "", subtitle: base.subtitle || "", showBack: false };
});
const isHome = computed(() => view.value === "home");
const isChatPage = computed(() => view.value === "chat" && Boolean(chatSession.value?.workgroupId));
const shellVariant = computed(() => (isHome.value ? "home" : "app"));

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

function readChatLaunchFromUrl() {
  try {
    const params = new URLSearchParams(window.location.search);
    if (String(params.get("view") || "").trim() !== "chat") return null;
    const workgroupId = String(
      params.get("workgroup_id") || params.get("workgroupId") || "",
    ).trim();
    if (!workgroupId) return null;
    const displayName = String(
      params.get("name") || params.get("display_name") || workgroupId,
    ).trim();
    return { workgroupId, displayName: displayName || workgroupId };
  } catch {
    return null;
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

function applyDocumentTitle() {
  if (isChatPage.value) {
    document.title = `对话 · ${chatSession.value.displayName}`;
  } else {
    document.title = "DAgents Manage";
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

async function enterAppAfterAuth() {
  await refreshHealth();
  const chat = readChatLaunchFromUrl();
  if (chat) {
    chatSession.value = chat;
    view.value = "chat";
  } else {
    chatSession.value = null;
    view.value = "home";
  }
  lastRefreshed.value = touchLastRefreshedLabel();
  applyDocumentTitle();
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
          await enterAppAfterAuth();
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
      await enterAppAfterAuth();
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
    await enterAppAfterAuth();
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
    healthLabel.value = `${data.agents} nodes`;
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

function normalizeView(nextView) {
  if (nextView === "registry") return "nodes";
  if (nextView === "nodeadmin") return "settings";
  return nextView;
}

function navigate(nextView) {
  const target = normalizeView(nextView);
  if (isChatPage.value) {
    // 对话是独立浏览器页：导航到其它模块时在本页切换并清掉 chat 深链
    clearChatParamsFromUrl();
    chatSession.value = null;
  }
  if (target !== "workgroup") {
    workgroupPageMeta.value = null;
  }
  if (view.value === target) {
    applyDocumentTitle();
    return;
  }
  view.value = target;
  if (target === "nodes") {
    loadAgents();
  }
  applyDocumentTitle();
}

function onWorkgroupPageMeta(meta) {
  workgroupPageMeta.value = meta;
}

function backFromWorkgroupSettings() {
  workgroupRef.value?.backToGrid?.();
}

function buildWorkgroupChatUrl(workgroupId, displayName) {
  const url = new URL(window.location.href);
  url.search = "";
  url.hash = "";
  url.searchParams.set("view", "chat");
  url.searchParams.set("workgroup_id", workgroupId);
  url.searchParams.set("name", displayName);
  return url.toString();
}

function clearChatParamsFromUrl() {
  try {
    const url = new URL(window.location.href);
    ["view", "workgroup_id", "workgroupId", "name", "display_name"].forEach((key) => {
      url.searchParams.delete(key);
    });
    window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
  } catch {
    /* ignore */
  }
}

function openWorkgroupChat({ workgroupId, displayName }) {
  const id = String(workgroupId || "").trim();
  if (!id) return;
  const name = String(displayName || id).trim() || id;
  const href = buildWorkgroupChatUrl(id, name);
  window.open(href, "_blank", "noopener,noreferrer");
}

function closeWorkgroupChat() {
  clearChatParamsFromUrl();
  chatSession.value = null;
  window.close();
  // 浏览器若拦截 window.close（非脚本打开），回落到工作组页
  if (!window.closed) {
    view.value = "workgroup";
    applyDocumentTitle();
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
    <div class="app-shell" :class="[isHome ? 'app-shell--home' : 'app-shell--app', isChatPage ? 'app-shell--chat' : '']">
      <AppTopNav
        v-if="!isChatPage"
        :view="view"
        :variant="shellVariant"
        :health-label="healthLabel"
        :health-online="healthOnline"
        :session-label="sessionLabel"
        :last-refreshed="lastRefreshed"
        @navigate="navigate"
        @logout="onLogout"
      />

      <div class="main-area">
        <PageHeader
          v-if="!isHome && !isChatPage"
          :title="viewMeta.title"
          :trail="viewMeta.trail"
          :subtitle="viewMeta.subtitle"
        >
          <template v-if="viewMeta.showBack" #actions>
            <button type="button" class="btn btn-ghost" @click="backFromWorkgroupSettings">
              返回
            </button>
          </template>
        </PageHeader>

        <main class="page-content" :class="{ 'page-content--home': isHome, 'page-content--chat': isChatPage }">
          <HomeDashboard
            v-if="view === 'home'"
            ref="homeRef"
            :active="view === 'home'"
            @navigate="navigate"
            @toast="showToast($event.message, $event.type)"
            @refreshed="lastRefreshed = $event"
          />

          <WorkgroupView
            v-if="view === 'workgroup'"
            ref="workgroupRef"
            :active="view === 'workgroup'"
            @toast="showToast($event.message, $event.type)"
            @open-chat="openWorkgroupChat"
            @page-meta="onWorkgroupPageMeta"
          />

          <WorkgroupChatView
            v-if="isChatPage"
            :active="isChatPage"
            :workgroup-id="chatSession.workgroupId"
            :display-name="chatSession.displayName"
            @toast="showToast($event.message, $event.type)"
            @close="closeWorkgroupChat"
          />

          <TemplatesView
            v-if="view === 'templates'"
            :active="view === 'templates'"
          />

          <MarketplaceView
            v-if="view === 'marketplace'"
            :active="view === 'marketplace'"
            @toast="showToast($event.message, $event.type)"
          />

          <template v-if="view === 'nodes'">
            <StatsRow
              :online="stats.online"
              :offline="stats.offline"
              :total="stats.total"
            />
            <RegistryView
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
          </template>

          <PermissionsView
            v-if="view === 'permissions'"
            :active="view === 'permissions'"
          />

          <SettingsView
            v-if="view === 'settings'"
            :active="view === 'settings'"
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

    <AskAiButton v-if="!isChatPage" @toast="showToast($event.message, $event.type)" />
  </template>

  <ToastHost :toasts="toasts" />
</template>
