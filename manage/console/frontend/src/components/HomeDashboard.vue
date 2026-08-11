<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import { fetchAgents, fetchHealth, fetchWorkgroups } from "../api.js";
import { computeStats, touchLastRefreshedLabel } from "../utils.js";
import brandIcon from "@dagents-brand/brand-icon.png";

const props = defineProps({
  active: { type: Boolean, default: false },
});

const emit = defineEmits(["navigate", "toast", "refreshed"]);

const loading = ref(false);
const error = ref("");
const healthAgents = ref(0);
const agents = ref([]);
const workgroups = ref([]);

const stats = reactive({
  online: 0,
  offline: 0,
  total: 0,
  workgroupsActive: 0,
  workgroupsTotal: 0,
});

const statusLine = computed(() => {
  if (loading.value && !agents.value.length) return "正在汇总运行状态…";
  return `${stats.online} 个 Node 在线 · ${stats.workgroupsActive} 个工作组进行中`;
});

const modules = [
  {
    id: "workgroup",
    label: "工作组",
    hint: "创建、修改协作组，或进入与协作组协同工作",
    tone: "workgroup",
  },
  {
    id: "templates",
    label: "Agent 模板",
    hint: "维护可复用蓝图，工作组加成员时一键选用",
    tone: "templates",
  },
  {
    id: "marketplace",
    label: "能力市场",
    hint: "上传与分发 Skills、Hooks、External Tools",
    tone: "marketplace",
  },
];

async function loadDashboard() {
  loading.value = true;
  error.value = "";
  try {
    const [health, agentPage, wgs] = await Promise.all([
      fetchHealth().catch(() => ({ agents: 0 })),
      fetchAgents({ status: "all", page: 1, page_size: 200 }),
      fetchWorkgroups().catch(() => []),
    ]);
    healthAgents.value = Number(health?.agents || 0);
    agents.value = agentPage?.agents || [];
    workgroups.value = Array.isArray(wgs) ? wgs : [];
    Object.assign(stats, computeStats(agents.value));
    stats.workgroupsTotal = workgroups.value.length;
    stats.workgroupsActive = workgroups.value.filter((w) => w.status === "active").length;
    emit("refreshed", touchLastRefreshedLabel());
  } catch (err) {
    error.value = err.message || "加载首页失败";
    emit("toast", { message: error.value, type: "error" });
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.active,
  (active) => {
    if (active) void loadDashboard();
  },
);

onMounted(() => {
  if (props.active) void loadDashboard();
});

defineExpose({ refresh: loadDashboard });
</script>

<template>
  <section class="home-dashboard">
    <header class="home-hero">
      <img class="home-hero-mark" :src="brandIcon" alt="" aria-hidden="true" />
      <p class="home-hero-kicker">Overview</p>
      <h1 class="home-hero-status">{{ statusLine }}</h1>
      <p class="home-hero-note">从工作组开始协作，用模板快速构建成员，在能力市场扩展 Node。</p>
    </header>

    <p v-if="error" class="state state-error">{{ error }}</p>

    <section class="home-metrics" aria-label="概览指标">
      <div class="home-metric">
        <span class="home-metric-label">在线 Node</span>
        <strong class="home-metric-value">{{ stats.online }}</strong>
      </div>
      <div class="home-metric">
        <span class="home-metric-label">注册合计</span>
        <strong class="home-metric-value">{{ stats.total || healthAgents }}</strong>
      </div>
      <div class="home-metric">
        <span class="home-metric-label">活跃工作组</span>
        <strong class="home-metric-value">
          {{ stats.workgroupsActive }}<small>/{{ stats.workgroupsTotal }}</small>
        </strong>
      </div>
    </section>

    <nav class="home-modules" aria-label="功能模块">
      <button
        v-for="item in modules"
        :key="item.id"
        type="button"
        class="home-module-card"
        :data-tone="item.tone"
        @click="emit('navigate', item.id)"
      >
        <span class="home-module-icon" aria-hidden="true">
          <svg v-if="item.tone === 'workgroup'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <circle cx="9" cy="8" r="3" />
            <circle cx="16.5" cy="9.5" r="2.5" />
            <path d="M3.5 19c.6-3 2.8-4.5 5.5-4.5s4.9 1.5 5.5 4.5" />
            <path d="M14 14.2c1.4-.7 3.2-.6 4.8.5 1.1.8 1.8 2 2 3.3" />
          </svg>
          <svg v-else-if="item.tone === 'templates'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <rect x="4" y="3" width="16" height="18" rx="2" />
            <path d="M8 8h8M8 12h8M8 16h5" />
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <path d="M4 8l8-4 8 4v8l-8 4-8-4V8z" />
            <path d="M12 12v8M4 8l8 4 8-4" />
          </svg>
        </span>
        <strong class="home-module-title">{{ item.label }}</strong>
        <span class="home-module-hint">{{ item.hint }}</span>
        <span class="home-module-go" aria-hidden="true">进入</span>
      </button>
    </nav>
  </section>
</template>
