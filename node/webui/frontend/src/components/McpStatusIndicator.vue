<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import { onConfigurationChanged } from "../utils/configurationEvents.js";

const props = defineProps({
  agentId: { type: String, default: "" },
});

const open = ref(false);
const rootRef = ref(null);
const loading = ref(false);
const error = ref("");
const servers = ref([]);
let loadSequence = 0;
let refreshTimer = null;
let stopConfigurationEvents = () => {};

const activeServers = computed(() =>
  servers.value.filter((server) => server && server.enabled !== false),
);
const summary = computed(() => String(activeServers.value.length));
const countBadge = computed(() => (activeServers.value.length > 9 ? "9+" : summary.value));

function statusLabel(status) {
  return (
    {
      ready: "健康",
      checking: "检查中",
      error: "异常",
      offline: "离线",
      disabled: "已禁用",
    }[String(status || "")] || String(status || "未知")
  );
}

function toolSummary(server) {
  const count = Number(server?.enabled_tool_count || 0);
  return `${statusLabel(server?.status)} · ${count} 个工具`;
}

async function load() {
  const agentId = String(props.agentId || "").trim();
  const sequence = ++loadSequence;
  if (!agentId) {
    servers.value = [];
    error.value = "";
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const result = await api.getAgentMcp(agentId);
    if (sequence !== loadSequence || String(props.agentId || "").trim() !== agentId) return;
    servers.value = Array.isArray(result?.servers)
      ? result.servers
      : (Array.isArray(result?.bindings) ? result.bindings : []).map((binding) => ({
          id: binding.server_id,
          display_name: binding.server_id,
          enabled: binding.enabled !== false,
          status: "offline",
        }));
    error.value = "";
  } catch (e) {
    if (sequence !== loadSequence) return;
    servers.value = [];
    error.value = e?.message || "MCP 服务读取失败";
  } finally {
    if (sequence === loadSequence) loading.value = false;
  }
}

function toggle() {
  open.value = !open.value;
  if (open.value) void load();
}

function onDocumentPointerDown(event) {
  if (open.value && !rootRef.value?.contains(event.target)) open.value = false;
}

function onDocumentKeydown(event) {
  if (event.key === "Escape") open.value = false;
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointerDown);
  document.addEventListener("keydown", onDocumentKeydown);
  void load();
  // Health is read-only here. Polling keeps the small overview current without
  // introducing refresh/retry controls into the composer.
  refreshTimer = setInterval(() => void load(), 30_000);
  stopConfigurationEvents = onConfigurationChanged((change) => {
    if (["mcp", "mcp-catalog", "tools"].includes(change?.kind)) void load();
  });
});

watch(() => props.agentId, () => void load());

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onDocumentKeydown);
  if (refreshTimer) clearInterval(refreshTimer);
  stopConfigurationEvents();
});
</script>

<template>
  <div ref="rootRef" class="mcp-status-indicator">
    <button
      type="button"
      class="mcp-status-indicator__trigger"
      :aria-expanded="open"
      :aria-label="`当前 Agent 的 MCP 服务，${summary} 个，点击查看`"
      :title="`当前 Agent 的 MCP 服务 · ${summary}`"
      @click="toggle"
    >
      <span class="mcp-status-indicator__icon" aria-hidden="true">
        <svg viewBox="0 0 20 20" fill="none">
          <path d="M7.2 5.3 4.8 7.7a3.25 3.25 0 0 0 4.6 4.6l2.1-2.1M12.8 14.7l2.4-2.4a3.25 3.25 0 0 0-4.6-4.6l-2.1 2.1" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
      <span v-if="activeServers.length" class="mcp-status-indicator__count" aria-hidden="true">{{ countBadge }}</span>
    </button>

    <div v-if="open" class="mcp-status-indicator__popover" role="dialog" aria-label="当前 Agent 的 MCP 服务">
      <div class="mcp-status-indicator__popover-head">
        <strong>当前 Agent 的 MCP</strong>
        <span>{{ summary }} 个服务</span>
      </div>
      <p v-if="loading && !activeServers.length" class="mcp-status-indicator__muted">读取中…</p>
      <p v-else-if="error" class="mcp-status-indicator__error" role="alert">{{ error }}</p>
      <p v-else-if="!activeServers.length" class="mcp-status-indicator__muted">当前 Agent 未绑定 MCP 服务。</p>
      <ul v-else class="mcp-status-indicator__list">
        <li v-for="server in activeServers" :key="server.id" class="mcp-status-indicator__item">
          <span class="mcp-status-indicator__server-dot" :data-status="server.status" aria-hidden="true"></span>
          <span class="mcp-status-indicator__server-main">
            <strong>{{ server.display_name || server.id }}</strong>
            <small>{{ toolSummary(server) }}</small>
          </span>
          <small v-if="server.last_error" class="mcp-status-indicator__server-error" :title="server.last_error">{{ server.last_error }}</small>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.mcp-status-indicator { position: relative; display: inline-flex; flex: 0 0 auto; min-width: 26px; }
.mcp-status-indicator__trigger { position: relative; display: inline-flex; width: 26px; min-width: 26px; height: 26px; flex: 0 0 26px; align-items: center; justify-content: center; padding: 0; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--color-text-muted); cursor: pointer; }
.mcp-status-indicator__trigger:hover, .mcp-status-indicator__trigger:focus-visible { border-color: var(--color-border); background: var(--color-surface-alt, #f5f7f9); }
.mcp-status-indicator__icon, .mcp-status-indicator__icon svg { display: block; width: 18px; height: 18px; }
.mcp-status-indicator__count { position: absolute; right: -3px; bottom: -3px; display: inline-flex; min-width: 12px; height: 12px; align-items: center; justify-content: center; padding: 0 2px; border: 1px solid var(--color-surface, #fff); border-radius: 999px; background: var(--color-accent, #64748b); color: #fff; font-size: 8px; font-weight: 700; line-height: 10px; }
.mcp-status-indicator__popover { position: absolute; left: 0; bottom: calc(100% + 8px); z-index: 30; width: min(300px, calc(100vw - 24px)); padding: 10px; border: 1px solid var(--color-border); border-radius: 9px; background: var(--color-surface, #fff); box-shadow: 0 10px 28px rgb(20 35 50 / 16%); }
.mcp-status-indicator__popover-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.mcp-status-indicator__popover-head strong { color: var(--color-text); font-size: 12px; }
.mcp-status-indicator__popover-head span, .mcp-status-indicator__muted { color: var(--color-text-subtle); font-size: 10px; }
.mcp-status-indicator__error { margin: 12px 0 2px; color: var(--color-danger, #c45757); font-size: 11px; }
.mcp-status-indicator__muted { margin: 12px 0 2px; text-align: center; }
.mcp-status-indicator__list { display: grid; gap: 5px; max-height: 280px; margin: 9px 0 0; padding: 0; overflow: auto; list-style: none; }
.mcp-status-indicator__item { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px; padding: 8px; border: 1px solid var(--color-border); border-radius: 7px; }
.mcp-status-indicator__server-dot { width: 7px; height: 7px; margin-top: 4px; flex: 0 0 auto; border-radius: 50%; background: var(--color-text-muted); }
.mcp-status-indicator__server-dot[data-status="ready"] { background: var(--color-success, #3d9a5f); }
.mcp-status-indicator__server-dot[data-status="checking"] { background: var(--color-primary, #3689d6); }
.mcp-status-indicator__server-dot[data-status="error"], .mcp-status-indicator__server-dot[data-status="offline"] { background: var(--color-danger, #c45757); }
.mcp-status-indicator__server-main { display: grid; min-width: 0; gap: 3px; }
.mcp-status-indicator__server-main strong { overflow: hidden; color: var(--color-text); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.mcp-status-indicator__server-main small { color: var(--color-text-subtle); font-size: 10px; }
.mcp-status-indicator__server-error { grid-column: 2; overflow: hidden; color: var(--color-danger, #c45757); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
</style>
