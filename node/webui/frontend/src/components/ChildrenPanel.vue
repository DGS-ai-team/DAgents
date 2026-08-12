<script setup>
import { computed } from "vue";
import { agentStore } from "../stores/agent.js";
import { useChildAgents, formatChildAgentStatus, isChildAgentActive } from "../composables/useChildAgents.js";
import { formatRelativeTime } from "../utils/format.js";
import { shortId } from "../utils/panelFormat.js";

const props = defineProps({
  agentId: { type: String, default: "" },
});

const emit = defineEmits(["close"]);

const agentId = computed(() => props.agentId || agentStore.agentId);

const {
  loading,
  error,
  data,
  items,
  cancellingId,
  cancelChild,
} = useChildAgents(agentId);
</script>

<template>
  <section class="panel panel-overlay__card command-panel children-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">子 Agent</div>
        <div class="command-panel__subtitle">{{ agentId || "—" }}</div>
      </div>
      <div class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <template v-else-if="data">
        <section class="command-section">
          <h3 class="command-section__title">临时 Agent ({{ items.length }})</h3>
          <ul v-if="items.length" class="command-card-list">
            <li
              v-for="(item, index) in items"
              :key="item.child_agent_id"
              class="command-card"
              :class="{ 'command-card--active-child': isChildAgentActive(item.status) }"
            >
              <div class="command-card__main">
                <div class="command-card__title">
                  {{ index + 1 }}. {{ item.purpose?.trim() || shortId(item.child_agent_id, 24) }}
                  <span
                    class="command-card__badge"
                    :class="isChildAgentActive(item.status) ? 'command-card__badge--active' : 'command-card__badge--muted'"
                  >
                    {{ formatChildAgentStatus(item.status) }}
                  </span>
                </div>
                <dl class="command-kv-list command-kv-list--compact">
                  <div class="command-kv"><dt>ID</dt><dd>{{ shortId(item.child_agent_id, 32) }}</dd></div>
                  <div class="command-kv"><dt>用途</dt><dd>{{ item.purpose || "—" }}</dd></div>
                  <div class="command-kv">
                    <dt>工具</dt>
                    <dd>{{ (item.allowed_tools || []).join(", ") || "—" }}</dd>
                  </div>
                  <div class="command-kv">
                    <dt>轮次</dt>
                    <dd>{{ item.turn_count ?? 0 }} / {{ item.max_turns ?? "—" }}</dd>
                  </div>
                  <div class="command-kv">
                    <dt>过期</dt>
                    <dd>{{ item.expires_at ? formatRelativeTime(item.expires_at) : "—" }}</dd>
                  </div>
                </dl>
                <div v-if="isChildAgentActive(item.status)" class="command-card__actions">
                  <button
                    type="button"
                    class="btn btn--ghost btn--sm"
                    :disabled="cancellingId === item.child_agent_id"
                    @click="cancelChild(item.child_agent_id)"
                  >
                    {{ cancellingId === item.child_agent_id ? "取消中…" : "取消" }}
                  </button>
                </div>
              </div>
            </li>
          </ul>
          <p v-else class="command-panel__empty">当前 Agent 无临时子 Agent</p>
        </section>
      </template>
    </div>
  </section>
</template>
