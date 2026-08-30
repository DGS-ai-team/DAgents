<script setup>
import BrandActivityIndicator from "./BrandActivityIndicator.vue";
import WorkgroupApprovalCard from "./WorkgroupApprovalCard.vue";

defineProps({
  item: { type: Object, required: true },
  hitlBusy: { type: Boolean, default: false },
});

const emit = defineEmits(["resolve"]);
</script>

<template>
  <div
    class="wg-tool-row"
    :class="{
      'wg-tool-row--progress': item.inProgress,
      [`wg-tool-row--${item.toolKind || 'tool'}`]: true,
    }"
  >
    <div class="wg-tool-row__bar">
      <span class="wg-tool-row__glyph" aria-hidden="true">
        <span v-if="item.inProgress" class="tool-exec-spinner" />
        <span v-else-if="item.failed" class="wg-tool-row__mark">−</span>
        <span v-else class="wg-tool-row__check">✓</span>
      </span>
      <span class="wg-tool-row__text">{{ item.summary }}</span>
      <span class="wg-tool-row__status">
        <BrandActivityIndicator
          v-if="item.inProgress"
          class="wg-tool-row__dots"
          mode="tool"
          :show-label="false"
          compact
        />
        {{ item.statusText }}
      </span>
    </div>
    <WorkgroupApprovalCard
      v-if="item.approval"
      :approval="item.approval"
      :hitl-busy="hitlBusy"
      inline
      @resolve="(callId, approve) => emit('resolve', callId, approve)"
    />
  </div>
</template>

<style scoped>
.wg-tool-row {
  width: 100%;
  max-width: 100%;
  margin: 2px 0;
  border-radius: 6px;
  border: 1px solid var(--color-border, #d1d5db);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111827);
}
.wg-tool-row:hover,
.wg-tool-row--progress {
  border-color: var(--color-border, #d1d5db);
  background: var(--color-surface, #fff);
}
.wg-tool-row__bar {
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  gap: 6px;
  width: 100%;
  min-width: 0;
  padding: 5px 8px;
}
.wg-tool-row__glyph {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  color: var(--color-success, #0f7b0f);
  font-size: 12px;
}
.wg-tool-row__check {
  opacity: 0.9;
}
.wg-tool-row__mark {
  opacity: 0.75;
  color: var(--color-text-subtle, #9ca3af);
}
.wg-tool-row__text {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  line-height: 1.35;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace);
  color: var(--color-text, #111827);
}
.wg-tool-row__status {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--color-text-subtle, #9ca3af);
  white-space: nowrap;
}
.wg-tool-row__dots {
  --stream-meta-dot-size: 4px;
  --stream-meta-dot-gap: 2px;
}
</style>
