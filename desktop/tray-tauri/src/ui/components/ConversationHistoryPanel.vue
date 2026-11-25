<script setup>
import { onMounted, ref, watch } from 'vue'
import MainChatPanel from './MainChatPanel.vue'
import UiIcon from './UiIcon.vue'
import { getConversationResubmitTarget, listConversationLegacyItems, listConversationRoundItems, listConversationRoundRevisions, listConversationRounds, submitConversationResubmit } from '../api/node.js'

const emit = defineEmits(['resubmitted'])

const props = defineProps({
  agentId: { type: String, required: true },
  agentName: { type: String, default: '' },
})

const rounds = ref([])
const hasOlderRounds = ref(false)
const nextBeforeRoundId = ref('')
const legacyItemCount = ref(0)
const historyCompleteness = ref('complete')
const selected = ref(null)
const revisions = ref([])
const selectedRevisionID = ref('')
const entries = ref([])
const entriesRevision = ref(0)
const hasMoreItems = ref(false)
const nextAfterItemId = ref('')
const loading = ref(false)
const loadingRounds = ref(false)
const loadingItems = ref(false)
const error = ref('')
const requestGeneration = ref(0)
const resubmitTarget = ref(null)
const editingResubmit = ref(false)
const resubmitContent = ref('')
const resubmitting = ref(false)
const resubmitError = ref('')

function roundLabel(round) {
  const source = String(round?.source || '').toLowerCase()
  if (source === 'trigger' || source === 'auto') return '自主运行'
  if (source === 'child_agent') return '子智能体'
  if (source === 'side_effect') return '后台任务'
  return '对话'
}

function finishLabel(round) {
  const status = String(round?.status || '').toLowerCase()
  if (status === 'completed' || status === 'succeeded') return '已完成'
  if (status === 'cancelled' || status === 'interrupted') return '已停止'
  if (status === 'failed' || status === 'budget_exhausted') return '未完成'
  return '已结束'
}

function selectedKey(row) {
  return row?.type === 'legacy' ? 'legacy' : String(row?.round_id || '')
}

function mergeRoundHistoryPage(current, incoming) {
  const combined = [...(Array.isArray(current) ? current : []), ...(Array.isArray(incoming) ? incoming : [])]
  const visibleCallIds = new Set(combined
    .filter(entry => entry?.kind === 'tool_call')
    .map(entry => String(entry?.data?.tool_call_id || entry?.blockId || '').trim())
    .filter(Boolean))
  // Tool summaries are paged at the message-record boundary, so one assistant
  // record with many calls can be separated from its later result records.
  // Keep result rows only when their visible call is in this loaded window;
  // expanding a call fetches its authoritative full result on demand.
  return combined.filter(entry => entry?.kind !== 'tool_result'
    || visibleCallIds.has(String(entry?.data?.tool_call_id || entry?.blockId || '').trim()))
}

async function loadRoundItems(row, { append = false, generation = requestGeneration.value } = {}) {
  if (!props.agentId || !row?.round_id || loadingItems.value) return
  loadingItems.value = true
  error.value = ''
  try {
    const page = await listConversationRoundItems(props.agentId, props.agentId, row.round_id, {
      revisionId: selectedRevisionID.value || row.selected_revision_id || '',
      afterItemId: append ? nextAfterItemId.value : '',
      limit: 50,
    })
    if (generation !== requestGeneration.value || selectedKey(selected.value) !== row.round_id) return
    entries.value = mergeRoundHistoryPage(append ? entries.value : [], page.items || [])
    entriesRevision.value += 1
    hasMoreItems.value = Boolean(page.has_more)
    nextAfterItemId.value = String(page.next_after_item_id || '')
  } catch (cause) {
    if (generation === requestGeneration.value) error.value = String(cause?.message || '读取这轮对话失败')
  } finally {
    if (generation === requestGeneration.value) loadingItems.value = false
  }
}

async function loadResubmitTarget(row, revisionID, generation) {
  resubmitTarget.value = null
  editingResubmit.value = false
  resubmitError.value = ''
  if (!row?.round_id || !revisionID || row.round_id !== rounds.value.at(-1)?.round_id) return
  if (revisionID !== row.selected_revision_id) return
  try {
    const target = await getConversationResubmitTarget(props.agentId, props.agentId, row.round_id, revisionID)
    if (generation !== requestGeneration.value || selectedKey(selected.value) !== row.round_id || selectedRevisionID.value !== revisionID) return
    resubmitTarget.value = target
  } catch {
    // A busy, queued, or approval-blocked conversation is intentionally not editable.
  }
}

function beginResubmit() {
  if (!resubmitTarget.value || resubmitting.value) return
  resubmitContent.value = String(resubmitTarget.value.input?.content || '')
  resubmitError.value = ''
  editingResubmit.value = true
}

function cancelResubmit() {
  if (resubmitting.value) return
  editingResubmit.value = false
  resubmitError.value = ''
}

async function submitResubmit() {
  const target = resubmitTarget.value
  if (!target || resubmitting.value) return
  const originalInput = target.input || {}
  const imageParts = (Array.isArray(originalInput.content_parts) ? originalInput.content_parts : [])
    .filter(part => part?.type === 'image_url' && part.image_url?.url)
  if (!resubmitContent.value.trim() && !imageParts.length && !(originalInput.file_refs || []).length) {
    resubmitError.value = '消息内容不能为空。'
    return
  }
  resubmitting.value = true
  resubmitError.value = ''
  try {
    await submitConversationResubmit(props.agentId, target.round_id, {
      revisionId: target.revision_id,
      conversationRevision: target.conversation_revision,
      conversationEpoch: target.conversation_epoch,
      preCheckpointRevision: target.pre_checkpoint_revision,
      content: resubmitContent.value,
      // The edited text is authoritative; retain only the original image parts.
      contentParts: imageParts,
      fileRefs: originalInput.file_refs || [],
      userMessageName: originalInput.user_message_name || '',
    })
    editingResubmit.value = false
    emit('resubmitted')
  } catch (cause) {
    resubmitError.value = String(cause?.message || '重新发送失败，请检查连接后重试。')
  } finally {
    resubmitting.value = false
  }
}

async function loadLegacyItems({ append = false, generation = requestGeneration.value } = {}) {
  if (!props.agentId || loadingItems.value) return
  loadingItems.value = true
  error.value = ''
  try {
    const page = await listConversationLegacyItems(props.agentId, props.agentId, {
      afterItemId: append ? nextAfterItemId.value : '',
      limit: 50,
    })
    if (generation !== requestGeneration.value || selectedKey(selected.value) !== 'legacy') return
    historyCompleteness.value = String(page.history_completeness || 'complete')
    legacyItemCount.value = Number(page.item_count) || 0
    entries.value = append ? [...entries.value, ...(page.items || [])] : [...(page.items || [])]
    entriesRevision.value += 1
    hasMoreItems.value = Boolean(page.has_more)
    nextAfterItemId.value = String(page.next_after_item_id || '')
  } catch (cause) {
    if (generation === requestGeneration.value) error.value = String(cause?.message || '读取早期记录失败')
  } finally {
    if (generation === requestGeneration.value) loadingItems.value = false
  }
}

async function selectRound(round) {
  const generation = ++requestGeneration.value
  loadingRounds.value = false
  loadingItems.value = false
  selected.value = round ? { ...round, type: 'round' } : null
  revisions.value = []
  selectedRevisionID.value = ''
  entries.value = []
  entriesRevision.value += 1
  hasMoreItems.value = false
  nextAfterItemId.value = ''
  if (round === null || typeof round !== 'object') return
  loadingItems.value = true
  error.value = ''
  try {
    const page = await listConversationRoundRevisions(props.agentId, props.agentId, round.round_id)
    if (generation !== requestGeneration.value || selectedKey(selected.value) !== round.round_id) return
    revisions.value = page.revisions || []
    selectedRevisionID.value = String(page.selected_revision_id || round.selected_revision_id || '')
    loadingItems.value = false
    await Promise.all([
      loadRoundItems(round, { generation }),
      loadResubmitTarget(round, selectedRevisionID.value, generation),
    ])
  } catch (cause) {
    if (generation === requestGeneration.value) error.value = String(cause?.message || '读取历史版本失败')
  } finally {
    if (generation === requestGeneration.value) loadingItems.value = false
  }
}

async function selectRevision(revisionID) {
  const row = selected.value
  if (!row || row.type !== 'round' || !revisions.value.some(revision => revision.revision_id === revisionID)) return
  const generation = ++requestGeneration.value
  selectedRevisionID.value = revisionID
  loadingItems.value = false
  entries.value = []
  entriesRevision.value += 1
  hasMoreItems.value = false
  nextAfterItemId.value = ''
  await Promise.all([
    loadRoundItems(row, { generation }),
    loadResubmitTarget(row, revisionID, generation),
  ])
}

async function selectLegacy() {
  const generation = ++requestGeneration.value
  loadingRounds.value = false
  loadingItems.value = false
  selected.value = { type: 'legacy' }
  revisions.value = []
  selectedRevisionID.value = ''
  entries.value = []
  entriesRevision.value += 1
  hasMoreItems.value = false
  nextAfterItemId.value = ''
  await loadLegacyItems({ generation })
}

async function loadMoreItems() {
  if (!selected.value || loadingItems.value) return
  const generation = requestGeneration.value
  if (selected.value.type === 'legacy') await loadLegacyItems({ append: true, generation })
  else await loadRoundItems(selected.value, { append: true, generation })
}

async function loadOlderRounds() {
  if (!props.agentId || !hasOlderRounds.value || loadingRounds.value) return
  loadingRounds.value = true
  error.value = ''
  const generation = requestGeneration.value
  try {
    const page = await listConversationRounds(props.agentId, props.agentId, { beforeRoundId: nextBeforeRoundId.value, limit: 10 })
    if (generation !== requestGeneration.value) return
    rounds.value = [...(page.rounds || []), ...rounds.value]
    hasOlderRounds.value = Boolean(page.has_more)
    nextBeforeRoundId.value = String(page.next_before_round_id || '')
    historyCompleteness.value = String(page.history_completeness || historyCompleteness.value)
  } catch (cause) {
    if (generation === requestGeneration.value) error.value = String(cause?.message || '读取更早的轮次失败')
  } finally {
    if (generation === requestGeneration.value) loadingRounds.value = false
  }
}

async function refresh() {
  if (!props.agentId || loading.value) return
  const generation = ++requestGeneration.value
  loading.value = true
  loadingRounds.value = false
  loadingItems.value = false
  error.value = ''
  selected.value = null
  revisions.value = []
  selectedRevisionID.value = ''
  entries.value = []
  try {
    const [roundPage, legacyPage] = await Promise.all([
      listConversationRounds(props.agentId, props.agentId, { limit: 10 }),
      listConversationLegacyItems(props.agentId, props.agentId, { limit: 1 }),
    ])
    if (generation !== requestGeneration.value) return
    rounds.value = roundPage.rounds || []
    hasOlderRounds.value = Boolean(roundPage.has_more)
    nextBeforeRoundId.value = String(roundPage.next_before_round_id || '')
    historyCompleteness.value = String(roundPage.history_completeness || legacyPage.history_completeness || 'complete')
    legacyItemCount.value = Number(legacyPage.item_count) || 0
    loading.value = false
    const latest = rounds.value.at(-1)
    if (latest) await selectRound(latest)
    else if (legacyItemCount.value > 0) await selectLegacy()
  } catch (cause) {
    if (generation === requestGeneration.value) error.value = Number(cause?.status) === 404 ? '' : String(cause?.message || '读取对话历史失败')
  } finally {
    if (generation === requestGeneration.value) loading.value = false
  }
}

onMounted(refresh)
watch(() => props.agentId, refresh)
</script>

<template>
  <section class="conversation-history-panel" aria-label="对话历史">
    <header class="conversation-history-panel__header">
      <div>
        <h1>对话历史</h1>
        <p>{{ agentName }} · 已完成轮次与早期记录</p>
      </div>
      <button class="icon-only" type="button" aria-label="刷新历史" title="刷新历史" :disabled="loading" @click="refresh"><UiIcon name="refresh-cw" :size="16" /></button>
    </header>
    <p v-if="historyCompleteness !== 'complete'" class="conversation-history-panel__notice" role="status">部分历史尚未完整保存；这里展示 Node 当前可恢复的记录。</p>
    <div class="conversation-history-panel__body">
      <aside class="conversation-history-panel__list" aria-label="历史轮次">
        <div class="conversation-history-panel__list-heading">最近对话</div>
        <button
          v-for="round in rounds"
          :key="round.round_id"
          class="conversation-history-panel__round"
          :class="{ 'is-selected': selectedKey(selected) === round.round_id }"
          type="button"
          :aria-current="selectedKey(selected) === round.round_id ? 'true' : undefined"
          @click="selectRound(round)"
        >
          <span class="conversation-history-panel__round-icon"><UiIcon name="message-circle" :size="15" /></span>
          <span class="conversation-history-panel__round-copy">
            <strong>第 {{ round.ordinal }} 轮 · {{ roundLabel(round) }}</strong>
            <small>{{ finishLabel(round) }} · {{ round.item_count }} 条内容</small>
          </span>
          <UiIcon name="chevron-right" :size="14" />
        </button>
        <button v-if="hasOlderRounds" class="conversation-history-panel__load" type="button" :disabled="loadingRounds" @click="loadOlderRounds">{{ loadingRounds ? '正在加载…' : '加载更早轮次' }}</button>
        <button
          v-if="legacyItemCount > 0"
          class="conversation-history-panel__round conversation-history-panel__legacy"
          :class="{ 'is-selected': selectedKey(selected) === 'legacy' }"
          type="button"
          :aria-current="selectedKey(selected) === 'legacy' ? 'true' : undefined"
          @click="selectLegacy"
        >
          <span class="conversation-history-panel__round-icon"><UiIcon name="clock" :size="15" /></span>
          <span class="conversation-history-panel__round-copy"><strong>早期记录</strong><small>未按轮次整理 · {{ legacyItemCount }} 条内容</small></span>
          <UiIcon name="chevron-right" :size="14" />
        </button>
        <p v-if="!loading && !rounds.length && !legacyItemCount" class="conversation-history-panel__empty-list">还没有可查看的历史对话。</p>
      </aside>
      <section class="conversation-history-panel__content" aria-label="所选历史消息">
        <div v-if="loading" class="conversation-history-panel__state" role="status">正在读取历史…</div>
        <div v-else-if="error" class="conversation-history-panel__state is-error" role="alert"><span>{{ error }}</span><button type="button" @click="refresh">重试</button></div>
        <div v-else-if="!selected" class="conversation-history-panel__state">选择左侧的一轮对话，查看当时的完整消息与工具记录。</div>
        <template v-else>
          <div class="conversation-history-panel__selection">
            <strong>{{ selected.type === 'legacy' ? '早期记录' : `第 ${selected.ordinal} 轮 · ${roundLabel(selected)}` }}</strong>
            <div v-if="selected.type === 'round'" class="conversation-history-panel__revision-controls">
              <span>{{ finishLabel(revisions.find(revision => revision.revision_id === selectedRevisionID) || selected) }} · 只读</span>
              <label v-if="revisions.length > 1" class="conversation-history-panel__revision-select">
                <span>版本</span>
                <select :value="selectedRevisionID" aria-label="选择历史版本" @change="selectRevision($event.target.value)">
                  <option v-for="(revision, index) in revisions" :key="revision.revision_id" :value="revision.revision_id">版本 {{ index + 1 }}{{ revision.selected ? ' · 当前' : '' }} · {{ finishLabel(revision) }}</option>
                </select>
              </label>
            </div>
            <span v-else>按原始顺序展示，未猜测轮次边界</span>
          </div>
          <section v-if="resubmitTarget && selected.type === 'round'" class="conversation-history-panel__resubmit" aria-label="编辑并重新发送">
            <template v-if="editingResubmit">
              <label class="conversation-history-panel__resubmit-label" for="conversation-resubmit-content">修改这条消息</label>
              <textarea id="conversation-resubmit-content" v-model="resubmitContent" :disabled="resubmitting" rows="4" />
              <p class="conversation-history-panel__resubmit-note">原消息中的图片与文件引用会保留。之前执行过的工具操作不会撤销；重新发送后，智能体会从这一轮之前的上下文重新处理。</p>
              <p v-if="resubmitError" class="conversation-history-panel__resubmit-error" role="alert">{{ resubmitError }}</p>
              <div class="conversation-history-panel__resubmit-actions">
                <button type="button" :disabled="resubmitting" @click="cancelResubmit">取消</button>
                <button type="button" class="is-primary" :disabled="resubmitting" @click="submitResubmit">{{ resubmitting ? '正在重新发送…' : '确认重新发送' }}</button>
              </div>
            </template>
            <button v-else class="conversation-history-panel__resubmit-trigger" type="button" @click="beginResubmit"><UiIcon name="pencil" :size="14" />编辑并重新发送</button>
          </section>
          <div v-if="!entries.length && !loadingItems" class="conversation-history-panel__state">这段记录没有可展示的消息。</div>
          <div v-else class="conversation-history-panel__transcript desktop-chat-wrapper app__body--chat-v61">
            <MainChatPanel :key="`${agentId}:${selectedKey(selected)}`" :agent-id="agentId" :agent-title="agentName" :entries="entries" :entries-revision="entriesRevision" :hitl-queue="[]" :sending="false" :show-workspace-switcher="false" />
          </div>
          <div v-if="loadingItems" class="conversation-history-panel__loading-more" role="status">正在读取消息…</div>
          <button v-if="hasMoreItems" class="conversation-history-panel__load-more" type="button" :disabled="loadingItems" @click="loadMoreItems">{{ loadingItems ? '正在加载…' : '加载后续消息' }}</button>
        </template>
      </section>
    </div>
  </section>
</template>

<style scoped>
.conversation-history-panel { display: flex; flex: 1; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden; background: #f7f9fa; color: #283a43; }
.conversation-history-panel__header { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 16px; padding: 20px 24px 14px; border-bottom: 1px solid #e0e7ea; background: #fff; }
.conversation-history-panel__header h1 { margin: 0; font-size: 18px; font-weight: 650; }
.conversation-history-panel__header p { margin: 5px 0 0; color: #73838b; font-size: 12px; }
.conversation-history-panel__notice { flex: 0 0 auto; margin: 12px 20px 0; padding: 9px 12px; border: 1px solid #ead9b5; border-radius: 6px; background: #fff8e9; color: #785b2a; font-size: 12px; }
.conversation-history-panel__body { display: flex; flex: 1; min-height: 0; padding: 16px 20px 20px; gap: 14px; }
.conversation-history-panel__list { display: flex; flex: 0 0 min(310px, 32%); flex-direction: column; min-width: 230px; min-height: 0; gap: 5px; overflow: auto; padding: 10px; border: 1px solid #e1e7e9; border-radius: 8px; background: #fff; }
.conversation-history-panel__list-heading { padding: 2px 8px 8px; color: #73838b; font-size: 11px; font-weight: 650; }
.conversation-history-panel__round { display: flex; align-items: center; gap: 9px; width: 100%; min-height: 54px; padding: 8px; border: 1px solid transparent; border-radius: 6px; background: transparent; color: #354951; text-align: left; cursor: pointer; }
.conversation-history-panel__round:hover { background: #f4f7f8; }
.conversation-history-panel__round.is-selected { border-color: #dce5e8; background: #eff3f4; color: #263c47; }
.conversation-history-panel__round > :last-child { flex: 0 0 auto; color: #8b999f; }
.conversation-history-panel__round-icon { display: grid; flex: 0 0 28px; width: 28px; height: 28px; place-items: center; border-radius: 6px; background: #f2f5f6; color: #667980; }
.conversation-history-panel__round-copy { display: flex; flex: 1; flex-direction: column; min-width: 0; gap: 3px; }
.conversation-history-panel__round-copy strong { overflow: hidden; font-size: 12px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.conversation-history-panel__round-copy small { color: #7c8a90; font-size: 10px; }
.conversation-history-panel__legacy { margin-top: 7px; border-top: 1px solid #edf0f1; border-radius: 0 0 6px 6px; }
.conversation-history-panel__load { min-height: 32px; border: 0; border-radius: 5px; background: transparent; color: #5b707b; font-size: 11px; cursor: pointer; }
.conversation-history-panel__load:hover { background: #f4f7f8; }
.conversation-history-panel__load:disabled { opacity: .55; cursor: wait; }
.conversation-history-panel__empty-list { margin: 8px; color: #7c8a90; font-size: 12px; line-height: 1.6; }
.conversation-history-panel__content { display: flex; flex: 1; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden; border: 1px solid #e1e7e9; border-radius: 8px; background: #fff; }
.conversation-history-panel__selection { display: flex; flex: 0 0 auto; align-items: baseline; justify-content: space-between; gap: 12px; padding: 11px 15px; border-bottom: 1px solid #e8edef; color: #354951; font-size: 12px; }
.conversation-history-panel__selection span { color: #839198; font-size: 10px; }
.conversation-history-panel__revision-controls { display: flex; align-items: center; gap: 10px; }
.conversation-history-panel__revision-select { display: inline-flex; align-items: center; gap: 6px; color: #839198; font-size: 10px; }
.conversation-history-panel__revision-select select { max-width: 210px; height: 28px; padding: 0 24px 0 8px; border: 1px solid #dbe3e6; border-radius: 5px; background: #fff; color: #536a75; font: inherit; font-size: 10px; }
.conversation-history-panel__resubmit { flex: 0 0 auto; padding: 12px 15px; border-bottom: 1px solid #e8edef; background: #fbfcfc; }
.conversation-history-panel__resubmit-trigger,.conversation-history-panel__resubmit-actions button { display: inline-flex; align-items: center; justify-content: center; gap: 7px; min-height: 30px; padding: 0 10px; border: 1px solid #dbe3e6; border-radius: 5px; background: #fff; color: #536a75; font: inherit; font-size: 11px; cursor: pointer; }
.conversation-history-panel__resubmit-trigger:hover,.conversation-history-panel__resubmit-actions button:hover { background: #f3f6f7; }
.conversation-history-panel__resubmit-label { display: block; margin-bottom: 6px; color: #354951; font-size: 12px; font-weight: 600; }
.conversation-history-panel__resubmit textarea { display: block; width: 100%; min-height: 76px; resize: vertical; padding: 9px 10px; border: 1px solid #dbe3e6; border-radius: 6px; background: #fff; color: #283a43; font: inherit; font-size: 12px; line-height: 1.55; }
.conversation-history-panel__resubmit textarea:focus-visible { outline: 2px solid #a9c4ce; outline-offset: 1px; }
.conversation-history-panel__resubmit-note,.conversation-history-panel__resubmit-error { margin: 7px 0 0; color: #78878e; font-size: 10px; line-height: 1.5; }
.conversation-history-panel__resubmit-error { color: #a64e48; }
.conversation-history-panel__resubmit-actions { display: flex; justify-content: flex-end; gap: 7px; margin-top: 9px; }
.conversation-history-panel__resubmit-actions button.is-primary { border-color: #52636a; background: #52636a; color: #fff; }
.conversation-history-panel__resubmit-actions button:disabled { cursor: wait; opacity: .55; }
.conversation-history-panel__transcript { display: flex; flex: 1; min-width: 0; min-height: 0; overflow: hidden; }
.conversation-history-panel__transcript :deep(.chat__stream-wrap) { overflow-y: auto; }
.conversation-history-panel__load-more { align-self: center; flex: 0 0 auto; margin: 8px 0 12px; padding: 6px 12px; border: 1px solid #dbe3e6; border-radius: 6px; background: #fff; color: #536a75; font-size: 11px; cursor: pointer; }
.conversation-history-panel__loading-more,.conversation-history-panel__state { display: flex; flex: 1; align-items: center; justify-content: center; gap: 12px; min-height: 80px; padding: 24px; color: #7a898f; font-size: 12px; text-align: center; }
.conversation-history-panel__state.is-error { color: #9b4d45; }
.conversation-history-panel__state button { border: 1px solid #d9e1e3; border-radius: 5px; background: #fff; color: #526771; padding: 5px 10px; cursor: pointer; }
@media (max-width: 760px) { .conversation-history-panel__body { flex-direction: column; } .conversation-history-panel__list { flex: 0 0 auto; max-height: 220px; min-width: 0; } .conversation-history-panel__content { flex: 1 1 0; } }
</style>
