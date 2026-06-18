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
        :class="{ active: view === 'llm' }"
        @click="emit('navigate', 'llm')"
      >
        <svg class="nav-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M11 3a1 1 0 10-2 0v1a1 1 0 102 0V3zM15.657 5.757a1 1 0 00-1.414-1.414l-.707.707a1 1 0 001.414 1.414l.707-.707zM18 10a1 1 0 01-1 1h-1a1 1 0 110-2h1a1 1 0 011 1zM5.05 6.464A1 1 0 106.464 5.05l-.707-.707a1 1 0 00-1.414 1.414l.707.707zM5 10a1 1 0 01-1 1H3a1 1 0 110-2h1a1 1 0 011 1zM8 16v-1h4v1a2 2 0 11-4 0zM12 14c.015-.34.208-.646.477-.859a4 4 0 10-4.954 0c.27.213.462.519.476.859h4.002z" />
        </svg>
        LLM 配置
      </button>
      <button
        type="button"
        class="nav-tab"
        :class="{ active: view === 'skills' }"
        @click="emit('navigate', 'skills')"
      >
        <svg class="nav-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M9 4.804A7.968 7.968 0 005.5 4c-1.255 0-2.443.29-3.5.804v10A7.969 7.969 0 015.5 14c1.669 0 3.218.51 4.5 1.385A7.962 7.962 0 0114.5 14c1.255 0 2.443.29 3.5.804v-10A7.968 7.968 0 0014.5 4c-1.255 0-2.443.29-3.5.804V12a1 1 0 11-2 0V4.804z" />
        </svg>
        Skills
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
