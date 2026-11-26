<script setup>
import { computed, ref } from "vue";
import { extractUserInfo } from "../stores/hitl.js";
import ToolGroupIcon from "./ToolGroupIcon.vue";

const props = defineProps({
  data: { type: Object, required: true },
  selected: { type: [Number, Array], default: 0 },
  busy: { type: Boolean, default: false },
  streamItemKey: { type: String, default: "" },
});

const emit = defineEmits(["update:selected", "submit"]);

const req = computed(() => extractUserInfo(props.data));
const answer = ref("");

const selectedSet = computed(() => {
  if (Array.isArray(props.selected)) {
    return new Set(props.selected.map((id) => String(id || "").trim()).filter(Boolean));
  }
  const idx = Number(props.selected);
  const opt = Number.isInteger(idx) ? req.value.options[idx] : null;
  return new Set(opt?.id ? [String(opt.id)] : []);
});
const canSubmit = computed(() => Boolean(answer.value.trim()) || selectedSet.value.size > 0 || (!req.value.required && !req.value.options.length));

function submit() {
  if (props.busy || !canSubmit.value) return;
  emit("submit", answer.value);
}

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
  <div class="msg msg--approval" :data-stream-item-key="streamItemKey || undefined">
    <div class="msg__body msg__body--wide">
      <div class="approval-bubble approval-bubble--user-info">
        <div class="tool-exec-bubble__source">
          <span class="tool-source-badge tool-source-badge--user" title="Agent 询问">
            <ToolGroupIcon class="tool-source-badge__svg" name="hitl" />
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
              :disabled="busy"
              @change="onMultiToggle(opt.id, $event.target.checked)"
            />
            <input
              v-else
              type="radio"
              name="user-info-option"
              :checked="Number(selected) === idx"
              :disabled="busy"
              @change="onSingleSelect(idx)"
            />
            <span>{{ opt.label }}</span>
          </label>
        </div>
        <textarea
          v-model="answer"
          class="user-info-answer"
          rows="2"
          :disabled="busy"
          :placeholder="req.placeholder || '填写补充回答（可选）'"
          aria-label="补充回答"
        ></textarea>
        <button type="button" class="btn btn--primary btn--sm user-info-submit" :disabled="busy || !canSubmit" @click="submit">
          {{ busy ? "提交中…" : "提交回答" }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.approval-bubble--user-info {
  border-left: 3px solid var(--color-info);
  background: linear-gradient(
    90deg,
    var(--color-info-soft),
    var(--color-surface) 48px
  );
}

.approval-bubble__intro--question {
  font-size: 14px;
  color: var(--color-text);
  margin: 0;
}

.user-info-options {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.user-info-answer {
  width: 100%;
  min-height: 52px;
  resize: vertical;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text);
  font: inherit;
  font-size: 13px;
}

.user-info-answer:focus-visible {
  outline: 2px solid var(--color-focus, var(--color-primary));
  outline-offset: 1px;
}

.user-info-submit { align-self: flex-start; }

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
  border-color: var(--color-primary);
  background: var(--color-primary-soft);
}
</style>
