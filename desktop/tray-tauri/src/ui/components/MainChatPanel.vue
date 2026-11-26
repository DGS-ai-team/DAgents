<script setup>
import { computed, ref, shallowRef, watch, nextTick, onBeforeUnmount, triggerRef } from "vue";
import WorkspaceSwitcher from "./WorkspaceSwitcher.vue";
import MessageBubble from "./MessageBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import MemoryConflictBubble from "./MemoryConflictBubble.vue";
import ScrollToTailButton from "./ScrollToTailButton.vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import ToolGroupRow from "./ToolGroupRow.vue";
import UiIcon from "./UiIcon.vue";
import { buildStream } from "../composables/useStream.js";
import { groupConsecutiveToolSteps } from "../utils/streamDisplay.js";
import { countNewStreamItems } from "../utils/streamUnread.js";
import { approvalRequestKey } from "../stores/hitl.js";
import {
  measureElementLayout,
  measureSync,
  startPerformanceSpan,
  updateRuntimeMetrics,
} from "../stores/performanceDiagnostics.js";
import { createFollowTailController, distanceFromTail } from "../utils/scrollTail.js";
import { listConversationRoundToolSteps } from "../api/node.js";
const props = defineProps({
  entries: { type: Array, default: () => [] },
  entriesRevision: { type: Number, default: 0 },
  entriesChange: { type: Object, default: null },
  turnError: { type: String, default: "" },
  hasEarlierHistory: { type: Boolean, default: false },
  modelRecoveryAvailable: { type: Boolean, default: false },
  modelName: { type: String, default: "" },
  modelProfile: { type: String, default: "" },
  hitlQueue: { type: Array, default: () => [] },
  turnState: { type: Object, default: () => ({ toolExecutions: [] }) },
  toolVerbose: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
  hitlBusyIndex: { type: Number, default: -1 },
  agentTitle: { type: String, default: "" },
  readingWindow: { type: Object, default: null },
  compact: { type: Boolean, default: false },
  workspaceView: { type: String, default: "messages" },
  showWorkspaceSwitcher: { type: Boolean, default: true },
  agentId: { type: String, default: "" },
  restoreToolMedia: { type: Function, default: null },
  releaseToolMedia: { type: Function, default: null },
});

const emit = defineEmits([
  "approve-selection",
  "reject-all",
  "open-model-picker",
  "open-model-settings",
  "user-info-submit",
  "user-info-selected",
  "memory-conflict-decide",
  "memory-conflict-cancel",
  "workspace-change",
  "queued-input-context-menu",
  "open-history",
  "reading-window-change",
]);

const streamRef = ref(null);
const streamContentRef = ref(null);
const userInfoSelected = ref([]);

function onUserInfoSelected(next) {
  userInfoSelected.value = Array.isArray(next) ? [...next] : Number(next);
  emit("user-info-selected", userInfoSelected.value);
}

const scrollTail = createFollowTailController();
if (props.readingWindow?.anchorItemKey) scrollTail.setFollow(false);
const showScrollToTail = ref(false);
const unreadMessageCount = ref(0);
let streamResizeObserver = null;
const MAX_RENDERED_STREAM_ITEMS = 180;
const streamWindowStart = ref(0);
let previousStreamItemCount = 0;
let streamBuildHandle = null;
let scrollUpdateHandle = null;
let readingWindowHandle = null;
let readingWindowUsesTimeout = false;
let streamWatchInitialized = false;
let lastReadingWindowSignature = "";
let readingAnchor = null;

const stream = shallowRef([]);
const displayStream = shallowRef([]);
const loadedToolPages = ref(new Map());
const loadingToolPageIDs = ref(new Set());
const toolPageErrors = ref(new Map());

function entriesWithLoadedToolPages(entries) {
  const out = [];
  for (const entry of entries || []) {
    if (entry?.kind !== "tool_group_more") { out.push(entry); continue; }
    let marker = entry;
    const visited = new Set();
    while (marker?.kind === "tool_group_more") {
      const markerID = String(marker.id || marker.item_id || "");
      const page = loadedToolPages.value.get(markerID);
      if (!page || visited.has(markerID)) {
        out.push(marker);
        break;
      }
      visited.add(markerID);
      for (const step of page.steps || []) {
        if (step?.call_entry) out.push(step.call_entry);
        if (step?.result_entry) out.push(step.result_entry);
      }
      if (!page.has_more) break;

      const cursor = String(page.next_after_tool_call_id || "");
      const nextID = `${marker.id || marker.item_id}/after/${cursor}`;
      marker = {
        ...marker,
        id: nextID,
        item_id: nextID,
        after_tool_call_id: cursor,
        remaining_count: Math.max(0, Number(marker.remaining_count) - (Number(page.steps?.length) || 0)),
      };
    }
  }
  return out;
}

async function loadMoreToolSteps(entry) {
  const markerID = String(entry?.id || entry?.item_id || "");
  if (!markerID || loadingToolPageIDs.value.has(markerID) || !props.agentId) return;
  loadingToolPageIDs.value = new Set([...loadingToolPageIDs.value, markerID]);
  const errors = new Map(toolPageErrors.value);
  errors.delete(markerID);
  toolPageErrors.value = errors;
  try {
    const page = await listConversationRoundToolSteps(props.agentId, props.agentId, entry.round_id, {
      revisionId: entry.round_revision_id,
      sourceItemId: entry.tool_call_item_id || entry.detail_item_id,
      afterToolCallId: entry.after_tool_call_id,
      limit: 50,
    });
    if (String(page.round_id) !== String(entry.round_id) || String(page.revision_id) !== String(entry.round_revision_id)) return;
    const pages = new Map(loadedToolPages.value);
    pages.set(markerID, page);
    loadedToolPages.value = pages;
    scheduleStreamBuild();
  } catch (cause) {
    const next = new Map(toolPageErrors.value);
    next.set(markerID, String(cause?.message || "读取工具记录失败"));
    toolPageErrors.value = next;
  } finally {
    const next = new Set(loadingToolPageIDs.value);
    next.delete(markerID);
    loadingToolPageIDs.value = next;
  }
}

/**
 * 流式 token、工具轮询和 SSE 状态可能在同一帧内连续变化。
 * 将展示层重建合并到下一帧，避免每个 token 都重复构建消息列表和测量布局。
 */
function rebuildStream() {
  streamBuildHandle = null;
  const projectionSpan = startPerformanceSpan("stream.projection", { entries: props.entries.length });
  const entries = entriesWithLoadedToolPages(props.entries);
  const items = measureSync(
    "stream.build",
    () => buildStream(entries, props.hitlQueue, props.turnState),
    { entries: entries.length },
  );
  updateRuntimeMetrics({ entries: entries.length, streamItems: items.length });
  stream.value = items;
  displayStream.value = groupConsecutiveToolSteps(items);
  projectionSpan.end({ streamItems: displayStream.value.length });
  const renderSpan = startPerformanceSpan("stream.render_commit", {
    streamItems: displayStream.value.length,
    visibleItems: Math.min(displayStream.value.length, MAX_RENDERED_STREAM_ITEMS),
  });
  void nextTick(() => {
    renderSpan.end();
    measureElementLayout("stream.layout", streamContentRef.value, {
      streamItems: displayStream.value.length,
      visibleItems: Math.min(displayStream.value.length, MAX_RENDERED_STREAM_ITEMS),
    });
  });
}

function insertDraftStreamItem(change) {
  const index = Number(change?.index);
  if (!Number.isInteger(index) || index < 0 || index >= props.entries.length) return false;
  const entry = props.entries[index];
  if (!entry || !["assistant", "reasoning"].includes(entry.kind) || !entry.streaming || !entry.item_id || entry.id !== entry.item_id) return false;
  const item = { key: `e-${entry.id}`, kind: entry.kind, entry };
  if (index === props.entries.length - 1) {
    stream.value.push(item);
    displayStream.value.push(item);
  } else if (
    index === props.entries.length - 2 &&
    props.entries[index + 1]?.id === stream.value.at(-1)?.entry?.id &&
    displayStream.value.at(-1)?.entry?.id === props.entries[index + 1]?.id
  ) {
    const insertAt = stream.value.length - 1;
    stream.value.splice(insertAt, 0, item);
    displayStream.value.splice(displayStream.value.length - 1, 0, item);
  } else {
    return false;
  }
  triggerRef(stream);
  triggerRef(displayStream);
  updateRuntimeMetrics({ entries: props.entries.length, streamItems: displayStream.value.length });
  return true;
}

function scheduleStreamBuild() {
  if (streamBuildHandle !== null) return;
  const run = () => rebuildStream();
  if (typeof requestAnimationFrame === "function") {
    streamBuildHandle = requestAnimationFrame(run);
  } else {
    streamBuildHandle = setTimeout(run, 0);
  }
}

const executionInputKey = computed(() => [
  ...(Array.isArray(props.turnState?.toolExecutions) ? props.turnState.toolExecutions : [])
    .map((execution) => `${execution.toolCallId}:${execution.status}:${execution.attempt}`),
].join("\0"));

// The chat item store is the authority for structural changes. Rebuilding
// from this revision avoids rescanning H history rows on every prop update.
watch(() => props.entriesRevision, (revision, previousRevision) => {
  const change = props.entriesChange;
  if (
    previousRevision !== undefined &&
    change?.revision === revision &&
    change.kind === "insert" &&
    insertDraftStreamItem(change)
  ) return;
  scheduleStreamBuild();
}, { immediate: true });
watch(() => props.hitlQueue, scheduleStreamBuild);
watch(executionInputKey, scheduleStreamBuild);

const renderedStream = computed(() => {
  let visible = displayStream.value;
  if (displayStream.value.length > MAX_RENDERED_STREAM_ITEMS) {
    const start = Math.min(
      Math.max(0, streamWindowStart.value),
      Math.max(0, displayStream.value.length - MAX_RENDERED_STREAM_ITEMS),
    );
    visible = displayStream.value.slice(start, start + MAX_RENDERED_STREAM_ITEMS);
  }
  updateRuntimeMetrics({
    entries: props.entries.length,
    streamItems: displayStream.value.length,
    visibleItems: visible.length,
  });
  return visible;
});
const renderedStreamAnchorSignature = computed(() => renderedStream.value.map(item => item.key).join("\0"));
const hasEarlierStreamItems = computed(
  () => displayStream.value.length > MAX_RENDERED_STREAM_ITEMS && streamWindowStart.value > 0,
);
const earlierStreamItemCount = computed(() => Math.max(0, streamWindowStart.value));
const latestConversationOrdinal = computed(() => {
  for (let index = props.entries.length - 1; index >= 0; index -= 1) {
    const ordinal = Number(props.entries[index]?.conversation_ordinal);
    if (Number.isFinite(ordinal)) return ordinal;
  }
  return 0;
});

function conversationOrdinalForEntry(entry) {
  const ordinal = Number(entry?.conversation_ordinal);
  return Number.isFinite(ordinal) && ordinal > 0 ? ordinal : 0;
}

function conversationOrdinalForStreamItem(item) {
  if (item?.entry) return conversationOrdinalForEntry(item.entry);
  if (item?.callEntry) return conversationOrdinalForEntry(item.callEntry);
  if (item?.resultEntry) return conversationOrdinalForEntry(item.resultEntry);
  const firstStep = item?.steps?.[0];
  return conversationOrdinalForEntry(firstStep?.callEntry || firstStep?.resultEntry);
}

function streamItemBox(element, container) {
  if (!element || !container) return null;
  const box = element.getBoundingClientRect();
  const viewport = container.getBoundingClientRect();
  return { top: box.top - viewport.top, bottom: box.bottom - viewport.top };
}

function findStreamItem(key, container = streamRef.value) {
  if (!key || !container) return null;
  return Array.from(container.querySelectorAll("[data-stream-item-key]"))
    .find(element => element.dataset.streamItemKey === key) || null;
}

function captureReadingAnchor() {
  const container = streamRef.value;
  if (!container) return null;
  const visible = Array.from(container.querySelectorAll("[data-stream-item-key][data-conversation-ordinal]"))
    .map(element => ({ element, box: streamItemBox(element, container) }))
    .find(({ box }) => box && box.bottom > 1 && box.top < container.clientHeight - 1);
  const key = String(visible?.element?.dataset?.streamItemKey || "");
  if (!key) return readingAnchor;
  readingAnchor = { key, offset: visible.box.top, element: visible.element };
  return readingAnchor;
}

function restoreReadingAnchor(anchor = readingAnchor) {
  const container = streamRef.value;
  if (!container || !anchor?.key) return false;
  const element = anchor.element?.isConnected && anchor.element.dataset.streamItemKey === anchor.key
    ? anchor.element
    : findStreamItem(anchor.key, container);
  if (!element) return false;
  const box = streamItemBox(element, container);
  if (!box) return false;
  const delta = box.top - anchor.offset;
  if (Math.abs(delta) > 0.5) {
    const previousBehavior = container.style.scrollBehavior;
    container.style.scrollBehavior = "auto";
    container.scrollTop += delta;
    container.style.scrollBehavior = previousBehavior;
  }
  readingAnchor = { key: anchor.key, offset: anchor.offset, element };
  return true;
}

watch(() => [
  props.readingWindow?.anchorItemKey,
  props.readingWindow?.anchorOffset,
  props.readingWindow?.conversationEpoch,
  props.entriesRevision,
  renderedStreamAnchorSignature.value,
], async ([anchorItemKey, anchorOffset], previous) => {
  if (!anchorItemKey || !Number.isFinite(Number(anchorOffset))) {
    if (previous?.[0] && !props.readingWindow) {
      readingAnchor = null;
      await nextTick();
      scrollTail.forcePin(streamRef.value);
      updateScrollToTailVisibility();
    }
    return;
  }
  if (previous && previous[0] === anchorItemKey && previous[1] === anchorOffset && previous[3] === props.entriesRevision && previous[4] === renderedStreamAnchorSignature.value) return;
  const rowsChanged = !previous || previous[3] !== props.entriesRevision || previous[4] !== renderedStreamAnchorSignature.value;
  if (!rowsChanged && readingAnchor?.key === anchorItemKey && readingAnchor.offset === Number(anchorOffset)) return;
  await nextTick();
  const element = findStreamItem(String(anchorItemKey));
  if (!element) return;
  scrollTail.setFollow(false);
  const anchor = { key: String(anchorItemKey), offset: Number(anchorOffset), element };
  readingAnchor = anchor;
  restoreReadingAnchor(anchor);
}, { immediate: true, flush: "post" });

function publishReadingWindowChange() {
  const following = scrollTail.follow;
  if (following) {
    if (lastReadingWindowSignature === "following") return;
    lastReadingWindowSignature = "following";
    emit("reading-window-change", { following: true });
    return;
  }
  const container = streamRef.value;
  const containerTop = container?.getBoundingClientRect?.().top;
  const visibleItems = container ? Array.from(container.querySelectorAll("[data-conversation-ordinal]")) : [];
  const anchor = visibleItems.find(child => child.getBoundingClientRect().bottom > containerTop + 1);
  const anchorOrdinal = Number(anchor?.dataset?.conversationOrdinal);
  const endOrdinal = latestConversationOrdinal.value;
  if (!Number.isFinite(anchorOrdinal) || anchorOrdinal <= 0 || !Number.isFinite(endOrdinal) || endOrdinal < anchorOrdinal) return;
  const visibleAnchor = readingAnchor || captureReadingAnchor();
  const anchorItemKey = visibleAnchor?.key || "";
  const anchorOffset = Number.isFinite(visibleAnchor?.offset) ? visibleAnchor.offset : undefined;
  const signature = `${anchorOrdinal}:${endOrdinal}:${anchorItemKey}:${Number.isFinite(anchorOffset) ? Math.round(anchorOffset) : ""}`;
  if (lastReadingWindowSignature === signature) return;
  lastReadingWindowSignature = signature;
  emit("reading-window-change", {
    following: false,
    anchorOrdinal,
    endOrdinal,
    anchorItemKey,
    anchorOffset,
  });
}

function scheduleReadingWindowChange() {
  if (readingWindowHandle !== null) return;
  const run = () => {
    readingWindowHandle = null;
    publishReadingWindowChange();
  };
  if (typeof requestAnimationFrame === "function") {
    readingWindowUsesTimeout = false;
    readingWindowHandle = requestAnimationFrame(run);
  } else {
    readingWindowUsesTimeout = true;
    readingWindowHandle = setTimeout(run, 0);
  }
}

const hitlQueueKey = computed(() =>
  props.hitlQueue
    .map((item) => `${item?.kind || ""}:${item?.data?.request_id || item?.data?.approval_id || ""}`)
    .join("\0"),
);
/** 消息/HITL/状态条等内容变化指纹；流式 assistant 改 text 长度也会触发。 */
function maybeScrollToTail() {
  if (scrollUpdateHandle !== null) return;
  const run = () => {
    scrollUpdateHandle = null;
    nextTick(() => {
      measureSync("scroll.update", () => {
        scrollTail.pinIfFollowing(streamRef.value);
        updateScrollToTailVisibility();
      });
    });
  };
  if (typeof requestAnimationFrame === "function") {
    scrollUpdateHandle = requestAnimationFrame(run);
  } else {
    scrollUpdateHandle = setTimeout(run, 0);
  }
}

watch([displayStream, hitlQueueKey], () => {
  maybeScrollToTail();
});

// HITL 卡片属于消息区末尾的新内容。沿用消息流当前的跟随状态：用户原本
// 在底部时滚到新的底部，用户正在查看历史时不打断其阅读位置。
watch(hitlQueueKey, (next, previous) => {
  if (next === previous) return;
  nextTick(() => {
    const scroll = () => maybeScrollToTail();
    if (typeof requestAnimationFrame === "function") requestAnimationFrame(scroll);
    else setTimeout(scroll, 0);
  });
});

function onStreamScroll() {
  scrollTail.onScroll(streamRef.value);
  if (scrollTail.follow) readingAnchor = null;
  else captureReadingAnchor();
  updateScrollToTailVisibility();
  scheduleReadingWindowChange();
}

function updateScrollToTailVisibility() {
  const el = streamRef.value;
  if (scrollTail.follow) unreadMessageCount.value = 0;
  showScrollToTail.value = Boolean(el && !scrollTail.follow && distanceFromTail(el) > 48);
}

function scrollToTail() {
  streamWindowStart.value = Math.max(0, displayStream.value.length - MAX_RENDERED_STREAM_ITEMS);
  readingAnchor = null;
  unreadMessageCount.value = 0;
  nextTick(() => {
    scrollTail.forcePin(streamRef.value);
    updateScrollToTailVisibility();
    publishReadingWindowChange();
  });
}

function loadEarlierStreamItems() {
  const el = streamRef.value;
  const beforeHeight = el?.scrollHeight || 0;
  const beforeTop = el?.scrollTop || 0;
  const anchor = captureReadingAnchor() || readingAnchor;
  streamWindowStart.value = Math.max(0, streamWindowStart.value - MAX_RENDERED_STREAM_ITEMS);
  nextTick(() => {
    if (!restoreReadingAnchor(anchor) && el) el.scrollTop = beforeTop + Math.max(0, el.scrollHeight - beforeHeight);
    publishReadingWindowChange();
  });
}

function bindStreamResizeObserver() {
  const el = streamRef.value;
  const content = streamContentRef.value;
  if (!el || !content || typeof ResizeObserver === "undefined") return;
  streamResizeObserver?.disconnect();
  streamResizeObserver = new ResizeObserver(() => {
    if (scrollTail.follow) {
      scrollTail.pinIfFollowing(el);
      readingAnchor = null;
    } else {
      restoreReadingAnchor();
    }
    updateScrollToTailVisibility();
  });
  streamResizeObserver.observe(content);
  streamResizeObserver.observe(el);
}

onBeforeUnmount(() => {
  streamResizeObserver?.disconnect();
  streamResizeObserver = null;
  if (streamBuildHandle !== null) {
    if (typeof cancelAnimationFrame === "function") cancelAnimationFrame(streamBuildHandle);
    else clearTimeout(streamBuildHandle);
    streamBuildHandle = null;
  }
  if (scrollUpdateHandle !== null) {
    if (typeof cancelAnimationFrame === "function") cancelAnimationFrame(scrollUpdateHandle);
    else clearTimeout(scrollUpdateHandle);
    scrollUpdateHandle = null;
  }
  if (readingWindowHandle !== null) {
    if (readingWindowUsesTimeout) clearTimeout(readingWindowHandle);
    else if (typeof cancelAnimationFrame === "function") cancelAnimationFrame(readingWindowHandle);
    readingWindowHandle = null;
  }
});

watch(streamRef, (el) => {
  if (el) bindStreamResizeObserver();
});

watch(displayStream, (items, previousItems) => {
  if (!streamWatchInitialized) {
    streamWatchInitialized = true;
  } else if (!scrollTail.follow && Array.isArray(previousItems)) {
    const addedCount = countNewStreamItems(items, previousItems);
    if (addedCount > 0) unreadMessageCount.value += addedCount;
  }
  if (scrollTail.follow && items.length > previousStreamItemCount) {
    streamWindowStart.value = Math.max(0, items.length - MAX_RENDERED_STREAM_ITEMS);
  } else {
    streamWindowStart.value = Math.min(
      streamWindowStart.value,
      Math.max(0, items.length - MAX_RENDERED_STREAM_ITEMS),
    );
  }
  previousStreamItemCount = items.length;
}, { immediate: true });

defineExpose({
  scrollToTail,
});
</script>

<template>
  <section class="panel panel--flex chat" :class="{ 'chat--compact': compact }">
    <header class="chat__header">
      <div class="chat__title">
        <span class="chat__title-main">{{ agentTitle || "助手" }}</span>
      </div>
      <div class="chat__header-meta">
        <WorkspaceSwitcher
          v-if="showWorkspaceSwitcher && !compact"
          :active="workspaceView"
          @change="(view) => emit('workspace-change', view)"
        />
      </div>
    </header>

    <div class="chat__stream-wrap">
      <div
        ref="streamRef"
        class="chat__stream"
        role="log"
        aria-label="消息记录"
        :aria-busy="sending || cancelling"
        @scroll="onStreamScroll"
      >
      <div ref="streamContentRef" class="chat__stream-content">
      <div v-if="!stream.length" class="chat__empty">
        <div class="chat__empty-inner">
          <div class="chat__empty-title">开始对话</div>
          <div class="chat__empty-hint">输入消息与 Agent 协作，或在设置 › 帮助中查看命令</div>
        </div>
      </div>
      <button
        v-if="hasEarlierStreamItems"
        type="button"
        class="chat__load-earlier"
        :aria-label="`加载更早的 ${earlierStreamItemCount} 条消息`"
        @click="loadEarlierStreamItems"
      >
        加载更早的 {{ earlierStreamItemCount }} 条消息
      </button>
      <button
        v-if="hasEarlierHistory && !hasEarlierStreamItems"
        type="button"
        class="chat__history-link"
        @click="emit('open-history')"
      >
        查看更早的对话历史
      </button>
      <template v-for="item in renderedStream" :key="item.key">
        <MessageBubble
          v-if="['user', 'assistant', 'reasoning', 'system', 'context_boundary'].includes(item.kind)"
          :entry="item.entry"
          :conversation-ordinal="conversationOrdinalForStreamItem(item)"
          :stream-item-key="item.key"
          :agent-id="agentId"
          @queue-context-menu="emit('queued-input-context-menu', $event)"
        />
        <ToolSummaryRow
          v-else-if="item.kind === 'tool_step'"
          :call-entry="item.callEntry"
          :result-entry="item.resultEntry"
          :phase="item.phase"
          :conversation-ordinal="conversationOrdinalForStreamItem(item)"
          :stream-item-key="item.key"
          :verbose="toolVerbose"
          :agent-id="agentId"
          :restore-tool-media="restoreToolMedia"
          :release-tool-media="releaseToolMedia"
        />
        <ToolGroupRow
          v-else-if="item.kind === 'tool_group'"
          :steps="item.steps"
          :conversation-ordinal="conversationOrdinalForStreamItem(item)"
          :stream-item-key="item.key"
          :verbose="toolVerbose"
          :agent-id="agentId"
          :restore-tool-media="restoreToolMedia"
          :release-tool-media="releaseToolMedia"
        />
        <div v-else-if="item.kind === 'tool_group_more'" class="chat__tool-more-wrap" :data-conversation-ordinal="conversationOrdinalForStreamItem(item) || undefined" :data-stream-item-key="item.key">
          <button class="chat__tool-more" type="button" :disabled="loadingToolPageIDs.has(String(item.entry?.id || item.entry?.item_id || ''))" @click="loadMoreToolSteps(item.entry)">
            {{ loadingToolPageIDs.has(String(item.entry?.id || item.entry?.item_id || '')) ? '正在加载…' : `查看剩余 ${Number(item.entry?.remaining_count) || 0} 项工具记录` }}
          </button>
          <span v-if="toolPageErrors.get(String(item.entry?.id || item.entry?.item_id || ''))" class="chat__tool-more-error" role="alert">{{ toolPageErrors.get(String(item.entry?.id || item.entry?.item_id || '')) }}</span>
        </div>
        <ApprovalBubble
          v-else-if="item.kind === 'approval'"
          :data="item.hitl.data"
          :request-key="approvalRequestKey(item.hitl.data)"
          :stream-item-key="item.key"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @approve-selection="(approvedCallIds) => emit('approve-selection', { index: item.hitlIndex, approvedCallIds })"
          @reject-all="emit('reject-all', item.hitlIndex)"
        />
        <UserInfoBubble
          v-else-if="item.kind === 'user_information'"
          :data="item.hitl.data"
          :selected="userInfoSelected"
          :stream-item-key="item.key"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @update:selected="onUserInfoSelected"
          @submit="answer => emit('user-info-submit', item.hitlIndex, answer)"
        />
        <MemoryConflictBubble
          v-else-if="item.kind === 'memory_conflict'"
          :data="item.hitl.data"
          :stream-item-key="item.key"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @decide="(decision) => emit('memory-conflict-decide', { index: item.hitlIndex, decision })"
          @cancel="emit('memory-conflict-cancel', item.hitlIndex)"
        />
      </template>
      <div v-if="turnError" class="chat__turn-error">
        <p role="alert">{{ turnError }}</p>
        <div v-if="modelRecoveryAvailable" class="chat__turn-error-meta">
          <span v-if="modelName || modelProfile" class="chat__turn-error-model">
            当前选择 · {{ modelName ? `模型：${modelName}` : "" }}{{ modelName && modelProfile ? " · " : "" }}{{ modelProfile ? `配置：${modelProfile}` : "" }}
          </span>
          <div class="chat__turn-error-actions">
            <button type="button" class="chat__turn-error-action" title="切换模型配置" aria-label="切换模型配置" @click="emit('open-model-picker')"><UiIcon name="arrow-left-right" :size="14" /></button>
            <button v-if="modelProfile" type="button" class="chat__turn-error-action" title="打开模型设置" aria-label="打开模型设置" @click="emit('open-model-settings')"><UiIcon name="settings" :size="14" /></button>
          </div>
        </div>
      </div>
      </div>
      </div>
      <ScrollToTailButton
        :visible="showScrollToTail"
        :unread-count="unreadMessageCount"
        @click="scrollToTail"
      />
    </div>

  </section>
</template>
