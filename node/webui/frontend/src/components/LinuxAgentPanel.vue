<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";

const props = defineProps({ agentId: { type: String, required: true } });
const emit = defineEmits(["changed"]);
const channels = ref([]);
const selected = ref(new Set());
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
    selected.value = new Set((bindingResult?.bindings || []).filter((item) => item.enabled !== false).map((item) => item.channel_id));
  } catch (e) {
    error.value = e.message || "加载 Linux 通道绑定失败";
  } finally {
    loading.value = false;
  }
}

function toggle(channelId) {
  const next = new Set(selected.value);
  if (next.has(channelId)) next.delete(channelId);
  else next.add(channelId);
  selected.value = next;
}

async function save() {
  saving.value = true;
  error.value = "";
  status.value = "";
  try {
    const result = await api.putAgentLinuxChannels(props.agentId, [...selected.value].map((channel_id) => ({ channel_id, enabled: true, approval_mode: "require_approval" })));
    selected.value = new Set((result?.bindings || []).filter((item) => item.enabled !== false).map((item) => item.channel_id));
    status.value = "Linux 通道绑定已保存，重新加载运行时后生效";
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
    <div class="linux-agent-panel__head"><div><h2 class="linux-agent-panel__title">Linux 通道</h2><p class="linux-agent-panel__desc">只向这个智能体暴露选中的 SSH 通道；每条命令仍使用独立 session。</p></div><button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button></div>
    <p v-if="loading" class="linux-agent-panel__muted">加载中…</p>
    <p v-else-if="error" class="linux-agent-panel__error">{{ error }}</p>
    <p v-else-if="!channels.length" class="linux-agent-panel__muted">尚未配置 Linux 通道，请先到“设置 › Linux 通道”添加。</p>
    <div v-else class="linux-agent-panel__list">
      <label v-for="channel in channels" :key="channel.channel_id" class="linux-agent-panel__row">
        <input type="checkbox" :checked="selected.has(channel.channel_id)" :disabled="saving || channel.enabled === false" @change="toggle(channel.channel_id)" />
        <span><strong>{{ channel.display_name || channel.channel_id }}</strong><small>{{ channel.username }}@{{ channel.host }}:{{ channel.port }}</small></span>
      </label>
    </div>
    <div v-if="channels.length" class="linux-agent-panel__actions"><span class="linux-agent-panel__muted">已选择 {{ selectedCount }} 个</span><button type="button" class="btn btn--primary btn--sm" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存绑定" }}</button></div>
    <p v-if="status" class="linux-agent-panel__ok">{{ status }}</p>
  </section>
</template>

<style scoped>
.linux-agent-panel{margin-top:20px;padding-top:16px;border-top:1px solid color-mix(in srgb,var(--color-border) 80%,transparent)}.linux-agent-panel__head,.linux-agent-panel__actions{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.linux-agent-panel__title{margin:0;font-size:15px}.linux-agent-panel__desc,.linux-agent-panel__muted,.linux-agent-panel__error,.linux-agent-panel__ok{margin:6px 0 0;font-size:12px;color:var(--color-text-subtle)}.linux-agent-panel__error{color:var(--color-danger)}.linux-agent-panel__ok{color:var(--color-success,#3d9a5f)}.linux-agent-panel__list{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:8px;margin-top:12px}.linux-agent-panel__row{display:flex;align-items:flex-start;gap:8px;padding:10px;border:1px solid var(--color-border);border-radius:8px}.linux-agent-panel__row small{display:block;margin-top:4px;color:var(--color-text-subtle);font-size:11px}.linux-agent-panel__actions{align-items:center;margin-top:12px}
</style>
