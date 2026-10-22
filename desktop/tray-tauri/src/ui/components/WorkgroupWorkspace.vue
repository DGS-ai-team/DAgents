<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import * as api from '@desktop-ui/api/node.js'
import { confirmDesktopAction } from '@desktop-ui/utils/desktopDialogs.js'
import { isCurrentWorkgroupRequest } from '../workgroup-state.js'
import WorkgroupApprovalCard from '@desktop-ui/components/WorkgroupApprovalCard.vue'
import WorkgroupToolRow from '@desktop-ui/components/WorkgroupToolRow.vue'
import WorkgroupUserInformationCard from '@desktop-ui/components/WorkgroupUserInformationCard.vue'
import { useWorkgroupTimeline } from '@desktop-ui/composables/useWorkgroupTimeline.js'
import ResourceMarkdown from '@desktop-ui/components/ResourceMarkdown.vue'
import { workgroupApprovalItemsFromMetadata } from '@desktop-ui/utils/workgroupApproval.js'
import { workgroupMemberInformationRequests } from '@desktop-ui/utils/workgroupUserInformation.js'

const props = defineProps({ workgroupId: { type: String, default: '' } })
const emit = defineEmits(['select', 'list', 'name', 'open-connection-settings', 'removed', 'context-menu'])
const groups = ref([]), group = ref(null), members = ref([]), events = ref([]), hitl = ref([])
const llmConfigs = ref([]), selectedProfileId = ref('')
const humanQueue = ref([]), queueBusy = ref(false), editingQueueId = ref(''), editQueueDraft = ref('')
const agents = ref([]), draft = ref(''), newName = ref(''), newMember = ref({ agent_id: '', display_name: '' })
const selfNodeId = ref('')
const loading = ref(false), busy = ref(false), error = ref(''), notice = ref(''), creating = ref(false), manageUnavailable = ref(false)
const memberNameById = computed(() => Object.fromEntries(members.value.map(m => [m.member_id, m.display_name || m.agent_id || m.member_id])))
const publishDisabled = computed(() => busy.value || !selectedProfileId.value || !llmConfigs.value.some(item => String(item.id || '') === selectedProfileId.value))
const selectedGroupEntry = computed(() => groups.value.find(item => item.workgroup_id === props.workgroupId) || group.value)
const selectedIsSubscribed = computed(() => Boolean(selectedGroupEntry.value?.subscribed))
const liveUser = ref(null), liveAssistant = ref(null), sending = ref(false), streamMode = ref(''), streamActorId = ref('leader'), streamPhase = ref(''), streamToolName = ref('')
const statusWatermarkSeq = ref(0), expandedMemberReports = ref({}), expandedAssignTasks = ref({})
let epoch = 0, pollTimer = 0, pollResolve = null, queueTimer = 0, queueResolve = null, sendWake = null, pollDelay = 1500, mounted = true
let retryClientMessageId = '', retryClientMessageText = ''
const isCurrent = (token, id = props.workgroupId) => isCurrentWorkgroupRequest({ epoch: token, workgroupId: id }, { epoch, workgroupId: props.workgroupId, mounted })
const memberApprovalByAssign = computed(() => {
  const out = {}
  for (const item of hitl.value) { const metadata = item?.metadata || {}; const aid = String(metadata.assign_id || '').trim(); if (metadata.source !== 'agent_ref' || !aid || !workgroupApprovalItemsFromMetadata(metadata).length) continue; const current = out[aid]; if (!current || String(item.created_at || '') >= String(current.created_at || '')) out[aid] = item }
  return out
})
const showLiveAssistant = computed(() => Boolean(liveAssistant.value?.text?.trim()) && streamMode.value !== 'member')
const timeline = useWorkgroupTimeline({ events, memberNameById, memberApprovalByAssign, selfNodeId, selfNodeName: ref('你'), liveUser, showLiveAssistant, liveAssistant, sending, streamMode, streamActorId, streamPhase, streamToolName, statusWatermarkSeq, expandedMemberReports, expandedAssignTasks })
const eventGroups = timeline.eventGroups
const memberInfoRequests = computed(() => workgroupMemberInformationRequests(hitl.value, memberNameById.value))
const supervisorHitl = computed(() => hitl.value.find(item => { const metadata = item?.metadata || {}; return (metadata.source || item.source) !== 'agent_ref' && !workgroupApprovalItemsFromMetadata(metadata).length }) || null)
function unwrap(value, key) { return Array.isArray(value) ? value : Array.isArray(value?.[key]) ? value[key] : [] }
function clearPolling() { if (pollTimer) window.clearTimeout(pollTimer); pollTimer = 0; const resolve = pollResolve; pollResolve = null; resolve?.() }
function clearQueueWatch() { if (queueTimer) window.clearTimeout(queueTimer); queueTimer = 0; const resolve = queueResolve; queueResolve = null; resolve?.() }
function epochIsCurrent(token) { return mounted && token === epoch }
async function loadList() {
  const token = epoch; loading.value = true; error.value = ''; notice.value = ''; manageUnavailable.value = false
  try {
    const result = await api.getDesktopWorkgroupList()
    if (!epochIsCurrent(token)) return
    groups.value = unwrap(result, 'workgroups').map(item => ({ ...item, subscribed: item?.subscribed === true }))
    emit('list', groups.value.filter(item => item.subscribed))
  }
  catch (e) {
    if (!epochIsCurrent(token)) return
    const message = String(e?.message || '无法加载工作组')
    if (/manage.*(?:not enabled|disabled|not configured)|workgroup.*not enabled/i.test(message)) {
      groups.value = []
      emit('list', [])
      manageUnavailable.value = true
      notice.value = 'Manage 尚未启用。请先配置连接并启用 Workgroup，然后刷新工作组列表。'
    } else error.value = message
  }
  finally { if (epochIsCurrent(token)) loading.value = false }
}
async function refreshDetail(token = epoch, id = props.workgroupId, { includeAgents = false } = {}) {
  if (!id) return
  const snapshot = await api.getDesktopWorkgroupSnapshot(id, { includeAgents })
  if (!isCurrent(token, id)) return
  group.value = snapshot?.workgroup?.workgroup || snapshot?.workgroup
  emit('name', String(group.value?.display_name || id))
  selectedProfileId.value = String(group.value?.llm_profile_id || '')
  members.value = unwrap(snapshot, 'members'); events.value = unwrap(snapshot, 'events')
  hitl.value = unwrap(snapshot, 'hitl')
  if (Array.isArray(snapshot?.agents)) agents.value = snapshot.agents
  llmConfigs.value = unwrap(snapshot, 'llm_configs')
  humanQueue.value = Array.isArray(snapshot?.human_queue?.items) ? snapshot.human_queue.items : []
  queueBusy.value = Boolean(snapshot?.human_queue?.busy)
  selfNodeId.value = String(snapshot?.node_id || '').trim()
}
async function refreshProgress(token = epoch, id = props.workgroupId) {
  const snapshot = await api.getDesktopWorkgroupProgress(id)
  if (!isCurrent(token, id)) return null
  events.value = unwrap(snapshot, 'events')
  const queue = snapshot?.human_queue || null
  if (queue) {
    humanQueue.value = Array.isArray(queue.items) ? queue.items : []
    queueBusy.value = Boolean(queue.busy)
  }
  return queue
}
async function loadHumanQueue(token = epoch, id = props.workgroupId) {
  const result = await api.fetchWorkgroupHumanQueue(id)
  if (!isCurrent(token, id)) return null
  humanQueue.value = Array.isArray(result?.items) ? result.items : []
  queueBusy.value = Boolean(result?.busy)
  return result
}
function watchQueue(token, id, startedAt = Date.now()) {
  clearQueueWatch()
  const step = async () => {
    while (isCurrent(token, id) && Date.now() - startedAt < 60000) {
      await new Promise(resolve => { queueResolve = resolve; queueTimer = window.setTimeout(resolve, 3000) })
      queueTimer = 0; queueResolve = null
      if (!isCurrent(token, id)) return
      try {
        const result = await refreshProgress(token, id)
        if (result && !result.busy && !(result.items || []).length) return
      } catch (e) { if (isCurrent(token, id)) error.value = e?.message || '刷新工作组进度失败' }
    }
    if (isCurrent(token, id) && (queueBusy.value || humanQueue.value.length)) notice.value = '队列仍在等待或处理中；可点击刷新查看最新状态。'
  }
  void step()
}
async function openGroup(id) { emit('select', String(id || '')) }
async function createGroup() {
  if (manageUnavailable.value) return
  const name = newName.value.trim(); if (!name || busy.value) return
  const token = epoch; busy.value = true; error.value = ''
  try { const res = await api.createWorkgroup(name); if (!epochIsCurrent(token)) return; const created = res?.workgroup || res; newName.value = ''; creating.value = false; await loadList(); if (epochIsCurrent(token) && created?.workgroup_id) emit('select', created.workgroup_id) }
  catch(e) { if (epochIsCurrent(token)) error.value = e?.message || '创建工作组失败' } finally { if (epochIsCurrent(token)) busy.value = false }
}
async function addMember() {
  const name = newMember.value.display_name.trim(); if (!name || !newMember.value.agent_id || busy.value) return
  const id = props.workgroupId, token = epoch; busy.value = true; error.value = ''
  try { const available = agents.value.find(a => a.agent_id === newMember.value.agent_id); await api.createWorkgroupMember(id, { agent_id: newMember.value.agent_id, display_name: name, description: '', ...(available?.node_id ? { home_node_id: available.node_id } : {}) }); if (!isCurrent(token,id)) return; newMember.value = { agent_id: '', display_name: '' }; await refreshDetail(token,id) }
  catch(e) { if (isCurrent(token,id)) error.value = e?.message || '添加成员失败' } finally { if (isCurrent(token,id)) busy.value = false }
}
async function subscribe() { await mutate(api.subscribeWorkgroup, '已订阅工作组') }
async function subscribeListed(item) {
  const id = String(item?.workgroup_id || '').trim()
  if (!id || busy.value) return
  const token = epoch; busy.value = true; error.value = ''
  try { await api.subscribeWorkgroup(id); if (!epochIsCurrent(token)) return; await loadList(); if (epochIsCurrent(token)) notice.value = `已订阅「${item.display_name || id}」` }
  catch (e) { if (epochIsCurrent(token)) error.value = e?.message || '订阅工作组失败' }
  finally { if (epochIsCurrent(token)) busy.value = false }
}
async function removeWorkgroup(item = group.value) {
  const id = String(item?.workgroup_id || props.workgroupId || '').trim()
  if (!id || busy.value || !item?.subscribed) return
  const label = String(item?.display_name || id)
  if (!await confirmDesktopAction(`从此 Node 移除工作组「${label}」？\n这会取消当前 Node 的订阅，Manage 中的工作组数据会保留。`)) return
  const token = epoch; busy.value = true; error.value = ''
  try {
    await api.unsubscribeWorkgroup(id)
    if (!epochIsCurrent(token)) return
    await loadList()
    if (!epochIsCurrent(token)) return
    notice.value = `已从此 Node 移除「${label}」；Manage 中的数据仍保留。`
    emit('removed', id)
  } catch (e) { if (epochIsCurrent(token)) error.value = e?.message || '移除工作组失败' }
  finally { if (epochIsCurrent(token)) busy.value = false }
}
function requestWorkgroupContextMenu(event, item) {
  if (!item?.workgroup_id) return
  const current = String(item.workgroup_id) === String(props.workgroupId)
  const items = []
  if (!current) items.push({ label: '打开工作组', run: () => openGroup(item.workgroup_id) })
  if (item.subscribed) items.push({ label: '从此 Node 移除工作组', danger: true, disabled: busy.value, run: () => removeWorkgroup(item) })
  if (items.length) {
    event.preventDefault()
    event.stopPropagation()
    emit('context-menu', { event, anchor: event.currentTarget, title: item.display_name || item.workgroup_id, items })
  }
}
function onWorkgroupContextMenuKeydown(event, item) {
  if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return
  event.preventDefault()
  requestWorkgroupContextMenu(event, item)
}
async function publish() { if (publishDisabled.value) return; await mutate(api.publishWorkgroup, '工作组已发布') }
async function changeSupervisorProfile() {
  const id = props.workgroupId, token = epoch, profile = llmConfigs.value.find(item => String(item.id || '') === selectedProfileId.value)
  if (!profile || !id || busy.value) return
  busy.value = true; error.value = ''
  try { const next = await api.patchWorkgroup(id, { llm_profile_id: profile.id, llm_profile_revision: profile.revision || profile.profile_revision || '1' }); if (!isCurrent(token,id)) return; group.value = next?.workgroup || next; notice.value = 'Supervisor 模型已更新' }
  catch(e) { if (isCurrent(token,id)) { selectedProfileId.value = String(group.value?.llm_profile_id || ''); error.value = e?.message || '更新 Supervisor 模型失败' } }
  finally { if (isCurrent(token,id)) busy.value = false }
}
async function mutate(fn, message) { const id = props.workgroupId, token = epoch; busy.value = true; error.value = ''; try { await fn(id); if (!isCurrent(token,id)) return; await refreshDetail(token,id); await loadList(); if (isCurrent(token,id)) notice.value = message } catch(e) { if (isCurrent(token,id)) error.value = e?.message || '操作失败' } finally { if (isCurrent(token,id)) busy.value = false } }
async function safeRefresh() { try { if (props.workgroupId) await refreshDetail(); else await loadList() } catch(e) { error.value = e?.message || '刷新失败' } }
async function send() {
  const text = draft.value.trim(); if (!text || busy.value || !props.workgroupId) return
  const token = epoch, id = props.workgroupId; busy.value = true; draft.value = ''; error.value = ''; notice.value = '工作组正在处理…'
  sending.value = true; liveUser.value = { id: `desktop-user-${Date.now()}`, text, directMemberId: '' }; liveAssistant.value = { id: `desktop-assistant-${Date.now()}`, text: '' }; streamMode.value = 'leader'; statusWatermarkSeq.value = events.value.reduce((max, event) => Math.max(max, Number(event?.seq || 0)), 0)
  clearPolling(); pollDelay = 1500
  const clientMessageId = retryClientMessageText === text && retryClientMessageId ? retryClientMessageId : globalThis.crypto?.randomUUID?.() || `desktop-${Date.now()}`
  const submission = api.postWorkgroupMessage(id, text, clientMessageId)
  let submitted = false, postResult = null
  const monitoredSubmission = submission.then(value => { postResult = value; submitted = true; clearPolling(); return value }, error => { submitted = true; clearPolling(); throw error })
  const monitor = async () => { while (isCurrent(token, id) && busy.value && !submitted) { await new Promise(resolve => { pollResolve = resolve; pollTimer = window.setTimeout(resolve, pollDelay) }); pollTimer = 0; pollResolve = null; if (!isCurrent(token, id) || !busy.value || submitted) break; try { await refreshProgress(token, id) } catch(e) { if (isCurrent(token, id)) error.value = e?.message || '刷新工作组进度失败' } pollDelay = Math.min(5000, pollDelay + 500) } }
  let wakeThisSend
  const invalidated = new Promise(resolve => { wakeThisSend = resolve; sendWake = resolve })
  try {
    await Promise.race([Promise.all([monitoredSubmission, monitor()]), invalidated])
    if (isCurrent(token, id)) {
      retryClientMessageId = ''; retryClientMessageText = ''
      await refreshDetail(token,id)
      if (postResult?.queued) {
        if (!humanQueue.value.some(item => String(item?.queue_id || '') === String(postResult.queue_id || ''))) await loadHumanQueue(token,id)
        const position = Number(postResult.position || postResult.queue?.items?.find(item => item.queue_id === postResult.queue_id)?.position || 0)
        notice.value = `消息已排队${position ? ` · 第 ${position} 位` : ''}；可在队列中修改或取消。`
        watchQueue(token,id)
      } else {
        const status = String(postResult?.loop?.status || '').toLowerCase()
        notice.value = status === 'succeeded' ? '本轮已完成' : status === 'canceled' || status === 'cancelled' ? '本轮已取消' : status === 'failed' ? '本轮失败' : status === 'needs_attention' || hitl.value.length ? '本轮暂停，等待你处理' : status ? `本轮状态：${status}` : '消息已提交，状态待确认。'
      }
      busy.value = false; sending.value = false; liveUser.value = null; liveAssistant.value = null; clearPolling()
    }
  }
  catch(e) { if (isCurrent(token, id)) { error.value = e?.message || '发送失败'; if (!draft.value.trim()) draft.value = text; retryClientMessageId = clientMessageId; retryClientMessageText = text; busy.value = false; sending.value = false; liveUser.value = null; liveAssistant.value = null; notice.value = ''; clearPolling() } }
  finally { if (sendWake === wakeThisSend) { sendWake = null; wakeThisSend() } }
}
async function stop() { const id = props.workgroupId, token = epoch; if (!id) return; try { await api.cancelWorkgroupTurn(id); if (isCurrent(token,id)) { notice.value = '已请求停止'; await refreshDetail(token,id) } } catch(e) { if (isCurrent(token,id)) error.value = e?.message || '停止失败' } }
async function editQueued(item) { editingQueueId.value = item.queue_id; editQueueDraft.value = item.text || '' }
async function saveQueued(item) { const id=props.workgroupId, token=epoch; try { await api.patchWorkgroupHumanQueueItem(id,item.queue_id,editQueueDraft.value.trim()); if (isCurrent(token,id)) { editingQueueId.value=''; editQueueDraft.value=''; await loadHumanQueue(token,id) } } catch(e) { if(isCurrent(token,id)) error.value=e?.message || '修改排队消息失败' } }
async function removeQueued(item) { const id=props.workgroupId, token=epoch; try { await api.cancelWorkgroupHumanQueueItem(id,item.queue_id); if(isCurrent(token,id)) { await loadHumanQueue(token,id); notice.value='已取消排队消息' } } catch(e) { if(isCurrent(token,id)) error.value=e?.message || '取消排队消息失败' } }
async function sendQueuedNow(item) { const id=props.workgroupId, token=epoch; try { await api.sendWorkgroupHumanQueueItemNow(id,item.queue_id); if(isCurrent(token,id)) { await loadHumanQueue(token,id); notice.value='已请求优先发送此消息'; watchQueue(token,id) } } catch(e) { if(isCurrent(token,id)) error.value=e?.message || '优先发送失败' } }
async function resolve(hitlId, resolution) { const id = props.workgroupId, token = epoch; busy.value = true; error.value = ''; try { await api.resolveWorkgroupHITL(id, hitlId, resolution); if (isCurrent(token,id)) await refreshDetail(token,id) } catch(e) { if (isCurrent(token,id)) error.value = e?.message || '处理待办失败' } finally { if (isCurrent(token,id)) busy.value = false } }
function resolveApproval(approval, callId, approve) { const all = (approval?.allItems || approval?.items || []).map(row => row.callId).filter(Boolean); const selected = String(callId || '').trim(); const approved = selected ? (approve ? [selected] : []) : (approve ? all : []); const rejected = selected ? (approve ? [] : [selected]) : (approve ? [] : all); return resolve(approval.hitlId, { type: 'selection', approved, rejected }) }
watch(() => props.workgroupId, async () => { epoch++; const token = epoch; sendWake?.(); sendWake = null; clearPolling(); clearQueueWatch(); busy.value = false; sending.value = false; liveUser.value = null; liveAssistant.value = null; group.value = null; members.value = []; events.value = []; hitl.value = []; humanQueue.value = []; queueBusy.value=false; editingQueueId.value=''; agents.value = []; llmConfigs.value = []; selectedProfileId.value = ''; error.value = ''; notice.value = ''; draft.value = ''; retryClientMessageId=''; retryClientMessageText=''; if (!props.workgroupId) { await loadList(); return } loading.value = true; try { await Promise.all([refreshDetail(token, props.workgroupId, { includeAgents: true }), loadList()]); if (epochIsCurrent(token) && group.value?.status === 'active' && (queueBusy.value || humanQueue.value.length)) watchQueue(token,props.workgroupId) } catch(e) { if (epochIsCurrent(token)) { const msg = e?.message || '加载工作组失败'; error.value = /manage/i.test(msg) ? `${msg}；请先启动 Manage 并启用 Workgroup` : msg } } finally { if (epochIsCurrent(token)) loading.value = false } }, { immediate: true })
onUnmounted(() => { mounted = false; epoch++; sendWake?.(); sendWake = null; clearPolling(); clearQueueWatch() })
</script>

<template>
  <main class="wg-workspace">
    <header class="wg-head" :tabindex="workgroupId ? -1 : undefined" @contextmenu="workgroupId && requestWorkgroupContextMenu($event, selectedGroupEntry)" @keydown="workgroupId && onWorkgroupContextMenuKeydown($event, selectedGroupEntry)"><div><h1>{{ group?.display_name || '工作组' }}</h1><p v-if="group">{{ group.status || '状态未知' }} · {{ members.length }} 位成员</p><p v-else>协作消息、成员与待处理请求</p></div><div class="wg-actions"><button @click="safeRefresh" :disabled="loading">刷新</button><button v-if="workgroupId && !selectedIsSubscribed" @click="subscribe" :disabled="busy">订阅</button><button v-if="group?.status === 'configuring'" @click="publish" :disabled="publishDisabled" :title="publishDisabled ? '请先配置并选择可用的 Supervisor 模型' : ''">发布</button><button v-if="!workgroupId" @click="creating = !creating" :disabled="manageUnavailable" :title="manageUnavailable ? '请先启用 Manage 和 Workgroup' : ''">新建工作组</button></div></header>
    <p v-if="error" class="wg-error" role="alert">{{ error }} <button @click="emit('open-connection-settings')">前往连接配置</button></p><p v-if="notice" class="wg-notice" role="status">{{ notice }} <button v-if="manageUnavailable" @click="emit('open-connection-settings')">配置连接</button></p>
    <template v-if="!workgroupId">
      <form v-if="creating && !manageUnavailable" class="wg-create" @submit.prevent="createGroup"><input v-model="newName" placeholder="工作组名称" required><button :disabled="busy">创建</button></form>
      <p v-if="loading" class="wg-muted">正在加载…</p><p v-else-if="!groups.length && !error && !manageUnavailable" class="wg-muted">暂无可访问的工作组</p>
      <div class="wg-list"><article v-for="item in groups" :key="item.workgroup_id" class="wg-group" tabindex="-1" @contextmenu="requestWorkgroupContextMenu($event, item)" @keydown="onWorkgroupContextMenuKeydown($event, item)"><button class="wg-group-open" type="button" @click="openGroup(item.workgroup_id)"><strong>{{ item.display_name || item.workgroup_id }}</strong><span>{{ item.status || '状态未知' }} · {{ item.workgroup_id }}</span></button><button v-if="!item.subscribed" class="wg-group-subscribe" type="button" :disabled="busy" @click="subscribeListed(item)">订阅</button></article></div>
    </template>
    <template v-else>
      <section v-if="group?.status === 'configuring'" class="wg-model"><label for="wg-supervisor-model">Supervisor 模型</label><select id="wg-supervisor-model" v-model="selectedProfileId" @change="changeSupervisorProfile"><option value="">选择模型配置</option><option v-for="profile in llmConfigs" :key="profile.id" :value="profile.id">{{ profile.name || profile.id }}{{ profile.is_default ? '（默认）' : '' }}</option></select><small v-if="llmConfigs.length">新建工作组先使用 Manage 默认模型，可在发布前切换。</small><p v-else class="wg-model-warning" role="status">Manage 尚无当前节点可用的 LLM 配置。请在 Manage 控制台配置并授权模型后刷新；完成前不能发布工作组。</p><button v-if="!llmConfigs.length" @click="emit('open-connection-settings')">前往连接配置</button></section>
      <section class="wg-members"><strong>成员</strong><span v-for="m in members" :key="m.member_id">{{ m.display_name || m.agent_id }}</span><form @submit.prevent="addMember"><select v-model="newMember.agent_id" required><option value="">选择已注册 Agent</option><option v-for="a in agents" :key="a.agent_id" :value="a.agent_id">{{ a.name || a.agent_id }}</option></select><input v-model="newMember.display_name" placeholder="成员名称" required><button :disabled="busy">添加成员</button></form></section>
      <section v-if="queueBusy || humanQueue.length" class="wg-queue"><h2>消息队列 <small>{{ queueBusy ? '当前回合仍在处理' : '等待发送' }}</small></h2><article v-for="item in humanQueue" :key="item.queue_id"><template v-if="editingQueueId === item.queue_id"><textarea v-model="editQueueDraft" aria-label="修改排队消息"/><button @click="saveQueued(item)">保存</button><button @click="editingQueueId = ''; editQueueDraft = ''">取消编辑</button></template><template v-else><p><small>第 {{ item.position || '—' }} 位</small>{{ item.text }}</p><button @click="editQueued(item)">修改</button><button @click="removeQueued(item)">取消排队</button><button @click="sendQueuedNow(item)" :disabled="!queueBusy" title="优先发送会中断当前回合">优先发送</button></template></article></section>
      <section v-if="supervisorHitl" class="wg-hitl"><h2>需要你回答</h2><p>{{ supervisorHitl.prompt || supervisorHitl.message || supervisorHitl.metadata?.question || 'Supervisor 正在等待你的回答' }}</p><input v-model="supervisorHitl._answer" placeholder="输入回答"><button @click="resolve(supervisorHitl.hitl_id, { answer: supervisorHitl._answer || '' })" :disabled="busy">提交回答</button></section>
      <section v-if="memberInfoRequests.length" class="wg-hitl"><h2>成员问题</h2><article v-for="request in memberInfoRequests" :key="request.key"><WorkgroupUserInformationCard :request="request" :busy="busy" @resolve="resolution => resolve(request.hitlId, { type: 'user_information', tool_call_id: request.callId, ...resolution })"/></article></section>
      <section class="wg-timeline" aria-live="polite"><article v-for="group in eventGroups" :key="group.key" :class="{ human: group.role === 'user' }"><small>{{ group.label }}</small><template v-for="item in group.items" :key="item.key"><div v-if="item.kind === 'assign'" class="wg-assign"><strong>{{ item.mention ? `@${item.mention} · ` : '' }}{{ item.done ? item.statusText : '执行中' }}</strong><p>{{ item.taskText }}</p><template v-for="step in item.steps" :key="step.key"><ResourceMarkdown v-if="step.kind === 'content'" class="wg-markdown" :entry="{ kind: 'assistant', text: step.text }" agent-id=""/><WorkgroupApprovalCard v-else-if="step.kind === 'approval'" :approval="step.approval" :hitl-busy="busy" @resolve="(id, ok) => resolveApproval(step.approval, id, ok)"/><WorkgroupToolRow v-else :item="step" :hitl-busy="busy" @resolve="(id, ok) => resolveApproval(step.approval, id, ok)"/></template><details v-if="item.hasReport"><summary>{{ item.reportPreview || '成员结论' }}</summary><ResourceMarkdown class="wg-markdown" :entry="{ kind: 'assistant', text: item.reportText }" agent-id=""/></details></div><WorkgroupToolRow v-else-if="item.kind === 'tool'" :item="item" :hitl-busy="busy" @resolve="(id, ok) => resolveApproval(item.approval, id, ok)"/><div v-else-if="item.kind === 'message'" :class="item.ev.type === 'human_message' ? 'wg-user-message' : 'wg-markdown'">{{ item.ev.type === 'human_message' ? item.ev.text : '' }}<ResourceMarkdown v-if="item.ev.type !== 'human_message'" class="wg-markdown" :entry="{ kind: 'assistant', text: item.ev.text || '' }" agent-id=""/></div><div v-else-if="item.kind === 'live_user'" class="wg-user-message">{{ item.text }}</div><ResourceMarkdown v-else-if="item.kind === 'live_assistant'" class="wg-markdown" :entry="{ kind: 'assistant', text: item.text || '' }" agent-id=""/><div v-else class="wg-muted">{{ item.text || item.statusText || item.kind }}</div></template></article><p v-if="!eventGroups.length" class="wg-muted">暂无消息</p></section>
      <form class="wg-composer" @submit.prevent="send"><textarea v-model="draft" placeholder="发送给工作组…" @keydown.ctrl.enter.prevent="send"/><button v-if="busy" type="button" @click="stop">停止</button><button class="wg-send-button" :disabled="!draft.trim()" aria-label="发送到工作组" title="发送到工作组"><span class="wg-send-icon" aria-hidden="true"/></button></form>
    </template>
  </main>
</template>

<style scoped>
.wg-workspace{height:100%;min-height:0;display:flex;flex-direction:column;background:#f7f9fa;color:#263740;padding:22px 26px;gap:14px;overflow:auto}.wg-head{display:flex;justify-content:space-between;align-items:flex-start;border-bottom:1px solid #dce5eb;padding-bottom:14px}.wg-head h1{font-size:20px;margin:4px 0}.wg-head p,.wg-muted{color:#627581;margin:4px 0}.wg-actions{display:flex;gap:8px}.wg-workspace button,.wg-workspace input,.wg-workspace select,.wg-workspace textarea{color:#263740;background:#ffffff;border:1px solid #d1d9de;border-radius:6px;padding:8px 10px}.wg-workspace button{cursor:pointer}.wg-workspace button:disabled{opacity:.5}.wg-error{color:#a4262c}.wg-notice{display:flex;align-items:center;gap:10px;padding:10px 12px;border:1px solid #dce5eb;border-radius:8px;background:#eef3f5;color:#52616a}.wg-create,.wg-members form,.wg-composer{display:flex;gap:8px}.wg-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:10px}.wg-group{position:relative;display:flex;align-items:center;gap:8px;min-width:0;padding:8px!important;border:1px solid #dce5eb;border-radius:8px;background:#fff}.wg-group-open{display:flex;flex:1;min-width:0;flex-direction:column;align-items:flex-start;gap:6px;text-align:left;border:0!important;padding:6px!important}.wg-group-open strong,.wg-group-open span{max-width:100%;overflow-wrap:anywhere}.wg-group span{color:#627581;font-size:12px}.wg-group-subscribe{flex:none}.wg-model{display:flex;align-items:center;gap:8px;flex-wrap:wrap;padding:10px 12px;border:1px solid #dce5eb;border-radius:8px;background:#fff}.wg-model label{font-weight:600}.wg-model small{color:#627581}.wg-members{display:flex;align-items:center;gap:8px;flex-wrap:wrap;color:#52616a}.wg-members form{margin-left:auto}.wg-hitl{border:1px solid #e2d3a2;border-radius:8px;padding:12px}.wg-hitl h2{font-size:14px;margin:0 0 8px}.wg-hitl article{display:flex;align-items:center;gap:8px;flex-wrap:wrap;border-top:1px solid #e3e7ea}.wg-hitl article p{flex:1}.wg-hitl :deep(.wg-task__approval){color:#263740;background:#f4f6f8;border:1px solid #d1d9de;border-radius:7px;padding:12px}.wg-hitl :deep(.wg-task__approval-head),.wg-hitl :deep(.wg-task__approval-item-head),.wg-hitl :deep(.wg-task__approval-actions){display:flex;align-items:center;gap:8px;flex-wrap:wrap}.wg-hitl :deep(.wg-task__approval-item){padding:8px 0;border-top:1px solid #d1d9de}.wg-hitl :deep(.wg-task__approval-hint),.wg-hitl :deep(.wg-task__approval-count){color:#627581}.wg-timeline{min-height:100px;flex:1;overflow:auto;display:flex;flex-direction:column;gap:10px;padding:6px 2px}.wg-timeline article{max-width:85%;background:#ffffff;border:1px solid #dce5eb;border-radius:8px;padding:10px 12px}.wg-timeline article.human{align-self:flex-end;background:#eef3f5}.wg-timeline small{color:#627581}.wg-timeline p{white-space:pre-wrap;overflow-wrap:anywhere;margin:5px 0 0}.wg-composer{align-items:center;min-height:54px;padding:6px 7px 6px 12px;border:1px solid #d9e0e2;border-radius:12px;background:#fff;box-shadow:0 2px 8px #172f3b0d}.wg-composer:focus-within{border-color:#aeb7bb;box-shadow:0 0 0 2px #58616612}.wg-composer textarea{flex:1;min-height:36px;max-height:140px;padding:7px 0;border:0!important;outline:0;background:transparent!important;resize:vertical;box-shadow:none!important}.wg-composer button{min-height:38px}.wg-composer .wg-send-button{display:grid;flex:0 0 36px;place-items:center;width:36px;height:36px;min-height:36px;padding:0;border:0;border-radius:50%;background:#353c40;color:#fff}.wg-composer .wg-send-button:disabled{background:#eceeef;color:#8c9498}.wg-send-icon{width:17px;height:17px;background:#fff;mask:url('../../../../../shared/icons/desktop/send.svg') center/contain no-repeat;-webkit-mask:url('../../../../../shared/icons/desktop/send.svg') center/contain no-repeat}.wg-composer .wg-send-button:disabled .wg-send-icon{background:#8c9498}
.wg-queue{display:flex;flex-direction:column;gap:8px;padding:12px;border:1px solid #dce5eb;border-radius:8px;background:#fff}.wg-queue h2{font-size:14px;margin:0}.wg-queue h2 small{margin-left:8px;color:#627581;font-weight:400}.wg-queue article{display:flex;align-items:center;gap:8px;flex-wrap:wrap;border-top:1px solid #e3e7ea;padding-top:8px}.wg-queue article p{flex:1;min-width:180px;white-space:pre-wrap;overflow-wrap:anywhere;margin:0}.wg-queue article p small{display:block;color:#627581;font-size:11px;margin-bottom:3px}.wg-queue textarea{flex:1;min-width:200px;min-height:54px}.wg-model-warning{flex-basis:100%;margin:2px 0;color:#8b5b00}
</style>
