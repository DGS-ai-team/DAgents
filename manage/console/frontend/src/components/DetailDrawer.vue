<script setup>
import { computed, ref, watch } from "vue";
import { fetchAudit, saveAgentGroups } from "../api.js";
import {
  agentInitials,
  formatUnix,
  statusPillClass,
} from "../utils.js";

const props = defineProps({
  agent: { type: Object, default: null },
});

const emit = defineEmits(["close", "groups-saved"]);

const groupInput = ref("");
const groupMsg = ref("");
const savingGroups = ref(false);

const auditEvents = ref([]);
const auditLoading = ref(false);
const auditError = ref("");

const open = computed(() => Boolean(props.agent));

watch(
  () => props.agent,
  async (agent) => {
    if (!agent) return;
    groupInput.value = (agent.discovery_group || []).join(", ");
    groupMsg.value = "";
    await loadAudit(agent.agent_id);
  },
  { immediate: true },
);

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
            <p class="muted mono">node_id · {{ agent.agent_id }}</p>
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
