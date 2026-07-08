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
      <div class="msg__bubble msg__bubble--user">
        <div v-if="entry.images?.length" class="msg__images">
          <img
            v-for="(src, idx) in entry.images"
            :key="idx"
            class="msg__image"
            :src="src"
            alt="用户上传图片"
          />
        </div>
        <div v-if="entry.text">{{ entry.text }}</div>
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'user_deferred'" class="msg msg--user msg--user-deferred" :class="{ 'msg--user-applied': entry.sideEffectApplied, 'msg--user-stale': entry.sideEffectStale }">
    <div class="msg__body">
      <div class="msg__bubble msg__bubble--user msg__bubble--deferred">
        <span v-if="entry.userName" class="msg__deferred-tag">{{ entry.userName }}</span>
        <span v-if="entry.sideEffectApplied" class="msg__applied-tag">已入库</span>
        <span v-else-if="entry.sideEffectStale" class="msg__stale-tag">已失效</span>
        {{ entry.text }}
      </div>
    </div>
  </div>

  <div v-else-if="entry.kind === 'assistant'" class="msg msg--assistant" data-kind="assistant" :class="{ 'msg--generating': entry.streaming }">
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
        <pre v-if="entry.streaming" class="assistant-msg__stream-plain">{{ entry.text }}</pre>
        <div v-else class="tool-exec-bubble__markdown assistant-msg__md" v-html="renderMarkdown(entry.text)" />
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
.msg__images {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}
.msg__image {
  max-width: min(240px, 100%);
  max-height: 180px;
  border-radius: 8px;
  object-fit: contain;
  background: rgba(0, 0, 0, 0.15);
}
.msg__usage {
  margin-top: 8px;
  text-align: right;
  font-size: 11px;
  color: var(--color-text-subtle);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.assistant-msg__stream-plain {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.55;
  color: var(--color-text);
}
.msg__bubble--deferred {
  opacity: 0.85;
  border-style: dashed;
}
.msg--user-applied .msg__bubble--deferred {
  opacity: 1;
  border-style: solid;
  border-color: var(--color-border-subtle, #444);
}
.msg--user-stale .msg__bubble--deferred {
  opacity: 0.55;
  text-decoration: line-through;
}
.msg__applied-tag,
.msg__stale-tag {
  display: inline-block;
  margin-right: 6px;
  font-size: 10px;
  text-transform: uppercase;
}
.msg__applied-tag {
  color: var(--color-success, #6a9);
}
.msg__stale-tag {
  color: var(--color-text-subtle);
}
.msg__deferred-tag {
  display: inline-block;
  margin-right: 6px;
  font-size: 10px;
  text-transform: uppercase;
  color: var(--color-text-subtle);
}
</style>
