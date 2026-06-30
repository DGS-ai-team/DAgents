<script setup>
import { computed } from "vue";

const props = defineProps({
  label: { type: String, required: true },
  options: { type: Array, default: () => [] },
  modelValue: { type: Array, default: () => [] },
  emptyHint: { type: String, default: "暂无已发布资源，请先在 Node 配置页上传并发布" },
  pillClass: { type: String, default: "" },
});

const emit = defineEmits(["update:modelValue"]);

const selected = computed(() => new Set(props.modelValue || []));

function toggle(id) {
  const next = new Set(selected.value);
  if (next.has(id)) {
    next.delete(id);
  } else {
    next.add(id);
  }
  emit("update:modelValue", [...next]);
}

function optionLabel(item) {
  const ver = item.version ? `@${item.version}` : "";
  const name = item.name ? ` — ${item.name}` : "";
  return `${item.id}${ver}${name}`;
}
</script>

<template>
  <div class="resource-picker">
    <span class="resource-picker__label">{{ label }}</span>
    <p v-if="!options.length" class="muted resource-picker__empty">{{ emptyHint }}</p>
    <div v-else class="resource-picker__list">
      <label
        v-for="item in options"
        :key="item.id"
        class="resource-picker__item"
        :class="{ 'is-selected': selected.has(item.id) }"
      >
        <input
          type="checkbox"
          :checked="selected.has(item.id)"
          @change="toggle(item.id)"
        />
        <span class="tag-pill" :class="pillClass">{{ item.id }}</span>
        <span class="resource-picker__meta muted">{{ optionLabel(item) }}</span>
      </label>
    </div>
  </div>
</template>

<style scoped>
.resource-picker {
  grid-column: 1 / -1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.resource-picker__label {
  font-size: 0.875rem;
  font-weight: 500;
}
.resource-picker__list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 200px;
  overflow-y: auto;
  padding: 8px;
  border: 1px solid var(--border-subtle, #333);
  border-radius: 6px;
}
.resource-picker__item {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 4px;
}
.resource-picker__item.is-selected {
  background: rgba(99, 102, 241, 0.08);
}
.resource-picker__meta {
  font-size: 0.8125rem;
}
.resource-picker__empty {
  margin: 0;
  font-size: 0.875rem;
}
</style>
