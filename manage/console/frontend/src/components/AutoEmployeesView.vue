<script setup>
import { onMounted, ref, watch } from "vue";
import { fetchAutoOverview } from "../api.js";

const props = defineProps({ active: { type: Boolean, default: false } });
const emit = defineEmits(["toast"]);
const items = ref([]);
const loading = ref(false);
const error = ref("");
const filter = ref("");
const nodeFilter = ref("");
const page = ref(1);
const pageSize = 20;
const total = ref(0);
let requestId = 0;

function formatTime(value) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1 ? "—" : date.toLocaleString("zh-CN", { hour12: false });
}
function stateLabel(value) { return ({ working: "工作中", standby: "待命", activation_off: "自主激活关闭", needs_attention: "需处理" }[value] || "未知状态"); }
function dreamingStateLabel(value) { return ({ disabled: "已关闭", waiting: "等待执行", running: "整理中", succeeded: "最近成功", failed: "上次失败", recovery_pending: "等待恢复" }[value] || "未知状态"); }
function dreamingText(item) {
  const dreaming = item?.dreaming;
  if (!dreaming || typeof dreaming !== "object") return "未获取";
  const parts = [dreamingStateLabel(dreaming.state)];
  if (dreaming.next_at) parts.push(`下次 ${formatTime(dreaming.next_at)}`);
  if (dreaming.last_success) parts.push(`上次成功 ${formatTime(dreaming.last_success)}`);
  return parts.join(" · ");
}
function todoText(item) {
  const counts = item.todo_counts && typeof item.todo_counts === "object" ? item.todo_counts : {};
  const completed = Math.max(0, Number(counts.completed || 0) || 0);
  const counted = Object.entries(counts).reduce((sum, [status, value]) => status === "total" ? sum : sum + (Math.max(0, Number(value) || 0)), 0);
  const total = Math.max(0, Number(counts.total ?? counted) || 0);
  return total ? `未完成 ${Math.max(0, total - completed)} · 已完成 ${completed}` : "暂无待办";
}

async function load() {
  const current = ++requestId;
  loading.value = true;
  error.value = "";
  try {
    const data = await fetchAutoOverview({ state: filter.value || undefined, node_id: nodeFilter.value || undefined, page: page.value, page_size: pageSize });
    if (current !== requestId) return;
    items.value = Array.isArray(data?.items) ? data.items : [];
    total.value = Number(data?.total || 0);
  } catch (err) {
    if (current !== requestId) return;
    error.value = `无法加载 Auto 员工摘要：${err.message || "服务暂不可用"}`;
    emit("toast", { message: error.value, type: "error" });
  } finally {
    if (current === requestId) loading.value = false;
  }
}

onMounted(() => { if (props.active) void load(); });
watch(() => props.active, (active) => { if (active) void load(); });
watch([filter, nodeFilter], () => { page.value = 1; if (props.active) void load(); });
function previousPage() { if (page.value > 1) { page.value -= 1; void load(); } }
function nextPage() { if (page.value * pageSize < total.value) { page.value += 1; void load(); } }
</script>

<template>
  <section class="auto-employees-view">
    <div class="panel-toolbar">
      <label class="field-inline"><span>Node</span><input v-model.trim="nodeFilter" placeholder="筛选 Node" /></label>
    <label class="field-inline"><span>状态</span><select v-model="filter"><option value="">全部</option><option value="working">工作中</option><option value="standby">待命</option><option value="activation_off">自主激活关闭</option><option value="needs_attention">需处理</option></select></label>
      <button type="button" class="btn btn-ghost" :disabled="loading" @click="load">刷新</button>
    </div>
    <p v-if="error" class="state-error" role="alert">{{ error }}</p>
    <div v-else-if="loading" class="empty-state">加载摘要中…</div>
    <div v-else-if="!items.length" class="empty-state"><strong>暂无 Auto 员工摘要</strong><span>Node 上报后，状态与待办计数会显示在这里。</span></div>
    <div v-else class="auto-employees-table-wrap">
      <table class="data-table auto-employees-table"><thead><tr><th>员工</th><th>Node</th><th>状态</th><th>下次自动检查</th><th>待办</th><th>上报时间</th></tr></thead>
        <tbody><tr v-for="item in items" :key="`${item.node_id}:${item.agent_id}`"><td><strong>{{ item.display_name || item.agent_id }}</strong><small>{{ item.agent_id }}</small></td><td>{{ item.node_id }}</td><td><span class="status-pill" :class="{ 'is-stale': item.stale }">{{ item.stale ? "摘要过期" : stateLabel(item.state) }}<small v-if="item.reason">{{ item.reason }}</small></span><small>dreaming：{{ dreamingText(item) }}</small></td><td>{{ formatTime(item.next_at) }}</td><td>{{ todoText(item) }}</td><td>{{ formatTime(item.received_at) }}<small v-if="item.stale">已 {{ item.age_seconds }} 秒未更新</small></td></tr></tbody>
      </table>
      <div class="auto-employees-cards"><article v-for="item in items" :key="`${item.node_id}:${item.agent_id}:card`" class="auto-employee-card"><div><strong>{{ item.display_name || item.agent_id }}</strong><small>{{ item.agent_id }}</small><small>{{ item.node_id }}</small></div><span class="status-pill" :class="{ 'is-stale': item.stale }">{{ item.stale ? "摘要过期" : stateLabel(item.state) }}</span><small>dreaming：{{ dreamingText(item) }}</small><p>下次自动检查：{{ formatTime(item.next_at) }}</p><small>{{ todoText(item) }} · 上报：{{ formatTime(item.received_at) }}<template v-if="item.stale"> · 已 {{ item.age_seconds }} 秒未更新</template></small></article></div>
    </div>
    <div v-if="total > pageSize" class="pagination"><button type="button" class="btn btn-ghost" :disabled="page <= 1 || loading" @click="previousPage">上一页</button><span>第 {{ page }} 页 · 共 {{ total }} 条</span><button type="button" class="btn btn-ghost" :disabled="page * pageSize >= total || loading" @click="nextPage">下一页</button></div>
  </section>
</template>

<style scoped>
.auto-employees-view { display: grid; gap: 16px; }
.panel-toolbar { display: flex; align-items: end; justify-content: space-between; gap: 12px; padding: 14px 16px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg-surface); }
.field-inline { display: grid; gap: 5px; color: var(--text-muted); font-size: 12px; }
.field-inline input, .field-inline select { min-width: 120px; border: 1px solid var(--border); border-radius: 6px; padding: 8px; background: var(--bg-surface); color: var(--text); font: inherit; }
.empty-state { display: grid; gap: 8px; min-height: 180px; place-items: center; align-content: center; padding: 28px; border: 1px dashed var(--border); border-radius: 8px; color: var(--text-muted); text-align: center; }
.empty-state strong { color: var(--text); }
.state-error { margin: 0; padding: 16px; border-radius: 8px; color: var(--danger); background: var(--bg-muted); }
.auto-employees-table-wrap { overflow-x: auto; }
.auto-employees-table td small, .auto-employees-table .status-pill small { display: block; margin-top: 4px; color: var(--text-secondary); font-size: 12px; }
.status-pill.is-stale { color: var(--text-secondary); }
.auto-employees-cards { display: none; }
.pagination { display: flex; justify-content: flex-end; align-items: center; gap: 12px; color: var(--text-secondary); font-size: 13px; }
@media (max-width: 760px) { .auto-employees-table { display: none; } .auto-employees-cards { display: grid; gap: 10px; } .auto-employee-card { display: grid; gap: 8px; padding: 14px; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--surface-raised); } .auto-employee-card small { display: block; color: var(--text-secondary); line-height: 1.4; overflow-wrap: anywhere; word-break: break-word; } .auto-employee-card p { margin: 0; overflow-wrap: anywhere; } .panel-toolbar { align-items: stretch; flex-wrap: wrap; } }
</style>
