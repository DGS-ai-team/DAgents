<script setup>
import { computed } from "vue";
import { approvalItemDisplayName, approvalItemHint, approvalItemHintVisible } from "../utils/format.js";

const props = defineProps({
  approval: { type: Object, required: true },
  hitlBusy: { type: Boolean, default: false },
  inline: { type: Boolean, default: false },
});

const emit = defineEmits(["resolve"]);

const items = computed(() => (Array.isArray(props.approval?.items) ? props.approval.items : []));
const itemCount = computed(() => {
  const allItems = props.approval?.allItems;
  return Array.isArray(allItems) && allItems.length ? allItems.length : items.value.length;
});
const isBatch = computed(() => itemCount.value > 1);

function resolve(callId, approve) {
  emit("resolve", callId, approve);
}

function rejectLabel() {
  return isBatch.value ? "拒绝本批" : "拒绝";
}

function approveLabel() {
  if (props.hitlBusy) return "处理中…";
  return isBatch.value ? "仅批准此项" : "批准";
}
</script>

<template>
  <div class="wg-task__approval" :class="{ 'wg-task__approval--inline': inline }">
    <div class="wg-task__approval-head">
      <span class="wg-task__approval-badge">需要批准</span>
      <span class="wg-task__approval-count">
        {{ isBatch ? `批量审批 · ${itemCount} 个工具调用` : "单项审批" }}
      </span>
    </div>
    <div
      v-for="approvalItem in items"
      :key="approvalItem.callId"
      class="wg-task__approval-item"
    >
      <div class="wg-task__approval-item-head">
        <span class="wg-task__approval-name">{{ approvalItemDisplayName(approvalItem) }}</span>
        <span
          v-if="approvalItem.risk === 'high' || approvalItem.risk === 'medium'"
          class="wg-task__approval-risk"
        >
          {{ approvalItem.risk === "high" ? "高风险" : "中风险" }}
        </span>
      </div>
      <div v-if="approvalItem.duplicateWindowSec > 0" class="wg-task__approval-hint">
        重复调用 · {{ approvalItem.duplicateWindowSec }}s 内
      </div>
      <details v-if="approvalItem.duplicatePreview" class="wg-task__approval-raw">
        <summary>上次结果摘要</summary>
        <pre>{{ approvalItem.duplicatePreview }}</pre>
      </details>
      <div v-if="approvalItemHintVisible(approvalItem)" class="wg-task__approval-hint">
        {{ approvalItemHint(approvalItem) }}
      </div>
      <details v-if="approvalItem.rawArgs" class="wg-task__approval-raw">
        <summary>参数详情</summary>
        <pre>{{ approvalItem.rawArgs }}</pre>
      </details>
      <div class="wg-task__approval-actions">
        <button
          type="button"
          class="approval-action-btn approval-action-btn--reject"
          :disabled="hitlBusy"
          @click.stop="resolve(approvalItem.callId, false)"
        >
          {{ rejectLabel() }}
        </button>
        <button
          type="button"
          class="approval-action-btn approval-action-btn--approve"
          :disabled="hitlBusy"
          @click.stop="resolve(approvalItem.callId, true)"
        >
          {{ approveLabel() }}
        </button>
      </div>
    </div>
    <div v-if="isBatch" class="wg-task__approval-bulk">
      <span>{{ itemCount }} 个工具调用待处理</span>
      <div class="wg-task__approval-actions">
        <button
          type="button"
          class="approval-action-btn approval-action-btn--reject"
          :disabled="hitlBusy"
          @click.stop="resolve('', false)"
        >
          全部拒绝
        </button>
        <button
          type="button"
          class="approval-action-btn approval-action-btn--approve"
          :disabled="hitlBusy"
          @click.stop="resolve('', true)"
        >
          全部批准
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wg-task__approval {
  margin: 4px 0 2px;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--color-primary, #0078d4) 28%, var(--color-border, #d1d5db));
  border-left: 3px solid var(--color-primary, #0078d4);
  border-radius: 8px;
  background: color-mix(in srgb, var(--color-primary, #0078d4) 5%, var(--color-surface, #fff));
}
.wg-task__approval--inline {
  margin: 6px 8px 8px;
}
.wg-task__approval-head,
.wg-task__approval-item-head,
.wg-task__approval-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.wg-task__approval-head {
  justify-content: space-between;
  margin-bottom: 6px;
}
.wg-task__approval-badge {
  color: var(--color-primary, #0078d4);
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.04em;
}
.wg-task__approval-count {
  color: var(--color-text-muted, #6b7280);
  font-size: 11px;
}
.wg-task__approval-item + .wg-task__approval-item {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid color-mix(in srgb, var(--color-border, #d1d5db) 80%, transparent);
}
.wg-task__approval-item-head {
  min-width: 0;
}
.wg-task__approval-name {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text, #111827);
  font-size: 12px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wg-task__approval-risk {
  flex: 0 0 auto;
  color: #b45309;
  font-size: 10px;
  font-weight: 600;
}
.wg-task__approval-hint {
  margin-top: 4px;
  color: var(--color-text-subtle, #9ca3af);
  font-size: 11.5px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}
.wg-task__approval-actions {
  justify-content: flex-end;
  margin-top: 7px;
}
.wg-task__approval-bulk {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid color-mix(in srgb, var(--color-border, #d1d5db) 80%, transparent);
  color: var(--color-text-muted, #6b7280);
  font-size: 11px;
}
.wg-task__approval-bulk .wg-task__approval-actions {
  margin-top: 0;
}
.wg-task__approval-raw {
  margin-top: 5px;
  color: var(--color-text-muted, #6b7280);
  font-size: 11px;
}
.wg-task__approval-raw summary {
  cursor: pointer;
}
.wg-task__approval-raw pre {
  max-height: 160px;
  margin: 5px 0 0;
  overflow: auto;
  padding: 6px;
  border-radius: 5px;
  background: color-mix(in srgb, var(--color-text, #111827) 4%, var(--color-surface, #fff));
  font: 11px/1.45 ui-monospace, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
