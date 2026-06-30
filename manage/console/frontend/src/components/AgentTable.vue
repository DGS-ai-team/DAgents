<script setup>
import { agentInitials, formatUnix, riskPillClass, statusPillClass, truncate } from "../utils.js";

defineProps({
  agents: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  error: { type: String, default: "" },
});

const emit = defineEmits(["open"]);
</script>

<template>
  <section class="panel table-panel table-panel--stretch">
    <div v-if="error" class="banner banner-error" role="alert">{{ error }}</div>
    <div class="table-scroll">
      <table class="data-table">
        <thead>
          <tr>
            <th>Agent</th>
            <th>agent_id</th>
            <th>team</th>
            <th>状态</th>
            <th>版本</th>
            <th>最近心跳</th>
            <th>A2A</th>
            <th>风险</th>
            <th>工具</th>
            <th>分组</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="10" class="empty">
              <div class="empty-state">
                <span class="spinner" aria-hidden="true"></span>
                加载中…
              </div>
            </td>
          </tr>
          <tr v-else-if="!agents.length">
            <td colspan="10" class="empty">
              <div class="empty-state">无匹配 Node</div>
            </td>
          </tr>
          <tr
            v-for="agent in agents"
            v-else
            :key="agent.agent_id"
            tabindex="0"
            @click="emit('open', agent)"
          >
            <td>
              <div class="agent-cell">
                <div
                  class="agent-avatar"
                  :class="{ offline: agent.status !== 'online' }"
                  aria-hidden="true"
                >
                  {{ agentInitials(agent) }}
                </div>
                <div>
                  <div class="agent-name">{{ agent.name || agent.agent_id }}</div>
                  <div v-if="agent.description" class="agent-desc">
                    {{ truncate(agent.description, 48) }}
                  </div>
                </div>
              </div>
            </td>
            <td><code class="mono">{{ agent.agent_id }}</code></td>
            <td>{{ agent.team || "—" }}</td>
            <td>
              <span class="pill" :class="statusPillClass(agent.status)">{{ agent.status }}</span>
            </td>
            <td>{{ agent.version || "—" }}</td>
            <td class="mono">{{ formatUnix(agent.last_seen_unix) }}</td>
            <td>
              <span class="pill" :class="agent.expose_to_peers ? 'pill-yes' : 'pill-no'">
                {{ agent.expose_to_peers ? "是" : "否" }}
              </span>
            </td>
            <td>
              <span class="pill" :class="riskPillClass(agent.risk_level)">
                {{ agent.risk_level || "medium" }}
              </span>
            </td>
            <td>
              <span v-if="!agent.tools?.length" class="muted">—</span>
              <span v-else class="chips">
                <span v-for="tool in agent.tools.slice(0, 3)" :key="tool" class="chip">{{ tool }}</span>
                <span v-if="agent.tools.length > 3" class="chip">+{{ agent.tools.length - 3 }}</span>
              </span>
            </td>
            <td>
              <span v-if="!agent.discovery_group?.length" class="pill pill-task-awaiting">未分配</span>
              <span v-else class="chips">
                <span
                  v-for="g in agent.discovery_group.slice(0, 2)"
                  :key="g"
                  class="chip"
                >{{ g }}</span>
                <span v-if="agent.discovery_group.length > 2" class="chip">
                  +{{ agent.discovery_group.length - 2 }}
                </span>
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <slot name="pager" />
  </section>
</template>
