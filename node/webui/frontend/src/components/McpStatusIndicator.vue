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
        <svg viewBox="0 0 1072 1024" fill="currentColor">
          <path d="M446.031676 116.873621L80.115479 482.728143q-2.466989 2.466989-4.317231 5.304027-1.973591 2.898712-3.268761 6.105799-1.356844 3.207086-2.035266 6.599197-0.616747 3.39211-0.616747 6.78422 0 3.51546 0.616747 6.90757 0.678422 3.39211 2.035266 6.599197 1.295169 3.207086 3.268761 6.044124 1.850242 2.898712 4.317231 5.365702 2.466989 2.466989 5.365702 4.317231 2.837038 1.973591 6.044124 3.268761 3.207086 1.356844 6.599197 2.035266 3.39211 0.616747 6.845895 0.616748t6.907571-0.616748q3.39211-0.678422 6.537521-2.035266 3.207086-1.295169 6.105799-3.207086 2.898712-1.911917 5.304027-4.378906L495.741512 166.521782q28.000329-28.062004 67.102111-28.062004 39.101781 0 67.10211 28.062004 28.062004 28.000329 28.062004 67.10211 0 39.101781-28.062004 67.102111L354.012973 576.720438q-2.466989 2.466989-4.317231 5.365702-1.973591 2.837038-3.268761 6.044124-1.356844 3.207086-2.035266 6.599196-0.616747 3.39211-0.616748 6.845896 0 3.51546 0.616748 6.90757 0.678422 3.39211 2.035266 6.599196 1.295169 3.145411 3.207086 6.044124 1.973591 2.898712 4.378906 5.304027 2.466989 2.466989 5.365702 4.378907 2.837038 1.911917 6.044124 3.26876 3.207086 1.295169 6.599197 1.973592t6.845895 0.678422q3.453785 0 6.845896-0.616747 3.39211-0.740097 6.599196-2.035267 3.207086-1.356844 6.105799-3.26876 2.898712-1.850242 5.304027-4.317232l275.994435-275.994435q27.013534-27.075208 68.27393-25.533339 41.938819 1.480194 71.049294 30.652342 27.075208 27.075208 25.595014 63.216603-1.541868 36.758141-30.714017 65.93029l-326.99944 326.99944q-55.507261 55.507261 0 111.076196l61.304686 61.304686q2.466989 2.466989 5.304027 4.378906 2.898712 1.911917 6.105799 3.268761 3.207086 1.295169 6.599196 1.973591t6.784221 0.678422q3.51546 0 6.90757-0.616747 3.39211-0.740097 6.599197-2.035266 3.207086-1.356844 6.044123-3.268761 2.898712-1.911917 5.365702-4.317231 2.466989-2.466989 4.317232-5.365702 1.973591-2.898712 3.268761-6.105799 1.356844-3.207086 2.035266-6.599197 0.616747-3.39211 0.616747-6.78422 0-3.51546-0.616747-6.90757-0.678422-3.39211-2.035266-6.599197-1.295169-3.207086-3.207087-6.044124-1.911917-2.898712-4.378906-5.365702l-61.304685-61.304685q-5.8591-5.797425 0-11.59485l326.99944-327.061115q48.59969-48.59969 51.190029-112.741414 2.775363-66.978761-46.071027-115.82515-48.72304-48.72304-118.16879-51.190029-12.088248-0.493398-23.559748 0.555072 1.233495-10.484705 1.233494-21.586157 0-68.212256-48.538015-116.873621-48.661365-48.59969-116.873621-48.59969-68.212256 0-116.811947 48.661365z m275.93276 275.93276q2.466989-2.466989 5.304028-4.378906 2.898712-1.911917 6.105798-3.268761 3.207086-1.295169 6.599197-1.973592t6.845895-0.678422q3.453785 0 6.845896 0.616748 3.39211 0.740097 6.599196 2.035266 3.207086 1.356844 6.105799 3.268761 2.837038 1.911917 5.304027 4.317231 2.466989 2.466989 4.317231 5.365702 1.973591 2.898712 3.268761 6.105799 1.356844 3.145411 2.035267 6.599196 0.616747 3.39211 0.616747 6.784221 0 3.51546-0.616747 6.90757-0.678422 3.39211-2.035267 6.599197-1.295169 3.207086-3.207086 6.044124-1.911917 2.898712-4.378906 5.365702l-273.897494 273.835819q-48.59969 48.72304-116.873621 48.72304-68.212256 0-116.811946-48.72304-48.59969-48.59969-48.59969-116.811947 0-68.212256 48.59969-116.811946l273.897494-273.835819q2.466989-2.466989 5.304027-4.440581 2.898712-1.850242 6.105799-3.207086 3.207086-1.356844 6.599196-2.035266 3.39211-0.616747 6.845896-0.616747t6.845895 0.616747q3.39211 0.678422 6.599197 2.035266 3.207086 1.295169 6.105798 3.207086 2.837038 1.911917 5.304028 4.378906 2.466989 2.466989 4.317231 5.365702 1.973591 2.837038 3.330436 6.044124 1.295169 3.207086 1.973591 6.599197 0.616747 3.39211 0.616747 6.845895t-0.616747 6.845896q-0.678422 3.39211-1.973591 6.599196-1.356844 3.207086-3.268761 6.105799-1.911917 2.837038-4.378906 5.304027l-273.83582 273.835819q-28.062004 28.062004-28.062004 67.22546 0 39.040107 28.00033 67.102111 28.062004 28.000329 67.10211 28.000329 39.101781 0 67.163786-28.000329l273.835819-273.897494h0.061674z" />
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
