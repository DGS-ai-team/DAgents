<script setup>
import { computed, ref, shallowRef, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import ChatComposer from "./ChatComposer.vue";
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
import { countNewStreamItems } from "../utils/streamUnread.js";
import {
  measureSync,
  updateRuntimeMetrics,
} from "../stores/performanceDiagnostics.js";
import { createFollowTailController, distanceFromTail } from "../utils/scrollTail.js";
const props = defineProps({
  entries: { type: Array, default: () => [] },
  hitlQueue: { type: Array, default: () => [] },
  toolVerbose: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
  hitlBusyIndex: { type: Number, default: -1 },
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
  agentTitle: { type: String, default: "" },
  error: { type: String, default: "" },
  hideComposer: { type: Boolean, default: false },
  compact: { type: Boolean, default: false },
  workspaceView: { type: String, default: "messages" },
  showWorkspaceSwitcher: { type: Boolean, default: true },
  agentId: { type: String, default: "" },
  terminalRefreshKey: { type: Number, default: 0 },
});

const emit = defineEmits([
  "send",
  "cancel",
  "toggle-thinking",
  "cycle-effort",
  "switch-profile",
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
const composerRef = ref(null);
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

/**
 * 流式 token、工具轮询和 SSE 状态可能在同一帧内连续变化。
 * 将展示层重建合并到下一帧，避免每个 token 都重复构建消息列表和测量布局。
 */
const streamInputKey = computed(() => {
  const parts = [props.entries.length, props.hitlQueue.length];
  for (const entry of props.entries) {
    parts.push(
      entry.id,
      entry.kind,
      (entry.text || "").length,
      Array.isArray(entry.file_refs) ? entry.file_refs.map((file) => file?.path || "").join(",") : "",
      entry.streaming ? 1 : 0,
      entry.partial ? 1 : 0,
      String(entry.summary || "").length,
      String(entry.data?.content || "").length,
      String(entry.data?.arguments || "").length,
    );
  }
  for (const hitl of props.hitlQueue) {
    parts.push(hitl.kind, hitl.data?.request_id || hitl.data?.approval_id || "");
  }
  parts.push(
    toolJobsStore.runningCallIds.join(","),
    toolJobsStore.backgroundCallIds.join(","),
  );
  return parts.join("\0");
});

function rebuildStream() {
  streamBuildHandle = null;
  const items = measureSync(
    "stream.build",
    () => buildStream(props.entries, props.hitlQueue, toolJobsStore),
    { entries: props.entries.length },
  );
  updateRuntimeMetrics({ entries: props.entries.length, streamItems: items.length });
  stream.value = items;
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

const displayStream = computed(() => groupConsecutiveToolSteps(stream.value));

watch(streamInputKey, scheduleStreamBuild, { immediate: true });

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
const tailContentKey = computed(() => {
  return measureSync("tail.key", () => {
    const parts = [stream.value.length, props.hitlQueue.length];
    for (const entry of props.entries) {
      parts.push(
        entry.id,
        entry.kind,
        (entry.text || "").length,
        Array.isArray(entry.file_refs) ? entry.file_refs.length : 0,
        entry.streaming ? 1 : 0,
      );
      // 工具气泡内容在 data 上，text 常为空；纳入 summary / content 长度以免漏钉尾
      if (entry.kind === "tool_call" || entry.kind === "tool_result") {
        parts.push(
          String(entry.summary || "").length,
          String(entry.data?.content || "").length,
          entry.partial ? 1 : 0,
        );
      }
    }
    for (const hitl of props.hitlQueue) {
      parts.push(hitl.kind, hitl.data?.request_id || hitl.data?.approval_id || "");
    }
    return parts.join("\0");
  }, { entries: props.entries.length });
});

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

watch(tailContentKey, () => {
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

function onComposerSend(payload) {
  scrollToTail();
  emit("send", payload);
}

defineExpose({
  setDraft(text) {
    composerRef.value?.setDraft(text);
  },
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
          v-if="['user', 'assistant', 'reasoning', 'system'].includes(item.kind)"
          :entry="item.entry"
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
      </div>
      <ScrollToTailButton
        :visible="showScrollToTail"
        :unread-count="unreadMessageCount"
        @click="scrollToTail"
      />
    </div>

    <ChatComposer
      v-if="!hideComposer"
      ref="composerRef"
      :hitl-queue="hitlQueue"
      :disabled="disabled"
      :sending="sending"
      :cancelling="cancelling"
      :hitl-busy="hitlBusy"
      :thinking-supported="thinkingSupported"
      :llm-settings="llmSettings"
      :error="error"
      :agent-id="agentId"
      :workspace-view="workspaceView"
      :terminal-refresh-key="terminalRefreshKey"
      @send="onComposerSend"
      @cancel="emit('cancel')"
      @toggle-thinking="emit('toggle-thinking')"
      @cycle-effort="emit('cycle-effort')"
      @switch-profile="(id) => emit('switch-profile', id)"
    />
  </section>
</template>
