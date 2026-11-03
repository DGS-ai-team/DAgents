<script setup>
import { computed, ref, shallowRef, watch, nextTick, onMounted, onBeforeUnmount, triggerRef } from "vue";
import WorkspaceSwitcher from "./WorkspaceSwitcher.vue";
import MessageBubble from "./MessageBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import MemoryConflictBubble from "./MemoryConflictBubble.vue";
import ScrollToTailButton from "./ScrollToTailButton.vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import ToolGroupRow from "./ToolGroupRow.vue";
import { buildStream } from "../composables/useStream.js";
import { groupConsecutiveToolSteps } from "../utils/streamDisplay.js";
import { toolJobsStore } from "../stores/toolJobs.js";
import { turnStateStore } from "../stores/turnState.js";
import { countNewStreamItems } from "../utils/streamUnread.js";
import {
  measureSync,
  updateRuntimeMetrics,
} from "../stores/performanceDiagnostics.js";
import { createFollowTailController, distanceFromTail } from "../utils/scrollTail.js";
const props = defineProps({
  entries: { type: Array, default: () => [] },
  streamingText: { type: String, default: "" },
  turnError: { type: String, default: "" },
  hitlQueue: { type: Array, default: () => [] },
  toolVerbose: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
  hitlBusyIndex: { type: Number, default: -1 },
  agentTitle: { type: String, default: "" },
  compact: { type: Boolean, default: false },
  workspaceView: { type: String, default: "messages" },
  showWorkspaceSwitcher: { type: Boolean, default: true },
  agentId: { type: String, default: "" },
});

const emit = defineEmits([
  "approve-all",
  "reject-all",
  "approve-one",
  "reject-one",
  "user-info-submit",
  "user-info-selected",
  "memory-conflict-decide",
  "memory-conflict-cancel",
  "workspace-change",
]);

const streamRef = ref(null);
const userInfoSelected = ref([]);

function onUserInfoSelected(next) {
  userInfoSelected.value = Array.isArray(next) ? [...next] : Number(next);
  emit("user-info-selected", userInfoSelected.value);
}

const scrollTail = createFollowTailController();
const showScrollToTail = ref(false);
const unreadMessageCount = ref(0);
let streamResizeObserver = null;
const MAX_RENDERED_STREAM_ITEMS = 180;
const streamWindowStart = ref(0);
let previousStreamItemCount = 0;
let streamBuildHandle = null;
let scrollUpdateHandle = null;
let streamWatchInitialized = false;

const stream = shallowRef([]);
const displayStream = shallowRef([]);

function entryValueKey(value) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value) ?? String(value ?? "");
  } catch {
    return String(value ?? "");
  }
}

// Track exact history payloads for same-length edits. This key is independent
// of live assistant text, so token deltas never rescan or stringify history.
const entriesInputKey = computed(() => {
  const parts = [props.entries.length];
  for (const entry of props.entries) {
    parts.push(
      entry.id,
      entry.kind,
      entry.text,
      entryValueKey(entry.file_refs),
      entry.streaming ? 1 : 0,
      entry.partial ? 1 : 0,
      entry.summary,
      entry.data?.tool_call_id,
      entry.data?.tool_name,
      entryValueKey(entry.data?.content),
      entryValueKey(entry.data?.arguments),
    );
  }
  return parts.join("\0");
});

/**
 * 流式 token、工具轮询和 SSE 状态可能在同一帧内连续变化。
 * 将展示层重建合并到下一帧，避免每个 token 都重复构建消息列表和测量布局。
 */
function rebuildStream() {
  streamBuildHandle = null;
  const entries = props.streamingText
    ? [...props.entries, liveAssistantEntry(props.streamingText)]
    : props.entries;
  const items = measureSync(
    "stream.build",
    () => buildStream(entries, props.hitlQueue),
    { entries: entries.length },
  );
  updateRuntimeMetrics({ entries: entries.length, streamItems: items.length });
  stream.value = items;
  displayStream.value = groupConsecutiveToolSteps(items);
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
  toolJobsStore.runningCallIds.join(","),
  ...turnStateStore.toolExecutions.map((execution) => `${execution.toolCallId}:${execution.status}:${execution.attempt}`),
].join("\0"));

function liveAssistantEntry(text) {
  return {
    id: `${props.agentId || "agent"}:live-assistant`,
    kind: "assistant",
    text: String(text || ""),
    streaming: true,
  };
}

function patchStreamingTail(text) {
  const last = stream.value.at(-1);
  const isLiveTail = last?.kind === "assistant" && last.entry?.streaming && last.entry?.id === `${props.agentId || "agent"}:live-assistant`;
  if (!text) {
    if (!isLiveTail) return;
    stream.value.pop();
    displayStream.value.pop();
    triggerRef(stream);
    triggerRef(displayStream);
    return;
  }
  if (!isLiveTail) {
    scheduleStreamBuild();
    return;
  }
  const entry = liveAssistantEntry(text);
  last.entry = entry;
  displayStream.value.at(-1).entry = entry;
  triggerRef(stream);
  triggerRef(displayStream);
}

watch(entriesInputKey, scheduleStreamBuild, { immediate: true });
watch(() => props.hitlQueue, scheduleStreamBuild);
watch(executionInputKey, scheduleStreamBuild);
watch(() => props.streamingText, patchStreamingTail);

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
const hasEarlierStreamItems = computed(
  () => displayStream.value.length > MAX_RENDERED_STREAM_ITEMS && streamWindowStart.value > 0,
);
const earlierStreamItemCount = computed(() => Math.max(0, streamWindowStart.value));

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

watch([displayStream, hitlQueueKey, () => props.streamingText], () => {
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
  updateScrollToTailVisibility();
}

function updateScrollToTailVisibility() {
  const el = streamRef.value;
  if (scrollTail.follow) unreadMessageCount.value = 0;
  showScrollToTail.value = Boolean(el && !scrollTail.follow && distanceFromTail(el) > 48);
}

function scrollToTail() {
  streamWindowStart.value = Math.max(0, displayStream.value.length - MAX_RENDERED_STREAM_ITEMS);
  unreadMessageCount.value = 0;
  nextTick(() => {
    scrollTail.forcePin(streamRef.value);
    updateScrollToTailVisibility();
  });
}

function loadEarlierStreamItems() {
  const el = streamRef.value;
  const beforeHeight = el?.scrollHeight || 0;
  const beforeTop = el?.scrollTop || 0;
  streamWindowStart.value = Math.max(0, streamWindowStart.value - MAX_RENDERED_STREAM_ITEMS);
  nextTick(() => {
    if (el) el.scrollTop = beforeTop + Math.max(0, el.scrollHeight - beforeHeight);
  });
}

function bindStreamResizeObserver() {
  const el = streamRef.value;
  if (!el || typeof ResizeObserver === "undefined") return;
  streamResizeObserver?.disconnect();
  streamResizeObserver = new ResizeObserver(() => {
    if (scrollTail.follow) scrollTail.pinIfFollowing(el);
    updateScrollToTailVisibility();
  });
  const inner = el.firstElementChild || el;
  streamResizeObserver.observe(inner);
  streamResizeObserver.observe(el);
}

onMounted(() => {
  bindStreamResizeObserver();
});

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
      <template v-for="item in renderedStream" :key="item.key">
        <MessageBubble
          v-if="['user', 'assistant', 'reasoning', 'system', 'context_boundary'].includes(item.kind)"
          :entry="item.entry"
          :agent-id="agentId"
        />
        <ToolSummaryRow
          v-else-if="item.kind === 'tool_step'"
          :call-entry="item.callEntry"
          :result-entry="item.resultEntry"
          :execution-hint="item.executionHint"
          :verbose="toolVerbose"
        />
        <ToolGroupRow
          v-else-if="item.kind === 'tool_group'"
          :steps="item.steps"
          :verbose="toolVerbose"
        />
        <ApprovalBubble
          v-else-if="item.kind === 'approval'"
          :data="item.hitl.data"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @approve-all="emit('approve-all', item.hitlIndex)"
          @reject-all="emit('reject-all', item.hitlIndex)"
          @approve-one="(id) => emit('approve-one', { index: item.hitlIndex, callId: id })"
          @reject-one="(id) => emit('reject-one', { index: item.hitlIndex, callId: id })"
        />
        <UserInfoBubble
          v-else-if="item.kind === 'user_information'"
          :data="item.hitl.data"
          :selected="userInfoSelected"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @update:selected="onUserInfoSelected"
          @submit="emit('user-info-submit', item.hitlIndex)"
        />
        <MemoryConflictBubble
          v-else-if="item.kind === 'memory_conflict'"
          :data="item.hitl.data"
          :busy="hitlBusy && hitlBusyIndex === item.hitlIndex"
          @decide="(decision) => emit('memory-conflict-decide', { index: item.hitlIndex, decision })"
          @cancel="emit('memory-conflict-cancel', item.hitlIndex)"
        />
      </template>
      <p v-if="turnError" class="chat__turn-error" role="alert">{{ turnError }}</p>
      </div>
      <ScrollToTailButton
        :visible="showScrollToTail"
        :unread-count="unreadMessageCount"
        @click="scrollToTail"
      />
    </div>

  </section>
</template>
