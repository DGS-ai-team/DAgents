<script setup>
import { computed } from "vue";
import { chromeStore } from "../stores/chrome.js";

const RADIUS = 7;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

const tokens = computed(() => {
  const n = Number(chromeStore.contextTokens);
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : -1;
});

const limit = computed(() => {
  const info = chromeStore.agentInfo || {};
  const compression = info.compression || {};
  const blocking = Number(compression.blocking_trigger_tokens);
  const silent = Number(compression.silent_trigger_tokens);
  if (Number.isFinite(blocking) && blocking > 0) return Math.floor(blocking);
  if (Number.isFinite(silent) && silent > 0) return Math.floor(silent);
  return 0;
});

const visible = computed(() => tokens.value >= 0 && limit.value > 0);

/** 压缩阈值内已用占比；超过阈值时按 100% 显示。 */
const usedRatio = computed(() => {
  if (!visible.value) return 0;
  return Math.min(1, tokens.value / limit.value);
});

const usedPercent = computed(() => Math.round(usedRatio.value * 100));

const dashOffset = computed(() => CIRCUMFERENCE * (1 - usedRatio.value));

const tone = computed(() => {
  if (usedRatio.value >= 1) return "critical";
  if (usedRatio.value >= 0.85) return "warn";
  return "ok";
});

const title = computed(() => {
  if (!visible.value) return "";
  const used = tokens.value.toLocaleString("en-US");
  const lim = limit.value.toLocaleString("en-US");
  return `上下文 ${used} / ${lim} tokens · 已用 ${usedPercent.value}%`;
});
</script>

<template>
  <div
    v-if="visible"
    class="context-meter"
    :class="`context-meter--${tone}`"
    :title="title"
    role="meter"
    :aria-valuenow="usedPercent"
    aria-valuemin="0"
    aria-valuemax="100"
    :aria-label="title"
  >
    <svg class="context-meter__ring" viewBox="0 0 18 18" width="16" height="16" aria-hidden="true">
      <circle class="context-meter__track" cx="9" cy="9" :r="RADIUS" fill="none" stroke-width="2.5" />
      <circle
        class="context-meter__fill"
        cx="9"
        cy="9"
        :r="RADIUS"
        fill="none"
        stroke-width="2.5"
        stroke-linecap="round"
        :stroke-dasharray="CIRCUMFERENCE"
        :stroke-dashoffset="dashOffset"
        transform="rotate(-90 9 9)"
      />
    </svg>
    <span class="context-meter__label">上下文 {{ usedPercent }}%</span>
  </div>
</template>

<style scoped>
.context-meter {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  color: var(--color-text-muted);
  user-select: none;
}

.context-meter__ring {
  display: block;
  flex: 0 0 auto;
}

.context-meter__track {
  stroke: var(--color-border-strong);
}

.context-meter__fill {
  stroke: var(--color-text-subtle);
  transition: stroke-dashoffset 0.35s ease;
}

.context-meter__label {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.02em;
  color: inherit;
}

.context-meter--ok .context-meter__fill {
  stroke: var(--color-text-subtle);
}

.context-meter--warn {
  color: var(--color-warning);
}

.context-meter--warn .context-meter__fill {
  stroke: var(--color-warning);
}

.context-meter--critical {
  color: var(--color-danger);
}

.context-meter--critical .context-meter__fill {
  stroke: var(--color-danger);
}
</style>
