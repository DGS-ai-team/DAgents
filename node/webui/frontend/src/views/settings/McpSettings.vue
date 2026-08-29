<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import * as api from "../../api/node.js";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
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
const redactedSecrets = ref({});
const draft = reactive({ configText: "" });

const REDACTED_SECRET = "__DAGENTS_SECRET_REDACTED__";

function isReferenceValue(value) {
  return /^\$\{[^}]+\}$/.test(value) || /^env:/i.test(value);
}

function isSensitiveField(key, parents) {
  const normalized = String(key || "").toLowerCase();
  return parents.some((parent) => parent === "headers" || parent === "env")
    || /token|secret|password|api[_-]?key|authorization|credential/.test(normalized);
}

function maskConfigValue(value, path, parents, secrets) {
  if (Array.isArray(value)) {
    return value.map((item, index) => maskConfigValue(item, `${path}.${index}`, parents, secrets));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [
        key,
        maskConfigValue(child, path ? `${path}.${key}` : key, [...parents, key.toLowerCase()], secrets),
      ]),
    );
  }
  if (typeof value === "string" && value && isSensitiveField(parents.at(-1), parents.slice(0, -1)) && !isReferenceValue(value)) {
    secrets[path] = value;
    return REDACTED_SECRET;
  }
  return value;
}

function restoreConfigValue(value, path, secrets) {
  if (Array.isArray(value)) {
    return value.map((item, index) => restoreConfigValue(item, `${path}.${index}`, secrets));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [
        key,
        restoreConfigValue(child, path ? `${path}.${key}` : key, secrets),
      ]),
    );
  }
  return value === REDACTED_SECRET && secrets[path] !== undefined ? secrets[path] : value;
}

function maskConfigText(text) {
  try {
    const secrets = {};
    const parsed = JSON.parse(text);
    const masked = maskConfigValue(parsed, "", [], secrets);
    redactedSecrets.value = secrets;
    return `${JSON.stringify(masked, null, 2)}\n`;
  } catch {
    redactedSecrets.value = {};
    return text;
  }
}

function configTextForSave(text) {
  try {
    const parsed = JSON.parse(text);
    return `${JSON.stringify(restoreConfigValue(parsed, "", redactedSecrets.value), null, 2)}\n`;
  } catch {
    return text;
  }
}

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
  draft.configText = maskConfigText(configText.value);
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
    const result = await api.saveMcpConfig(configTextForSave(draft.configText));
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
    <SettingsPageHeader
      title="MCP 服务"
      eyebrow="全局工具设置"
      description="管理 Node 全局可用的 MCP 服务和工具目录；具体智能体的启用范围在智能体设置中绑定。"
    >
      <template #actions>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="reloadDraft">重新加载</button>
      </template>
    </SettingsPageHeader>

    <details class="mcp-settings__advanced">
      <summary>
        <span>高级：编辑 mcpServers JSON</span>
        <small>适用于导入、批量修改或配置 stdio 服务</small>
      </summary>
      <section class="mcp-settings__editor settings-section settings-section--standalone">
      <div class="settings-section__head">
        <div>
          <h2 class="settings-section__title">原始配置</h2>
          <p class="settings-section__desc">支持标准 mcpServers 格式。敏感值建议使用环境变量引用，例如 <code>${ENV_NAME}</code>；直接输入的凭据会加密存储。</p>
        </div>
        <div class="settings-section__actions">
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
        autocomplete="off"
        aria-label="MCP 原始配置"
        placeholder='{
  "mcpServers": {}
}'
      ></textarea>
      <p class="mcp-settings__hint">stdio 示例：command、args、env；远程服务示例：type、url、headers。不要把访问令牌提交到截图、日志或聊天记录中。</p>
      </section>
    </details>

    <p v-if="loading" class="mcp-settings__muted">加载中…</p>
    <p v-if="error" class="mcp-settings__error">{{ error }}</p>
    <p v-if="status" class="mcp-settings__ok">{{ status }}</p>

    <section class="mcp-settings__servers settings-section settings-section--standalone">
      <div class="settings-section__head">
        <div>
          <h2 class="settings-section__title">已配置服务</h2>
          <p class="settings-section__desc">保存配置后，服务会以独立条目显示。点击条目进入工具目录。</p>
        </div>
        <span class="mcp-settings__count">{{ servers.length }} 个服务</span>
      </div>
      <div v-if="!servers.length && !loading" class="mcp-settings__empty settings-empty-state">还没有配置 MCP 服务。</div>
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
      <div class="settings-section__head">
        <div>
          <button type="button" class="mcp-settings__back" @click="activeServerId = ''">‹ 返回服务列表</button>
          <h2 class="settings-section__title">{{ activeServer.display_name || activeServer.id }} 的工具</h2>
          <p class="settings-section__desc">只启用确实需要暴露给智能体的工具。</p>
        </div>
        <div class="settings-section__actions">
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
      <div v-if="!activeServer.tools?.length" class="mcp-settings__empty settings-empty-state">还没有工具目录，请先刷新服务。</div>
      <div v-else-if="!activeTools.length" class="mcp-settings__empty settings-empty-state">没有匹配的工具。</div>
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
.mcp-settings__tool-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.mcp-settings__editor,
.mcp-settings__servers,
.mcp-settings__detail { margin-top: 16px; }
.mcp-settings__advanced {
  margin-top: 18px;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: var(--color-surface-muted, #fbfcfd);
}
.mcp-settings__advanced > summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 14px;
  color: var(--color-text);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  list-style-position: inside;
}
.mcp-settings__advanced > summary::marker { color: var(--color-primary); }
.mcp-settings__advanced > summary small {
  color: var(--color-text-muted);
  font-size: 11px;
  font-weight: 400;
}
.mcp-settings__advanced[open] > summary {
  border-bottom: 1px solid var(--color-border);
}
.mcp-settings__advanced .mcp-settings__editor {
  margin-top: 0;
  padding: 16px 14px 14px;
  border-top: 0;
}
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
.mcp-settings__ok { margin: 10px 0 0; font-size: 12px; }
.mcp-settings__hint,
.mcp-settings__muted { color: var(--color-text-muted); }
.mcp-settings__error { color: var(--color-danger); }
.mcp-settings__ok { color: var(--color-success, #3d9a5f); }
.mcp-settings__count { color: var(--color-text-muted); font-size: 12px; }
.mcp-settings__server-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(360px, 1fr)); gap: 10px; margin-top: 14px; }
.mcp-settings__server-card {
  display: grid;
  grid-template-columns: auto 1fr auto auto;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-height: 70px;
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
  .mcp-settings__server-card { grid-template-columns: auto 1fr auto; }
  .mcp-settings__server-meta { grid-column: 2 / -1; text-align: left; }
  .mcp-settings__server-arrow { grid-column: 3; grid-row: 1; }
}
</style>
