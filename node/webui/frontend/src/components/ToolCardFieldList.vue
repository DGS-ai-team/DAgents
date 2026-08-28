<script setup>
defineProps({
  fields: { type: Array, default: () => [] },
  copyState: { type: String, default: "" },
});

const emit = defineEmits(["copy"]);

function copyFieldLabel(label) {
  if (label === "命令") return "复制命令";
  if (label === "输入内容") return "复制输入内容";
  return `复制${label || "文本"}`;
}
</script>

<template>
  <dl v-if="fields.length" class="tool-card__fields tool-card__fields--compact">
    <template v-for="item in fields" :key="item.label">
      <div v-if="item.kind === 'code'" class="tool-card__code-panel">
        <div class="tool-card__code-panel-heading">
          <span class="tool-card__code-panel-label">{{ item.label }}</span>
          <button
            v-if="item.value"
            type="button"
            class="tool-output__action"
            :aria-label="copyFieldLabel(item.label)"
            :title="copyFieldLabel(item.label)"
            @click.stop="emit('copy', item.value)"
          >{{ copyState || copyFieldLabel(item.label) }}</button>
        </div>
        <pre class="tool-exec-bubble__code tool-card__code-block">{{ item.value || "—" }}</pre>
      </div>
      <div v-else class="tool-card__field">
        <dt>{{ item.label }}</dt>
        <dd :class="`tool-card__value tool-card__value--${item.kind}`">
          <pre v-if="item.kind === 'multiline'">{{ item.value }}</pre>
          <span v-else>{{ item.value }}</span>
        </dd>
      </div>
    </template>
  </dl>
</template>

<style>
.tool-card__fields {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0;
  min-width: 0;
}
.tool-card__fields--compact {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
}
.tool-card__fields--compact .tool-card__code-panel {
  grid-column: 1 / -1;
}
.tool-card__field {
  display: grid;
  grid-template-columns: minmax(44px, 72px) minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  min-width: 0;
  padding: 7px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  font-size: 12px;
  line-height: 1.45;
}
.tool-card__field dt {
  color: var(--color-text-subtle);
  font-weight: 500;
  line-height: 1.45;
}
.tool-card__field dd {
  margin: 0;
  min-width: 0;
  color: var(--color-text);
  overflow-wrap: anywhere;
}
.tool-card__value--code,
.tool-card__value--mono {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Consolas, monospace);
}
.tool-card__value--code {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 5px;
}
.tool-card__value--code,
.tool-card__value--multiline {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.tool-card__value--error {
  color: var(--color-danger, #b42318) !important;
}
.tool-card__value pre {
  margin: 0;
  white-space: inherit;
  font: inherit;
}
.tool-card__code-panel {
  display: flex;
  flex-direction: column;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-code-bg);
}
.tool-card__code-panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 24px;
  padding: 3px 7px 3px 8px;
  border-bottom: 1px solid var(--color-border);
  background: var(--color-surface-muted);
}
.tool-card__code-panel-label {
  min-width: 0;
  color: var(--color-text-subtle);
  font-size: 10.5px;
  line-height: 1.35;
}
.tool-card__code-panel > .tool-exec-bubble__code {
  width: 100%;
  box-sizing: border-box;
  border: 0;
  border-radius: 0;
  background: transparent;
  padding: 8px 10px 9px;
}
@media (max-width: 640px) {
  .tool-card__fields--compact {
    grid-template-columns: minmax(0, 1fr);
  }
  .tool-card__fields--compact .tool-card__code-panel {
    grid-column: auto;
  }
  .tool-card__field {
    grid-template-columns: minmax(42px, 64px) minmax(0, 1fr);
    gap: 8px;
  }
}
</style>
