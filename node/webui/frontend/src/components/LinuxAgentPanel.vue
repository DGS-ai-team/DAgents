<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";

const props = defineProps({ agentId: { type: String, required: true } });
const emit = defineEmits(["changed"]);
const channels = ref([]);
const selected = ref(new Set());
const bindings = ref({});
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const status = ref("");

const selectedCount = computed(() => selected.value.size);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [channelResult, bindingResult] = await Promise.all([api.listLinuxChannels(), api.getAgentLinuxChannels(props.agentId)]);
    channels.value = Array.isArray(channelResult?.channels) ? channelResult.channels : [];
    const nextBindings = {};
    for (const item of bindingResult?.bindings || []) {
      if (item?.channel_id) nextBindings[item.channel_id] = { ...item };
    }
    bindings.value = nextBindings;
    selected.value = new Set((bindingResult?.bindings || []).filter((item) => item.enabled !== false).map((item) => item.channel_id));
  } catch (e) {
    error.value = e.message || "加载 Linux 通道绑定失败";
  } finally {
    loading.value = false;
  }
}

function toggle(channelId) {
  const next = new Set(selected.value);
  if (next.has(channelId)) {
    next.delete(channelId);
  } else {
    next.add(channelId);
    if (!bindings.value[channelId]) {
      bindings.value = { ...bindings.value, [channelId]: { channel_id: channelId, max_concurrency: 1, approval_mode: "require_approval" } };
    }
  }
  selected.value = next;
}

function channelLimit(channel) {
  return Math.max(1, Number(channel?.max_sessions) || 1);
}

function bindingFor(channelId) {
  return bindings.value[channelId] || { channel_id: channelId, max_concurrency: 1, approval_mode: "require_approval" };
}

function setConcurrency(channelId, value) {
  const current = bindingFor(channelId);
  const channel = channels.value.find((item) => item.channel_id === channelId);
  const limit = channelLimit(channel);
  const parsed = Math.min(limit, Math.max(1, Number(value) || 1));
  bindings.value = { ...bindings.value, [channelId]: { ...current, max_concurrency: parsed } };
}

async function save() {
  saving.value = true;
  error.value = "";
  status.value = "";
  try {
    const payload = [...selected.value].map((channel_id) => {
      const current = bindingFor(channel_id);
      const channel = channels.value.find((item) => item.channel_id === channel_id);
      return {
        channel_id,
        enabled: true,
        is_default: current.is_default === true,
        remote_cwd: current.remote_cwd || "",
        shell: current.shell || "",
        max_concurrency: Math.min(channelLimit(channel), Math.max(1, Number(current.max_concurrency) || 1)),
        approval_mode: current.approval_mode || "require_approval",
        allowed_commands: Array.isArray(current.allowed_commands) ? current.allowed_commands : [],
        denied_commands: Array.isArray(current.denied_commands) ? current.denied_commands : [],
      };
    });
    const result = await api.putAgentLinuxChannels(props.agentId, payload);
    const nextBindings = {};
    for (const item of result?.bindings || []) {
      if (item?.channel_id) nextBindings[item.channel_id] = { ...item };
    }
    bindings.value = nextBindings;
    selected.value = new Set((result?.bindings || []).filter((item) => item.enabled !== false).map((item) => item.channel_id));
    status.value = "Linux 通道绑定和并发限制已保存，重新加载运行时后生效";
    emit("changed");
  } catch (e) {
    error.value = e.message || "保存 Linux 通道绑定失败";
  } finally {
    saving.value = false;
  }
}

onMounted(() => void load());
</script>

<template>
  <section class="linux-agent-panel">
    <div class="linux-agent-panel__head"><div><h2 class="linux-agent-panel__title">Linux 通道</h2><p class="linux-agent-panel__desc">选择这个智能体可以使用的远程终端；可同时打开的终端数由通道配置决定。</p></div><button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button></div>
    <p v-if="loading" class="linux-agent-panel__muted">加载中…</p>
    <p v-else-if="error" class="linux-agent-panel__error">{{ error }}</p>
    <p v-else-if="!channels.length" class="linux-agent-panel__muted">尚未配置 Linux 通道，请先到“设置 › Linux 通道”添加。</p>
    <div v-else class="linux-agent-panel__list">
      <div v-for="channel in channels" :key="channel.channel_id" class="linux-agent-panel__row">
        <input type="checkbox" :checked="selected.has(channel.channel_id)" :disabled="saving || channel.enabled === false" @change="toggle(channel.channel_id)" />
        <div class="linux-agent-panel__main"><strong>{{ channel.display_name || channel.channel_id }}</strong><small>{{ channel.username }}@{{ channel.host }}:{{ channel.port }} · 通道上限 {{ channelLimit(channel) }}</small></div>
        <label v-if="selected.has(channel.channel_id)" class="linux-agent-panel__limit"><span>智能体并发</span><input type="number" min="1" :max="channelLimit(channel)" :value="bindingFor(channel.channel_id).max_concurrency" :disabled="saving" @input="setConcurrency(channel.channel_id, $event.target.value)" /></label>
      </div>
    </div>
    <div v-if="channels.length" class="linux-agent-panel__actions"><span class="linux-agent-panel__muted">已选择 {{ selectedCount }} 个</span><button type="button" class="btn btn--primary btn--sm" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存绑定" }}</button></div>
    <p v-if="status" class="linux-agent-panel__ok">{{ status }}</p>
  </section>
</template>

<style scoped>
.linux-agent-panel{margin-top:20px;padding-top:16px;border-top:1px solid color-mix(in srgb,var(--color-border) 80%,transparent)}.linux-agent-panel__head,.linux-agent-panel__actions{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.linux-agent-panel__title{margin:0;font-size:15px}.linux-agent-panel__desc,.linux-agent-panel__muted,.linux-agent-panel__error,.linux-agent-panel__ok{margin:6px 0 0;font-size:12px;color:var(--color-text-subtle)}.linux-agent-panel__error{color:var(--color-danger)}.linux-agent-panel__ok{color:var(--color-success,#3d9a5f)}.linux-agent-panel__list{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:8px;margin-top:12px}.linux-agent-panel__row{display:flex;align-items:center;gap:8px;padding:10px;border:1px solid var(--color-border);border-radius:8px}.linux-agent-panel__main{min-width:0;flex:1}.linux-agent-panel__row small{display:block;margin-top:4px;color:var(--color-text-subtle);font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.linux-agent-panel__limit{display:flex;align-items:center;gap:6px;flex:0 0 auto;color:var(--color-text-subtle);font-size:11px}.linux-agent-panel__limit input{width:58px;padding:5px 6px;border:1px solid var(--color-border);border-radius:6px;background:var(--color-surface,#fff);color:var(--color-text);font:inherit}.linux-agent-panel__actions{align-items:center;margin-top:12px}
</style>
