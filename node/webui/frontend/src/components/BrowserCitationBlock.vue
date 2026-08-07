<script setup>
import { computed, ref } from "vue";
import { mediaFullUrl, mediaThumbnailUrl } from "../utils/media.js";
import { openLightbox } from "../stores/lightbox.js";

const props = defineProps({
  refs: { type: Array, default: () => [] },
});

const openKey = ref("");

const items = computed(() => (Array.isArray(props.refs) ? props.refs.filter(Boolean) : []));

function toggle(key) {
  openKey.value = openKey.value === key ? "" : key;
}

function outcomeLabel(ref) {
  if (ref.success === true) return "成功";
  if (ref.success === false) return "未完成";
  if (ref.status === "failed") return "失败";
  if (ref.status === "cancelled") return "已取消";
  if (ref.status === "completed") return "完成";
  return ref.status || "引用";
}

function truncate(text, n = 72) {
  const s = String(text || "").replace(/\s+/g, " ").trim();
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}

function shotList(ref) {
  return Array.isArray(ref?.screenshots) ? ref.screenshots.filter((s) => s?.url) : [];
}

function openShots(ref, index) {
  const shots = shotList(ref);
  if (!shots.length) return;
  openLightbox(
    shots.map((s) => ({
      src: mediaFullUrl(s.url),
      alt: s.label || s.caption || "浏览器截图",
      label: s.label,
      caption: s.caption,
    })),
    index,
  );
}
</script>

<template>
  <div v-if="items.length" class="browser-cites" aria-label="浏览器任务引用">
    <div class="browser-cites__head">浏览器引用</div>
    <div
      v-for="(ref, idx) in items"
      :key="ref.key || idx"
      class="browser-cite"
      :class="{ 'browser-cite--open': openKey === (ref.key || idx) }"
    >
      <button
        type="button"
        class="browser-cite__bar"
        :aria-expanded="openKey === (ref.key || idx) ? 'true' : 'false'"
        @click="toggle(ref.key || idx)"
      >
        <span class="browser-cite__badge">{{ outcomeLabel(ref) }}</span>
        <span class="browser-cite__summary">{{ truncate(ref.summary) }}</span>
        <span v-if="shotList(ref).length" class="browser-cite__shots-n">{{ shotList(ref).length }} 图</span>
        <span class="browser-cite__chev" aria-hidden="true">{{ openKey === (ref.key || idx) ? "▾" : "▸" }}</span>
      </button>
      <div v-if="openKey === (ref.key || idx)" class="browser-cite__body">
        <p v-if="ref.task" class="browser-cite__row">
          <span class="browser-cite__label">目标</span>
          <span>{{ ref.task }}</span>
        </p>
        <p v-if="ref.summary" class="browser-cite__row">
          <span class="browser-cite__label">结论</span>
          <span>{{ ref.summary }}</span>
        </p>
        <p v-if="ref.task_id" class="browser-cite__row">
          <span class="browser-cite__label">任务</span>
          <code>{{ ref.task_id }}</code>
          <span v-if="ref.steps != null" class="browser-cite__muted"> · {{ ref.steps }} 步</span>
        </p>
        <div v-if="shotList(ref).length" class="browser-cite__block">
          <div class="browser-cite__label">截图</div>
          <div class="browser-cite__shots">
            <button
              v-for="(shot, si) in shotList(ref).slice(0, 8)"
              :key="shot.id || shot.url || si"
              type="button"
              class="browser-cite__shot-btn"
              @click="openShots(ref, si)"
            >
              <img
                class="browser-cite__shot"
                :src="mediaThumbnailUrl(shot.url)"
                :alt="shot.label || '浏览器截图'"
                loading="lazy"
              />
            </button>
          </div>
        </div>
        <div v-if="ref.urls?.length" class="browser-cite__block">
          <div class="browser-cite__label">URL</div>
          <ul class="browser-cite__list">
            <li v-for="(u, ui) in ref.urls.slice(0, 8)" :key="ui">
              <a v-if="/^https?:/i.test(u)" :href="u" target="_blank" rel="noopener noreferrer">{{ u }}</a>
              <span v-else>{{ u }}</span>
            </li>
          </ul>
        </div>
        <div v-if="ref.action_names?.length" class="browser-cite__block">
          <div class="browser-cite__label">动作</div>
          <ol class="browser-cite__list browser-cite__list--actions">
            <li v-for="(a, ai) in ref.action_names.slice(0, 24)" :key="ai"><code>{{ a }}</code></li>
          </ol>
        </div>
        <div v-if="ref.step_trace?.length" class="browser-cite__block">
          <div class="browser-cite__label">过程</div>
          <ul class="browser-cite__list">
            <li v-for="(st, si) in ref.step_trace.slice(0, 12)" :key="si">
              <template v-if="st && typeof st === 'object'">
                <strong>Step {{ st.step ?? si + 1 }}</strong>
                <span v-if="st.next_goal || st.goal"> — {{ st.next_goal || st.goal }}</span>
                <span v-if="st.evaluation" class="browser-cite__muted">（{{ st.evaluation }}）</span>
              </template>
              <span v-else>{{ st }}</span>
            </li>
          </ul>
        </div>
        <p v-if="ref.error || ref.errors?.length" class="browser-cite__row browser-cite__row--err">
          <span class="browser-cite__label">错误</span>
          <span>{{ ref.error || ref.errors.join("；") }}</span>
        </p>
        <p v-if="ref.detail_md" class="browser-cite__row browser-cite__muted">
          详情文件：<code>{{ ref.detail_md }}</code>
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.browser-cites {
  margin-top: 0.55rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  max-width: 42rem;
}
.browser-cites__head {
  font-size: 0.72rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--text-muted, #6b7280);
  font-weight: 600;
}
.browser-cite {
  border: 1px solid var(--border-subtle, rgba(0, 0, 0, 0.08));
  border-radius: 8px;
  background: var(--surface-2, rgba(0, 0, 0, 0.03));
  overflow: hidden;
}
.browser-cite__bar {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.45rem 0.65rem;
  border: 0;
  background: transparent;
  cursor: pointer;
  text-align: left;
  color: inherit;
  font: inherit;
}
.browser-cite__bar:hover {
  background: rgba(0, 0, 0, 0.03);
}
.browser-cite__badge {
  flex: 0 0 auto;
  font-size: 0.7rem;
  padding: 0.1rem 0.4rem;
  border-radius: 4px;
  background: rgba(37, 99, 235, 0.12);
  color: var(--accent, #2563eb);
  font-weight: 600;
}
.browser-cite__summary {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 0.85rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.browser-cite__shots-n {
  flex: 0 0 auto;
  font-size: 0.72rem;
  color: var(--text-muted, #6b7280);
}
.browser-cite__chev {
  flex: 0 0 auto;
  opacity: 0.55;
  font-size: 0.75rem;
}
.browser-cite__body {
  padding: 0.35rem 0.75rem 0.7rem;
  border-top: 1px solid var(--border-subtle, rgba(0, 0, 0, 0.06));
  font-size: 0.82rem;
  line-height: 1.45;
}
.browser-cite__row {
  margin: 0.35rem 0 0;
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.browser-cite__row--err {
  color: var(--danger, #b91c1c);
}
.browser-cite__label {
  flex: 0 0 auto;
  font-weight: 600;
  color: var(--text-muted, #6b7280);
  min-width: 2.5rem;
}
.browser-cite__block {
  margin-top: 0.45rem;
}
.browser-cite__list {
  margin: 0.2rem 0 0;
  padding-left: 1.1rem;
}
.browser-cite__list--actions {
  columns: 2;
  column-gap: 1rem;
}
.browser-cite__muted {
  color: var(--text-muted, #6b7280);
  font-size: 0.78rem;
}
.browser-cite code {
  font-size: 0.78rem;
}
.browser-cite__shots {
  display: flex;
  flex-wrap: wrap;
  gap: 0.45rem;
  margin-top: 0.35rem;
}
.browser-cite__shot-btn {
  display: block;
  padding: 0;
  border: 0;
  background: transparent;
  line-height: 0;
  cursor: zoom-in;
  border-radius: 6px;
  overflow: hidden;
}
.browser-cite__shot {
  display: block;
  max-width: 120px;
  max-height: 80px;
  object-fit: cover;
  border-radius: 6px;
  border: 1px solid var(--border-subtle, rgba(0, 0, 0, 0.08));
}
</style>
