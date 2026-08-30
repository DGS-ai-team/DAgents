<script setup>
import { computed, nextTick, ref, watch } from "vue";
import ComposerToolbar from "./ComposerToolbar.vue";
import ContextMeter from "./ContextMeter.vue";
import McpStatusIndicator from "./McpStatusIndicator.vue";
import SkillsStatusIndicator from "./SkillsStatusIndicator.vue";
import TerminalSessionIndicator from "./TerminalSessionIndicator.vue";
import { getThinkingControl, hasThinkingSecondaryControl } from "../utils/llmControls.js";

const props = defineProps({
  agentId: { type: String, default: "" },
  agentTitle: { type: String, default: "Agent" },
  agentCanSend: { type: Boolean, default: false },
  agentInputDisabled: { type: Boolean, default: false },
  agentSending: { type: Boolean, default: false },
  agentCancelling: { type: Boolean, default: false },
  thinkingSupported: { type: Boolean, default: false },
  llmSettings: { type: Object, default: null },
  terminals: { type: Array, default: () => [] },
  activeTerminalId: { type: String, default: "" },
  activeTerminalStatus: { type: String, default: "idle" },
  terminalLoading: { type: Boolean, default: false },
});

const emit = defineEmits([
  "send-agent",
  "cancel-agent",
  "toggle-thinking",
  "cycle-effort",
  "switch-profile",
  "select-terminal",
]);

const text = ref("");
const inputRef = ref(null);
const recipientLabel = computed(() => String(props.agentTitle || "Agent").trim() || "Agent");

const placeholder = computed(() =>
  props.agentSending
    ? `${recipientLabel.value} 正在执行，可先编辑下一条消息…`
    : "输入消息…",
);
const canSubmit = computed(() => Boolean(
  String(text.value || "").trim() &&
  props.agentCanSend &&
  !props.agentInputDisabled &&
  !props.agentCancelling,
));
const thinkingControl = computed(() => getThinkingControl(props.llmSettings));
const thinkingEnabled = computed(() => !["disabled", "off"].includes(String(props.llmSettings?.thinking || "").toLowerCase()));
const thinkingFixed = computed(() => thinkingControl.value === "fixed");
const thinkingLabel = computed(() => String(props.llmSettings?.thinking_label || "思考"));
const thinkingSecondarySupported = computed(
  () => hasThinkingSecondaryControl(props.llmSettings) && props.llmSettings?.reasoning_effort_supported !== false,
);
const thinkingSecondaryLabel = computed(() => String(props.llmSettings?.thinking_secondary_label || (thinkingControl.value === "budget" ? "思考预算" : "推理强度")));
const thinkingEffort = computed(() => String(props.llmSettings?.reasoning_effort || "high").toLowerCase());

function focusInput() {
  nextTick(() => inputRef.value?.focus());
}

function submit() {
  if (!canSubmit.value) return;
  emit("send-agent", { text: text.value.trim() });
  text.value = "";
}

function onKeydown(event) {
  if (event.key !== "Enter" || event.shiftKey) return;
  event.preventDefault();
  submit();
}

watch(() => props.agentSending, focusInput);

defineExpose({ focusInput, submit });
</script>

<template>
  <section class="chat__composer terminal-workbench-composer" aria-label="Agent 输入">
    <div
      class="chat__composer-runtime-rail"
      :class="{ 'chat__composer-runtime-rail--idle': !props.agentSending }"
      role="status"
      aria-live="polite"
      :aria-hidden="!props.agentSending"
    >
      {{ props.agentSending ? "本轮执行中" : "空闲" }}
    </div>

    <div class="chat__composer-pill">
      <div class="chat__composer-pill-left">
        <span class="terminal-workbench-composer__recipient" :title="`消息将发送给 ${recipientLabel}`">
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M3.25 3.75h9.5v6.5h-5l-2.5 1.75v-1.75h-2Z" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" />
          </svg>
          <span>发给 {{ recipientLabel }}</span>
        </span>
      </div>
      <div class="chat__composer-pill-center">
        <textarea
          ref="inputRef"
          v-model="text"
          class="chat__textarea"
          rows="1"
          :placeholder="placeholder"
          aria-label="输入消息"
          :disabled="props.agentInputDisabled || props.agentCancelling"
          @keydown="onKeydown"
        />
      </div>
      <div class="chat__composer-pill-right">
        <ComposerToolbar
          class="chat__composer-toolbar"
          :llm-settings="props.llmSettings"
          :disabled="props.agentInputDisabled || props.agentCancelling"
          @switch-profile="(id) => emit('switch-profile', id)"
        />
        <button
          v-if="props.agentSending"
          type="button"
          class="chat__composer-send chat__composer-send--cancel"
          :disabled="props.agentCancelling"
          :title="props.agentCancelling ? '正在停止本轮…' : '停止本轮'"
          :aria-label="props.agentCancelling ? '正在停止本轮' : '停止本轮'"
          @click="emit('cancel-agent')"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
        <button
          v-else
          type="button"
          class="chat__composer-send"
          :disabled="!canSubmit"
          title="发送"
          aria-label="发送"
          @click="submit"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M8 12.25V3.75M8 3.75L4.5 7.25M8 3.75l3.5 3.5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>
    </div>

    <div class="chat__composer-statusline" aria-label="输入状态与工具栏">
      <div class="chat__composer-statusline-left">
        <McpStatusIndicator :agent-id="props.agentId" />
        <TerminalSessionIndicator
          :terminals="props.terminals"
          :active-terminal-id="props.activeTerminalId"
          :active-terminal-status="props.activeTerminalStatus"
          :loading="props.terminalLoading"
          @terminal-select="(item) => emit('select-terminal', item)"
        />
        <SkillsStatusIndicator :agent-id="props.agentId" />
      </div>
      <div class="chat__composer-statusline-right">
        <div v-if="props.thinkingSupported && thinkingControl" class="chat__statusline-thinking">
          <span v-if="thinkingFixed" class="composer-toolbar__btn composer-toolbar__btn--status" :title="`${thinkingLabel}固定开启`">
            <span class="composer-toolbar__label">{{ thinkingLabel }}（固定）</span>
          </span>
          <button
            v-else
            type="button"
            class="composer-toolbar__btn"
            :class="{ 'composer-toolbar__btn--active': thinkingEnabled }"
            :disabled="props.agentInputDisabled || props.agentCancelling"
            :title="thinkingEnabled ? `${thinkingLabel}已开启，点击关闭` : `${thinkingLabel}已关闭，点击开启`"
            @click="emit('toggle-thinking')"
          >
            <span class="composer-toolbar__label">{{ thinkingEnabled ? thinkingLabel : `${thinkingLabel}关` }}</span>
          </button>
          <button
            v-if="thinkingEnabled && thinkingSecondarySupported"
            type="button"
            class="composer-toolbar__btn composer-toolbar__btn--secondary"
            :disabled="props.agentInputDisabled || props.agentCancelling"
            :title="`${thinkingSecondaryLabel} ${thinkingEffort}，点击切换`"
            @click="emit('cycle-effort')"
          >
            <span class="composer-toolbar__label">{{ thinkingSecondaryLabel }} {{ thinkingEffort }}</span>
          </button>
        </div>
        <ContextMeter />
      </div>
    </div>
  </section>
</template>

<style scoped>
.terminal-workbench-composer :deep(.chat__composer-pill-left) { min-width: 0; }
.terminal-workbench-composer :deep(.chat__textarea) { font-family: inherit; }

.terminal-workbench-composer__recipient {
  display: inline-flex;
  max-width: 180px;
  height: 28px;
  align-items: center;
  gap: 5px;
  padding: 0 8px;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: 7px;
  background: var(--color-surface-subtle, var(--color-surface-hover));
  color: var(--color-text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.terminal-workbench-composer__recipient svg { width: 14px; height: 14px; flex: 0 0 auto; }
.terminal-workbench-composer__recipient span { overflow: hidden; text-overflow: ellipsis; }
</style>
