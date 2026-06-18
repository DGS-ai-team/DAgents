<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import { fetchAgents, fetchHealth, fetchInboxTasks } from "./api.js";
import AgentBar from "./components/AgentBar.vue";
import AppSidebar from "./components/AppSidebar.vue";
import BulkGroupsPanel from "./components/BulkGroupsPanel.vue";
import DetailDrawer from "./components/DetailDrawer.vue";
import InboxView from "./components/InboxView.vue";
import LLMView from "./components/LLMView.vue";
import PageHeader from "./components/PageHeader.vue";
import RegistryView from "./components/RegistryView.vue";
import SkillsView from "./components/SkillsView.vue";
import StatsRow from "./components/StatsRow.vue";
import ToastHost from "./components/ToastHost.vue";
import { useToast } from "./composables/useToast.js";
import { computeStats, touchLastRefreshedLabel, VIEW_META } from "./utils.js";

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
  peers: "—",
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

const inbox = reactive({
  page: 1,
  total: 0,
  pageSize: 50,
  tasks: [],
  loading: false,
  error: "",
  filters: {
    to: "",
    from: "",
    status: "",
    pageSize: 50,
  },
  summary: "—",
});

const { toasts, showToast } = useToast();

const viewMeta = computed(() => VIEW_META[view.value] || VIEW_META.registry);

async function refreshHealth() {
  try {
    const data = await fetchHealth();
    healthOnline.value = true;
    healthLabel.value = `Manage · ${data.agents} nodes`;
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

async function loadInbox() {
  inbox.loading = true;
  inbox.error = "";
  inbox.pageSize = inbox.filters.pageSize || 50;
  const offset = (inbox.page - 1) * inbox.pageSize;
  const params = {
    limit: inbox.pageSize,
    offset,
    to_agent_id: inbox.filters.to.trim(),
    from_agent_id: inbox.filters.from.trim(),
    status: inbox.filters.status,
  };

  try {
    const data = await fetchInboxTasks(params);
    inbox.tasks = data.tasks || [];
    inbox.total = data.total ?? inbox.tasks.length;
    inbox.summary = `本页 ${inbox.tasks.length} 条，合计 ${inbox.total} 条`;
    lastRefreshed.value = touchLastRefreshedLabel();
  } catch (err) {
    inbox.error = err.message;
    showToast(err.message, "error");
  } finally {
    inbox.loading = false;
  }
}

function navigate(nextView) {
  if (view.value === nextView) return;
  view.value = nextView;
  if (nextView === "inbox") {
    inbox.page = 1;
    loadInbox();
  } else if (nextView === "registry") {
    loadAgents();
  }
  // llm / skills 视图自加载（见各组件的 active watch）
}

async function onRefresh() {
  refreshing.value = true;
  await refreshHealth();
  try {
    if (view.value === "registry") {
      registry.page = 1;
      await loadAgents();
    } else if (view.value === "inbox") {
      inbox.page = 1;
      await loadInbox();
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

function onInboxFilterChange(filters) {
  inbox.filters = { ...filters };
  inbox.pageSize = filters.pageSize || 50;
  inbox.page = 1;
  loadInbox();
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

function inboxPrevPage() {
  if (inbox.page > 1) {
    inbox.page -= 1;
    loadInbox();
  }
}

function inboxNextPage() {
  const totalPages = Math.max(1, Math.ceil(inbox.total / inbox.pageSize));
  if (inbox.page < totalPages) {
    inbox.page += 1;
    loadInbox();
  }
}

function onGroupsSaved(count) {
  showToast(`已为 ${count} 个 Node 分配分组`, "success");
  loadAgents();
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

onMounted(async () => {
  await refreshHealth();
  await loadAgents();
});
</script>

<template>
  <div class="app-shell">
    <AppSidebar
      :view="view"
      :health-label="healthLabel"
      :health-online="healthOnline"
      @navigate="navigate"
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
        <AgentBar @toast="showToast($event.message, $event.type)" />

        <StatsRow
          v-show="view === 'registry' || view === 'inbox'"
          :online="stats.online"
          :offline="stats.offline"
          :total="stats.total"
          :peers="stats.peers"
        />

        <BulkGroupsPanel
          v-show="view === 'registry'"
          :agents="registry.agents"
          @saved="onGroupsSaved"
          @error="showToast($event, 'error')"
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

        <InboxView
          v-show="view === 'inbox'"
          :tasks="inbox.tasks"
          :loading="inbox.loading"
          :error="inbox.error"
          :page="inbox.page"
          :total="inbox.total"
          :page-size="inbox.pageSize"
          :filters="inbox.filters"
          :summary="inbox.summary"
          @filter-change="onInboxFilterChange"
          @page-prev="inboxPrevPage"
          @page-next="inboxNextPage"
        />

        <LLMView
          v-show="view === 'llm'"
          :active="view === 'llm'"
          @toast="showToast($event.message, $event.type)"
        />

        <SkillsView
          v-show="view === 'skills'"
          :active="view === 'skills'"
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

  <ToastHost :toasts="toasts" />
</template>
