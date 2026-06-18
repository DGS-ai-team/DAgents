<script setup>
defineProps({
  online: { type: [Number, String], default: "—" },
  offline: { type: [Number, String], default: "—" },
  total: { type: [Number, String], default: "—" },
  peers: { type: [Number, String], default: "—" },
});
</script>

<template>
  <section class="stats-row" aria-label="概览指标">
    <article class="stat-card stat-online">
      <div class="stat-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M22 11.08V12a10 10 0 11-5.93-9.14" />
          <polyline points="22 4 12 14.01 9 11.01" />
        </svg>
      </div>
      <div class="stat-body">
        <span class="stat-label">在线 Node</span>
        <strong>{{ online }}</strong>
      </div>
    </article>
    <article class="stat-card stat-offline">
      <div class="stat-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
      </div>
      <div class="stat-body">
        <span class="stat-label">离线</span>
        <strong>{{ offline }}</strong>
      </div>
    </article>
    <article class="stat-card stat-total">
      <div class="stat-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="2" y="3" width="20" height="14" rx="2" />
          <line x1="8" y1="21" x2="16" y2="21" />
          <line x1="12" y1="17" x2="12" y2="21" />
        </svg>
      </div>
      <div class="stat-body">
        <span class="stat-label">注册合计</span>
        <strong>{{ total }}</strong>
      </div>
    </article>
    <article class="stat-card stat-peers">
      <div class="stat-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="18" cy="5" r="3" />
          <circle cx="6" cy="12" r="3" />
          <circle cx="18" cy="19" r="3" />
          <line x1="8.59" y1="13.51" x2="15.42" y2="17.49" />
          <line x1="15.41" y1="6.51" x2="8.59" y2="10.49" />
        </svg>
      </div>
      <div class="stat-body">
        <span class="stat-label">A2A 可达</span>
        <strong>{{ peers }}</strong>
      </div>
    </article>
  </section>
</template>
