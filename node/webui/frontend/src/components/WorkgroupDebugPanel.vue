<script setup>
import { computed, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({
  workgroupId: { type: String, required: true },
});

const debugLoading = ref(false);
const debugRuns = ref([]);
const debugLlm = ref(null);
const debugSelectedRunId = ref("");
const debugHistory = ref(null);
const debugError = ref("");

const debugLlmBadge = computed(() => {
  const mode = String(debugLlm.value?.mode || "").trim();
  if (mode === "mock") return "Mock · 回声/脚本";
  if (mode === "live") {
    const model = String(debugLlm.value?.model || "").trim();
    return model ? `Live · ${model}` : "Live";
  }
  return "";
});

async function loadDebugRuns() {
  debugLoading.value = true;
  debugError.value = "";
  try {
    const res = await api.listWorkgroupRuns(props.workgroupId, { limit: 12 });
    debugRuns.value = Array.isArray(res?.runs) ? res.runs : [];
    debugLlm.value = res?.llm || null;
    if (!debugRuns.value.length) {
      debugHistory.value = null;
      debugSelectedRunId.value = "";
      return;
    }
    const selected = debugRuns.value.some((run) => run.run_id === debugSelectedRunId.value)
      ? debugSelectedRunId.value
      : debugRuns.value[0].run_id;
    await selectDebugRun(selected);
  } catch (error) {
    debugError.value = error?.message || "加载 Run 失败";
    debugRuns.value = [];
    debugHistory.value = null;
  } finally {
    debugLoading.value = false;
  }
}

async function selectDebugRun(runId) {
  const id = String(runId || "").trim();
  if (!id) return;
  debugSelectedRunId.value = id;
  try {
    const res = await api.getWorkgroupRunHistory(props.workgroupId, id);
    debugHistory.value = res?.history || null;
    if (res?.llm) debugLlm.value = res.llm;
  } catch (error) {
    debugError.value = error?.message || "加载 History 失败";
    debugHistory.value = null;
  }
}

function formatDebugMsg(message) {
  const role = String(message?.role || "");
  if (role === "assistant" && Array.isArray(message?.tool_calls) && message.tool_calls.length) {
    const names = message.tool_calls
      .map((toolCall) => toolCall?.function?.name || toolCall?.name || "?")
      .join(", ");
    const body = String(message?.content || "").trim();
    return body ? `${body}\n\ntool_calls: ${names}` : `tool_calls: ${names}`;
  }
  if (role === "tool") {
    const body = String(message?.content || "").trim();
    return body.length > 180 ? `${body.slice(0, 180)}…` : body || "(empty)";
  }
  return String(message?.content || "").trim();
}

watch(
  () => props.workgroupId,
  () => {
    debugRuns.value = [];
    debugLlm.value = null;
    debugSelectedRunId.value = "";
    debugHistory.value = null;
    void loadDebugRuns();
  },
  { immediate: true },
);
</script>

<template>
  <aside class="wg-debug" aria-label="RunHistory 调试">
    <header class="wg-debug__head">
      <strong>RunHistory</strong>
      <span v-if="debugLlmBadge" class="wg-debug__badge" :data-mode="debugLlm?.mode">
        {{ debugLlmBadge }}
      </span>
      <button type="button" class="wg-debug__refresh" :disabled="debugLoading" @click="loadDebugRuns">
        刷新
      </button>
    </header>
    <p v-if="debugError" class="wg-debug__error">{{ debugError }}</p>
    <p v-else-if="debugLoading && !debugRuns.length" class="wg-debug__muted">加载中…</p>
    <p v-else-if="!debugRuns.length" class="wg-debug__muted">暂无 ActorRun（发一条消息后会出现）</p>
    <ul v-else class="wg-debug__runs">
      <li v-for="run in debugRuns" :key="run.run_id">
        <button
          type="button"
          class="wg-debug__run"
          :class="{ 'wg-debug__run--active': run.run_id === debugSelectedRunId }"
          @click="selectDebugRun(run.run_id)"
        >
          <span class="wg-debug__run-actor">{{ run.actor_id === "leader" ? "Supervisor" : run.actor_id }}</span>
          <span class="wg-debug__run-status">{{ run.status }}</span>
          <span class="wg-debug__run-id" :title="run.run_id">{{ run.run_id.slice(-8) }}</span>
        </button>
      </li>
    </ul>
    <div v-if="debugHistory" class="wg-debug__msgs">
      <div
        v-for="(message, index) in debugHistory.messages || []"
        :key="index"
        class="wg-debug__msg"
        :data-role="message.role"
      >
        <div class="wg-debug__msg-role">{{ message.role }}</div>
        <pre class="wg-debug__msg-body">{{ formatDebugMsg(message) }}</pre>
        <details
          v-if="message.role === 'assistant' && message.tool_calls?.length"
          class="wg-debug__details"
        >
          <summary>工具参数</summary>
          <pre
            v-for="(toolCall, toolIndex) in message.tool_calls"
            :key="toolIndex"
            class="wg-debug__msg-body"
          >{{ toolCall.function?.name || "?" }}
{{ toolCall.function?.arguments || "{}" }}</pre>
        </details>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.wg-debug {
  flex: 0 0 320px;
  max-width: 38%;
  min-width: 260px;
  border-left: 1px solid var(--color-border, #e5e7eb);
  background: var(--color-surface, #fafafa);
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.wg-debug__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--color-border, #e5e7eb);
  font-size: 13px;
}
.wg-debug__badge {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--color-primary, #0078d4) 12%, transparent);
  color: var(--color-primary, #0078d4);
}
.wg-debug__badge[data-mode="mock"] {
  background: color-mix(in srgb, #c50f1f 12%, transparent);
  color: #c50f1f;
}
.wg-debug__refresh {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: var(--color-text-muted, #6b7280);
  font-size: 12px;
  cursor: pointer;
}
.wg-debug__error {
  margin: 8px 12px;
  color: #c50f1f;
  font-size: 12px;
}
.wg-debug__muted {
  margin: 12px;
  color: var(--color-text-muted, #6b7280);
  font-size: 12px;
}
.wg-debug__runs {
  list-style: none;
  margin: 0;
  padding: 6px;
  border-bottom: 1px solid var(--color-border, #e5e7eb);
  max-height: 28%;
  overflow: auto;
}
.wg-debug__run {
  width: 100%;
  display: grid;
  grid-template-columns: 1fr auto auto;
  gap: 6px;
  align-items: center;
  text-align: left;
  border: 0;
  background: transparent;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12px;
  color: var(--color-text, #111827);
}
.wg-debug__run:hover,
.wg-debug__run--active {
  background: color-mix(in srgb, var(--color-primary, #0078d4) 10%, transparent);
}
.wg-debug__run-status,
.wg-debug__run-id {
  color: var(--color-text-muted, #6b7280);
}
.wg-debug__run-id {
  font-family: ui-monospace, Consolas, monospace;
}
.wg-debug__msgs {
  flex: 1;
  overflow: auto;
  padding: 8px 10px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.wg-debug__msg {
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 8px;
  padding: 6px 8px;
  background: var(--color-editor, #fff);
}
.wg-debug__msg-role {
  font-size: 10.5px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--color-text-muted, #6b7280);
  margin-bottom: 4px;
}
.wg-debug__msg-body {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.45;
  font-family: ui-monospace, Consolas, monospace;
}
.wg-debug__details {
  margin-top: 4px;
  font-size: 12px;
}
.wg-debug__details summary {
  cursor: pointer;
  color: var(--color-primary, #0078d4);
}
</style>
