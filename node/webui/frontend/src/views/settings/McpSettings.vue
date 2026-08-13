<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import * as api from "../../api/node.js";
import { notifyConfigurationChanged } from "../../utils/configurationEvents.js";

const DEFAULT_CONFIG = `{
  "mcpServers": {
    "tencent-docs": {
      "type": "streamable-http",
      "url": "https://docs.qq.com/openapi/mcp",
      "headers": {
        "Authorization": "\${TENCENT_DOC_KEY}"
      }
    }
  }
}`;

const configText = ref("");
const servers = ref([]);
const activeServerId = ref("");
const loading = ref(false);
const saving = ref(false);
const refreshing = ref("");
const toolSaving = ref("");
const error = ref("");
const status = ref("");
const toolQuery = ref("");
const draft = reactive({ configText: "" });

const activeServer = computed(() => servers.value.find((item) => item.id === activeServerId.value) || null);
const activeTools = computed(() => {
  const tools = Array.isArray(activeServer.value?.tools) ? activeServer.value.tools : [];
  const query = toolQuery.value.trim().toLowerCase();
  if (!query) return tools;
  return tools.filter((tool) => `${tool.name || ""} ${tool.description || ""}`.toLowerCase().includes(query));
});

function setConfigFromResponse(result) {
  const text = String(result?.config_text || "").trim();
  configText.value = text || '{\n  "mcpServers": {}\n}\n';
  draft.configText = configText.value;
  servers.value = Array.isArray(result?.servers) ? result.servers : [];
  if (activeServerId.value && !servers.value.some((server) => server.id === activeServerId.value)) {
    activeServerId.value = "";
  }
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    setConfigFromResponse(await api.getMcpConfig());
  } catch (e) {
    error.value = e.message || "加载 MCP 配置失败";
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  error.value = "";
  status.value = "";
  try {
    const result = await api.saveMcpConfig(draft.configText);
    setConfigFromResponse(result);
    notifyConfigurationChanged("mcp");
    status.value = "MCP 配置已保存并刷新服务目录";
  } catch (e) {
    error.value = e.message || "保存 MCP 配置失败";
  } finally {
    saving.value = false;
  }
}

function reloadDraft() {
  configText.value = draft.configText;
  void load();
}

function formatDraft() {
  try {
    draft.configText = `${JSON.stringify(JSON.parse(draft.configText), null, 2)}\n`;
    error.value = "";
  } catch (e) {
    error.value = `配置文本不是有效 JSON：${e.message}`;
  }
}

function selectServer(server) {
  activeServerId.value = server.id;
  toolQuery.value = "";
  error.value = "";
}

function serverToolEnabled(tool) {
  return tool?.enabled === true;
}

async function refreshServer(server) {
  if (!server?.id || refreshing.value) return;
  refreshing.value = server.id;
  error.value = "";
  try {
    const updated = await api.refreshMcpServer(server.id);
    const index = servers.value.findIndex((item) => item.id === server.id);
    if (index >= 0) servers.value[index] = updated;
    notifyConfigurationChanged("mcp-catalog");
    status.value = `${server.display_name || server.id} 工具目录已刷新`;
  } catch (e) {
    error.value = e.message || "刷新 MCP 工具失败";
  } finally {
    refreshing.value = "";
  }
}

async function setTools(server, enabledTools) {
  if (!server?.id || toolSaving.value) return;
  toolSaving.value = server.id;
  error.value = "";
  try {
    const updated = await api.patchMcpServer(server.id, { enabled_tools: enabledTools });
    const index = servers.value.findIndex((item) => item.id === server.id);
    if (index >= 0) servers.value[index] = updated;
    notifyConfigurationChanged("mcp-catalog");
    status.value = `${server.display_name || server.id} 的工具启停已保存`;
  } catch (e) {
    error.value = e.message || "保存工具启停状态失败";
    await load();
  } finally {
    toolSaving.value = "";
  }
}

function toggleTool(server, tool) {
  const next = (server.tools || [])
    .filter((item) => item.enabled === true)
    .map((item) => item.name);
  const index = next.indexOf(tool.name);
  if (index >= 0) next.splice(index, 1);
  else next.push(tool.name);
  void setTools(server, next.sort());
}

function enableAll() {
  void setTools(activeServer.value, (activeServer.value?.tools || []).map((tool) => tool.name).sort());
}

function disableAll() {
  void setTools(activeServer.value, []);
}

onMounted(() => {
  draft.configText = DEFAULT_CONFIG;
  void load();
});
</script>

<template>
  <div class="settings-page settings-embedded mcp-settings">
    <div class="mcp-settings__head">
      <div>
        <h1 class="settings-page__title">MCP 外部服务</h1>
        <p class="settings-page__intro">
          使用标准 mcpServers JSON 配置多个 MCP 服务。保存后会加载服务目录，工具默认不暴露给智能体；点击服务条目后再启用需要的工具。
        </p>
      </div>
      <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="reloadDraft">重新加载</button>
    </div>

    <section class="mcp-settings__editor settings-section settings-section--standalone">
      <div class="mcp-settings__section-head">
        <div>
          <h2 class="settings-section__title">MCP 配置</h2>
          <p class="settings-section__desc">兼容常见 MCP 客户端的 mcpServers 格式。凭据可以填写明文，也可以写成 ${ENV_NAME} 环境变量引用。</p>
        </div>
        <div class="mcp-settings__actions">
          <button type="button" class="btn btn--ghost btn--sm" :disabled="saving" @click="formatDraft">格式化</button>
          <button type="button" class="btn btn--primary btn--sm" :disabled="saving" @click="save">
            {{ saving ? "保存中…" : "保存并刷新" }}
          </button>
        </div>
      </div>
      <textarea
        v-model="draft.configText"
        class="mcp-settings__textarea"
        spellcheck="false"
        aria-label="MCP 原始配置"
        placeholder='{
  "mcpServers": {}
}'
      ></textarea>
      <p class="mcp-settings__hint">stdio 示例：command、args、env；远程服务示例：type、url、headers。明文凭据会保存在本机 Node 的 MCP 配置中，请谨慎使用。</p>
    </section>

    <p v-if="loading" class="mcp-settings__muted">加载中…</p>
    <p v-if="error" class="mcp-settings__error">{{ error }}</p>
    <p v-if="status" class="mcp-settings__ok">{{ status }}</p>

    <section class="mcp-settings__servers settings-section settings-section--standalone">
      <div class="mcp-settings__section-head">
        <div>
          <h2 class="settings-section__title">已配置服务</h2>
          <p class="settings-section__desc">保存配置后，服务会以独立条目显示。点击条目进入工具目录。</p>
        </div>
        <span class="mcp-settings__count">{{ servers.length }} 个服务</span>
      </div>
      <div v-if="!servers.length && !loading" class="mcp-settings__empty">还没有配置 MCP 服务。</div>
      <div v-else class="mcp-settings__server-list">
        <button
          v-for="server in servers"
          :key="server.id"
          type="button"
          class="mcp-settings__server-card"
          :class="{ 'mcp-settings__server-card--active': activeServerId === server.id }"
          @click="selectServer(server)"
        >
          <span class="mcp-settings__server-icon" aria-hidden="true">⌘</span>
          <span class="mcp-settings__server-main">
            <strong>{{ server.display_name || server.id }}</strong>
            <small>{{ server.id }} · {{ server.transport || "stdio" }}</small>
          </span>
          <span class="mcp-settings__server-meta">
            <span class="mcp-settings__status" :data-status="server.status">{{ server.status || "offline" }}</span>
            <small>{{ server.enabled_tool_count || 0 }} / {{ server.tool_count || 0 }} 个工具已启用</small>
          </span>
          <span class="mcp-settings__server-arrow" aria-hidden="true">›</span>
        </button>
      </div>
    </section>

    <section v-if="activeServer" class="mcp-settings__detail settings-section settings-section--standalone">
      <div class="mcp-settings__section-head">
        <div>
          <button type="button" class="mcp-settings__back" @click="activeServerId = ''">‹ 返回服务列表</button>
          <h2 class="settings-section__title">{{ activeServer.display_name || activeServer.id }} 的工具</h2>
          <p class="settings-section__desc">只启用确实需要暴露给智能体的工具。</p>
        </div>
        <div class="mcp-settings__actions">
          <button type="button" class="btn btn--ghost btn--sm" :disabled="refreshing === activeServer.id" @click="refreshServer(activeServer)">
            {{ refreshing === activeServer.id ? "刷新中…" : "刷新目录" }}
          </button>
          <button type="button" class="btn btn--ghost btn--sm" :disabled="toolSaving === activeServer.id" @click="enableAll">全部启用</button>
          <button type="button" class="btn btn--ghost btn--sm" :disabled="toolSaving === activeServer.id" @click="disableAll">全部禁用</button>
        </div>
      </div>
      <div class="mcp-settings__tool-toolbar">
        <input v-model="toolQuery" class="settings-field__input" placeholder="搜索工具名称或说明" />
        <span>{{ activeServer.enabled_tool_count || 0 }} / {{ activeServer.tool_count || 0 }} 个已启用</span>
      </div>
      <div v-if="!activeServer.tools?.length" class="mcp-settings__empty">还没有工具目录，请先刷新服务。</div>
      <div v-else-if="!activeTools.length" class="mcp-settings__empty">没有匹配的工具。</div>
      <div v-else class="mcp-settings__tool-list">
        <label v-for="tool in activeTools" :key="tool.name" class="mcp-settings__tool-row">
          <input type="checkbox" :checked="serverToolEnabled(tool)" :disabled="toolSaving === activeServer.id" @change="toggleTool(activeServer, tool)" />
          <span>
            <code>{{ tool.name }}</code>
            <small v-if="tool.description">{{ tool.description }}</small>
          </span>
        </label>
      </div>
    </section>
  </div>
</template>

<style scoped>
.mcp-settings__head,
.mcp-settings__section-head,
.mcp-settings__actions,
.mcp-settings__tool-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.mcp-settings__head { align-items: flex-start; }
.mcp-settings__section-head { align-items: flex-start; }
.mcp-settings__actions { flex-wrap: wrap; justify-content: flex-end; }
.mcp-settings__editor,
.mcp-settings__servers,
.mcp-settings__detail { margin-top: 18px; }
.mcp-settings__textarea {
  display: block;
  width: 100%;
  min-height: 260px;
  margin-top: 14px;
  padding: 14px;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: color-mix(in srgb, var(--color-bg, #fff) 88%, #dcecff);
  color: var(--color-text);
  font: 12px/1.6 var(--font-mono, ui-monospace, monospace);
  resize: vertical;
  box-sizing: border-box;
}
.mcp-settings__hint,
.mcp-settings__muted,
.mcp-settings__error,
.mcp-settings__ok,
.mcp-settings__empty { margin: 10px 0 0; font-size: 12px; }
.mcp-settings__hint,
.mcp-settings__muted,
.mcp-settings__empty { color: var(--color-text-muted); }
.mcp-settings__error { color: var(--color-danger); }
.mcp-settings__ok { color: var(--color-success, #3d9a5f); }
.mcp-settings__count { color: var(--color-text-muted); font-size: 12px; }
.mcp-settings__server-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 10px; margin-top: 14px; }
.mcp-settings__server-card {
  display: grid;
  grid-template-columns: auto 1fr auto auto;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 13px;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: var(--color-bg, #fff);
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.mcp-settings__server-card:hover,
.mcp-settings__server-card--active { border-color: var(--color-accent, #5594d6); background: color-mix(in srgb, var(--color-bg, #fff) 92%, #e8f3ff); }
.mcp-settings__server-icon { color: var(--color-text-muted); font-size: 18px; }
.mcp-settings__server-main,
.mcp-settings__server-meta { min-width: 0; }
.mcp-settings__server-main strong,
.mcp-settings__server-main small,
.mcp-settings__server-meta small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mcp-settings__server-main small,
.mcp-settings__server-meta small { margin-top: 4px; color: var(--color-text-muted); font-size: 11px; }
.mcp-settings__server-meta { text-align: right; }
.mcp-settings__status { color: var(--color-text-muted); font-size: 11px; }
.mcp-settings__status[data-status="ready"] { color: var(--color-success, #3d9a5f); }
.mcp-settings__status[data-status="error"] { color: var(--color-danger); }
.mcp-settings__server-arrow { color: var(--color-text-muted); font-size: 20px; }
.mcp-settings__back { padding: 0; border: 0; background: transparent; color: var(--color-accent, #5594d6); font-size: 12px; cursor: pointer; }
.mcp-settings__back + .settings-section__title { margin-top: 8px; }
.mcp-settings__tool-toolbar { margin-top: 14px; color: var(--color-text-muted); font-size: 12px; }
.mcp-settings__tool-toolbar input { flex: 1; min-width: 180px; }
.mcp-settings__tool-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 8px 12px; max-height: 480px; margin-top: 14px; overflow: auto; }
.mcp-settings__tool-row { display: flex; align-items: flex-start; gap: 8px; padding: 10px; border: 1px solid var(--color-border); border-radius: 8px; font-size: 12px; }
.mcp-settings__tool-row code { font-family: var(--font-mono, ui-monospace, monospace); }
.mcp-settings__tool-row small { display: block; margin-top: 4px; color: var(--color-text-muted); line-height: 1.4; }
@media (max-width: 760px) {
  .mcp-settings__head,
  .mcp-settings__section-head { flex-direction: column; }
  .mcp-settings__actions { justify-content: flex-start; }
  .mcp-settings__server-card { grid-template-columns: auto 1fr auto; }
  .mcp-settings__server-meta { grid-column: 2 / -1; text-align: left; }
  .mcp-settings__server-arrow { grid-column: 3; grid-row: 1; }
}
</style>
