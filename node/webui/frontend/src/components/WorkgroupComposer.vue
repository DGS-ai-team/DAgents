<script setup>
import BrandActivityIndicator from "./BrandActivityIndicator.vue";

defineProps({
  humanQueueItems: { type: Array, default: () => [] },
  editingQueueId: { type: String, default: "" },
  editQueueDraft: { type: String, default: "" },
  draft: { type: String, default: "" },
  composerRuntimeLabel: { type: String, default: "" },
  mentionOpen: { type: Boolean, default: false },
  mentionCandidates: { type: Array, default: () => [] },
  hitlMode: { type: Boolean, default: false },
  directMember: { type: Object, default: null },
  hitlDraft: { type: String, default: "" },
  canChat: { type: Boolean, default: false },
  isConfiguring: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  hitlBusy: { type: Boolean, default: false },
});

const emit = defineEmits([
  "update:edit-queue-draft",
  "update:mention-open",
  "update:hitl-draft",
  "draft-input",
  "draft-backspace",
  "send",
  "cancel",
  "submit-hitl-answer",
  "pick-mention",
  "clear-direct-mention",
  "begin-edit-queued",
  "cancel-edit-queued",
  "save-queued-edit",
  "send-queued-now",
  "remove-queued",
]);
</script>

<template>
  <footer class="chat__composer">
    <div v-if="humanQueueItems.length" class="chat__queue" aria-label="排队中的消息">
      <div
        v-for="item in humanQueueItems"
        :key="item.queue_id"
        class="chat__queue-item"
      >
        <span class="chat__queue-pos">#{{ item.position }}</span>
        <template v-if="editingQueueId === item.queue_id">
          <input
            :value="editQueueDraft"
            class="chat__queue-edit"
            type="text"
            @input="emit('update:edit-queue-draft', $event.target.value)"
            @keydown.enter.prevent="emit('save-queued-edit', item)"
            @keydown.escape.prevent="emit('cancel-edit-queued')"
          />
          <button type="button" class="chat__queue-btn" @click="emit('save-queued-edit', item)">保存</button>
          <button type="button" class="chat__queue-btn chat__queue-btn--ghost" @click="emit('cancel-edit-queued')">
            取消
          </button>
        </template>
        <template v-else>
          <span class="chat__queue-text" :title="item.text">{{ item.text }}</span>
          <button type="button" class="chat__queue-btn chat__queue-btn--send" @click="emit('send-queued-now', item)">
            立即发送
          </button>
          <button type="button" class="chat__queue-btn" @click="emit('begin-edit-queued', item)">修改</button>
          <button
            type="button"
            class="chat__queue-btn chat__queue-btn--ghost"
            title="取消排队"
            @click="emit('remove-queued', item)"
          >
            ×
          </button>
        </template>
      </div>
    </div>
    <div
      class="chat__composer-runtime-rail"
      :class="{ 'chat__composer-runtime-rail--idle': !composerRuntimeLabel }"
      role="status"
      aria-live="polite"
      :aria-hidden="!composerRuntimeLabel"
    >
      <BrandActivityIndicator
        v-if="composerRuntimeLabel"
        :label="composerRuntimeLabel"
        mode="generating"
        :show-label="false"
        compact
      />
      <span>{{ composerRuntimeLabel || "空闲" }}</span>
    </div>
    <div class="chat__composer-pill" style="position: relative">
      <div
        v-if="mentionOpen && mentionCandidates.length && !hitlMode"
        class="wg-mention-menu"
        role="listbox"
      >
        <button
          v-for="member in mentionCandidates"
          :key="member.member_id"
          type="button"
          class="wg-mention-menu__item"
          @mousedown.prevent="emit('pick-mention', member)"
        >
          <strong>{{ member.display_name }}</strong>
          <span class="muted">{{ member.member_id }}</span>
        </button>
      </div>
      <div class="chat__composer-pill-center wg-composer-field">
        <button
          v-if="directMember && !hitlMode"
          type="button"
          class="wg-composer-at"
          :title="`取消 @${directMember.display_name}`"
          :aria-label="`取消 @${directMember.display_name}`"
          @click="emit('clear-direct-mention')"
        >
          @{{ directMember.display_name }}
        </button>
        <input
          v-if="hitlMode"
          :value="hitlDraft"
          class="chat__textarea"
          type="text"
          placeholder="回答 Supervisor 的问题…"
          :disabled="hitlBusy"
          @input="emit('update:hitl-draft', $event.target.value)"
          @keydown.enter.prevent="emit('submit-hitl-answer')"
        />
        <input
          v-else
          :value="draft"
          class="chat__textarea"
          type="text"
          :placeholder="
            !canChat
              ? isConfiguring
                ? '请先发布工作组后再发言…'
                : '当前状态不可对话…'
              : directMember
                ? '输入直达成员的任务…'
                : '向工作组发言，输入 @ 直达成员…'
          "
          :disabled="!canChat"
          @input="emit('draft-input', $event)"
          @keydown.enter.prevent="emit('send')"
          @keydown.esc="emit('update:mention-open', false)"
          @keydown.backspace="emit('draft-backspace', $event)"
        />
      </div>
      <div class="chat__composer-pill-right">
        <button
          v-if="sending || hitlMode"
          type="button"
          class="chat__composer-send chat__composer-send--cancel"
          :title="cancelling ? '正在取消…' : '取消本轮'"
          :aria-label="cancelling ? '正在取消本轮' : '取消本轮'"
          :disabled="cancelling"
          @click="emit('cancel')"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
        <button
          v-if="hitlMode"
          type="button"
          class="chat__composer-send"
          title="提交回答"
          aria-label="提交回答"
          :disabled="!hitlDraft.trim() || hitlBusy"
          @click="emit('submit-hitl-answer')"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M8 12.25V3.75M8 3.75L4.5 7.25M8 3.75l3.5 3.5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <button
          v-if="!sending && !hitlMode"
          type="button"
          class="chat__composer-send"
          title="发送"
          aria-label="发送"
          :disabled="!draft.trim() || !canChat"
          @click="emit('send')"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M8 12.25V3.75M8 3.75L4.5 7.25M8 3.75l3.5 3.5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>
    </div>
    <div class="chat__composer-statusline">
      <div class="chat__composer-statusline-left">
        <span class="chat__input-strip-left">{{
          hitlMode
            ? hitlBusy
              ? "提交回答中…"
              : "回答询问 · Enter 提交"
            : directMember
              ? "直达成员 · Enter 发送 · 点击 @ 取消"
              : "Enter 发送 · @ 直达成员"
        }}</span>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.wg-composer-field {
  flex-direction: row;
  align-items: center;
  gap: 6px;
}
.wg-composer-at {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  padding: 2px 8px;
  border: 1px solid color-mix(in srgb, var(--color-primary, #0078d4) 22%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--color-primary, #0078d4) 10%, var(--color-surface, #fff));
  color: var(--color-primary, #0078d4);
  font: inherit;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  cursor: pointer;
}
.wg-composer-at:hover {
  filter: brightness(0.97);
}
.wg-composer-field .chat__textarea {
  flex: 1 1 auto;
  min-width: 0;
}
.wg-mention-menu {
  position: absolute;
  left: 12px;
  right: 48px;
  bottom: calc(100% + 6px);
  z-index: 20;
  max-height: 220px;
  overflow: auto;
  border: 1px solid var(--color-border, #d1d5db);
  border-radius: 10px;
  background: var(--color-surface, #fff);
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.12);
  padding: 4px;
}
.wg-mention-menu__item {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  border: 0;
  background: transparent;
  text-align: left;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
  font: inherit;
}
.wg-mention-menu__item:hover {
  background: color-mix(in srgb, var(--color-primary, #0078d4) 10%, transparent);
}
.wg-mention-menu__item .muted {
  font-size: 11px;
  color: var(--color-text-muted, #6b7280);
}
.chat__queue {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0 0 10px;
}
.chat__queue-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px dashed var(--color-border, rgba(0, 0, 0, 0.14));
  border-radius: 10px;
  background: var(--color-surface, #fff);
  font-size: 13px;
}
.chat__queue-pos {
  flex: 0 0 auto;
  color: var(--color-text-muted, #6b7280);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.chat__queue-text {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chat__queue-edit {
  flex: 1 1 auto;
  min-width: 0;
  padding: 4px 8px;
  border: 1px solid var(--color-border, #d1d5db);
  border-radius: 6px;
  font: inherit;
}
.chat__queue-btn {
  flex: 0 0 auto;
  padding: 2px 4px;
  border: 0;
  background: transparent;
  color: var(--color-primary, #0078d4);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.chat__queue-btn--ghost {
  color: var(--color-text-muted, #6b7280);
}
.chat__queue-btn--send {
  font-weight: 600;
}
.chat__composer-pill {
  padding-left: 16px;
}
.chat__textarea {
  padding-left: 6px;
  padding-right: 8px;
}
</style>
