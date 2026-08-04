<script setup>
defineProps({
  healthLabel: { type: String, default: "连接中…" },
  healthOnline: { type: Boolean, default: false },
  view: { type: String, required: true },
});

const emit = defineEmits(["navigate"]);
</script>

<template>
  <aside class="sidebar" aria-label="主导航">
    <div class="sidebar-brand">
      <div class="brand-logo" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <rect width="24" height="24" rx="6" fill="var(--primary)" />
          <path d="M6 17V7h2.4v7H15V17H6zm8.4-5V7H17v5h-2.6z" fill="#fff" />
        </svg>
      </div>
      <div class="brand-text">
        <strong>DAgents Manage</strong>
        <span>统一控制面</span>
      </div>
    </div>

    <nav class="sidebar-nav" aria-label="功能模块">
      <button
        type="button"
        class="nav-tab"
        :class="{ active: view === 'registry' }"
        @click="emit('navigate', 'registry')"
      >
        <svg class="nav-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path
            d="M3 4a1 1 0 011-1h12a1 1 0 011 1v2a1 1 0 01-1 1H4a1 1 0 01-1-1V4zM3 10a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H4a1 1 0 01-1-1v-6zM14 9a1 1 0 00-1 1v6a1 1 0 001 1h2a1 1 0 001-1v-6a1 1 0 00-1-1h-2z"
          />
        </svg>
        Agent 目录
      </button>
      <button
        type="button"
        class="nav-tab"
        :class="{ active: view === 'workgroup' }"
        @click="emit('navigate', 'workgroup')"
      >
        <svg class="nav-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M7 9a3 3 0 100-6 3 3 0 000 6zm6 2a3 3 0 100-6 3 3 0 000 6zM2 16a4 4 0 018 0v1H2v-1zm8 1v-1a5.98 5.98 0 00-1.2-3.6A4 4 0 0118 16v1h-8z" />
        </svg>
        工作组
      </button>
      <button
        type="button"
        class="nav-tab"
        :class="{ active: view === 'cases' }"
        @click="emit('navigate', 'cases')"
      >
        <svg class="nav-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M4 3a2 2 0 00-2 2v10a2 2 0 002 2h12a2 2 0 002-2V5a2 2 0 00-2-2H4zm3 2h6v2H7V5zm0 4h6v2H7V9zm0 4h4v2H7v-2z" />
        </svg>
        案例库
      </button>
      <button
        type="button"
        class="nav-tab"
        :class="{ active: view === 'nodeadmin' }"
        @click="emit('navigate', 'nodeadmin')"
      >
        <svg class="nav-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M11.49 3.17c-.38-1.56-2.6-1.56-2.98 0a1.532 1.532 0 01-2.286.948c-1.372-.836-2.942.734-2.106 2.106.54.886.061 2.042-.947 2.287-1.561.379-1.561 2.6 0 2.978a1.532 1.532 0 01.947 2.287c-.836 1.372.734 2.942 2.106 2.106a1.532 1.532 0 012.287.947c.379 1.561 2.6 1.561 2.978 0a1.533 1.533 0 012.287-.947c1.372.836 2.942-.734 2.106-2.106a1.533 1.533 0 01.947-2.287c1.561-.379 1.561-2.6 0-2.978a1.532 1.532 0 01-.947-2.287c.836-1.372-.734-2.942-2.106-2.106a1.532 1.532 0 01-2.287-.947zM10 13a3 3 0 100-6 3 3 0 000 6z" />
        </svg>
        Node 配置
      </button>
    </nav>

    <div class="sidebar-footer">
      <div
        class="pill health-pill"
        :class="healthOnline ? 'pill-online' : 'pill-offline'"
      >
        {{ healthLabel }}
      </div>
      <span class="sidebar-meta">Registry · Workgroup · Console</span>
    </div>
  </aside>
</template>
