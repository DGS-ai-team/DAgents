<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import * as api from "../api/node.js";
import NavRail from "../components/NavRail.vue";
import AutoBadge from "../components/AutoBadge.vue";

const router = useRouter();
const mobileNavOpen = ref(false);
const items = ref([]);
const counts = ref({});
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const search = ref("");
const status = ref("");
const workspace = ref("");
const appliedSearch = ref(""); const appliedStatus = ref(""); const appliedWorkspace = ref("");
const loaded = ref(false); let requestSeq = 0; let disposed = false;
const loading = ref(false);
const error = ref("");
let refreshTimer;

const statusLabels = { unconfigured: "未配置", disabled: "已停用", paused: "已暂停", running: "执行中", needs_attention: "需处理", standby: "待命", completed: "已完成", stopped: "已停止", failed: "失败" };
const employeeStatuses = Object.entries(statusLabels).filter(([key]) => !["completed", "stopped", "failed"].includes(key));
const stateLabel = (value) => statusLabels[value] || value || "未知";
const formatDate = (value) => value ? new Date(value).toLocaleString() : "—";
const tokenLabel = (item) => item.token_budget ? `${Number(item.tokens_used || 0).toLocaleString()} / ${Number(item.token_budget).toLocaleString()} tokens` : `${Number(item.tokens_used || 0).toLocaleString()} tokens`;
const hasPrev = computed(() => page.value > 1);
const hasNext = computed(() => items.value.length === pageSize.value && page.value * pageSize.value < total.value);

async function load() {
  const seq = ++requestSeq;
  loading.value = true;
  error.value = "";
  try {
    const result = await api.getAutoOverview({ search: appliedSearch.value || undefined, status: appliedStatus.value || undefined, workspace: appliedWorkspace.value || undefined, page: page.value, page_size: pageSize.value });
    if (disposed || seq !== requestSeq) return;
    items.value = result?.items || [];
    counts.value = result?.counts || {};
    total.value = Number(result?.total || 0);
    page.value = Number(result?.page || page.value);
    loaded.value = true;
  } catch (e) {
    if (disposed || seq !== requestSeq) return;
    error.value = e?.message || "无法加载自主任务总览";
  } finally { if (!disposed && seq === requestSeq) loading.value = false; }
}
function changeFilter() { appliedSearch.value = search.value; appliedStatus.value = status.value; appliedWorkspace.value = workspace.value; page.value = 1; void load(); }
function nextPage() { if (hasNext.value) { page.value += 1; void load(); } }
function prevPage() { if (hasPrev.value) { page.value -= 1; void load(); } }
function openWork(item) { router.push({ name: "auto-work", params: { agentId: item.agent_id } }); }
function openChat(item) { router.push({ name: "agents", params: { agentId: item.agent_id } }); }
function openSettings(item) { router.push({ name: "settings-agent-detail", params: { agentId: item.agent_id } }); }
onMounted(() => { appliedSearch.value = search.value; appliedStatus.value = status.value; appliedWorkspace.value = workspace.value; void load(); refreshTimer = window.setInterval(() => void load(), 30000); });
onUnmounted(() => { disposed = true; requestSeq += 1; if (refreshTimer) window.clearInterval(refreshTimer); });
</script>

<template>
  <div class="app__body app__body--chat-v61 auto-overview-shell">
    <button type="button" class="mobile-agent-nav-toggle" :aria-expanded="mobileNavOpen ? 'true' : 'false'" @click="mobileNavOpen = !mobileNavOpen">{{ mobileNavOpen ? "收起 Agent 列表" : "选择 Agent" }}</button>
    <aside class="app__col app__col--agents" :class="{ 'app__col--agents-mobile-open': mobileNavOpen }"><NavRail @switch="(id) => { mobileNavOpen = false; router.push({ name: 'agents', params: { agentId: id } }); }" @create="router.push({ name: 'agents', query: { createAgent: '1' } })" /></aside>
    <main class="app__main-col auto-overview" aria-labelledby="auto-overview-title">
      <header class="auto-overview__header">
        <div><p class="auto-overview__eyebrow">自主任务</p><h1 id="auto-overview-title">Auto 总览</h1><p class="auto-overview__intro">查看 Auto Agent 的工作状态、当前任务与预算使用情况。</p></div>
        <button type="button" class="auto-overview__refresh" :disabled="loading" @click="load">{{ loading ? "刷新中…" : "刷新" }}</button>
      </header>
      <section class="auto-overview__counts" aria-label="状态统计">
        <button type="button" class="auto-overview__count" @click="status=''; changeFilter()"><strong>{{ loaded ? (counts.total ?? total) : '—' }}</strong><span>Auto Agent</span></button>
        <button type="button" class="auto-overview__count" @click="status='running'; changeFilter()"><strong>{{ loaded ? (counts.running || 0) : '—' }}</strong><span>执行中</span></button>
        <button type="button" class="auto-overview__count" @click="status='needs_attention'; changeFilter()"><strong>{{ loaded ? (counts.needs_attention || 0) : '—' }}</strong><span>需处理</span></button>
      </section>
      <section class="auto-overview__filters" aria-label="筛选">
        <input v-model="search" type="search" placeholder="搜索名称、职责或成果" aria-label="搜索 Auto Agent" @keydown.enter="changeFilter" />
        <select v-model="status" aria-label="状态筛选" @change="changeFilter"><option value="">全部状态</option><option v-for="([key, label]) in employeeStatuses" :key="key" :value="key">{{ label }}</option></select>
        <input v-model="workspace" type="search" placeholder="工作目录" aria-label="工作目录筛选" @keydown.enter="changeFilter" />
        <button type="button" class="auto-overview__filter-button" @click="changeFilter">筛选</button>
      </section>
      <p v-if="error" class="auto-overview__message auto-overview__message--error" role="alert">{{ error }} <button type="button" class="auto-overview__refresh" @click="load">重试</button></p>
      <div v-else-if="loading && !items.length" class="auto-overview__message" role="status">正在加载 Auto Agent…</div>
      <div v-else-if="!items.length" class="auto-overview__empty">暂无符合条件的 Auto Agent</div>
      <section v-else class="auto-overview__table-wrap" aria-live="polite">
        <div class="auto-overview__table" role="table" aria-label="Auto Agent 列表">
          <div class="auto-overview__row auto-overview__row--head" role="row"><span>智能体</span><span>状态</span><span>自主任务</span><span>下次安排</span><span>累计用量</span><span>操作</span></div>
          <article v-for="item in items" :key="item.agent_id" class="auto-overview__row" role="row">
            <div data-label="智能体" class="auto-overview__agent" :title="item.agent_id"><strong>{{ item.display_name || item.agent_id }}</strong><AutoBadge :agent="item" /><small>{{ item.workspace?.path || '独立工作目录' }}</small></div>
            <div data-label="状态"><span class="auto-overview__state" :class="`auto-overview__state--${item.state}`">{{ stateLabel(item.state) }}</span><small v-if="item.state_reason">{{ item.state_reason }}</small></div>
            <div data-label="自主任务"><strong>{{ item.goal_title || (item.state === 'unconfigured' ? '尚未配置' : '暂无当前任务') }}</strong><small v-if="item.last_summary">{{ item.last_summary }}</small><small v-if="item.role_objective">职责：{{ item.role_objective }}</small></div>
            <div data-label="下次安排">{{ formatDate(item.next_at) }}</div>
            <div data-label="累计用量">{{ tokenLabel(item) }}<small v-if="item.unknown_usage">用量待确认</small></div>
            <div data-label="操作" class="auto-overview__actions"><button type="button" @click="openWork(item)">打开工作页</button><button type="button" @click="openChat(item)">打开聊天</button><button type="button" @click="openSettings(item)">设置</button></div>
          </article>
        </div>
      </section>
      <footer class="auto-overview__pagination"><span>共 {{ total }} 个</span><button type="button" :disabled="!hasPrev" @click="prevPage">上一页</button><span>第 {{ page }} 页</span><button type="button" :disabled="!hasNext" @click="nextPage">下一页</button></footer>
    </main>
  </div>
</template>

<style scoped>
.auto-overview-shell{display:flex;min-height:100vh}.auto-overview{flex:1;min-width:0;max-width:1440px;margin:0 auto;padding:40px clamp(20px,4vw,56px);color:var(--color-text)}.auto-overview__header{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:24px}.auto-overview__eyebrow{margin:0;color:var(--text-secondary);font-size:12px}.auto-overview h1{margin:4px 0;font-size:26px}.auto-overview__intro{margin:8px 0;color:var(--text-secondary)}.auto-overview__refresh,.auto-overview__filter-button,.auto-overview__actions button,.auto-overview__pagination button{border:1px solid var(--border-subtle);border-radius:6px;padding:8px 12px;background:var(--color-surface-elevated);color:var(--color-text);cursor:pointer}.auto-overview__refresh:disabled,.auto-overview__pagination button:disabled{opacity:.5;cursor:default}.auto-overview__counts{display:flex;gap:12px;margin-bottom:22px}.auto-overview__count{display:flex;flex-direction:column;min-width:130px;padding:14px 16px;border:1px solid var(--border-subtle);border-radius:8px}.auto-overview__count strong{font-size:22px}.auto-overview__count span,.auto-overview small{color:var(--text-secondary);font-size:12px}.auto-overview__filters{display:grid;grid-template-columns:minmax(180px,2fr) minmax(130px,1fr) minmax(180px,1.5fr) auto;gap:10px;margin-bottom:18px}.auto-overview__filters input,.auto-overview__filters select{min-width:0;padding:9px 10px;border:1px solid var(--border-subtle);border-radius:6px;background:var(--color-surface);color:var(--color-text)}.auto-overview__table-wrap{overflow-x:auto;border-top:1px solid var(--border-subtle)}.auto-overview__table{min-width:800px}.auto-overview__row{display:grid;grid-template-columns:1.2fr .8fr 1.5fr 1fr 1.1fr 150px;gap:14px;align-items:center;padding:14px 4px;border-bottom:1px solid var(--border-subtle)}.auto-overview__row--head{color:var(--text-secondary);font-size:12px}.auto-overview__agent,.auto-overview__row>div{min-width:0}.auto-overview__agent,.auto-overview__row>div:not(.auto-overview__actions){display:flex;flex-direction:column;gap:3px}.auto-overview__agent strong{overflow-wrap:anywhere}.auto-overview__state{font-size:13px;font-weight:650}.auto-overview__state--needs_attention{color:var(--color-warning)}.auto-overview__state--running{color:var(--color-accent)}.auto-overview__state--failed{color:var(--color-danger)}.auto-overview__actions{display:flex;flex-wrap:wrap;gap:6px}.auto-overview__message,.auto-overview__empty{padding:42px 12px;text-align:center;color:var(--text-secondary)}.auto-overview__message--error{color:var(--color-danger)}.auto-overview__message button{margin-left:8px}.auto-overview__pagination{display:flex;justify-content:flex-end;align-items:center;gap:10px;padding-top:16px;color:var(--text-secondary);font-size:12px}@media(max-width:760px){.auto-overview{padding:24px 16px}.auto-overview__header{align-items:stretch;flex-direction:column}.auto-overview__refresh{align-self:flex-start}.auto-overview__counts{overflow-x:auto}.auto-overview__filters{grid-template-columns:1fr 1fr}.auto-overview__filters input:first-child,.auto-overview__filter-button{grid-column:1/-1}.auto-overview__table{min-width:0}.auto-overview__row--head{display:none}.auto-overview__row{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding:16px 0}.auto-overview__row>div:nth-child(3){grid-column:1/-1}.auto-overview__row>div:nth-child(6){grid-column:1/-1}.auto-overview__actions{justify-content:flex-start}.auto-overview__pagination{justify-content:space-between}}
 .auto-overview__count{background:transparent;color:var(--color-text);text-align:left;cursor:pointer}.auto-overview__count:focus-visible,.auto-overview__refresh:focus-visible{outline:2px solid var(--color-accent);outline-offset:2px}
@media(max-width:760px){.auto-overview__row:not(.auto-overview__row--head)>div{position:relative;padding-top:18px}.auto-overview__row:not(.auto-overview__row--head)>div::before{content:attr(data-label);position:absolute;top:0;left:0;color:var(--text-secondary);font-size:11px}}
@media(max-width:760px){.auto-overview{box-sizing:border-box;width:100%;max-width:100%;min-height:0;height:auto;max-height:none;padding:24px 16px;overflow-y:auto}.auto-overview__counts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;overflow:visible}.auto-overview__count{min-width:0;padding:12px 8px}.auto-overview__table{min-width:0}.auto-overview__row--head{display:none!important}}
</style>
