<script setup>
import { computed } from "vue";
import { themeStore, toggleTheme } from "../theme.js";

defineProps({
  breadcrumb: { type: String, required: true },
  title: { type: String, required: true },
  subtitle: { type: String, required: true },
  lastRefreshed: { type: String, default: "—" },
  refreshing: { type: Boolean, default: false },
});

const emit = defineEmits(["refresh"]);

const themeLabel = computed(() =>
  themeStore.resolved === "dark" ? "切换浅色主题" : "切换深色主题",
);
</script>

<template>
  <header class="page-header">
    <div class="page-header-text">
      <p class="breadcrumb">Manage / <span>{{ breadcrumb }}</span></p>
      <h1>{{ title }}</h1>
      <p class="page-subtitle">{{ subtitle }}</p>
    </div>
    <div class="page-header-actions">
      <span class="last-refreshed" aria-live="polite">{{ lastRefreshed }}</span>
      <button
        type="button"
        class="theme-toggle"
        :title="themeLabel"
        :aria-label="themeLabel"
        @click="toggleTheme"
      >
        <svg v-if="themeStore.resolved === 'dark'" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <circle cx="8" cy="8" r="2.1" stroke="currentColor" stroke-width="1.2" />
          <path
            d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.2 3.2l1.1 1.1M11.7 11.7l1.1 1.1M3.2 12.8l1.1-1.1M11.7 4.3l1.1-1.1"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linecap="round"
          />
        </svg>
        <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path
            d="M10.9 2.3a5.8 5.8 0 1 0 2.8 10 5.9 5.9 0 0 1-2.8-10Z"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button
        type="button"
        class="btn btn-primary"
        :class="{ 'is-loading': refreshing }"
        :disabled="refreshing"
        @click="emit('refresh')"
      >
        <svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path
            fill-rule="evenodd"
            d="M4 2a1 1 0 011 1v2.101a7.002 7.002 0 0111.601 2.566 1 1 0 11-1.885.666A5.002 5.002 0 005.999 7H9a1 1 0 010 2H4a1 1 0 01-1-1V3a1 1 0 011-1zm.008 9.057a1 1 0 011.276.61A5.002 5.002 0 0014.001 13H11a1 1 0 110-2h5a1 1 0 011 1v5a1 1 0 11-2 0v-2.101a7.002 7.002 0 01-11.601-2.566 1 1 0 01.61-1.276z"
            clip-rule="evenodd"
          />
        </svg>
        刷新
      </button>
    </div>
  </header>
</template>
