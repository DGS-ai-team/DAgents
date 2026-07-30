<script setup>
import { ref, watch, onUnmounted, computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import WorkgroupPanel from "../components/WorkgroupPanel.vue";

const route = useRoute();
const router = useRouter();

const workgroupId = computed(() => String(route.params.workgroupId || "").trim());
const events = ref([]);
const draft = ref("");
const sending = ref(false);
const error = ref("");
const pollTimer = ref(null);

async function loadTimeline() {
  if (!workgroupId.value) {
    events.value = [];
    return;
  }
  try {
    const res = await api.getWorkgroupTimeline(workgroupId.value);
    events.value = res.events || [];
    error.value = "";
  } catch (e) {
    error.value = e?.message || "加载 Timeline 失败";
  }
}

function startPoll() {
  stopPoll();
  pollTimer.value = window.setInterval(loadTimeline, 3000);
}

function stopPoll() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value);
    pollTimer.value = null;
  }
}

async function send() {
  const text = draft.value.trim();
  if (!text || !workgroupId.value || sending.value) return;
  sending.value = true;
  error.value = "";
  try {
    await api.postWorkgroupMessage(workgroupId.value, text);
    draft.value = "";
    await loadTimeline();
  } catch (e) {
    error.value = e?.message || "发送失败";
  } finally {
    sending.value = false;
  }
}

function eventLabel(ev) {
  if (ev.type === "human_message") return ev.actor_id || "human";
  if (ev.type === "actor_final_text") return ev.actor_id || "member";
  return ev.type || "event";
}

watch(
  workgroupId,
  async (id) => {
    stopPoll();
    await loadTimeline();
    if (id) startPoll();
  },
  { immediate: true },
);

onUnmounted(stopPoll);
</script>

<template>
  <div class="app__body app__body--chat-v61">
    <aside class="app__col app__col--agents">
      <WorkgroupPanel />
    </aside>
    <div class="app__main-col wg-chat">
      <div v-if="error" class="chat-error-banner">{{ error }}</div>
      <div v-if="!workgroupId" class="chat-empty-agent">
        <p>选择左侧已订阅工作组查看 Timeline。</p>
        <button type="button" class="wg-chat__link" @click="router.push({ name: 'agents' })">
          返回 Agents
        </button>
      </div>
      <template v-else>
        <div class="wg-chat__timeline">
          <div v-for="ev in events" :key="ev.event_id || ev.seq" class="wg-chat__event">
            <div class="wg-chat__event-meta">
              <span>{{ eventLabel(ev) }}</span>
              <span v-if="ev.seq">#{{ ev.seq }}</span>
            </div>
            <div class="wg-chat__event-text">{{ ev.text }}</div>
          </div>
          <p v-if="!events.length" class="wg-chat__empty">暂无消息</p>
        </div>
        <form class="wg-chat__composer" @submit.prevent="send">
          <input
            v-model="draft"
            class="wg-chat__input"
            type="text"
            placeholder="向工作组发言…"
            :disabled="sending"
          />
          <button type="submit" class="wg-chat__send" :disabled="sending || !draft.trim()">
            发送
          </button>
        </form>
      </template>
    </div>
  </div>
</template>

<style scoped>
.wg-chat {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.wg-chat__timeline {
  flex: 1;
  overflow: auto;
  padding: 1rem 1.25rem;
}
.wg-chat__event {
  margin-bottom: 0.9rem;
}
.wg-chat__event-meta {
  display: flex;
  gap: 0.75rem;
  font-size: 0.75rem;
  color: var(--text-muted, #6b7280);
  margin-bottom: 0.2rem;
}
.wg-chat__event-text {
  white-space: pre-wrap;
  font-size: 0.9rem;
  line-height: 1.45;
}
.wg-chat__empty {
  color: var(--text-muted, #6b7280);
  font-size: 0.875rem;
}
.wg-chat__composer {
  display: flex;
  gap: 0.5rem;
  padding: 0.75rem 1rem;
  border-top: 1px solid var(--border-subtle, #e5e7eb);
}
.wg-chat__input {
  flex: 1;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--border-subtle, #d1d5db);
  border-radius: 6px;
  font: inherit;
}
.wg-chat__send {
  padding: 0.5rem 1rem;
  border: none;
  border-radius: 6px;
  background: var(--accent, #2563eb);
  color: #fff;
  cursor: pointer;
}
.wg-chat__send:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.wg-chat__link {
  margin-top: 0.75rem;
  border: none;
  background: none;
  color: var(--accent, #2563eb);
  cursor: pointer;
}
</style>
