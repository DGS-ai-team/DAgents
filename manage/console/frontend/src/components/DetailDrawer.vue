<script setup>
import { computed, ref, watch } from "vue";
import {
  fetchAudit,
  fetchNodeSessionContext,
  fetchNodeSessions,
  saveAgentGroups,
} from "../api.js";
import {
  agentInitials,
  formatUnix,
  riskPillClass,
  sortSessions,
  statusPillClass,
  truncate,
} from "../utils.js";

const props = defineProps({
  agent: { type: Object, default: null },
});

const emit = defineEmits(["close", "groups-saved"]);

const groupInput = ref("");
const groupMsg = ref("");
const savingGroups = ref(false);

const sessions = ref([]);
const sessionsLoading = ref(false);
const sessionsError = ref("");

const auditEvents = ref([]);
const auditLoading = ref(false);
const auditError = ref("");

const selectedSessionId = ref("");
const sessionContext = ref(null);
const contextLoading = ref(false);
const contextError = ref("");

const open = computed(() => Boolean(props.agent));

watch(
  () => props.agent,
  async (agent) => {
    selectedSessionId.value = "";
    sessionContext.value = null;
    contextError.value = "";
    if (!agent) return;
    groupInput.value = (agent.discovery_group || []).join(", ");
    groupMsg.value = "";
    await Promise.all([loadSessions(agent), loadAudit(agent.agent_id)]);
  },
  { immediate: true },
);

async function loadSessions(agent) {
  sessionsLoading.value = true;
  sessionsError.value = "";
  sessions.value = [];
  if (agent.status !== "online") {
    sessionsLoading.value = false;
    sessionsError.value = "Node 离线，无法拉取 session 列表。";
    return;
  }
  try {
    const data = await fetchNodeSessions(agent.agent_id);
    sessions.value = sortSessions(data.sessions || []);
  } catch (err) {
    sessionsError.value = err.message;
  } finally {
    sessionsLoading.value = false;
  }
}

async function loadAudit(agentId) {
  auditLoading.value = true;
  auditError.value = "";
  auditEvents.value = [];
  try {
    const data = await fetchAudit(100);
    auditEvents.value = (data.events || [])
      .filter((e) => e.target_agent_id === agentId)
      .slice(0, 8);
  } catch (err) {
    auditError.value = err.message;
  } finally {
    auditLoading.value = false;
  }
}

async function onSaveGroups() {
  if (!props.agent) return;
  savingGroups.value = true;
  groupMsg.value = "保存中…";
  try {
    const updated = await saveAgentGroups(props.agent.agent_id, groupInput.value);
    groupMsg.value = "已保存";
    emit("groups-saved", { agentId: props.agent.agent_id, updated });
  } catch (err) {
    groupMsg.value = err.message;
  } finally {
    savingGroups.value = false;
  }
}

async function selectSession(sessionId) {
  if (!props.agent) return;
  selectedSessionId.value = sessionId;
  contextLoading.value = true;
  contextError.value = "";
  sessionContext.value = null;
  try {
    sessionContext.value = await fetchNodeSessionContext(props.agent.agent_id, sessionId);
  } catch (err) {
    contextError.value = err.message;
  } finally {
    contextLoading.value = false;
  }
}
</script>

<template>
  <aside
    class="drawer"
    :class="{ hidden: !open }"
    :aria-hidden="!open"
  >
    <div class="drawer-backdrop" @click="emit('close')" />
    <div
      v-if="agent"
      class="drawer-panel"
      role="dialog"
      aria-labelledby="drawer-title"
      aria-modal="true"
    >
      <header class="drawer-header">
        <div class="drawer-title-block">
          <div
            class="agent-avatar"
            :class="{ offline: agent.status !== 'online' }"
            aria-hidden="true"
          >
            {{ agentInitials(agent) }}
          </div>
          <div>
            <h2 id="drawer-title">{{ agent.name || agent.agent_id }}</h2>
            <p class="muted mono">{{ agent.agent_id }}</p>
          </div>
        </div>
        <button
          type="button"
          class="btn btn-icon btn-ghost"
          aria-label="关闭"
          @click="emit('close')"
        >
          <svg viewBox="0 0 20 20" fill="currentColor" width="20" height="20">
            <path
              fill-rule="evenodd"
              d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
              clip-rule="evenodd"
            />
          </svg>
        </button>
      </header>

      <div class="drawer-body">
        <div class="kv-grid">
          <div class="kv">
            <div class="kv-label">status</div>
            <div class="kv-value">
              <span class="pill" :class="statusPillClass(agent.status)">{{ agent.status }}</span>
            </div>
          </div>
          <div class="kv">
            <div class="kv-label">expose_to_peers</div>
            <div class="kv-value">
              <span class="pill" :class="agent.expose_to_peers ? 'pill-yes' : 'pill-no'">
                {{ agent.expose_to_peers ? "是" : "否" }}
              </span>
            </div>
          </div>
          <div class="kv">
            <div class="kv-label">team / owner</div>
            <div class="kv-value">{{ agent.team || "—" }} / {{ agent.owner || "—" }}</div>
          </div>
          <div class="kv">
            <div class="kv-label">description</div>
            <div class="kv-value">{{ agent.description || "—" }}</div>
          </div>
          <div class="kv">
            <div class="kv-label">base_url</div>
            <div class="kv-value">
              <a :href="agent.base_url" target="_blank" rel="noopener">{{ agent.base_url }}</a>
            </div>
          </div>
          <div class="kv">
            <div class="kv-label">version</div>
            <div class="kv-value">{{ agent.version || "—" }}</div>
          </div>
          <div class="kv">
            <div class="kv-label">risk_level</div>
            <div class="kv-value">
              <span class="pill" :class="riskPillClass(agent.risk_level)">
                {{ agent.risk_level || "medium" }}
              </span>
            </div>
          </div>
          <div class="kv">
            <div class="kv-label">registered_at</div>
            <div class="kv-value">{{ formatUnix(agent.registered_at_unix) }}</div>
          </div>
          <div class="kv">
            <div class="kv-label">last_seen</div>
            <div class="kv-value">{{ formatUnix(agent.last_seen_unix) }}</div>
          </div>
          <div class="kv">
            <div class="kv-label">expires_at</div>
            <div class="kv-value">{{ formatUnix(agent.expires_at_unix) }}</div>
          </div>
          <div class="kv">
            <div class="kv-label">tools</div>
            <div class="kv-value">
              <span v-if="!agent.tools?.length" class="muted">—</span>
              <span v-else class="chips">
                <span v-for="t in agent.tools" :key="t" class="chip">{{ t }}</span>
              </span>
            </div>
          </div>
          <div class="kv">
            <div class="kv-label">skills</div>
            <div class="kv-value">
              <span v-if="!agent.skills?.length" class="muted">—</span>
              <span v-else class="chips">
                <span v-for="s in agent.skills" :key="s" class="chip">{{ s }}</span>
              </span>
            </div>
          </div>
          <div class="kv">
            <div class="kv-label">last_error</div>
            <div class="kv-value">{{ agent.last_error_summary || "—" }}</div>
          </div>
          <div class="kv">
            <div class="kv-label">recent_task</div>
            <div class="kv-value">{{ agent.recent_task_summary || "—" }}</div>
          </div>
        </div>

        <div class="section-title">discovery_group（Manage 分配）</div>
        <div class="groups-editor">
          <label class="field field-grow">
            <span>分组（逗号分隔）</span>
            <input v-model="groupInput" type="text" placeholder="ops, staging" autocomplete="off" />
          </label>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="savingGroups"
            @click="onSaveGroups"
          >
            保存分组
          </button>
          <span class="muted">{{ groupMsg }}</span>
        </div>

        <div class="section-title">Sessions（Manage 代理 Node API）</div>
        <div v-if="sessionsLoading" class="muted">加载中…</div>
        <p v-else-if="sessionsError" class="muted">{{ sessionsError }}</p>
        <p v-else-if="!sessions.length" class="muted">暂无 session。</p>
        <template v-else>
          <div class="table-scroll nested-table">
            <table>
              <thead>
                <tr>
                  <th>session_id</th>
                  <th>状态</th>
                  <th>messages</th>
                  <th>turn</th>
                  <th>首条用户消息</th>
                  <th>updated</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="session in sessions"
                  :key="session.session_id"
                  class="session-row"
                  :class="{ selected: selectedSessionId === session.session_id }"
                  @click="selectSession(session.session_id)"
                >
                  <td><code>{{ session.session_id }}</code></td>
                  <td>
                    <span
                      class="pill"
                      :class="session.active ? 'pill-online' : 'pill-muted'"
                    >
                      {{ session.active ? "active" : "persisted" }}
                    </span>
                  </td>
                  <td>
                    {{ session.message_count ?? 0 }}
                    <template v-if="session.queue_pending"> · 队列 {{ session.queue_pending }}</template>
                  </td>
                  <td>
                    <span
                      v-if="session.has_active_turn"
                      class="pill pill-task-processing"
                    >
                      {{ session.run_turn_phase || "turn" }}
                    </span>
                    <span v-else class="muted">—</span>
                  </td>
                  <td class="cell-wrap">{{ truncate(session.first_user_message || "—", 80) }}</td>
                  <td>{{ session.updated_at || "—" }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div class="section-title">Session 详情</div>
          <div v-if="!selectedSessionId" class="muted">点击上方 session 查看 context 摘要。</div>
          <div v-else-if="contextLoading" class="muted">加载 context…</div>
          <p v-else-if="contextError" class="muted">{{ contextError }}</p>
          <template v-else-if="sessionContext">
            <div class="kv-grid compact">
              <div class="kv">
                <div class="kv-label">messages</div>
                <div class="kv-value">{{ sessionContext.messages_count ?? 0 }}</div>
              </div>
              <div class="kv">
                <div class="kv-label">tokens</div>
                <div class="kv-value">{{ sessionContext.messages_total_tokens ?? "—" }}</div>
              </div>
              <div class="kv">
                <div class="kv-label">turn</div>
                <div class="kv-value">
                  {{ sessionContext.run_turn_phase || sessionContext.turn_state || "—" }}
                </div>
              </div>
              <div class="kv">
                <div class="kv-label">pending_hitl</div>
                <div class="kv-value">
                  <span
                    v-if="sessionContext.pending_tool_calls_count > 0"
                    class="pill pill-task-awaiting"
                  >
                    待审批 {{ sessionContext.pending_tool_calls_count }}
                  </span>
                  <span v-else class="muted">无</span>
                </div>
              </div>
              <div class="kv">
                <div class="kv-label">queue</div>
                <div class="kv-value">{{ sessionContext.queue_pending ?? 0 }}</div>
              </div>
            </div>
            <div class="section-title">最近消息（最多 10 条，截断）</div>
            <ul v-if="sessionContext.recent_messages?.length" class="message-list">
              <li v-for="(m, idx) in sessionContext.recent_messages" :key="idx">
                <strong>{{ m.role }}</strong>: {{ truncate(m.content, 300) }}
                <span v-if="m.tool_calls_count" class="muted">({{ m.tool_calls_count }} tools)</span>
              </li>
            </ul>
            <p v-else class="muted">无最近消息。</p>
          </template>
        </template>

        <div class="section-title">近期 Registry 审计</div>
        <div v-if="auditLoading" class="muted">加载中…</div>
        <p v-else-if="auditError" class="muted">{{ auditError }}</p>
        <p v-else-if="!auditEvents.length" class="muted">暂无记录。</p>
        <ul v-else class="audit-list">
          <li v-for="(e, idx) in auditEvents" :key="idx" class="audit-item">
            <strong>{{ e.action }}</strong>{{ formatUnix(e.at_unix) }} · {{ e.actor }}
          </li>
        </ul>
      </div>

      <footer class="drawer-footer">
        <a
          class="btn btn-primary btn-block"
          :href="agent.base_url"
          target="_blank"
          rel="noopener"
        >
          打开 {{ agent.base_url }}
        </a>
      </footer>
    </div>
  </aside>
</template>

<style scoped>
.session-row {
  cursor: pointer;
}
.session-row.selected {
  background: var(--row-hover, rgba(99, 102, 241, 0.08));
}
</style>
