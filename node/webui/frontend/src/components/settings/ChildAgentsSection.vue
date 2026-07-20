<script setup>
import { computed } from "vue";
import { sessionStore } from "../../stores/session.js";
import { useChildAgents, formatChildAgentStatus, isChildAgentActive } from "../../composables/useChildAgents.js";
import { formatRelativeTime } from "../../utils/format.js";
import { shortId } from "../../utils/panelFormat.js";

const sessionId = computed(() => sessionStore.sessionId);

const {
  loading,
  error,
  items,
  cancellingId,
  statusMessage,
  load,
  cancelChild,
} = useChildAgents(sessionId);
</script>

<template>
  <section class="settings-section child-agents-section">
    <div class="settings-section__head">
      <h2 class="settings-section__title">子 Agent</h2>
      <button type="button" class="btn btn--ghost btn--sm" :disabled="loading || !sessionId" @click="load">
        刷新
      </button>
    </div>
    <p class="settings-section__desc">
      当前会话下由主 Agent 派生的临时子 Agent。运行中的任务可在此取消。
    </p>

    <p v-if="!sessionId" class="child-agents-section__empty">请先在聊天页打开一个 Agent。</p>
    <div v-else-if="loading && !items.length" class="child-agents-section__empty">加载中…</div>
    <div v-else-if="error" class="command-panel__error">{{ error }}</div>
    <p v-else-if="statusMessage" class="child-agents-section__status">{{ statusMessage }}</p>

    <ul v-if="sessionId && items.length" class="command-card-list child-agents-section__list">
      <li
        v-for="item in items"
        :key="item.child_session_id"
        class="command-card"
        :class="{ 'command-card--active-child': isChildAgentActive(item.status) }"
      >
        <div class="command-card__main">
          <div class="command-card__title">
            {{ item.purpose?.trim() || shortId(item.child_session_id, 24) }}
            <span
              class="command-card__badge"
              :class="isChildAgentActive(item.status) ? 'command-card__badge--active' : 'command-card__badge--muted'"
            >
              {{ formatChildAgentStatus(item.status) }}
            </span>
          </div>
          <div class="command-card__meta command-card__meta--mono">{{ shortId(item.child_session_id, 36) }}</div>
          <dl class="command-kv-list command-kv-list--compact">
            <div class="command-kv">
              <dt>轮次</dt>
              <dd>{{ item.turn_count ?? 0 }} / {{ item.max_turns ?? "—" }}</dd>
            </div>
            <div v-if="item.expires_at" class="command-kv">
              <dt>过期</dt>
              <dd>{{ formatRelativeTime(item.expires_at) }}</dd>
            </div>
            <div v-if="(item.allowed_tools || []).length" class="command-kv">
              <dt>工具</dt>
              <dd>{{ item.allowed_tools.join(", ") }}</dd>
            </div>
          </dl>
          <div v-if="isChildAgentActive(item.status)" class="command-card__actions">
            <button
              type="button"
              class="btn btn--ghost btn--sm"
              :disabled="cancellingId === item.child_session_id"
              @click="cancelChild(item.child_session_id)"
            >
              {{ cancellingId === item.child_session_id ? "取消中…" : "取消" }}
            </button>
          </div>
        </div>
      </li>
    </ul>
    <p v-else-if="sessionId && !loading && !error" class="child-agents-section__empty">当前会话暂无子 Agent</p>
  </section>
</template>
