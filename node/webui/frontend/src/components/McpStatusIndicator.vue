<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import * as api from "../api/node.js";
import { connectStream } from "../sse/stream.js";

const open = ref(false);
const rootRef = ref(null);
const loading = ref(false);
const error = ref("");
const health = ref({ status: "unconfigured", enabled_count: 0, healthy_count: 0 });
const servers = ref([]);
const refreshing = ref(new Set());
const router = useRouter();
let refreshTimer = null;
let statusStream = null;

const healthMeta = computed(() => {
  const map = {
    unconfigured: { tone: "neutral", label: "MCP 未配置" },
    healthy: { tone: "healthy", label: "MCP 全部健康" },
    checking: { tone: "checking", label: "MCP 检查中" },
    degraded: { tone: "degraded", label: "MCP 存在异常" },
  };
  return map[health.value?.status] || { tone: "degraded", label: "MCP 状态未知" };
});

const summary = computed(() => {
  const enabled = Number(health.value?.enabled_count || 0);
  const healthy = Number(health.value?.healthy_count || 0);
  return enabled > 0 ? `${healthy}/${enabled}` : "—";
});

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

function failureLabel(server) {
  const stage = String(server?.health_stage || "").trim();
  const kind = String(server?.failure_kind || "").trim();
  if (!stage && !kind) return "";
  return [stage, kind].filter(Boolean).join(" · ");
}

function openSettings() {
  open.value = false;
  void router.push({ name: "settings-mcp" });
}

async function load() {
  loading.value = true;
  try {
    const result = await api.getMcpStatus();
    health.value = result?.health || { status: "unconfigured" };
    servers.value = Array.isArray(result?.servers) ? result.servers : [];
    error.value = "";
  } catch (e) {
    error.value = e?.message || "MCP 状态读取失败";
    health.value = { status: "degraded", enabled_count: 0, healthy_count: 0 };
  } finally {
    loading.value = false;
  }
}

async function refreshServer(server) {
  const id = String(server?.id || "").trim();
  if (!id || refreshing.value.has(id)) return;
  refreshing.value = new Set(refreshing.value).add(id);
  try {
    await api.refreshMcpServer(id);
    await load();
  } catch (e) {
    error.value = e?.message || "MCP 服务刷新失败";
    await load();
  } finally {
    const next = new Set(refreshing.value);
    next.delete(id);
    refreshing.value = next;
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
  refreshTimer = setInterval(() => void load(), 30_000);
  statusStream = connectStream({
    // MCP health is Node-scoped rather than Agent-scoped. The server emits
    // this event with an empty agent id, so subscribe to the global stream.
    getAgentId: () => "",
    onEvent: ({ type }) => {
      if (type === "mcp/status-changed") void load();
    },
    onReconnect: () => load(),
  });
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onDocumentKeydown);
  if (refreshTimer) clearInterval(refreshTimer);
  statusStream?.close();
  statusStream = null;
});
</script>

<template>
  <div ref="rootRef" class="mcp-status-indicator">
    <button
      type="button"
      class="mcp-status-indicator__trigger"
      :class="`mcp-status-indicator__trigger--${healthMeta.tone}`"
      :aria-expanded="open"
      :aria-label="`${healthMeta.label}，${summary}，点击查看服务列表`"
      :title="`${healthMeta.label} · ${summary}`"
      @click="toggle"
    >
      <span class="mcp-status-indicator__icon" aria-hidden="true">
        <svg viewBox="0 0 1072 1024" fill="currentColor">
          <path d="M446.031676 116.873621L80.115479 482.728143q-2.466989 2.466989-4.317231 5.304027-1.973591 2.898712-3.268761 6.105799-1.356844 3.207086-2.035266 6.599197-0.616747 3.39211-0.616747 6.78422 0 3.51546 0.616747 6.90757 0.678422 3.39211 2.035266 6.599197 1.295169 3.207086 3.268761 6.044124 1.850242 2.898712 4.317231 5.365702 2.466989 2.466989 5.365702 4.317231 2.837038 1.973591 6.044124 3.268761 3.207086 1.356844 6.599197 2.035266 3.39211 0.616747 6.845895 0.616748t6.907571-0.616748q3.39211-0.678422 6.537521-2.035266 3.207086-1.295169 6.105799-3.207086 2.898712-1.911917 5.304027-4.378906L495.741512 166.521782q28.000329-28.062004 67.102111-28.062004 39.101781 0 67.10211 28.062004 28.062004 28.000329 28.062004 67.10211 0 39.101781-28.062004 67.102111L354.012973 576.720438q-2.466989 2.466989-4.317231 5.365702-1.973591 2.837038-3.268761 6.044124-1.356844 3.207086-2.035266 6.599196-0.616747 3.39211-0.616748 6.845896 0 3.51546 0.616748 6.90757 0.678422 3.39211 2.035266 6.599196 1.295169 3.145411 3.207086 6.044124 1.973591 2.898712 4.378906 5.304027 2.466989 2.466989 5.365702 4.378907 2.837038 1.911917 6.044124 3.26876 3.207086 1.295169 6.599197 1.973592t6.845895 0.678422q3.453785 0 6.845896-0.616747 3.39211-0.740097 6.599196-2.035267 3.207086-1.356844 6.105799-3.26876 2.898712-1.850242 5.304027-4.317232l275.994435-275.994435q27.013534-27.075208 68.27393-25.533339 41.938819 1.480194 71.049294 30.652342 27.075208 27.075208 25.595014 63.216603-1.541868 36.758141-30.714017 65.93029l-326.99944 326.99944q-55.507261 55.507261 0 111.076196l61.304686 61.304686q2.466989 2.466989 5.304027 4.378906 2.898712 1.911917 6.105799 3.268761 3.207086 1.295169 6.599196 1.973591t6.784221 0.678422q3.51546 0 6.90757-0.616747 3.39211-0.740097 6.599197-2.035266 3.207086-1.356844 6.044123-3.268761 2.898712-1.911917 5.365702-4.317231 2.466989-2.466989 4.317232-5.365702 1.973591-2.898712 3.268761-6.105799 1.356844-3.207086 2.035266-6.599197 0.616747-3.39211 0.616747-6.78422 0-3.51546-0.616747-6.90757-0.678422-3.39211-2.035266-6.599197-1.295169-3.207086-3.207087-6.044124-1.911917-2.898712-4.378906-5.365702l-61.304685-61.304685q-5.8591-5.797425 0-11.59485l326.99944-327.061115q48.59969-48.59969 51.190029-112.741414 2.775363-66.978761-46.071027-115.82515-48.72304-48.72304-118.16879-51.190029-12.088248-0.493398-23.559748 0.555072 1.233495-10.484705 1.233494-21.586157 0-68.212256-48.538015-116.873621-48.661365-48.59969-116.873621-48.59969-68.212256 0-116.811947 48.661365z m275.93276 275.93276q2.466989-2.466989 5.304028-4.378906 2.898712-1.911917 6.105798-3.268761 3.207086-1.295169 6.599197-1.973592t6.845895-0.678422q3.453785 0 6.845896 0.616748 3.39211 0.740097 6.599196 2.035266 3.207086 1.356844 6.105799 3.268761 2.837038 1.911917 5.304027 4.317231 2.466989 2.466989 4.317231 5.365702 1.973591 2.898712 3.268761 6.105799 1.356844 3.145411 2.035267 6.599196 0.616747 3.39211 0.616747 6.784221 0 3.51546-0.616747 6.90757-0.678422 3.39211-2.035267 6.599197-1.295169 3.207086-3.207086 6.044124-1.911917 2.898712-4.378906 5.365702l-273.897494 273.835819q-48.59969 48.72304-116.873621 48.72304-68.212256 0-116.811946-48.72304-48.59969-48.59969-48.59969-116.811947 0-68.212256 48.59969-116.811946l273.897494-273.835819q2.466989-2.466989 5.304027-4.440581 2.898712-1.850242 6.105799-3.207086 3.207086-1.356844 6.599196-2.035266 3.39211-0.616747 6.845896-0.616747t6.845895 0.616747q3.39211 0.678422 6.599197 2.035266 3.207086 1.295169 6.105798 3.207086 2.837038 1.911917 5.304028 4.378906 2.466989 2.466989 4.317231 5.365702 1.973591 2.837038 3.330436 6.044124 1.295169 3.207086 1.973591 6.599197 0.616747 3.39211 0.616747 6.845895t-0.616747 6.845896q-0.678422 3.39211-1.973591 6.599196-1.356844 3.207086-3.268761 6.105799-1.911917 2.837038-4.378906 5.304027l-273.83582 273.835819q-28.062004 28.062004-28.062004 67.22546 0 39.040107 28.00033 67.102111 28.062004 28.000329 67.10211 28.000329 39.101781 0 67.163786-28.000329l273.835819-273.897494h0.061674z" />
        </svg>
      </span>
    </button>

    <div v-if="open" class="mcp-status-indicator__popover" role="dialog" aria-label="MCP 服务状态">
      <div class="mcp-status-indicator__popover-head">
        <div>
        <strong>{{ healthMeta.label }}</strong>
          <span>Node 级状态，不影响对话上下文</span>
        </div>
        <div class="mcp-status-indicator__head-actions">
          <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
          <button type="button" class="btn btn--ghost btn--sm" @click="openSettings">设置</button>
        </div>
      </div>
      <p v-if="error" class="mcp-status-indicator__error" role="alert">{{ error }}</p>
      <p v-if="loading && !servers.length" class="mcp-status-indicator__muted">读取服务状态中…</p>
      <p v-else-if="!servers.length" class="mcp-status-indicator__muted">当前没有配置启用的 MCP 服务。</p>
      <ul v-else class="mcp-status-indicator__list">
        <li v-for="server in servers" :key="server.id" class="mcp-status-indicator__item">
          <div class="mcp-status-indicator__server-main">
            <span class="mcp-status-indicator__server-dot" :data-status="server.status" aria-hidden="true"></span>
            <div>
              <strong>{{ server.display_name || server.id }}</strong>
              <span>{{ server.transport }} · {{ statusLabel(server.status) }}</span>
            </div>
          </div>
          <div class="mcp-status-indicator__server-meta">
            <span>{{ server.enabled_tool_count || 0 }}/{{ server.tool_count || 0 }} 工具</span>
            <button
              type="button"
              class="btn btn--ghost btn--sm"
              :disabled="refreshing.has(server.id) || server.enabled === false"
              @click="refreshServer(server)"
            >
              {{ refreshing.has(server.id) ? "检查中…" : "重试" }}
            </button>
          </div>
          <span v-if="failureLabel(server)" class="mcp-status-indicator__server-diagnostic">
            {{ failureLabel(server) }}<template v-if="server.retryable"> · 可重试</template>
          </span>
          <span v-if="server.last_error" class="mcp-status-indicator__server-error" :title="server.last_error">
            {{ server.last_error }}
          </span>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.mcp-status-indicator { position: relative; display: inline-flex; }
.mcp-status-indicator__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 4px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--color-text-muted);
  cursor: pointer;
  font-size: 11px;
}
.mcp-status-indicator__trigger:hover,
.mcp-status-indicator__trigger:focus-visible { border-color: var(--color-border); background: var(--color-surface-alt, #f5f7f9); }
.mcp-status-indicator__icon { display: inline-flex; width: 18px; height: 18px; align-items: center; justify-content: center; }
.mcp-status-indicator__icon svg { display: block; width: 18px; height: 18px; }
.mcp-status-indicator__trigger--healthy { color: var(--color-success, #3d9a5f); }
.mcp-status-indicator__trigger--checking { color: var(--color-primary, #3689d6); }
.mcp-status-indicator__trigger--degraded { color: var(--color-danger, #c45757); }
.mcp-status-indicator__trigger--neutral { color: var(--color-text-muted); }
.mcp-status-indicator__trigger--checking .mcp-status-indicator__icon { animation: mcp-status-pulse 1.25s ease-in-out infinite; }
@keyframes mcp-status-pulse { 50% { opacity: 0.45; } }
.mcp-status-indicator__popover {
  position: absolute;
  left: 0;
  bottom: calc(100% + 8px);
  z-index: 20;
  width: min(360px, calc(100vw - 24px));
  padding: 10px;
  border: 1px solid var(--color-border);
  border-radius: 9px;
  background: var(--color-surface, #fff);
  box-shadow: 0 10px 28px rgb(20 35 50 / 16%);
}
.mcp-status-indicator__popover-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.mcp-status-indicator__head-actions { display: inline-flex; flex: 0 0 auto; gap: 4px; }
.mcp-status-indicator__popover-head div { display: grid; gap: 3px; min-width: 0; }
.mcp-status-indicator__popover-head strong { color: var(--color-text); font-size: 12px; }
.mcp-status-indicator__popover-head span,
.mcp-status-indicator__muted { color: var(--color-text-subtle); font-size: 10px; }
.mcp-status-indicator__error { margin: 8px 0; color: var(--color-danger, #c45757); font-size: 11px; }
.mcp-status-indicator__muted { margin: 12px 0 2px; text-align: center; }
.mcp-status-indicator__list { display: grid; gap: 7px; max-height: 280px; margin: 10px 0 0; padding: 0; overflow: auto; list-style: none; }
.mcp-status-indicator__item { display: grid; gap: 5px; padding: 7px; border: 1px solid var(--color-border); border-radius: 7px; }
.mcp-status-indicator__server-main,
.mcp-status-indicator__server-meta { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.mcp-status-indicator__server-main { justify-content: flex-start; }
.mcp-status-indicator__server-main div { display: grid; min-width: 0; gap: 2px; }
.mcp-status-indicator__server-main strong { overflow: hidden; color: var(--color-text); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.mcp-status-indicator__server-main span,
.mcp-status-indicator__server-meta span { color: var(--color-text-subtle); font-size: 10px; }
.mcp-status-indicator__server-dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--color-text-muted); }
.mcp-status-indicator__server-dot[data-status="ready"] { background: var(--color-success, #3d9a5f); }
.mcp-status-indicator__server-dot[data-status="checking"] { background: var(--color-primary, #3689d6); }
.mcp-status-indicator__server-dot[data-status="error"],
.mcp-status-indicator__server-dot[data-status="offline"] { background: var(--color-danger, #c45757); }
.mcp-status-indicator__server-error { display: block; overflow: hidden; color: var(--color-danger, #c45757); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.mcp-status-indicator__server-diagnostic { color: var(--color-text-subtle); font-size: 10px; }
</style>
