<script setup>
import { ref, watch, onMounted, computed } from "vue";
import * as api from "../api/node.js";
import { agentStore } from "../stores/agent.js";
import { buildContextMessageView } from "../utils/contextMessagePreview.js";
import { formatNumber } from "../utils/markdown.js";
import { shortId } from "../utils/panelFormat.js";

defineProps({
  embedded: { type: Boolean, default: false },
  fullMessages: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const error = ref("");
const ctx = ref(null);
const expandedRows = ref(new Set());

const agentId = computed(() => agentStore.agentId || "");

const displayMessages = computed(() => {
  if (!ctx.value) return [];
  if (Array.isArray(ctx.value.messages) && ctx.value.messages.length) return ctx.value.messages;
  return ctx.value.recent_messages || [];
});

const messageViews = computed(() => displayMessages.value.map((m) => buildContextMessageView(m.content)));

function toggleExpand(index) {
  const next = new Set(expandedRows.value);
  if (next.has(index)) next.delete(index);
  else next.add(index);
  expandedRows.value = next;
}

function isExpanded(index) {
  return expandedRows.value.has(index);
}

function roleLabel(role) {
  switch (String(role || "").trim()) {
    case "user":
      return "用户";
    case "assistant":
      return "助手";
    case "system":
      return "系统";
    case "tool":
      return "工具";
    default:
      return role || "—";
  }
}

async function load() {
  const sid = agentId.value;
  if (!sid) {
    ctx.value = null;
    error.value = "";
    return;
  }
  loading.value = true;
  error.value = "";
  expandedRows.value = new Set();
  try {
    ctx.value = await api.getAgentContext(sid, { fullMessages: true });
  } catch (e) {
    error.value = e.message;
    ctx.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(agentId, load);
</script>

<template>
  <section class="panel panel-overlay__card context-panel" :class="{ 'context-panel--embedded': embedded }">
    <header v-if="!embedded" class="panel__header context-panel__header">
      <div>
        <div class="panel__title">对话上下文</div>
        <div class="context-panel__subtitle">{{ agentId || "—" }}</div>
      </div>
      <div class="context-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body context-panel__body">
      <div v-if="!agentId" class="context-panel__empty">请先在对话页选择或创建一个 Agent。</div>
      <div v-else-if="loading" class="context-panel__loading">加载中…</div>
      <div v-else-if="error" class="context-panel__error">{{ error }}</div>
      <template v-else-if="ctx">
        <div class="context-panel__agent-meta">
          当前 Agent <code class="context-panel__agent-id">{{ shortId(agentId, 40) }}</code>
        </div>

        <div class="context-panel__stats">
          <div class="context-stat">
            <span class="context-stat__label">消息数</span>
            <span class="context-stat__value">{{ ctx.messages_count ?? displayMessages.length }}</span>
          </div>
          <div class="context-stat">
            <span class="context-stat__label">Token</span>
            <span class="context-stat__value">{{ formatNumber(ctx.messages_total_tokens ?? 0) }}</span>
          </div>
          <div class="context-stat">
            <span class="context-stat__label">工具循环</span>
            <span class="context-stat__value">{{ ctx.tool_loop_count ?? 0 }}</span>
          </div>
          <div class="context-stat">
            <span class="context-stat__label">回合状态</span>
            <span class="context-stat__value">{{ ctx.turn_state || "空闲" }}</span>
          </div>
        </div>

        <section v-if="ctx.loaded_skills?.length" class="context-section">
          <h3 class="context-section__title">已加载技能</h3>
          <ul class="context-skill-list">
            <li v-for="sk in ctx.loaded_skills" :key="sk.name || sk.skill_name">{{ sk.name || sk.skill_name }}</li>
          </ul>
        </section>

        <section class="context-section">
          <h3 class="context-section__title">
            全部消息
            <span class="context-section__count">({{ displayMessages.length }})</span>
          </h3>
          <ul class="context-message-list">
            <li v-for="(m, i) in displayMessages" :key="i" class="context-message-item">
              <div class="context-message-item__head">
                <span class="context-message-item__index">#{{ i + 1 }}</span>
                <span class="context-message-item__role">{{ roleLabel(m.role) }}</span>
                <span v-if="m.tool_calls_count" class="context-message-item__meta"
                  >工具调用 × {{ m.tool_calls_count }}</span
                >
                <span v-if="m.tool_call_id" class="context-message-item__meta"
                  >id {{ shortId(m.tool_call_id, 12) }}</span
                >
                <span v-if="m.has_reasoning_content" class="context-message-item__meta">含思考内容</span>
              </div>
              <div class="context-message-item__body">
                <button
                  v-if="messageViews[i]?.expandable"
                  type="button"
                  class="context-message-item__text context-message-item__text--expandable"
                  :aria-expanded="isExpanded(i)"
                  @click="toggleExpand(i)"
                >
                  {{ isExpanded(i) ? messageViews[i].full : messageViews[i].preview }}
                </button>
                <pre v-else class="context-message-item__text context-message-item__text--pre">{{ messageViews[i]?.full }}</pre>
                <button
                  v-if="messageViews[i]?.expandable"
                  type="button"
                  class="context-message-item__toggle"
                  @click="toggleExpand(i)"
                >
                  {{ isExpanded(i) ? "收起" : "展开全文" }}
                </button>
              </div>
            </li>
            <li v-if="!displayMessages.length" class="context-panel__empty">暂无消息</li>
          </ul>
        </section>
      </template>
    </div>
  </section>
</template>
