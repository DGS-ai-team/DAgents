<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import * as api from "../api/node.js";
import { onConfigurationChanged } from "../utils/configurationEvents.js";
import SettingsSectionHeader from "./SettingsSectionHeader.vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({ agentId: { type: String, required: true } });
const emit = defineEmits(["changed"]);
const router = useRouter();

const loading = ref(false);
const saving = ref(false);
const refreshing = ref("");
const error = ref("");
const status = ref("");
const servers = ref([]);
const selected = reactive({});
const allowlists = reactive({});
const savedSelection = ref("");

const enabledServers = computed(() => servers.value.filter((server) => server.enabled));
const selectionDirty = computed(() => Boolean(savedSelection.value) && selectionSnapshot() !== savedSelection.value);

function serverTools(server) {
  return Array.isArray(server?.tools) ? server.tools.filter((tool) => tool.enabled === true) : [];
}

function reconcileServerSelection(server) {
  if (!server?.id) return;
  const enabledNames = new Set(serverTools(server).map((tool) => tool.name));
  const current = Array.isArray(allowlists[server.id]) ? allowlists[server.id] : [];
  allowlists[server.id] = current.filter((name) => enabledNames.has(name));
}

function selectionSnapshot() {
  return JSON.stringify(servers.value.map((server) => ({
    id: String(server.id || ""),
    enabled: Boolean(selected[server.id]),
    tools: [...(allowlists[server.id] || [])].sort(),
  })));
}

function syncSelection(bindings = [], preserved = null) {
  const byId = new Map((Array.isArray(bindings) ? bindings : []).map((item) => [String(item.server_id || ""), item]));
  for (const server of servers.value) {
    const id = String(server.id || "");
    const binding = byId.get(id);
    const keepSelection = preserved && Object.prototype.hasOwnProperty.call(preserved.selected, id);
    selected[id] = keepSelection ? preserved.selected[id] : !!binding?.enabled;
    const enabledNames = serverTools(server).map((tool) => tool.name);
    const keepAllowlist = preserved && Object.prototype.hasOwnProperty.call(preserved.allowlists, id);
    allowlists[id] = keepAllowlist
      ? [...preserved.allowlists[id]]
      : Array.isArray(binding?.tool_allowlist)
        ? binding.tool_allowlist.filter((name) => enabledNames.includes(name))
        : [];
  }
}

async function load({ preserveDraft = selectionDirty.value } = {}) {
  loading.value = true;
  error.value = "";
  if (!preserveDraft) status.value = "";
  try {
    const [serverData, bindingData] = await Promise.all([
      api.listMcpServers(),
      api.getAgentMcp(props.agentId),
    ]);
    const preserveCurrentDraft = preserveDraft || selectionDirty.value;
    const preserved = preserveCurrentDraft
      ? {
          selected: { ...selected },
          allowlists: Object.fromEntries(Object.entries(allowlists).map(([id, names]) => [id, [...names]])),
        }
      : null;
    servers.value = Array.isArray(serverData?.servers) ? serverData.servers : [];
    syncSelection(bindingData?.bindings, preserved);
    if (preserveCurrentDraft) {
      status.value = "MCP 配置已更新；未保存的绑定草稿已保留。";
    } else {
      savedSelection.value = selectionSnapshot();
    }
  } catch (e) {
    error.value = e.message || "加载 MCP 配置失败";
  } finally {
    loading.value = false;
  }
}

async function refresh(server) {
  const id = String(server?.id || "").trim();
  if (!id || refreshing.value) return;
  refreshing.value = id;
  error.value = "";
  try {
    const updated = await api.refreshMcpServer(id);
    const index = servers.value.findIndex((item) => item.id === id);
    if (index >= 0) servers.value[index] = updated;
    reconcileServerSelection(updated);
    emit("changed", { kind: "mcp-catalog", serverId: id });
    status.value = `${server.display_name || id} 工具目录已刷新`;
  } catch (e) {
    error.value = e.message || "刷新 MCP 工具失败";
  } finally {
    refreshing.value = "";
  }
}

function toggleTool(serverId, toolName) {
  const server = servers.value.find((item) => item.id === serverId);
  const allNames = serverTools(server).map((tool) => tool.name);
  const configured = Array.isArray(allowlists[serverId]) ? allowlists[serverId] : [];
  const current = new Set(configured.length ? configured : allNames);
  if (current.has(toolName)) current.delete(toolName);
  else current.add(toolName);
  const next = [...current];
  allowlists[serverId] = next.length === allNames.length ? [] : next;
}

function isToolSelected(serverId, toolName) {
  const list = allowlists[serverId];
  return !Array.isArray(list) || list.length === 0 || list.includes(toolName);
}

async function save() {
  saving.value = true;
  error.value = "";
  status.value = "";
  try {
    const bindings = enabledServers.value
      .filter((server) => selected[server.id])
      .map((server) => ({
        server_id: server.id,
        enabled: true,
        tool_allowlist: Array.isArray(allowlists[server.id]) ? allowlists[server.id] : [],
      }));
    await api.putAgentMcp(props.agentId, bindings);
    emit("changed", { kind: "mcp-binding" });
    savedSelection.value = selectionSnapshot();
    status.value = "MCP 已保存并应用";
  } catch (e) {
    error.value = e.message || "保存 MCP 配置失败";
  } finally {
    saving.value = false;
  }
}

watch(() => props.agentId, () => void load());
let stopConfigurationEvents = () => {};
onMounted(() => {
  void load();
  stopConfigurationEvents = onConfigurationChanged((change) => {
    if (change?.kind === "mcp" || change?.kind === "mcp-catalog") {
      void load({ preserveDraft: selectionDirty.value });
    }
  });
});
onUnmounted(() => stopConfigurationEvents());

function openGlobalMcpSettings() {
  void router.push({
    path: "/settings/mcp",
    query: { returnAgentId: props.agentId, returnSection: "resources", create: "1" },
  });
}
</script>

<template>
  <section class="mcp-panel">
    <SettingsSectionHeader
      :level="3"
      title="MCP 工具"
      description="仅展示服务侧已启用的工具；Agent 绑定后才会进入当前智能体的工具、说明和权限列表。"
    >
      <template #actions>
        <button type="button" class="btn btn--primary btn--sm" :disabled="loading || saving" @click="save">
          {{ saving ? "保存中…" : "保存" }}
        </button>
      </template>
    </SettingsSectionHeader>
    <p v-if="loading" class="mcp-panel__muted">加载中…</p>
    <p v-else-if="error" class="mcp-panel__error">{{ error }}</p>
    <div v-else-if="!servers.length" class="mcp-panel__empty">
      <p class="mcp-panel__muted">还没有全局 MCP 服务。添加服务后，还需在此处单独授权给当前智能体。</p>
      <button type="button" class="btn btn--ghost btn--sm" @click="openGlobalMcpSettings">添加全局 MCP 服务</button>
    </div>
    <div v-else class="mcp-panel__list">
      <article v-for="server in servers" :key="server.id" class="mcp-panel__server">
        <div class="mcp-panel__server-head">
          <label class="mcp-panel__server-toggle">
            <input v-model="selected[server.id]" class="settings-switch-input" type="checkbox" />
            <span>{{ server.display_name || server.id }}</span>
          </label>
          <span class="mcp-panel__status" :data-status="server.status">{{ server.status || "offline" }} · {{ server.enabled_tool_count || 0 }} 个已启用工具</span>
          <button
            type="button"
            class="btn btn--ghost btn--sm desktop-icon-button"
            :disabled="refreshing === server.id"
            :aria-label="refreshing === server.id ? '正在刷新工具目录' : `刷新 ${server.display_name || server.id} 工具目录`"
            :title="refreshing === server.id ? '正在刷新工具目录' : `刷新 ${server.display_name || server.id} 工具目录`"
            @click="refresh(server)"
          >
            <UiIcon name="refresh-cw" :size="16" />
          </button>
        </div>
        <div v-if="selected[server.id] && serverTools(server).length" class="mcp-panel__tools">
          <label v-for="tool in serverTools(server)" :key="tool.name" class="mcp-panel__tool">
            <input
              type="checkbox"
              :checked="isToolSelected(server.id, tool.name)"
              @change="toggleTool(server.id, tool.name)"
            />
            <span>
              <code>{{ tool.name }}</code>
              <small v-if="tool.description">{{ tool.description }}</small>
            </span>
          </label>
        </div>
        <p v-else-if="selected[server.id]" class="mcp-panel__muted">该服务暂无已启用工具，请先在“设置 › MCP”中启用。</p>
      </article>
    </div>
    <p v-if="status" class="mcp-panel__ok">{{ status }}</p>
  </section>
</template>

<style scoped>
.mcp-panel__server-head { display:flex; align-items:center; justify-content:flex-start; gap:12px; }
.mcp-panel__server-toggle { display:flex; align-items:center; gap:8px; min-width:150px; font-size:13px; font-weight:600; }
.mcp-panel__status { font-size:11px; color:var(--color-text-muted); }
.mcp-panel__status[data-status="ready"] { color:var(--color-success,#3d9a5f); }
.mcp-panel__status[data-status="error"] { color:var(--color-danger); }
.mcp-panel__list { display:flex; flex-direction:column; gap:10px; margin-top:0; }
.mcp-panel__server { padding:12px; border:1px solid var(--color-border); border-radius:10px; }
.mcp-panel__tools { display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr)); gap:7px 12px; margin:12px 0 0 22px; }
.mcp-panel__tool { display:flex; align-items:flex-start; gap:7px; font-size:12px; }
.mcp-panel__tool small { display:block; margin-top:2px; color:var(--color-text-muted); }
.mcp-panel__muted,.mcp-panel__error,.mcp-panel__ok { margin:12px 0 0; font-size:12px; }
.mcp-panel > .mcp-panel__muted,
.mcp-panel > .mcp-panel__error {
  margin-top: 0;
}
.mcp-panel__muted { color:var(--color-text-muted); }
.mcp-panel__error { color:var(--color-danger); }
.mcp-panel__ok { color:var(--color-success,#3d9a5f); }
.mcp-panel__empty { display:flex; align-items:center; justify-content:space-between; gap:12px; }
</style>
