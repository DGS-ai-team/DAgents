<script setup>
import { computed } from "vue";
import { extractUserInfo } from "../stores/hitl.js";

const props = defineProps({
  data: { type: Object, required: true },
  selected: { type: [Number, Array], default: 0 },
});

const emit = defineEmits(["update:selected", "submit"]);

const req = computed(() => extractUserInfo(props.data));

const selectedSet = computed(() => {
  if (Array.isArray(props.selected)) {
    return new Set(props.selected.map((id) => String(id || "").trim()).filter(Boolean));
  }
  const idx = Number(props.selected);
  const opt = Number.isInteger(idx) ? req.value.options[idx] : null;
  return new Set(opt?.id ? [String(opt.id)] : []);
});

function onSingleSelect(idx) {
  emit("update:selected", Number(idx));
}

function onMultiToggle(id, checked) {
  const next = new Set(selectedSet.value);
  if (checked) next.add(String(id));
  else next.delete(String(id));
  emit("update:selected", [...next]);
}
</script>

<template>
  <div class="msg msg--approval">
    <div class="msg__body msg__body--wide">
      <div class="approval-bubble approval-bubble--user-info">
        <div class="tool-exec-bubble__source">
          <span class="tool-source-badge tool-source-badge--user" title="Agent 询问">
            <span class="tool-source-badge__icon" aria-hidden="true">?</span>
            <span class="tool-source-badge__text">Agent 询问</span>
          </span>
        </div>
        <p class="approval-bubble__intro approval-bubble__intro--question">{{ req.question }}</p>
        <div v-if="req.options.length" class="approval-bubble__actions user-info-options">
          <label v-for="(opt, idx) in req.options" :key="opt.id" class="user-info-option">
            <input
              v-if="req.allowMultiple"
              type="checkbox"
              :checked="selectedSet.has(String(opt.id))"
              @change="onMultiToggle(opt.id, $event.target.checked)"
            />
            <input
              v-else
              type="radio"
              name="user-info-option"
              :checked="Number(selected) === idx"
              @change="onSingleSelect(idx)"
            />
            <span>{{ opt.label }}</span>
          </label>
          <button type="button" class="btn btn--primary btn--sm" @click="emit('submit', '')">提交选项</button>
        </div>
        <p v-else class="approval-bubble__hint">在下方输入框回答后 Enter 发送</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.approval-bubble--user-info {
  border-left: 3px solid #0284c7;
  background: linear-gradient(90deg, rgba(2, 132, 199, 0.06), #f8fafc 48px);
}

.approval-bubble__intro--question {
  font-size: 14px;
  color: var(--color-text);
  margin: 0;
}

.approval-bubble__hint {
  margin: 0;
  font-size: 12.5px;
  color: var(--color-text-muted);
}

.user-info-options {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.user-info-option {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--color-border);
  background: var(--color-surface);
  cursor: pointer;
}

.user-info-option:hover {
  border-color: rgba(2, 132, 199, 0.35);
  background: rgba(2, 132, 199, 0.04);
}
</style>
