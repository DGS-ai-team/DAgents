<script setup>
import { computed } from "vue";
import { approvalItemDisplayName, approvalItemHint, approvalItemHintVisible } from "../utils/format.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { resolveToolVisual } from "../utils/toolSource.js";

const props = defineProps({
  data: { type: Object, required: true },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits(["approve-one", "reject-one", "approve-all", "reject-all"]);

const items = computed(() => extractToolApprovals(props.data));
const visual = computed(() => resolveToolVisual({ data: props.data }));
const batchMessage = computed(() => String(props.data?.message || "").trim());
const childPurpose = computed(() => String(props.data?.child_purpose || "").trim());
const multi = computed(() => items.value.length > 1);
const maxRisk = computed(() => {
  const order = { high: 3, medium: 2, low: 1 };
  let best = "";
  let score = 0;
  for (const it of items.value) {
    const r = String(it.risk || "").toLowerCase();
    const s = order[r] || 0;
    if (s > score) {
      score = s;
      best = r;
    }
  }
  return best;
});
const riskLabel = computed(() => {
  if (maxRisk.value === "high") return "高风险";
  if (maxRisk.value === "medium") return "中风险";
  return "";
});

function riskClass(risk) {
  const r = String(risk || "").toLowerCase();
  if (r === "high") return "approval-risk--high";
  if (r === "medium") return "approval-risk--medium";
  return "";
}

function rejectOneLabel() {
  return multi.value ? "拒绝本批" : "拒绝";
}

function approveOneLabel() {
  if (props.busy) return "处理中…";
  return multi.value ? "仅批准此项" : "批准";
}
</script>

<template>
  <div class="msg msg--approval">
    <div class="msg__body msg__body--wide">
      <div
        class="approval-bubble approval-bubble--needs-action"
        :class="{
          'approval-bubble--risk-high': maxRisk === 'high',
          'approval-bubble--risk-medium': maxRisk === 'medium',
        }"
      >
        <div class="approval-bubble__header">
          <div class="tool-exec-bubble__source">
            <span
              class="tool-source-badge"
              :class="`tool-source-badge--${visual.kind}`"
              :title="visual.label"
            >
              <span class="tool-source-badge__icon" aria-hidden="true">{{ visual.icon }}</span>
              <span class="tool-source-badge__text">需要批准</span>
            </span>
          </div>
          <span v-if="riskLabel" class="approval-risk" :class="riskClass(maxRisk)">{{ riskLabel }}</span>
        </div>

        <p v-if="childPurpose" class="approval-bubble__context">子任务：{{ childPurpose }}</p>
        <p v-else-if="batchMessage" class="approval-bubble__context">{{ batchMessage }}</p>

        <ul class="approval-tool-list">
          <li v-for="it in items" :key="it.callId" class="approval-tool-item">
            <header class="approval-tool-item__head">
              <div class="approval-bubble__title">
                <span class="approval-bubble__name">{{ approvalItemDisplayName(it) }}</span>
                <span
                  v-if="it.risk === 'high' || it.risk === 'medium'"
                  class="approval-risk approval-risk--inline"
                  :class="riskClass(it.risk)"
                >{{ it.risk === 'high' ? '高风险' : '中风险' }}</span>
              </div>
              <div class="approval-tool-item__inline-actions">
                <button
                  type="button"
                  class="approval-action-btn approval-action-btn--reject"
                  :disabled="busy"
                  :title="multi ? '拒绝本批全部工具调用（不会只拒这一项）' : '拒绝执行'"
                  @click="emit('reject-one', it.callId)"
                >
                  {{ rejectOneLabel() }}
                </button>
                <button
                  type="button"
                  class="approval-action-btn approval-action-btn--approve"
                  :disabled="busy"
                  :title="multi ? '只批准这一项，其余同批调用将被拒绝' : '批准执行'"
                  @click="emit('approve-one', it.callId)"
                >
                  {{ approveOneLabel() }}
                </button>
              </div>
            </header>
            <div v-if="it.reason" class="approval-tool-item__policy">
              <div class="approval-tool-item__reason">{{ it.reason }}</div>
            </div>
            <p
              v-if="it.duplicateWindowSec > 0"
              class="approval-tool-item__dup"
            >
              重复调用
              <template v-if="it.duplicateWindowSec"> · {{ it.duplicateWindowSec }}s 内</template>
            </p>
            <p v-if="approvalItemHintVisible(it)" class="approval-tool-item__hint">
              {{ approvalItemHint(it) }}
            </p>
            <details v-if="it.duplicatePreview" class="approval-tool-item__raw">
              <summary>上次结果摘要</summary>
              <pre class="tool-card__args tool-card__args--compact">{{ it.duplicatePreview }}</pre>
            </details>
            <details v-if="it.rawArgs" class="approval-tool-item__raw">
              <summary>参数详情</summary>
              <pre class="tool-card__args tool-card__args--compact">{{ it.rawArgs }}</pre>
            </details>
          </li>
        </ul>
        <div v-if="multi" class="approval-bubble__bulk-actions approval-bubble__bulk-actions--footer">
          <span class="approval-bubble__bulk-text">{{ items.length }} 个工具调用待处理</span>
          <div class="approval-tool-item__inline-actions">
            <button
              type="button"
              class="approval-action-btn approval-action-btn--reject"
              :disabled="busy"
              @click="emit('reject-all')"
            >
              全部拒绝
            </button>
            <button
              type="button"
              class="approval-action-btn approval-action-btn--approve"
              :disabled="busy"
              @click="emit('approve-all')"
            >
              {{ busy ? "处理中…" : "全部批准" }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.approval-bubble__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.approval-bubble__context {
  margin: 0;
  font-size: 0.85rem;
  line-height: 1.45;
  color: var(--text-muted, #6b7280);
}

.approval-bubble__title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.approval-risk {
  flex: 0 0 auto;
  font-size: 0.72rem;
  font-weight: 600;
  padding: 2px 7px;
  border-radius: 999px;
  border: 1px solid transparent;
}

.approval-risk--inline {
  font-size: 0.7rem;
}

.approval-risk--high {
  color: #b91c1c;
  background: color-mix(in srgb, #dc2626 12%, transparent);
  border-color: color-mix(in srgb, #dc2626 28%, transparent);
}

.approval-risk--medium {
  color: #b45309;
  background: color-mix(in srgb, #f59e0b 14%, transparent);
  border-color: color-mix(in srgb, #f59e0b 30%, transparent);
}

.approval-tool-item__hint,
.approval-tool-item__dup {
  margin: 0.35rem 0 0;
  font-size: 0.85rem;
  line-height: 1.45;
  color: var(--text-primary, inherit);
  white-space: pre-wrap;
  word-break: break-word;
}

.approval-tool-item__dup {
  color: var(--text-muted, #6b7280);
  font-size: 0.8rem;
}

.approval-tool-item__raw {
  margin-top: 0.35rem;
  font-size: 0.78rem;
  color: var(--text-muted, #6b7280);
}

.approval-tool-item__raw summary {
  cursor: pointer;
  user-select: none;
}

.approval-tool-item__raw pre {
  margin-top: 0.25rem;
}

@media (max-width: 640px) {
  .approval-tool-item__head {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }

  .approval-tool-item__inline-actions {
    display: flex;
    width: 100%;
  }

  .approval-tool-item__inline-actions .approval-action-btn {
    flex: 1 1 0;
  }
}
</style>
