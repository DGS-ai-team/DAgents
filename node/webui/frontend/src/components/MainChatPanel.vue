<script setup>
import { computed, ref, watch, nextTick } from "vue";
import ComposerToolbar from "./ComposerToolbar.vue";
import MessageBubble from "./MessageBubble.vue";
import StreamStatusBubble from "./StreamStatusBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import ToolExecBubble from "./ToolExecBubble.vue";
import { buildStream } from "../composables/useStream.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { hasStreamingKind, hasStreamingTextContent } from "../stores/transcript.js";
import { chromeStore, inputStripRight } from "../stores/chrome.js";
import { workerStripText } from "../stores/remoteWorkers.js";
import { statusStore, statusPhaseOrder, hasStatus } from "../stores/statusLines.js";

const props = defineProps({
  entries: { type: Array, default: () => [] },
  hitlQueue: { type: Array, default: () => [] },
  showReasoning: { type: Boolean, default: false },
  toolVerbose: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
  hitlBusyIndex: { type: Number, default: -1 },
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
});

const emit = defineEmits([
  "send",
  "cancel",
  "open-context",
  "toggle-thinking",
  "cycle-effort",
  "approve-all",
  "reject-all",
  "approve-one",
  "reject-one",
  "user-info-submit",
  "user-info-selected",
]);

const input = ref("");
const streamRef = ref(null);
const userInfoSelected = ref(0);

const stream = computed(() => buildStream(props.entries, props.hitlQueue));

const activeStatusPhases = computed(() => {
  void statusStore.tick;
  return statusPhaseOrder.filter((phase) => {
    if (!hasStatus(phase)) return false;
    if (phase === "thinking" && hasStreamingKind("reasoning")) return false;
    if (phase === "prefilling" && hasStreamingTextContent()) return false;
    return true;
  });
});

const pendingApprovals = computed(() =>
  props.hitlQueue
    .filter((h) => h.kind === "approval")
    .reduce((n, h) => n + extractToolApprovals(h.data).length, 0),
);

const inputStripRightText = computed(() => {
  void chromeStore.usageStrip;
  void chromeStore.contextTokens;
  void chromeStore.llmSettings;
  return inputStripRight();
});

const workerStrip = computed(() => workerStripText());

const inputStripLeftText = computed(() => {
  if (props.cancelling) return "正在取消…";
  if (pendingApprovals.value > 0) {
    return `${pendingApprovals.value} 个工具待审批`;
  }
  if (props.hitlQueue.length > 1) {
    return `HITL 队列 ${props.hitlQueue.length}`;
  }
  return "";
});

const showCancel = computed(() => props.sending && !props.hitlBusy);
const canSubmit = computed(() => !props.disabled && !props.sending && !!input.value.trim());

watch(
  () => [stream.value.length, activeStatusPhases.value.length],
  async () => {
    await nextTick();
    const el = streamRef.value;
    if (el) el.scrollTop = el.scrollHeight;
  },
);

async function submit() {
  const text = input.value.trim();
  if (!text || props.disabled || props.sending) return;
  emit("send", text);
  input.value = "";
}

function onCancel() {
  if (!showCancel.value || props.cancelling) return;
  emit("cancel");
}

function onKeydown(e) {
  if (e.key === "Enter" && !e.ctrlKey) {
    e.preventDefault();
    submit();
  }
}

defineExpose({
  setDraft(text) {
    input.value = String(text || "");
  },
});
</script>

<template>
  <section class="panel panel--flex chat">
    <header class="chat__header">
      <div class="chat__title">
        <span class="chat__title-main">对话</span>
        <span class="chat__title-sub">与 Agent 协作</span>
      </div>
      <div class="chat__header-meta">
        <span v-if="pendingApprovals > 0" class="pill pill--warn">{{ pendingApprovals }} 待审批</span>
      </div>
    </header>

    <div ref="streamRef" class="chat__stream">
      <div v-if="!stream.length" class="chat__empty">
        <div class="chat__empty-inner">
          <div class="chat__empty-title">开始对话</div>
          <div class="chat__empty-hint">输入消息与 Agent 协作，或使用 <code>/help</code> 查看命令</div>
        </div>
      </div>
      <template v-for="item in stream" :key="item.key">
        <MessageBubble
          v-if="['user', 'assistant', 'reasoning', 'system'].includes(item.kind)"
          :entry="item.entry"
          :show-reasoning="showReasoning"
        />
        <ToolExecBubble
          v-else-if="item.kind === 'tool_call' || item.kind === 'tool_result'"
          :entry="item.entry"
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
          @update:selected="(v) => { userInfoSelected = v; emit('user-info-selected', v); }"
          @submit="emit('user-info-submit', item.hitlIndex)"
        />
      </template>
      <StreamStatusBubble
        v-for="phase in activeStatusPhases"
        :key="`status-${phase}`"
        :phase="phase"
      />
    </div>

    <footer class="chat__composer">
      <div class="chat__composer-card">
        <div class="chat__composer-meta">
          <div class="chat__composer-meta-left">
            <ComposerToolbar
              class="chat__composer-toolbar"
              :thinking-supported="thinkingSupported"
              :llm-settings="llmSettings"
              :disabled="disabled || cancelling"
              @open-context="emit('open-context')"
              @toggle-thinking="emit('toggle-thinking')"
              @cycle-effort="emit('cycle-effort')"
            />
            <div v-if="workerStrip || inputStripLeftText" class="chat__composer-meta-status">
              <span v-if="workerStrip" class="chat__worker-strip">{{ workerStrip }}</span>
              <span v-if="inputStripLeftText" class="chat__input-strip-left">{{ inputStripLeftText }}</span>
            </div>
          </div>
          <span v-if="inputStripRightText" class="chat__input-strip-right" :title="inputStripRightText">
            {{ inputStripRightText }}
          </span>
        </div>
        <div class="chat__composer-row">
          <textarea
            v-model="input"
            class="chat__textarea"
            rows="2"
            placeholder="输入消息或 /help 命令（Enter 发送，Ctrl+Enter 换行）"
            :disabled="disabled || sending || cancelling"
            @keydown="onKeydown"
          />
          <button
            v-if="showCancel"
            type="button"
            class="btn btn--danger chat__composer-btn"
            :disabled="cancelling"
            @click="onCancel"
          >
            {{ cancelling ? "取消中…" : "取消" }}
          </button>
          <button
            v-else
            type="button"
            class="btn btn--primary chat__composer-btn"
            :disabled="!canSubmit"
            @click="submit"
          >
            发送
          </button>
        </div>
      </div>
    </footer>
  </section>
</template>
