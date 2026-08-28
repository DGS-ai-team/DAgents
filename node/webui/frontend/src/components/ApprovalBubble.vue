<script setup>
import { computed } from "vue";
import {
  approvalItemHint,
  approvalItemHintVisible,
  approvalItemPurpose,
  approvalItemToolLabel,
  formatApprovalRawArguments,
} from "../utils/format.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { buildToolCardModel } from "../utils/toolResultPresentation.js";

const props = defineProps({
  data: { type: Object, required: true },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits(["approve-one", "reject-one", "approve-all", "reject-all"]);

const items = computed(() => extractToolApprovals(props.data));
const itemCards = computed(() =>
  items.value.map((item) => {
    const card = buildToolCardModel({
      callEntry: {
        data: {
          tool_name: item.name,
          arguments: item.arguments,
          raw_arguments: item.rawArgs,
        },
      },
    });
    return {
      ...item,
      toolLabel: approvalItemToolLabel(item),
      purpose: approvalItemPurpose(item),
      keyFields: card.inputFields,
      rawJson: formatApprovalRawArguments(item.rawArgs, item.arguments),
      hint: approvalItemHintVisible(item) ? approvalItemHint(item) : "",
    };
  }),
);
const visual = computed(() => resolveToolVisual({ data: props.data }));
const batchMessage = computed(() => {
  const message = String(props.data?.message || "").trim();
  // This transport fallback only repeats the badge and action state.
  if (/^检测到工具调用[，,]\s*等待用户确认后继续执行[。.]?$/.test(message)) return "";
  return message;
});
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
          <li v-for="it in itemCards" :key="it.callId" class="approval-tool-item">
            <header class="approval-tool-item__head">
              <div class="approval-bubble__title">
                <span class="approval-bubble__name">{{ it.toolLabel }}</span>
                <span
                  v-if="multi && (it.risk === 'high' || it.risk === 'medium')"
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
            <div v-if="it.purpose" class="approval-tool-item__purpose">
              <span class="approval-tool-item__purpose-label">执行目的</span>
              <span class="approval-tool-item__purpose-value">{{ it.purpose }}</span>
            </div>

            <dl v-if="it.keyFields.length" class="approval-tool-item__fields">
              <template v-for="field in it.keyFields" :key="`${it.callId}-${field.label}`">
                <div v-if="field.kind === 'code'" class="approval-tool-item__code-field">
                  <dt>{{ field.label }}</dt>
                  <dd><pre>{{ field.value || '—' }}</pre></dd>
                </div>
                <div v-else class="approval-tool-item__field">
                  <dt>{{ field.label }}</dt>
                  <dd>
                    <pre v-if="field.kind === 'multiline'">{{ field.value }}</pre>
                    <span v-else>{{ field.value }}</span>
                  </dd>
                </div>
              </template>
            </dl>
            <p
              v-if="it.duplicateWindowSec > 0"
              class="approval-tool-item__dup"
            >
              重复调用
              <template v-if="it.duplicateWindowSec"> · {{ it.duplicateWindowSec }}s 内</template>
            </p>
            <p v-if="it.hint && !it.keyFields.length" class="approval-tool-item__hint">
              {{ it.hint }}
            </p>
            <details v-if="it.duplicatePreview" class="approval-tool-item__raw">
              <summary>上次结果摘要</summary>
              <pre class="approval-tool-item__raw-content">{{ it.duplicatePreview }}</pre>
            </details>
            <details v-if="it.rawJson" class="approval-tool-item__raw">
              <summary>展开原始 JSON</summary>
              <pre class="approval-tool-item__raw-content">{{ it.rawJson }}</pre>
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

.approval-tool-item__fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  margin: 0;
  min-width: 0;
}

.approval-tool-item__field,
.approval-tool-item__code-field {
  min-width: 0;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  font-size: 12px;
  line-height: 1.45;
}

.approval-tool-item__field {
  display: grid;
  grid-template-columns: minmax(48px, 78px) minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  padding: 7px 8px;
}

.approval-tool-item__field dt,
.approval-tool-item__code-field dt {
  color: var(--color-text-subtle, #6b7280);
  font-weight: 500;
}

.approval-tool-item__field dd,
.approval-tool-item__code-field dd {
  min-width: 0;
  margin: 0;
  color: var(--color-text, inherit);
  overflow-wrap: anywhere;
}

.approval-tool-item__field pre,
.approval-tool-item__code-field pre {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.approval-tool-item__code-field {
  grid-column: 1 / -1;
  overflow: hidden;
}

.approval-tool-item__code-field dt {
  padding: 5px 8px;
  border-bottom: 1px solid var(--color-border);
  background: var(--color-surface-muted);
  font-size: 11px;
}

.approval-tool-item__code-field dd {
  padding: 8px 10px;
  background: var(--color-code-bg, #f8fafc);
  color: var(--color-text, #1f2937);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Consolas, monospace);
  font-size: 11.5px;
  line-height: 1.5;
}

.approval-tool-item__purpose {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 7px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  font-size: 12px;
  line-height: 1.45;
}

.approval-tool-item__purpose-label {
  color: var(--color-text-subtle);
  font-size: 11px;
  font-weight: 600;
  flex: 0 0 auto;
}

.approval-tool-item__purpose-value {
  min-width: 0;
  color: var(--color-text);
  overflow-wrap: anywhere;
  word-break: break-word;
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

.approval-tool-item__raw-content {
  margin: 0.25rem 0 0;
  max-height: 180px;
  overflow: auto;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-code-bg, #f8fafc);
  color: var(--color-text, #1f2937);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Consolas, monospace);
  font-size: 11px;
  line-height: 1.5;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

@media (max-width: 640px) {
  .approval-tool-item__fields {
    grid-template-columns: minmax(0, 1fr);
  }

  .approval-tool-item__code-field {
    grid-column: auto;
  }
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
