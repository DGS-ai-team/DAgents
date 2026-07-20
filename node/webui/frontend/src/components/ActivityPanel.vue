<script setup>
import { computed, ref, watch } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";
import { transcriptStore } from "../stores/transcript.js";
import { deriveActivityFromTranscript } from "../utils/workspaceActivity.js";

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const remote = ref(null);
const tab = ref("files"); // files | commands
const expandedCmd = ref({});

function toggleCmd(key) {
  const k = String(key);
  expandedCmd.value = { ...expandedCmd.value, [k]: !expandedCmd.value[k] };
}

const live = computed(() => deriveActivityFromTranscript(transcriptStore.entries));

const files = computed(() => remote.value?.files?.length ? remote.value.files : live.value.files);
const commands = computed(() => remote.value?.commands?.length ? remote.value.commands : live.value.commands);
const fileCount = computed(() => files.value?.length || 0);
const cmdCount = computed(() => commands.value?.length || 0);

async function refresh() {
  const id = sessionStore.sessionId?.trim();
  if (!id) {
    remote.value = null;
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    remote.value = await api.getWorkspaceActivity(id);
  } catch (e) {
    error.value = e.message || "加载失败";
    // 仍可用 live transcript
  } finally {
    loading.value = false;
  }
}

watch(
  () => sessionStore.sessionId,
  () => {
    void refresh();
  },
  { immediate: true },
);

defineExpose({ refresh });
</script>

<template>
  <section class="panel panel-overlay__card activity-panel">
    <header class="panel__header activity-panel__header">
      <div>
        <div class="panel__title">变更与命令</div>
        <div class="activity-panel__subtitle">本 Agent 对话中改过的文件与执行过的命令</div>
      </div>
      <div class="activity-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="refresh">刷新</button>
        <button type="button" class="btn btn--ghost btn--sm" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="activity-panel__tabs">
      <button
        type="button"
        class="activity-panel__tab"
        :class="{ 'activity-panel__tab--active': tab === 'files' }"
        @click="tab = 'files'"
      >
        文件 <span class="activity-panel__count">{{ fileCount }}</span>
      </button>
      <button
        type="button"
        class="activity-panel__tab"
        :class="{ 'activity-panel__tab--active': tab === 'commands' }"
        @click="tab = 'commands'"
      >
        命令 <span class="activity-panel__count">{{ cmdCount }}</span>
      </button>
    </div>

    <div class="panel__body activity-panel__body">
      <p v-if="error" class="activity-panel__error">{{ error }}</p>
      <div v-if="loading" class="activity-panel__empty">加载中…</div>

      <template v-else-if="tab === 'files'">
        <ul v-if="files.length" class="activity-list">
          <li v-for="f in files" :key="f.path" class="activity-list__item">
            <div class="activity-list__main">
              <code class="activity-list__path">{{ f.path }}</code>
              <div class="activity-list__meta">
                <span v-for="op in f.ops" :key="op" class="activity-chip">{{ op }}</span>
                <span v-if="f.rejected" class="activity-chip activity-chip--danger">已拒绝</span>
              </div>
            </div>
            <div v-if="f.preview" class="activity-list__preview">{{ f.preview }}</div>
          </li>
        </ul>
        <p v-else class="activity-panel__empty">尚无文件写入或编辑</p>
      </template>

      <template v-else>
        <ul v-if="commands.length" class="activity-list">
          <li v-for="(c, i) in commands" :key="c.tool_call_id || i" class="activity-list__item">
            <button type="button" class="activity-list__cmd-head" @click="toggleCmd(c.tool_call_id || i)">
              <span class="activity-list__chevron">{{ expandedCmd[c.tool_call_id || i] ? "▾" : "▸" }}</span>
              <code class="activity-list__cmd">$ {{ c.command }}</code>
              <span
                class="activity-chip"
                :class="{
                  'activity-chip--ok': c.status === 'ok',
                  'activity-chip--danger': c.status === 'error' || c.status === 'rejected',
                }"
              >{{ c.status }}</span>
            </button>
            <pre
              v-if="expandedCmd[c.tool_call_id || i] && c.content_preview"
              class="activity-list__preview activity-list__preview--folded"
            >{{ c.content_preview }}</pre>
          </li>
        </ul>
        <p v-else class="activity-panel__empty">尚无 shell 命令记录</p>
      </template>
    </div>
  </section>
</template>

<style scoped>
.activity-panel {
  width: min(440px, 92vw);
  max-height: min(80vh, 720px);
  display: flex;
  flex-direction: column;
  background: var(--color-surface-muted);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-md);
}
.activity-panel__header {
  align-items: flex-start;
  gap: 8px;
}
.activity-panel__subtitle {
  font-size: 11px;
  color: var(--color-text-subtle);
  margin-top: 2px;
}
.activity-panel__header-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
.activity-panel__tabs {
  display: flex;
  gap: 2px;
  padding: 0 12px;
  border-bottom: 1px solid var(--color-border);
}
.activity-panel__tab {
  border: 0;
  background: transparent;
  color: var(--color-text-muted);
  padding: 8px 12px;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  font-size: 12.5px;
}
.activity-panel__tab--active {
  color: var(--color-text);
  border-bottom-color: var(--color-primary);
}
.activity-panel__count {
  margin-left: 4px;
  color: var(--color-text-subtle);
  font-variant-numeric: tabular-nums;
}
.activity-panel__body {
  overflow: auto;
  padding: 8px 12px 12px;
  min-height: 0;
}
.activity-panel__empty,
.activity-panel__error {
  margin: 16px 4px;
  font-size: 12.5px;
  color: var(--color-text-subtle);
  text-align: center;
}
.activity-panel__error {
  color: var(--color-danger);
}
.activity-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.activity-list__item {
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  border: 1px solid var(--color-border);
}
.activity-list__item:hover {
  border-color: var(--color-border-strong);
  background: var(--color-surface-hover);
}
.activity-list__main {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}
.activity-list__path,
.activity-list__cmd {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-text);
  word-break: break-all;
}
.activity-list__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  flex-shrink: 0;
}
.activity-list__cmd-head {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  width: 100%;
  border: 0;
  background: transparent;
  padding: 0;
  cursor: pointer;
  text-align: left;
  color: inherit;
  font: inherit;
}
.activity-list__chevron {
  flex: 0 0 auto;
  font-size: 10px;
  color: var(--color-text-subtle);
  margin-top: 2px;
}
.activity-list__preview {
  margin-top: 6px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-text-subtle);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 4.5em;
  overflow: hidden;
}
.activity-list__preview--folded {
  max-height: 12em;
  overflow: auto;
  margin: 6px 0 0;
  padding: 8px;
  border-radius: var(--radius-sm);
  background: #0d0d0d;
  border: 1px solid var(--color-border-strong);
  color: #d4d4d4;
}
.activity-chip {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--color-surface-elevated);
  color: var(--color-text-muted);
  border: 1px solid var(--color-border);
  text-transform: lowercase;
}
.activity-chip--ok {
  color: var(--color-success);
  border-color: transparent;
  background: var(--color-success-soft);
}
.activity-chip--danger {
  color: var(--color-danger);
  border-color: transparent;
  background: var(--color-danger-soft);
}
</style>
