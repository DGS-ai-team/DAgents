<script setup>
import { computed } from "vue";
import { chromeStore } from "../stores/chrome.js";
import { formatCompactTokens } from "../utils/usage.js";

const usage = computed(() => chromeStore.usageStrip);
const visible = computed(() => {
  const snap = usage.value;
  return Boolean(snap && (Number(snap.prompt) > 0 || Number(snap.completion) > 0));
});

const total = computed(() => {
  const snap = usage.value || {};
  return Math.max(0, Number(snap.prompt) || 0) + Math.max(0, Number(snap.completion) || 0);
});

const cacheObserved = computed(() => {
  const snap = usage.value || {};
  return snap.cacheObserved === true || Number(snap.hit) > 0 || Number(snap.rate) >= 0;
});

const cacheText = computed(() => {
  if (!cacheObserved.value) return "";
  const rate = Number(usage.value?.rate);
  return rate >= 0 ? `缓存 ${Math.round(rate * 100)}%` : `缓存 ${formatCompactTokens(Number(usage.value?.hit) || 0)}`;
});

const title = computed(() => {
  if (!visible.value) return "";
  const snap = usage.value || {};
  const prompt = formatCompactTokens(Number(snap.prompt) || 0);
  const completion = formatCompactTokens(Number(snap.completion) || 0);
  const parts = [`总计 ${formatCompactTokens(total.value)} tokens`, `输入 ${prompt}`, `输出 ${completion}`];
  if (cacheText.value) parts.push(`缓存命中 ${cacheText.value.replace("缓存 ", "")}`);
  if (Number(snap.reasoning) > 0) parts.push(`思考 ${formatCompactTokens(Number(snap.reasoning))}`);
  return parts.join(" · ");
});
</script>

<template>
  <div v-if="visible" class="usage-summary" :title="title" :aria-label="title">
    <span class="usage-summary__total">Tokens {{ formatCompactTokens(total) }}</span>
    <span class="usage-summary__detail">
      ↑{{ formatCompactTokens(Number(usage.prompt) || 0) }} ↓{{ formatCompactTokens(Number(usage.completion) || 0) }}
    </span>
    <span v-if="cacheText" class="usage-summary__cache">{{ cacheText }}</span>
  </div>
</template>

<style scoped>
.usage-summary {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--color-text-subtle);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  line-height: 1.4;
  white-space: nowrap;
}

.usage-summary__total { color: var(--color-text-muted); }
.usage-summary__detail { color: var(--color-text-subtle); }
.usage-summary__cache { color: var(--color-text-subtle); }
</style>
