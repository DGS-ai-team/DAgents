<script setup>
import { computed, ref, useId, watch } from "vue";
import { extractToolApprovals } from "../stores/hitl.js";
import { resolveToolVisual } from "../utils/toolSource.js";
import { buildToolCardModel } from "../utils/toolResultPresentation.js";
import { approvalArguments, approvalRawJson, approvalSummary } from "../utils/approvalTools.js";
import ToolGroupIcon from "./ToolGroupIcon.vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  data: { type: Object, required: true },
  busy: { type: Boolean, default: false },
  requestKey: { type: String, default: "" },
});

const emit = defineEmits(["approve-selection", "reject-all"]);
const selectedIds = ref(new Set());
const expandedIds = ref(new Set());
const headingId = useId();

const requestIdentity = computed(() => {
  const explicit = String(props.requestKey || props.data?.approval_id || props.data?.hitl_id || props.data?.request_id || "").trim();
  if (explicit) return explicit;
  return extractToolApprovals(props.data).map((item) => item.callId).sort().join(",");
});

watch(requestIdentity, () => {
  selectedIds.value = new Set();
  expandedIds.value = new Set();
});

const items = computed(() => extractToolApprovals(props.data).map((item) => {
  const args = approvalArguments(item);
  const card = buildToolCardModel({
    callEntry: {
      data: {
        tool_name: item.name,
        arguments: args,
        raw_arguments: item.rawArgs,
      },
    },
  });
  return {
    ...item,
    summary: approvalSummary(item),
    keyFields: card.inputFields,
    rawJson: approvalRawJson(item),
    visual: resolveToolVisual({ data: { tool_name: item.name } }),
    risk: ["high", "medium", "low"].includes(String(item.risk || "").toLowerCase())
      ? String(item.risk).toLowerCase()
      : "",
  };
}));
const multi = computed(() => items.value.length > 1);
const selectedCount = computed(() => items.value.filter((item) => selectedIds.value.has(item.callId)).length);

function isExpanded(callId) {
  return expandedIds.value.has(callId);
}

function setExpanded(callId) {
  const next = new Set(expandedIds.value);
  if (next.has(callId)) next.delete(callId);
  else next.add(callId);
  expandedIds.value = next;
}

function setSelected(callId, checked) {
  const next = new Set(selectedIds.value);
  if (checked) next.add(callId);
  else next.delete(callId);
  selectedIds.value = next;
}

function approveSelected() {
  if (props.busy) return;
  const approvedCallIds = multi.value
    ? items.value.filter((item) => selectedIds.value.has(item.callId)).map((item) => item.callId)
    : items.value.map((item) => item.callId);
  if (!approvedCallIds.length) return;
  emit("approve-selection", approvedCallIds);
}

function rejectAll() {
  if (props.busy) return;
  emit("reject-all");
}

function riskText(risk) {
  if (risk === "high") return "高风险";
  if (risk === "medium") return "中风险";
  if (risk === "low") return "低风险";
  return "";
}
</script>

<template>
  <section class="approval-bubble" :aria-labelledby="headingId">
    <header class="approval-bubble__header">
      <h3 :id="headingId">需要确认</h3>
      <span v-if="items.length === 1 && items[0].risk" class="approval-risk" :class="`approval-risk--${items[0].risk}`">
        {{ riskText(items[0].risk) }}
      </span>
    </header>

    <ul class="approval-list">
      <li v-for="item in items" :key="item.callId" class="approval-item">
        <div class="approval-item__summary-row">
          <button
            type="button"
            class="approval-item__toggle"
            :aria-label="`${isExpanded(item.callId) ? '收起' : '展开'} ${item.summary} 的参数`"
            :aria-expanded="isExpanded(item.callId)"
            :aria-controls="`approval-detail-${headingId}-${item.callId}`"
            @click="setExpanded(item.callId)"
          >
            <span class="approval-item__icon" :title="item.visual.label" aria-hidden="true">
              <ToolGroupIcon :name="item.visual.kind" />
            </span>
            <span class="approval-item__summary" :title="item.summary">{{ item.summary }}</span>
            <span v-if="multi && item.risk" class="approval-risk approval-risk--inline" :class="`approval-risk--${item.risk}`">
              {{ riskText(item.risk) }}
            </span>
            <span class="approval-item__pending">待确认</span>
            <UiIcon :name="isExpanded(item.callId) ? 'chevron-down' : 'chevron-right'" :size="15" />
          </button>
          <input
            v-if="multi"
            class="approval-item__checkbox"
            type="checkbox"
            :aria-label="`批准 ${item.summary}`"
            :checked="selectedIds.has(item.callId)"
            :disabled="busy"
            @change="setSelected(item.callId, $event.target.checked)"
          />
        </div>

        <div v-if="isExpanded(item.callId)" :id="`approval-detail-${headingId}-${item.callId}`" class="approval-item__details">
          <p v-if="item.reason" class="approval-item__reason"><strong>审批原因</strong>{{ item.reason }}</p>
          <p v-if="item.duplicateWindowSec > 0" class="approval-item__duplicate">
            重复调用{{ item.duplicateWindowSec ? ` · ${item.duplicateWindowSec}s 内` : "" }}
          </p>
          <p v-if="item.duplicatePreview" class="approval-item__duplicate-preview">上次结果：{{ item.duplicatePreview }}</p>
          <dl v-if="item.keyFields.length" class="approval-item__fields">
            <template v-for="field in item.keyFields" :key="`${item.callId}-${field.label}`">
              <div class="approval-item__field">
                <dt>{{ field.label }}</dt>
                <dd><pre>{{ field.value || "—" }}</pre></dd>
              </div>
            </template>
          </dl>
          <details v-if="item.rawJson" class="approval-item__raw">
            <summary>查看原始 JSON</summary>
            <pre>{{ item.rawJson }}</pre>
          </details>
        </div>
      </li>
    </ul>

    <footer class="approval-actions" :class="{ 'approval-actions--multi': multi }">
      <p v-if="multi" class="approval-actions__note">未选中的操作本次不执行</p>
      <button type="button" class="approval-actions__reject" :disabled="busy" @click="rejectAll">
        {{ multi ? "拒绝本批" : "拒绝" }}
      </button>
      <button
        type="button"
        class="approval-actions__approve"
        :disabled="busy || (multi && selectedCount === 0)"
        @click="approveSelected"
      >
        {{ busy ? "处理中…" : multi ? `批准所选 ${selectedCount} 项` : "允许本次" }}
      </button>
    </footer>
  </section>
</template>

<style scoped>
.approval-bubble {
  flex: 0 0 auto;
  align-self: stretch;
  min-width: 0;
  min-height: min-content;
  width: 100%;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md, 12px);
  background: var(--color-surface);
  color: var(--color-text, inherit);
  overflow: hidden;
}
.approval-bubble__header,
.approval-item__summary-row,
.approval-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.approval-bubble__header { padding: 10px 12px 6px; }
.approval-bubble__header h3 { margin: 0; font-size: 0.9rem; font-weight: 650; }
.approval-list { list-style: none; margin: 0; padding: 0 8px; }
.approval-item { min-width: 0; border-top: 1px solid var(--color-border); }
.approval-item__summary-row { min-height: 42px; padding: 5px 3px; }
.approval-item__icon { display: inline-flex; flex: 0 0 24px; width: 24px; height: 24px; align-items: center; justify-content: center; color: var(--color-text-muted); }
.approval-item__summary { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.88rem; }
.approval-item__pending { flex: 0 0 auto; color: var(--color-text-muted); font-size: 0.76rem; }
.approval-item__toggle { display: inline-flex; flex: 1 1 auto; min-width: 0; align-items: center; gap: 8px; padding: 6px 3px; border: 0; border-radius: 6px; color: inherit; background: transparent; text-align: left; cursor: pointer; }
.approval-item__toggle:hover { background: var(--color-surface-muted); }
.approval-item__toggle:focus-visible,
.approval-item__raw summary:focus-visible,
.approval-actions button:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
.approval-item__checkbox { flex: 0 0 auto; width: 17px; height: 17px; margin: 0 3px 0 4px; accent-color: var(--color-primary); }
.approval-risk { flex: 0 0 auto; padding: 2px 7px; border: 1px solid transparent; border-radius: 999px; font-size: 0.69rem; line-height: 1.3; }
.approval-risk--inline { padding: 1px 6px; }
.approval-risk--high { color: var(--color-danger); background: var(--color-danger-soft); border-color: var(--color-danger); }
.approval-risk--medium { color: var(--color-warning); background: var(--color-warning-soft); border-color: var(--color-warning); }
.approval-risk--low { color: var(--text-muted, #6b7280); background: var(--color-surface-muted); border-color: var(--color-border); }
.approval-item__details { padding: 0 8px 10px; min-width: 0; }
.approval-item__reason,
.approval-item__duplicate,
.approval-item__duplicate-preview { margin: 0 0 7px; font-size: 0.8rem; line-height: 1.45; overflow-wrap: anywhere; white-space: pre-wrap; }
.approval-item__reason strong { margin-right: 6px; }
.approval-item__duplicate { color: var(--color-text-muted); }
.approval-item__fields { display: grid; grid-template-columns: minmax(0, 1fr); gap: 6px; margin: 0; }
.approval-item__field { display: flex; flex-direction: column; gap: 4px; min-width: 0; padding: 7px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); background: var(--color-surface-muted); font-size: 0.75rem; }
.approval-item__field dt { color: var(--text-muted, #6b7280); }
.approval-item__field dd { min-width: 0; margin: 0; overflow-wrap: anywhere; }
.approval-item__field pre { max-height: 240px; margin: 0; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; }
.approval-item__raw { margin-top: 8px; font-size: 0.78rem; }
.approval-item__raw summary { width: fit-content; cursor: pointer; color: var(--text-muted, #6b7280); }
.approval-item__raw pre { max-height: 260px; margin: 6px 0 0; overflow: auto; padding: 8px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); background: var(--color-code-bg, #f8fafc); white-space: pre-wrap; overflow-wrap: anywhere; font: 0.72rem/1.5 var(--font-mono, ui-monospace, monospace); }
.approval-actions { justify-content: flex-end; flex-wrap: wrap; padding: 10px 12px 12px; }
.approval-actions--multi { border-top: 1px solid var(--color-border); }
.approval-actions__note { flex: 1 1 100%; margin: 0; color: var(--text-muted, #6b7280); font-size: 0.76rem; }
.approval-actions button { min-height: 34px; padding: 6px 12px; border: 1px solid var(--color-border); border-radius: 8px; background: var(--color-surface); color: var(--color-text); cursor: pointer; }
.approval-actions button:disabled { opacity: 0.5; cursor: not-allowed; }
.approval-actions__approve { color: var(--color-primary-strong); background: var(--color-primary-soft); border-color: var(--color-primary); }
@media (max-width: 520px) {
  .approval-actions { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
  .approval-actions__note { grid-column: 1 / -1; }
}
</style>
