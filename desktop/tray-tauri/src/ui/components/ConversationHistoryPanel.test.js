// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../api/node.js', () => ({
  getConversationResubmitTarget: vi.fn(),
  listConversationRounds: vi.fn(),
  listConversationRoundRevisions: vi.fn(),
  listConversationRoundItems: vi.fn(),
  listConversationLegacyItems: vi.fn(),
  submitConversationResubmit: vi.fn(),
}))
import { getConversationResubmitTarget, listConversationLegacyItems, listConversationRoundItems, listConversationRoundRevisions, listConversationRounds, submitConversationResubmit } from '../api/node.js'
import ConversationHistoryPanel from './ConversationHistoryPanel.vue'

const round = (roundId, ordinal) => ({ round_id: roundId, ordinal, selected_revision_id: `${roundId}-revision`, source: 'human', status: 'completed', item_count: 2 })
const entry = (id, kind, text) => ({ id, item_id: id, kind, text })

let wrapper
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.resetAllMocks() })

describe('ConversationHistoryPanel', () => {
  it('loads the latest round, pages older rounds, and renders the selected read-only transcript', async () => {
    const latest = [round('r2', 2), round('r3', 3)]
    listConversationRounds.mockResolvedValueOnce({ rounds: latest, has_more: true, next_before_round_id: 'r2', history_completeness: 'complete' })
      .mockResolvedValueOnce({ rounds: [round('r1', 1)], has_more: false, history_completeness: 'complete' })
    listConversationRoundRevisions.mockImplementation(async (_agent, _conversation, roundId) => ({
      selected_revision_id: `${roundId}-revision`,
      revisions: [{ revision_id: `${roundId}-revision`, selected: true, status: 'completed' }],
    }))
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    listConversationRoundItems.mockResolvedValue({ items: [entry('u3', 'user', 'request'), entry('a3', 'assistant', 'reply')], has_more: false })

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper' },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ entries.map(item => item.text).join(" | ") }}</div>' },
      } },
    })
    await flushPromises()
    expect(listConversationRoundItems).toHaveBeenCalledWith('agent-a', 'agent-a', 'r3', { revisionId: 'r3-revision', afterItemId: '', limit: 50 })
    expect(wrapper.get('[data-test="transcript"]').text()).toBe('request | reply')
    expect(wrapper.text()).toContain('第 3 轮 · 对话')

    await wrapper.get('.conversation-history-panel__load').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('第 1 轮 · 对话')
  })

  it('restores Node media URLs before displaying history and releases them on unmount', async () => {
    listConversationRounds.mockResolvedValue({ rounds: [round('r1', 1)], has_more: false, history_completeness: 'complete' })
    listConversationRoundRevisions.mockResolvedValue({ selected_revision_id: 'r1-revision', revisions: [{ revision_id: 'r1-revision', selected: true, status: 'completed' }] })
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    listConversationRoundItems.mockResolvedValue({ items: [
      { ...entry('user-image', 'user', 'look at this'), images: ['/v1/agents/agent-a/media/media-1'] },
    ], has_more: false })
    const restoreMedia = vi.fn(async (items, isCurrent) => {
      if (!isCurrent()) return null
      return items.map(item => ({
        ...item,
        images: item.images?.map(() => 'blob:desktop/history-image') || item.images,
      }))
    })
    const releaseMedia = vi.fn()

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper', restoreMedia, releaseMedia },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ JSON.stringify(entries) }}</div>' },
      } },
    })
    await flushPromises()
    expect(restoreMedia).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test="transcript"]').text()).toContain('blob:desktop/history-image')

    wrapper.unmount()
    wrapper = null
    expect(releaseMedia).toHaveBeenCalledWith('agent-a')
  })

  it('keeps paged tool results attached to source-scoped calls visible earlier in the history window', async () => {
    listConversationRounds.mockResolvedValue({ rounds: [round('r1', 1)], has_more: false, history_completeness: 'complete' })
    listConversationRoundRevisions.mockResolvedValue({ selected_revision_id: 'r1-revision', revisions: [{ revision_id: 'r1-revision', selected: true, status: 'completed' }] })
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    const calls = Array.from({ length: 50 }, (_, index) => {
      const id = `call-${String(index + 1).padStart(2, '0')}`
      return { id: `tool-call/${id}`, item_id: `tool-call/${id}`, kind: 'tool_call', blockId: id, data: { tool_call_id: id } }
    })
    listConversationRoundItems.mockResolvedValueOnce({ items: [
      ...calls,
      { id: 'more', item_id: 'more', kind: 'tool_group_more', text: 'more' },
    ], has_more: true, next_after_item_id: 'assistant-tools' }).mockResolvedValueOnce({ items: [
      { id: 'visible-result-50', item_id: 'visible-result-50', kind: 'tool_result', data: { tool_call_id: 'call-50', tool_call_item_id: calls[49].item_id }, text: 'visible result' },
      { id: 'orphan-result-51', item_id: 'orphan-result-51', kind: 'tool_result', data: { tool_call_id: 'call-50', tool_call_item_id: 'tool-call/hidden-assistant/call-50' }, text: 'orphan result' },
    ], has_more: false })

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper' },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ entries.map(item => item.text || item.data?.tool_call_id || item.id).join(" | ") }}</div>' },
      } },
    })
    await flushPromises()
    await wrapper.get('.conversation-history-panel__load-more').trigger('click')
    await flushPromises()

    const transcript = wrapper.get('[data-test="transcript"]').text()
    expect(transcript).toContain('visible result')
    expect(transcript).not.toContain('orphan result')
  })

  it('keeps the selected latest transcript stable while paging through 100 history rounds', async () => {
    const pageFor = (start) => Array.from({ length: 10 }, (_, index) => {
      const ordinal = start + index
      return round(`r${ordinal}`, ordinal)
    })
    listConversationRounds.mockImplementation(async (_agent, _conversation, { beforeRoundId = '', limit }) => {
      const end = beforeRoundId ? Number(beforeRoundId.slice(1)) - 1 : 100
      const start = Math.max(1, end - limit + 1)
      return {
        rounds: pageFor(start).filter(item => item.ordinal <= end),
        has_more: start > 1,
        next_before_round_id: start > 1 ? `r${start}` : '',
        history_completeness: 'complete',
      }
    })
    listConversationRoundRevisions.mockImplementation(async (_agent, _conversation, roundId) => ({
      selected_revision_id: `${roundId}-revision`,
      revisions: [{ revision_id: `${roundId}-revision`, selected: true, status: 'completed' }],
    }))
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    listConversationRoundItems.mockResolvedValue({ items: [entry('u100', 'user', 'latest request'), entry('a100', 'assistant', 'latest reply')], has_more: false })

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper' },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ entries.map(item => item.text).join(" | ") }}</div>' },
      } },
    })
    await flushPromises()

    expect(wrapper.findAll('.conversation-history-panel__round')).toHaveLength(10)
    expect(wrapper.get('[data-test="transcript"]').text()).toBe('latest request | latest reply')
    for (let page = 0; page < 9; page++) {
      await wrapper.get('.conversation-history-panel__load').trigger('click')
      await flushPromises()
    }

    const rows = wrapper.findAll('.conversation-history-panel__round')
    expect(rows).toHaveLength(100)
    expect(rows[0].text()).toContain('第 1 轮')
    expect(rows[99].text()).toContain('第 100 轮')
    expect(rows.filter(row => row.classes().includes('is-selected'))).toHaveLength(1)
    expect(rows[99].classes()).toContain('is-selected')
    expect(wrapper.get('[data-test="transcript"]').text()).toBe('latest request | latest reply')
    expect(listConversationRounds).toHaveBeenCalledTimes(10)
  })

  it('exposes unassigned old messages separately and loads later pages without inventing round boundaries', async () => {
    listConversationRounds.mockResolvedValue({ rounds: [], has_more: false, history_completeness: 'complete' })
    listConversationRoundRevisions.mockResolvedValue({ selected_revision_id: '', revisions: [] })
    listConversationLegacyItems.mockResolvedValueOnce({ item_count: 2, items: [], has_more: false, history_completeness: 'complete' }).mockResolvedValueOnce({
      item_count: 2,
      items: [entry('old-user', 'user', 'old question')],
      has_more: true,
      next_after_item_id: 'old-user',
      history_completeness: 'complete',
    }).mockResolvedValueOnce({
      item_count: 2,
      items: [entry('old-assistant', 'assistant', 'old answer')],
      has_more: false,
      history_completeness: 'complete',
    })

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper' },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ entries.map(item => item.text).join(" | ") }}</div>' },
      } },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('早期记录')
    expect(wrapper.get('[data-test="transcript"]').text()).toBe('old question')
    expect(wrapper.text()).toContain('未猜测轮次边界')
    await wrapper.get('.conversation-history-panel__load-more').trigger('click')
    await flushPromises()
    expect(listConversationLegacyItems).toHaveBeenLastCalledWith('agent-a', 'agent-a', { afterItemId: 'old-user', limit: 50 })
    expect(wrapper.get('[data-test="transcript"]').text()).toBe('old question | old answer')
  })

  it('ignores a slow response after the user selects another round', async () => {
    listConversationRounds.mockResolvedValue({ rounds: [round('r2', 2), round('r3', 3)], has_more: false, history_completeness: 'complete' })
    listConversationRoundRevisions.mockImplementation(async (_agent, _conversation, roundId) => ({
      selected_revision_id: `${roundId}-revision`,
      revisions: [{ revision_id: `${roundId}-revision`, selected: true, status: 'completed' }],
    }))
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    let resolveLatest
    listConversationRoundItems.mockImplementation((_agent, _conversation, roundId) => roundId === 'r3'
      ? new Promise(resolve => { resolveLatest = resolve })
      : Promise.resolve({ items: [entry('r2-user', 'user', 'second round')], has_more: false }))
    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper' },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ entries.map(item => item.text).join(" | ") }}</div>' },
      } },
    })
    await flushPromises()
    await wrapper.findAll('.conversation-history-panel__round')[0].trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="transcript"]').text()).toBe('second round')
    resolveLatest({ items: [entry('r3-user', 'user', 'late response')], has_more: false })
    await flushPromises()
    expect(wrapper.get('[data-test="transcript"]').text()).toBe('second round')
  })

  it('lets the user inspect a prior immutable revision without changing the selected conversation revision', async () => {
    listConversationRounds.mockResolvedValue({ rounds: [round('r7', 7)], has_more: false, history_completeness: 'complete' })
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    listConversationRoundRevisions.mockResolvedValue({
      selected_revision_id: 'rev-current',
      revisions: [
        { revision_id: 'rev-first', selected: false, status: 'superseded' },
        { revision_id: 'rev-current', selected: true, status: 'completed' },
      ],
    })
    listConversationRoundItems.mockImplementation(async (_agent, _conversation, _roundId, { revisionId }) => ({
      items: [entry(`user-${revisionId}`, 'user', revisionId)],
      has_more: false,
    }))

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper' },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ entries.map(item => item.text).join(" | ") }}</div>' },
      } },
    })
    await flushPromises()
    const selector = wrapper.get('[aria-label="选择历史版本"]')
    expect(selector.element.value).toBe('rev-current')
    await selector.setValue('rev-first')
    await flushPromises()

    expect(wrapper.get('[data-test="transcript"]').text()).toBe('rev-first')
    expect(listConversationRoundItems).toHaveBeenLastCalledWith('agent-a', 'agent-a', 'r7', { revisionId: 'rev-first', afterItemId: '', limit: 50 })
    expect(wrapper.get('[aria-label="选择历史版本"]').element.value).toBe('rev-first')
  })

  it('offers edit and resubmit only for the latest selected revision and preserves its attachments', async () => {
    const latest = round('r9', 9)
    latest.selected_revision_id = 'rev-current'
    listConversationRounds.mockResolvedValue({ rounds: [round('r8', 8), latest], has_more: false, history_completeness: 'complete' })
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    listConversationRoundRevisions.mockResolvedValue({
      selected_revision_id: 'rev-current',
      revisions: [
        { revision_id: 'rev-current', selected: true, status: 'completed' },
        { revision_id: 'rev-old', selected: false, status: 'superseded' },
      ],
    })
    listConversationRoundItems.mockResolvedValue({ items: [entry('u9', 'user', 'original request')], has_more: false })
    getConversationResubmitTarget.mockResolvedValue({
      conversation_revision: 20,
      conversation_epoch: 4,
      round_id: 'r9',
      revision_id: 'rev-current',
      pre_checkpoint_revision: 7,
      input: {
        content: 'original request',
        content_parts: [
          { type: 'text', text: 'original request' },
          { type: 'image_url', image_url: { url: 'data:image/png;base64,aGVsbG8=' } },
        ],
        file_refs: [{ path: 'notes.md', name: 'notes.md', revision: 'file-v1' }],
        user_message_name: 'human',
      },
    })
    submitConversationResubmit.mockResolvedValue({ accepted: true, input_seq: 12 })

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a', agentName: 'Helper' },
      global: { stubs: {
        UiIcon: { template: '<i />' },
        MainChatPanel: { props: ['entries'], template: '<div data-test="transcript">{{ entries.map(item => item.text).join(" | ") }}</div>' },
      } },
    })
    await flushPromises()
    expect(getConversationResubmitTarget).toHaveBeenCalledWith('agent-a', 'agent-a', 'r9', 'rev-current')
    expect(wrapper.get('.conversation-history-panel__resubmit-trigger').text()).toContain('编辑并重新发送')

    await wrapper.get('.conversation-history-panel__resubmit-trigger').trigger('click')
    const editor = wrapper.get('#conversation-resubmit-content')
    expect(editor.element.value).toBe('original request')
    await editor.setValue('revised request')
    await wrapper.get('.conversation-history-panel__resubmit-actions .is-primary').trigger('click')
    await flushPromises()

    expect(submitConversationResubmit).toHaveBeenCalledWith('agent-a', 'r9', {
      revisionId: 'rev-current',
      conversationRevision: 20,
      conversationEpoch: 4,
      preCheckpointRevision: 7,
      content: 'revised request',
      contentParts: [{ type: 'image_url', image_url: { url: 'data:image/png;base64,aGVsbG8=' } }],
      fileRefs: [{ path: 'notes.md', name: 'notes.md', revision: 'file-v1' }],
      userMessageName: 'human',
    })
    expect(wrapper.emitted('resubmitted')).toHaveLength(1)
  })

  it('does not offer resubmission for a superseded revision or a non-latest round', async () => {
    listConversationRounds.mockResolvedValue({ rounds: [round('r8', 8), round('r9', 9)], has_more: false, history_completeness: 'complete' })
    listConversationLegacyItems.mockResolvedValue({ item_count: 0, items: [], history_completeness: 'complete' })
    listConversationRoundRevisions.mockResolvedValue({
      selected_revision_id: 'r8-revision',
      revisions: [{ revision_id: 'r8-revision', selected: true, status: 'completed' }, { revision_id: 'r8-old', selected: false, status: 'superseded' }],
    })
    listConversationRoundItems.mockResolvedValue({ items: [entry('u8', 'user', 'older request')], has_more: false })

    wrapper = mount(ConversationHistoryPanel, {
      props: { agentId: 'agent-a' },
      global: { stubs: { UiIcon: { template: '<i />' }, MainChatPanel: { template: '<div />' } } },
    })
    await flushPromises()
    expect(getConversationResubmitTarget).not.toHaveBeenCalled()
    expect(wrapper.find('.conversation-history-panel__resubmit-trigger').exists()).toBe(false)
    await wrapper.get('[aria-label="选择历史版本"]').setValue('r8-old')
    await flushPromises()
    expect(getConversationResubmitTarget).not.toHaveBeenCalled()
  })
})
