<script setup>
import { computed, ref, watch } from "vue";
import { buildUserInfoSubmitResume } from "../stores/hitl.js";

const props = defineProps({
  request: { type: Object, required: true },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits(["resolve"]);
const draft = ref("");
const selected = ref([]);
const validationMessage = ref("");

const info = computed(() => props.request?.request || {});
const cardKey = computed(() => `${props.request?.hitlId || ""}:${props.request?.callId || ""}`);
const selectedSet = computed(() => new Set(selected.value));

watch(
  cardKey,
  () => {
    draft.value = "";
    const firstOptionId = String(info.value.options?.[0]?.id || "").trim();
    selected.value = !info.value.allowMultiple && firstOptionId ? [firstOptionId] : [];
    validationMessage.value = "";
  },
  { immediate: true },
);

function toggleOption(id) {
  const optionId = String(id || "").trim();
  if (!optionId || props.busy) return;
  validationMessage.value = "";
  if (!info.value.allowMultiple) {
    selected.value = [optionId];
    return;
  }
  const next = new Set(selected.value);
  if (next.has(optionId)) next.delete(optionId);
  else next.add(optionId);
  selected.value = [...next];
}

function submit() {
  if (props.busy) return;
  const selectedValue = info.value.allowMultiple
    ? selected.value
    : Math.max(
        -1,
        (info.value.options || []).findIndex((option) => selectedSet.value.has(option.id)),
      );
  const built = buildUserInfoSubmitResume(props.request?.data || {}, {
    text: draft.value,
    selected: selectedValue,
  });
  if (!built.ok) {
    validationMessage.value = built.error || "请填写回答";
    return;
  }
  validationMessage.value = "";
  emit("resolve", built.resume);
}

function submitOnEnter(event) {
  if (event?.isComposing) return;
  event?.preventDefault();
  submit();
}
</script>

<template>
  <div class="wg-member-question">
    <div class="wg-member-question__head">
      <span class="wg-member-question__badge">成员询问</span>
      <span class="wg-member-question__actor">{{ request.memberLabel }}</span>
    </div>
    <p class="wg-member-question__prompt">{{ info.question }}</p>
    <div v-if="info.options?.length" class="wg-member-question__options">
      <button
        v-for="option in info.options"
        :key="option.id"
        type="button"
        class="wg-member-question__option"
        :class="{ 'wg-member-question__option--selected': selectedSet.has(option.id) }"
        :aria-pressed="selectedSet.has(option.id)"
        :disabled="busy"
        @click="toggleOption(option.id)"
      >
        <span class="wg-member-question__choice" aria-hidden="true">
          {{ selectedSet.has(option.id) ? "✓" : "" }}
        </span>
        <span>{{ option.label }}</span>
      </button>
    </div>
    <div class="wg-member-question__answer">
      <input
        v-model="draft"
        type="text"
        aria-label="成员问题回答"
        :placeholder="info.placeholder || (info.options?.length ? '也可以输入自定义回答…' : '输入回答…')"
        :disabled="busy"
        @input="validationMessage = ''"
        @keydown.enter="submitOnEnter"
      />
      <button type="button" :disabled="busy" @click="submit">
        {{ busy ? "提交中…" : "提交回答" }}
      </button>
    </div>
    <p v-if="validationMessage" class="wg-member-question__error">{{ validationMessage }}</p>
  </div>
</template>

<style scoped>
.wg-member-question {
  padding: 11px 12px;
  border: 1px solid color-mix(in srgb, var(--color-info, #2563eb) 26%, var(--color-border, #d1d5db));
  border-left: 3px solid var(--color-info, #2563eb);
  border-radius: 9px;
  background: color-mix(in srgb, var(--color-info-soft, #eff6ff) 52%, var(--color-surface, #fff));
}
.wg-member-question__head,
.wg-member-question__answer {
  display: flex;
  align-items: center;
}
.wg-member-question__head {
  gap: 8px;
}
.wg-member-question__badge {
  color: var(--color-info, #2563eb);
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.04em;
}
.wg-member-question__actor {
  color: var(--color-text-muted, #6b7280);
  font-size: 11.5px;
}
.wg-member-question__prompt {
  margin: 7px 0 0;
  color: var(--color-text, #111827);
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
}
.wg-member-question__options {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 9px;
}
.wg-member-question__option {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 30px;
  padding: 4px 9px;
  border: 1px solid var(--color-border, #d1d5db);
  border-radius: 7px;
  background: var(--color-surface, #fff);
  color: var(--color-text, #111827);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.wg-member-question__option--selected {
  border-color: color-mix(in srgb, var(--color-info, #2563eb) 62%, var(--color-border, #d1d5db));
  background: color-mix(in srgb, var(--color-info-soft, #eff6ff) 80%, var(--color-surface, #fff));
}
.wg-member-question__choice {
  width: 13px;
  color: var(--color-info, #2563eb);
  font-size: 11px;
  font-weight: 700;
  text-align: center;
}
.wg-member-question__answer {
  gap: 7px;
  margin-top: 9px;
}
.wg-member-question__answer input {
  min-width: 0;
  flex: 1 1 auto;
  height: 32px;
  padding: 0 9px;
  border: 1px solid var(--color-border, #d1d5db);
  border-radius: 7px;
  background: var(--color-surface, #fff);
  color: var(--color-text, #111827);
  font: inherit;
  font-size: 12.5px;
}
.wg-member-question__answer button {
  flex: 0 0 auto;
  height: 32px;
  padding: 0 11px;
  border: 0;
  border-radius: 7px;
  background: var(--color-primary, #0078d4);
  color: #fff;
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.wg-member-question__answer button:disabled,
.wg-member-question__option:disabled {
  cursor: default;
  opacity: 0.58;
}
.wg-member-question__error {
  margin: 5px 0 0;
  color: var(--color-danger, #b91c1c);
  font-size: 11.5px;
}
</style>
