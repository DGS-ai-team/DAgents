<script setup>
import { onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import AutoBadge from "../components/AutoBadge.vue";
import NavRail from "../components/NavRail.vue";

const route = useRoute();
const router = useRouter();
const mobileNavOpen = ref(false);
const agentId = ref(String(route.params.agentId || ""));
const agent = ref(null);
const autonomy = ref(null);
const cycles = ref([]);
const cyclePage = ref(1);
const cycleTotal = ref(0);
const expanded = ref(new Set());
const runs = ref({});
const runErrors = ref({});
const loading = ref(true);
const error = ref("");
let seq = 0;
let disposed = false;

const labels = {
  active: "已启用",
  waiting: "等待处理",
  paused: "已暂停",
  stopped: "已停止",
  completed: "已完成",
  failed: "需处理",
  disabled: "已停用",
  standby: "待命",
  needs_attention: "需处理",
  running: "执行中",
};

const statusLabel = (status) => labels[status] || status || "未配置";
const summary = () => autonomy.value?.summary || {};
const date = (value) => (value ? new Date(value).toLocaleString() : "—");

async function load() {
  const token = ++seq;
  loading.value = true;
  error.value = "";
  try {
    const id = agentId.value;
    const currentAgent = await api.getAgent(id);
    if (disposed || token !== seq) return;
    agent.value = currentAgent;
    if (currentAgent?.agent_type !== "auto") return;
    const [profile, cyclePageData] = await Promise.all([
      api.getAgentAutonomy(id),
      api.getAgentAutonomyCycles(id, {
        page: cyclePage.value,
        page_size: 20,
      }),
    ]);
    if (disposed || token !== seq) return;
    autonomy.value = profile;
    cycles.value = cyclePageData?.items || [];
    cycleTotal.value = Number(cyclePageData?.total || 0);
  } catch (exception) {
    if (!disposed && token === seq) {
      error.value = exception?.message || "无法加载自主任务";
    }
  } finally {
    if (!disposed && token === seq) loading.value = false;
  }
}

async function toggle(cycle, force = false) {
  if (expanded.value.has(cycle.id) && !force) {
    expanded.value.delete(cycle.id);
    return;
  }
  expanded.value.add(cycle.id);
  const token = seq;
  try {
    const result = (await api.getGoalRuns(cycle.id))?.runs || [];
    if (!disposed && token === seq) runs.value[cycle.id] = result;
  } catch (exception) {
    if (!disposed && token === seq) {
      runErrors.value[cycle.id] = exception?.message || "无法加载运行历史";
    }
  }
}

function retryRuns(cycle) {
  delete runErrors.value[cycle.id];
  void toggle(cycle, true);
}

function changeCyclePage(delta) {
  cyclePage.value += delta;
  void load();
}

onMounted(load);
watch(
  () => route.params.agentId,
  (id) => {
    seq += 1;
    agentId.value = String(id || "");
    cyclePage.value = 1;
    agent.value = null;
    autonomy.value = null;
    cycles.value = [];
    runs.value = {};
    runErrors.value = {};
    expanded.value = new Set();
    void load();
  },
);
onUnmounted(() => {
  disposed = true;
  seq += 1;
});
</script>

<template>
  <div class="app__body app__body--chat-v61 auto-work-shell">
    <button
      type="button"
      class="mobile-agent-nav-toggle"
      :aria-expanded="mobileNavOpen ? 'true' : 'false'"
      @click="mobileNavOpen = !mobileNavOpen"
    >
      {{ mobileNavOpen ? "收起 Agent 列表" : "选择 Agent" }}
    </button>
    <aside
      class="app__col app__col--agents"
      :class="{ 'app__col--agents-mobile-open': mobileNavOpen }"
    >
      <NavRail
        @switch="(id) => {
          mobileNavOpen = false;
          router.push({ name: 'agents', params: { agentId: id } });
        }"
        @create="router.push({ name: 'agents', query: { createAgent: '1' } })"
      />
    </aside>
    <main class="auto-work">
      <header>
      <button type="button" @click="router.push({ name: 'auto-overview' })">
        ← Auto 总览
      </button>
      <div v-if="agent" class="auto-work__identity">
        <h1>
          {{ agent.display_name || agent.agent_id }}
          <AutoBadge :agent="agent" />
        </h1>
        <p>{{ autonomy?.profile?.role_objective || "尚未配置岗位职责" }}</p>
        <small>{{ summary().state_reason || "" }}</small>
      </div>
      <span class="auto-work__top-links">
        <router-link :to="{ name: 'agents', params: { agentId } }">打开聊天</router-link>
        <router-link :to="{ name: 'settings-agent-detail', params: { agentId } }">设置</router-link>
        <button type="button" @click="load">刷新</button>
      </span>
    </header>

    <p v-if="loading" role="status">加载中…</p>
    <p v-else-if="error" role="alert">
      {{ error }}
      <button type="button" @click="load">重试</button>
    </p>
    <section v-else-if="agent && agent.agent_type !== 'auto'" class="auto-work__empty">
      <h2>这是普通 Agent</h2>
      <p>自主任务工作页只适用于 Auto Agent。</p>
      <button type="button" @click="router.push({ name: 'agents', params: { agentId } })">
        打开聊天
      </button>
      <button type="button" @click="router.push({ name: 'settings-agent-detail', params: { agentId } })">
        打开设置
      </button>
    </section>
    <template v-else>
      <section class="auto-work__summary">
        <span>状态：{{ statusLabel(summary().state) }}</span>
        <span>目录：{{ agent?.workspace?.path || "独立工作目录" }}</span>
        <span>下次安排：{{ date(summary().next_at) }}</span>
        <span>
          累计用量：{{ Number(autonomy?.usage?.business_tokens || 0) + Number(autonomy?.usage?.maintenance_tokens || 0) }}
          tokens
        </span>
      </section>

      <section v-if="autonomy?.current_cycle || autonomy?.limits" class="auto-work__card">
        <h2>{{ autonomy.current_cycle?.title || autonomy.limits?.title || "当前任务" }}</h2>
        <p>{{ autonomy.current_cycle?.objective || autonomy.limits?.objective }}</p>
        <p>验收：{{ autonomy.current_cycle?.acceptance || autonomy.limits?.acceptance }}</p>
        <p v-if="autonomy.progress?.summary">最近成果：{{ autonomy.progress.summary }}</p>
        <p v-if="autonomy.progress?.next_steps?.length">
          下一步：{{ autonomy.progress.next_steps.join("；") }}
        </p>
        <router-link v-if="agentId" :to="{ name: 'agents', params: { agentId } }">打开聊天</router-link>
        <router-link :to="{ name: 'settings-agent-detail', params: { agentId } }">自主任务设置</router-link>
      </section>

      <section class="auto-work__card">
        <h2>历史周期</h2>
        <p v-if="!cycles.length">暂无周期记录</p>
        <article v-for="cycle in cycles" :key="cycle.id">
          <button type="button" @click="toggle(cycle)">
            {{ cycle.title || cycle.objective }} · {{ statusLabel(cycle.status) }}
          </button>
          <p v-if="runErrors[cycle.id]" role="alert">
            {{ runErrors[cycle.id] }}
            <button type="button" @click="retryRuns(cycle)">重试</button>
          </p>
          <ul v-if="expanded.has(cycle.id) && !runErrors[cycle.id]">
            <li v-for="run in runs[cycle.id] || []" :key="run.id">
              {{ statusLabel(run.status) }} · {{ date(run.started_at) }}
            </li>
          </ul>
        </article>
        <div v-if="cycleTotal > 20" class="auto-work__pages">
          <button type="button" :disabled="cyclePage <= 1" @click="changeCyclePage(-1)">上一页</button>
          <span>第 {{ cyclePage }} 页</span>
          <button type="button" :disabled="cyclePage * 20 >= cycleTotal" @click="changeCyclePage(1)">下一页</button>
        </div>
      </section>
    </template>
    </main>
  </div>
</template>

<style scoped>
.auto-work-shell {
  display: flex;
  min-height: 100vh;
}

.auto-work {
  box-sizing: border-box;
  width: min(1080px, 100%);
  margin: 0 auto;
  padding: 32px clamp(16px, 4vw, 48px);
  color: var(--color-text);
}

.auto-work a {
  color: var(--color-accent);
  text-decoration: none;
}

.auto-work a:hover {
  text-decoration: underline;
}

.auto-work a:focus-visible {
  border-radius: 4px;
  outline: 2px solid var(--color-accent);
  outline-offset: 3px;
}

header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}

header button,
.auto-work button {
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  padding: 8px 12px;
  background: var(--color-surface-elevated);
  color: var(--color-text);
}

.auto-work__identity {
  min-width: 0;
  flex: 1;
}

h1 {
  min-width: 0;
  margin: 0 0 8px;
  overflow-wrap: anywhere;
}

.auto-work__identity p,
.auto-work__identity small,
.auto-work__summary span,
.auto-work__card p,
.auto-work__card li {
  overflow-wrap: anywhere;
}

.auto-work__top-links {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
}

.auto-work__top-links a {
  overflow-wrap: anywhere;
}

.auto-work__summary,
.auto-work__card {
  border-top: 1px solid var(--border-subtle);
  padding: 18px 0;
}

.auto-work__summary {
  display: flex;
  flex-wrap: wrap;
  gap: 18px;
  color: var(--text-secondary);
}

.auto-work__card {
  display: grid;
  gap: 10px;
  min-width: 0;
}

.auto-work__card article {
  min-width: 0;
  border-bottom: 1px solid var(--border-subtle);
  padding: 10px 0;
}

.auto-work__card article > button {
  max-width: 100%;
  overflow-wrap: anywhere;
  text-align: left;
}

.auto-work__empty {
  padding: 48px 0;
  text-align: center;
}

.auto-work__pages {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

@media (max-width: 720px) {
  .auto-work {
    padding: 22px 16px;
  }

  header {
    align-items: stretch;
    flex-direction: column;
  }

  .auto-work__top-links {
    justify-content: flex-start;
  }

  .auto-work__summary {
    display: grid;
    gap: 8px;
  }

  .auto-work-shell {
    min-width: 0;
  }
}
</style>
