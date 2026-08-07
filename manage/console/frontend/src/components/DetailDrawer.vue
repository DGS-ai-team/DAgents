<script setup>
import { computed } from "vue";
import {
  agentInitials,
  formatUnix,
  statusPillClass,
} from "../utils.js";

const props = defineProps({
  agent: { type: Object, default: null },
});

defineEmits(["close"]);

const open = computed(() => Boolean(props.agent));

const statusText = computed(() => {
  const s = props.agent?.status;
  if (s === "online") return "在线";
  if (s === "offline") return "离线";
  return s || "—";
});

const discoveryGroups = computed(() => {
  const gs = props.agent?.discovery_group;
  return Array.isArray(gs) ? gs.filter(Boolean) : [];
});
</script>

<template>
  <aside
    class="drawer"
    :class="{ hidden: !open }"
    :aria-hidden="!open"
  >
    <div class="drawer-backdrop" @click="$emit('close')" />
    <div
      v-if="agent"
      class="drawer-panel node-drawer"
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
          <div class="node-drawer__identity">
            <div class="node-drawer__title-row">
              <h2 id="drawer-title">{{ agent.name || agent.agent_id }}</h2>
              <span class="pill" :class="statusPillClass(agent.status)">{{ statusText }}</span>
            </div>
            <p class="node-drawer__id mono" :title="agent.agent_id">{{ agent.agent_id }}</p>
          </div>
        </div>
        <button
          type="button"
          class="btn btn-icon btn-ghost"
          aria-label="关闭"
          @click="$emit('close')"
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
        <section class="node-drawer__section">
          <p v-if="agent.description" class="node-drawer__desc">{{ agent.description }}</p>
          <p v-else class="node-drawer__desc node-drawer__desc--empty">暂无描述</p>

          <dl class="node-drawer__facts">
            <div class="node-drawer__fact">
              <dt>版本</dt>
              <dd>{{ agent.version || "—" }}</dd>
            </div>
            <div class="node-drawer__fact">
              <dt>注册时间</dt>
              <dd class="mono">{{ formatUnix(agent.registered_at_unix) }}</dd>
            </div>
            <div class="node-drawer__fact">
              <dt>最近心跳</dt>
              <dd class="mono">{{ formatUnix(agent.last_seen_unix) }}</dd>
            </div>
            <div class="node-drawer__fact">
              <dt>心跳过期</dt>
              <dd class="mono">{{ formatUnix(agent.expires_at_unix) }}</dd>
            </div>
            <div class="node-drawer__fact node-drawer__fact--wide">
              <dt>访问地址</dt>
              <dd>
                <a
                  class="node-drawer__url"
                  :href="agent.base_url"
                  target="_blank"
                  rel="noopener"
                >{{ agent.base_url }}</a>
              </dd>
            </div>
            <div class="node-drawer__fact node-drawer__fact--wide">
              <dt>本机 IP</dt>
              <dd class="mono">{{ agent.host_ips || "—" }}</dd>
            </div>
            <div class="node-drawer__fact node-drawer__fact--wide">
              <dt>发现组</dt>
              <dd>
                <span v-if="!discoveryGroups.length" class="muted">未分配</span>
                <span v-else class="chips">
                  <span v-for="g in discoveryGroups" :key="g" class="chip">{{ g }}</span>
                </span>
              </dd>
            </div>
          </dl>
          <p class="node-drawer__hint">创建与关联请到「发现组」页管理。</p>
        </section>
      </div>

      <footer class="drawer-footer">
        <a
          class="btn btn-primary btn-block"
          :href="agent.base_url"
          target="_blank"
          rel="noopener"
        >
          打开 Node Web UI
        </a>
      </footer>
    </div>
  </aside>
</template>
