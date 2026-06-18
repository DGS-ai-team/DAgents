<script setup>
import { renderMarkdown } from "../utils/markdown.js";

defineProps({
  entry: { type: Object, required: true },
  showReasoning: { type: Boolean, default: false },
});
</script>

<template>
  <div v-if="entry.kind === 'user'" class="msg msg--user">
    <div class="msg__body">
      <div class="msg__bubble msg__bubble--user">{{ entry.text }}</div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'assistant'" class="msg msg--assistant" :class="{ 'msg--generating': entry.streaming }">
    <div class="msg__body">
      <div v-if="entry.streaming && !entry.text" class="msg__body--hint-only">
        <div class="msg__hint msg__hint--stream-meta">
          <span class="msg__meta-label">generating</span>
          <span class="msg__meta-dots" aria-hidden="true">
            <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
          </span>
        </div>
      </div>
      <div v-else class="msg__bubble msg__bubble--assistant-md">
        <div class="tool-exec-bubble__markdown assistant-msg__md" v-html="renderMarkdown(entry.text)" />
        <div v-if="entry.usage" class="msg__usage">{{ entry.usage }}</div>
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'reasoning' && showReasoning" class="msg msg--reasoning" :class="{ 'msg--reasoning-collapsed': !entry.text && entry.streaming }">
    <div class="msg__body">
      <div class="msg__hint msg__hint--stream-meta">
        <span class="msg__meta-label">thinking</span>
        <span v-if="entry.streaming" class="msg__meta-dots" aria-hidden="true">
          <span class="msg__meta-dot" /><span class="msg__meta-dot" /><span class="msg__meta-dot" />
        </span>
        <span v-else class="msg__meta-done">done</span>
      </div>
      <div v-if="entry.text" class="msg__bubble msg__bubble--reasoning">{{ entry.text }}</div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'system'" class="msg msg--system">
    <div class="msg__body msg__body--wide">
      <div class="msg__bubble msg__bubble--system">{{ entry.text }}</div>
    </div>
  </div>
</template>

<style scoped>
.msg__usage {
  margin-top: 8px;
  text-align: right;
  font-size: 11px;
  color: var(--color-text-subtle);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
</style>
