<script setup>
import { computed, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import TerminalPanel from "./TerminalPanel.vue";

const props = defineProps({
  agentId: { type: String, required: true },
  refreshKey: { type: Number, default: 0 },
  selectedTerminalId: { type: String, default: "" },
});

const emit = defineEmits(["update:selectedTerminalId", "count-changed"]);
const terminals = ref([]);
const loading = ref(false);
const error = ref("");

const selectedTerminal = computed(
  () => terminals.value.find((item) => item.terminal_id === props.selectedTerminalId) || null,
);

function selectTerminal(id) {
  emit("update:selectedTerminalId", String(id || ""));
}

async function load() {
  if (!props.agentId) return;
  loading.value = true;
  error.value = "";
  try {
    const result = await api.listAgentTerminals(props.agentId);
    terminals.value = Array.isArray(result?.terminals) ? result.terminals : [];
    emit("count-changed", terminals.value.length);
    const current = terminals.value.find((item) => item.terminal_id === props.selectedTerminalId);
    if (!current && terminals.value.length) selectTerminal(terminals.value[0].terminal_id);
    if (!terminals.value.length && props.selectedTerminalId) selectTerminal("");
  } catch (e) {
    error.value = e.message || "加载终端列表失败";
  } finally {
    loading.value = false;
  }
}

watch(() => [props.agentId, props.refreshKey], load);
onMounted(load);
</script>

<template>
  <section class="terminal-dock">
    <div class="terminal-dock__head">
      <div>
        <h2 class="terminal-dock__title">终端工作区</h2>
        <p class="terminal-dock__desc">Agent 打开的终端会持续运行；切回消息区不会中断终端。</p>
      </div>
      <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
    </div>

    <p v-if="loading" class="terminal-dock__muted">加载终端列表中…</p>
    <p v-else-if="error" class="terminal-dock__error">{{ error }}</p>
    <div v-else-if="!terminals.length" class="terminal-dock__empty">
      当前 Agent 尚未打开终端。让 Agent 使用 terminal_open 后，这里会实时出现终端入口。
    </div>
    <template v-else>
      <div class="terminal-dock__tabs" role="tablist" aria-label="终端列表">
        <button
          v-for="item in terminals"
          :key="item.terminal_id"
          type="button"
          class="terminal-dock__tab"
          :class="{ 'terminal-dock__tab--active': item.terminal_id === props.selectedTerminalId }"
          role="tab"
          :aria-selected="item.terminal_id === props.selectedTerminalId"
          @click="selectTerminal(item.terminal_id)"
        >
          <span class="terminal-dock__tab-dot" :class="`terminal-dock__tab-dot--${item.status}`"></span>
          <span>{{ item.shell || item.target_kind || "终端" }}</span>
          <small>{{ item.status === "running" ? "运行中" : "已退出" }}</small>
        </button>
      </div>
      <TerminalPanel
        v-if="selectedTerminal"
        :key="selectedTerminal.terminal_id"
        :agent-id="props.agentId"
        :terminal-id="selectedTerminal.terminal_id"
        :terminal-meta="selectedTerminal"
        auto-connect
        preserve-session
        embedded
      />
    </template>
  </section>
</template>

<style scoped>
.terminal-dock {
  height: 100%;
  padding: 22px 28px 28px;
  overflow: auto;
  background: var(--color-surface, #fff);
}
.terminal-dock__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.terminal-dock__title { margin: 0; color: var(--color-text); font-size: 17px; }
.terminal-dock__desc,
.terminal-dock__muted,
.terminal-dock__empty,
.terminal-dock__error { margin: 6px 0 0; color: var(--color-text-subtle); font-size: 12px; }
.terminal-dock__error { color: var(--color-danger); }
.terminal-dock__empty {
  margin-top: 22px;
  padding: 28px 18px;
  border: 1px dashed var(--color-border);
  border-radius: 10px;
  text-align: center;
}
.terminal-dock__tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 18px;
}
.terminal-dock__tab {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface, #fff);
  color: var(--color-text-subtle);
  cursor: pointer;
  font-size: 12px;
}
.terminal-dock__tab--active {
  border-color: color-mix(in srgb, var(--color-primary, #3689d6) 55%, var(--color-border));
  background: color-mix(in srgb, var(--color-primary, #3689d6) 8%, #fff);
  color: var(--color-text);
}
.terminal-dock__tab small { color: var(--color-text-subtle); font-size: 10px; }
.terminal-dock__tab-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--color-text-subtle); }
.terminal-dock__tab-dot--running { background: var(--color-success, #3d9a5f); }
</style>
