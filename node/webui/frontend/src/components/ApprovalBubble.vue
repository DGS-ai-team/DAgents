<script setup>
import { computed } from "vue";
import { approvalItemDisplayName } from "../utils/format.js";
import { extractToolApprovals } from "../stores/hitl.js";
import { resolveToolVisual } from "../utils/toolSource.js";

const props = defineProps({
  data: { type: Object, required: true },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits(["approve-one", "reject-one", "approve-all", "reject-all"]);

const items = () => extractToolApprovals(props.data);
const visual = computed(() => resolveToolVisual({ data: props.data }));
</script>

<template>
  <div class="msg msg--approval">
    <div class="msg__body msg__body--wide">
      <div class="approval-bubble">
        <div class="tool-exec-bubble__source">
          <span class="tool-source-badge" :class="`tool-source-badge--${visual.kind}`" :title="visual.label">
            <span class="tool-source-badge__icon" aria-hidden="true">{{ visual.icon }}</span>
            <span class="tool-source-badge__text">待审批</span>
          </span>
        </div>
        <ul class="approval-tool-list">
          <li v-for="it in items()" :key="it.callId" class="approval-tool-item">
            <header class="approval-tool-item__head">
              <div class="approval-bubble__title">
                <span class="approval-bubble__name">{{ approvalItemDisplayName(it) }}</span>
              </div>
              <div class="approval-tool-item__right">
                <span class="tool-status-chip tool-status-chip--pending">待审批</span>
                <div class="approval-tool-item__inline-actions">
                  <button type="button" class="approval-action-btn approval-action-btn--reject" :disabled="busy" @click="emit('reject-one', it.callId)">拒绝</button>
                  <button type="button" class="approval-action-btn approval-action-btn--approve" :disabled="busy" @click="emit('approve-one', it.callId)">{{ busy ? "处理中…" : "批准" }}</button>
                </div>
              </div>
            </header>
            <div v-if="it.reason" class="approval-tool-item__policy">
              <div class="approval-tool-item__reason">{{ it.reason }}</div>
            </div>
            <pre class="tool-card__args tool-card__args--compact">{{ it.rawArgs }}</pre>
          </li>
        </ul>
        <div v-if="items().length > 1" class="approval-bubble__bulk-actions approval-bubble__bulk-actions--footer">
          <span class="approval-bubble__bulk-text">{{ items().length }} 个工具调用待处理</span>
          <div class="approval-tool-item__inline-actions">
            <button type="button" class="approval-action-btn approval-action-btn--reject" :disabled="busy" @click="emit('reject-all')">全部拒绝</button>
            <button type="button" class="approval-action-btn approval-action-btn--approve" :disabled="busy" @click="emit('approve-all')">{{ busy ? "处理中…" : "全部批准" }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
