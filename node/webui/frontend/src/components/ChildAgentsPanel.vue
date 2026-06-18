<script setup>
import { ref, watch } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";

const children = ref([]);

async function refresh() {
  if (!sessionStore.sessionId) {
    children.value = [];
    return;
  }
  try {
    const res = await api.listChildAgents(sessionStore.sessionId);
    children.value = res.items || [];
  } catch {
    children.value = [];
  }
}

watch(() => sessionStore.sessionId, refresh, { immediate: true });
defineExpose({ refresh });
</script>

<template>
  <section class="panel thread-panel">
    <header class="panel__header">
      <div class="panel__title">临时 Agent</div>
      <span class="pill pill--idle">{{ children.length }}</span>
    </header>
    <div class="panel__body panel__body--tight">
      <ul v-if="children.length" class="session-history-list">
        <li v-for="c in children" :key="c.child_session_id" class="session-history-item">
          <div class="session-history-item__main">
            <div class="session-history-item__title">{{ c.purpose || c.child_session_id?.slice(0, 16) }}</div>
            <div class="session-history-item__id">{{ c.status || "—" }}</div>
          </div>
        </li>
      </ul>
      <div v-else class="chat__empty">暂无临时 Agent</div>
    </div>
  </section>
</template>
