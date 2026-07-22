<script setup>
import { computed, ref } from "vue";
import { extractMemoryConflict } from "../stores/hitl.js";

const props = defineProps({
  data: { type: Object, required: true },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits(["decide", "cancel"]);

const selected = ref("keep_both");
const meta = computed(() => extractMemoryConflict(props.data));
</script>

<template>
  <div class="msg msg--approval">
    <div class="msg__body msg__body--wide">
      <div class="approval-bubble approval-bubble--memory">
        <div class="tool-exec-bubble__source">
          <span class="tool-source-badge tool-source-badge--memory" title="长期记忆冲突">
            <span class="tool-source-badge__icon" aria-hidden="true">M</span>
            <span class="tool-source-badge__text">记忆冲突</span>
          </span>
        </div>
        <p class="approval-bubble__intro">{{ meta.question }}</p>
        <div class="memory-conflict-panels">
          <section class="memory-conflict-panel">
            <h4 class="memory-conflict-panel__title">原有记忆</h4>
            <pre class="memory-conflict-panel__body">{{ meta.existing || "（空）" }}</pre>
          </section>
          <section class="memory-conflict-panel">
            <h4 class="memory-conflict-panel__title">新信息</h4>
            <pre class="memory-conflict-panel__body">{{ meta.newInformation || "（空）" }}</pre>
          </section>
        </div>
        <div class="approval-bubble__actions memory-conflict-actions">
          <label class="memory-conflict-option">
            <input v-model="selected" type="radio" value="keep_old" :disabled="busy" />
            <span>保留原有记忆</span>
          </label>
          <label class="memory-conflict-option">
            <input v-model="selected" type="radio" value="use_new" :disabled="busy" />
            <span>使用新记忆替换</span>
          </label>
          <label class="memory-conflict-option">
            <input v-model="selected" type="radio" value="keep_both" :disabled="busy" />
            <span>全部保留（合并）</span>
          </label>
          <div class="memory-conflict-buttons">
            <button type="button" class="btn btn--ghost btn--sm" :disabled="busy" @click="emit('cancel')">取消</button>
            <button type="button" class="btn btn--primary btn--sm" :disabled="busy" @click="emit('decide', selected)">
              {{ busy ? "提交中…" : "确认" }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.approval-bubble--memory {
  border-left: 3px solid #7c3aed;
  background: linear-gradient(90deg, rgba(124, 58, 237, 0.08), #f8fafc 48px);
}

.tool-source-badge--memory {
  color: #7c3aed;
  background: rgba(124, 58, 237, 0.12);
}

.memory-conflict-panels {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  margin: 12px 0;
}

.memory-conflict-panel {
  min-width: 0;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface);
  overflow: hidden;
}

.memory-conflict-panel__title {
  margin: 0;
  padding: 8px 10px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--color-text-subtle);
  border-bottom: 1px solid var(--color-border);
  background: var(--color-surface-muted);
}

.memory-conflict-panel__body {
  margin: 0;
  padding: 10px;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 160px;
  overflow: auto;
  font-family: var(--font-mono);
}

.memory-conflict-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.memory-conflict-option {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  cursor: pointer;
}

.memory-conflict-buttons {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 4px;
}

@media (max-width: 720px) {
  .memory-conflict-panels {
    grid-template-columns: 1fr;
  }
}
</style>
