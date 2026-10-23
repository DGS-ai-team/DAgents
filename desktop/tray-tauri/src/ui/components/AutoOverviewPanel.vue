<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { getAutoOverview } from '@desktop-ui/api/node.js'
const props = defineProps({ actionError: { type: String, default: '' } })
const emit = defineEmits(['open-chat', 'open-config', 'context-menu'])
const items = ref([]), counts = ref({}), total = ref(0), page = ref(1), pageSize = ref(20), search = ref(''), status = ref(''), workspace = ref(''), loading = ref(false), loaded = ref(false), error = ref('')
let sequence = 0, disposed = false, timer = 0
const statusNames = { working: '工作中', activation_off: '自主激活关闭', standby: '待命', needs_attention: '需处理' }
const statusCounts = computed(() => [
  ['Auto Agent', counts.value.total ?? total.value], ['工作中', counts.value.working || 0], ['需处理', counts.value.needs_attention || 0],
])
function date(value, empty = '暂未安排') { if (!value) return empty; const d = new Date(value); return Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1 ? empty : d.toLocaleString() }
function todos(item) { const c = item.todo_counts; if (c && typeof c === 'object') { const totalCount = Number(c.total ?? 0), done = Number(c.completed ?? 0); const summary = Array.isArray(item.todo_summary) ? item.todo_summary.join('；') : String(item.todo_summary || ''); return `${Math.max(0,totalCount-done)} 项未完成 · ${done} 项已完成${summary ? ` · ${summary}` : ''}` } return Array.isArray(item.todo_summary) ? item.todo_summary.join('；') || '暂无待办' : item.todo_summary || '暂无待办' }
function dreaming(item) { const d = item.dreaming; if (!d) return '未获取'; const labels = { disabled: '已关闭', waiting: '等待执行', running: '整理中', succeeded: '最近成功', failed: '上次失败', recovery_pending: '等待恢复' }; return `${labels[d.state] || d.state || '未知'}${d.next_at ? ` · 下次 ${date(d.next_at)}` : ''}` }
async function load() { const seq = ++sequence; loading.value = true; error.value = ''; try { const result = await getAutoOverview({ search: search.value || undefined, status: status.value || undefined, workspace: workspace.value || undefined, page: page.value, page_size: pageSize.value }); if (disposed || seq !== sequence) return; items.value = Array.isArray(result?.items) ? result.items : Array.isArray(result) ? result : []; counts.value = result?.counts || {}; total.value = Number(result?.total ?? items.value.length); page.value = Number(result?.page || page.value); pageSize.value = Number(result?.page_size || pageSize.value); loaded.value = true } catch(e) { if (!disposed && seq === sequence) error.value = e?.message || '无法加载 Auto 总览' } finally { if (!disposed && seq === sequence) loading.value = false } }
function changeFilter() { page.value = 1; void load() }
function selectStatus(value) { status.value = value; changeFilter() }
function next() { if (page.value * pageSize.value < total.value) { page.value++; void load() } }
function prev() { if (page.value > 1) { page.value--; void load() } }
onMounted(() => { void load(); timer = window.setInterval(() => void load(), 30000) })
onUnmounted(() => { disposed = true; sequence++; if (timer) window.clearInterval(timer) })
defineExpose({ refresh: load })
</script>

<template>
  <main class="auto-panel">
    <header><div><h1>Auto 总览</h1><p>运行状态、待办与下次唤醒</p></div><button @click="load" :disabled="loading">{{ loading ? '刷新中…' : '刷新' }}</button></header>
    <section class="auto-counts"><button v-for="([label,value],i) in statusCounts" :key="label" @click="selectStatus(['','working','needs_attention'][i])"><strong>{{ loaded ? value : '—' }}</strong><span>{{ label }}</span></button></section>
    <form class="auto-filters" @submit.prevent="changeFilter"><input v-model="search" placeholder="搜索 Agent" aria-label="搜索 Agent"><select v-model="status" aria-label="状态"><option value="">全部状态</option><option value="working">工作中</option><option value="activation_off">自主激活关闭</option><option value="standby">待命</option><option value="needs_attention">需处理</option></select><input v-model="workspace" placeholder="工作目录" aria-label="工作目录"><button>筛选</button></form>
    <p v-if="props.actionError" class="auto-error" role="alert">{{ props.actionError }}</p>
    <p v-if="error" class="auto-error" role="alert">{{ error }} <button @click="load">重试</button></p><p v-else-if="loading && !loaded" class="auto-empty">正在加载 Auto Agent…</p><p v-else-if="!items.length" class="auto-empty">暂无 Auto Agent</p>
    <section v-else class="auto-list" aria-live="polite"><article v-for="item in items" :key="item.agent_id" class="auto-row" tabindex="0" @contextmenu.prevent.stop="emit('context-menu', { event: $event, anchor: $event.currentTarget, item })" @keydown="event => { if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) { event.preventDefault(); emit('context-menu', { event, anchor: event.currentTarget, item }) } }"><div class="agent"><strong>{{ item.display_name || item.agent_id }}</strong><small>{{ item.agent_id }}</small></div><div><span class="state">{{ statusNames[item.state] || item.state || '未知' }}</span><small v-if="item.state_reason">{{ item.state_reason }}</small><small>Dreaming：{{ dreaming(item) }}</small></div><div><small>下次唤醒</small><span>{{ date(item.next_at) }}</span></div><div><small>待办</small><span>{{ todos(item) }}</span></div><div class="actions"><button @click="emit('open-chat', item.agent_id)">打开聊天</button><button @click="emit('open-config', item.agent_id)">设置</button></div></article></section>
    <footer v-if="total > pageSize"><span>共 {{ total }} 个 · 第 {{ page }} 页</span><div><button @click="prev" :disabled="page <= 1">上一页</button><button @click="next" :disabled="page * pageSize >= total">下一页</button></div></footer>
  </main>
</template>

<style scoped>
.auto-panel{height:auto;max-height:100%;min-height:0;box-sizing:border-box;overflow:auto;background:#f7f9fa;color:#263740;padding:18px 22px}.auto-panel header{display:flex;justify-content:space-between;align-items:flex-start;border-bottom:1px solid #dce5eb;padding:0 32px 10px 0}.auto-panel header small,.auto-panel small{color:#627581}.auto-panel header>div>small{letter-spacing:.12em}.auto-panel h1{font-size:19px;margin:0}.auto-panel p{color:#627581;margin:5px 0}.auto-panel button,.auto-panel input,.auto-panel select{background:#ffffff;border:1px solid #d1d9de;border-radius:6px;color:#263740;padding:8px 10px}.auto-panel button{cursor:pointer}.auto-panel button:disabled{opacity:.5}.auto-counts{display:flex;gap:10px;margin:16px 0}.auto-counts button{min-width:130px;display:flex;flex-direction:column;gap:4px;text-align:left}.auto-counts strong{font-size:20px}.auto-counts span{color:#627581}.auto-filters{display:grid;grid-template-columns:2fr 1fr 1.5fr auto;gap:8px;margin-bottom:14px}.auto-list{border-top:1px solid #dce5eb}.auto-row:focus-visible{outline:2px solid #77838a;outline-offset:2px;border-radius:4px}.auto-row{display:grid;grid-template-columns:1.2fr 1.15fr 1fr 1.5fr 210px;gap:14px;padding:14px 4px;border-bottom:1px solid #dce5eb;align-items:center}.auto-row>div{min-width:0;display:flex;flex-direction:column;gap:4px;overflow-wrap:anywhere}.auto-row .state{color:#486478}.auto-row .actions{flex-direction:row;flex-wrap:wrap}.auto-empty,.auto-error{padding:36px;text-align:center}.auto-error{color:#a4262c}.auto-panel footer{display:flex;justify-content:space-between;padding-top:14px;color:#627581}.auto-panel footer div{display:flex;gap:8px}@media(max-width:900px){.auto-filters{grid-template-columns:1fr 1fr}.auto-row{grid-template-columns:1fr 1fr}.auto-row .actions{grid-column:1/-1}}
</style>
