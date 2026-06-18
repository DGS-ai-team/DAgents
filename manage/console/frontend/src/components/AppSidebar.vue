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
        <svg viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
          <rect width="32" height="32" rx="8" fill="url(#brand-grad)" />
          <path d="M8 22V10h3.2v9.2H20V22H8zm11.2-6.4V10H22v5.6h-2.8z" fill="#fff" />
          <defs>
            <linearGradient id="brand-grad" x1="4" y1="4" x2="28" y2="28">
              <stop stop-color="#6366f1" />
              <stop offset="1" stop-color="#312e81" />
            </linearGradient>
          </defs>
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
        :class="{ active: view === 'inbox' }"
        @click="emit('navigate', 'inbox')"
      >
        <svg class="nav-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M2.003 5.884L10 9.882l7.997-3.998A2 2 0 0016 4H4a2 2 0 00-1.997 1.884z" />
          <path d="M18 8.118l-8 4-8-4V14a2 2 0 002 2h12a2 2 0 002-2V8.118z" />
        </svg>
        A2A Inbox
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
        Node 管理
      </button>
    </nav>

    <div class="sidebar-footer">
      <div
        class="pill health-pill"
        :class="healthOnline ? 'pill-online' : 'pill-offline'"
      >
        {{ healthLabel }}
      </div>
      <span class="sidebar-meta">Registry · A2A · Console</span>
    </div>
  </aside>
</template>
