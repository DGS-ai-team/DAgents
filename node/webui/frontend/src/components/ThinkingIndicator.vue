<template>
  <div class="thinking-indicator" role="status" aria-label="思考中">
    <span class="thinking-indicator__orb" aria-hidden="true" />
    <span class="thinking-indicator__waves" aria-hidden="true">
      <span /><span /><span /><span />
    </span>
    <span class="thinking-indicator__label">思考中</span>
  </div>
</template>

<style scoped>
.thinking-indicator {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px 4px 6px;
  border-radius: 999px;
  background: var(--color-info-soft);
  border: 1px solid var(--color-info);
  color: var(--color-text-subtle);
  font-size: 12px;
  line-height: 1.45;
}

.thinking-indicator__orb {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-info);
  box-shadow: 0 0 0 3px var(--color-info-soft);
  animation: thinking-orb-pulse 1.2s ease-in-out infinite;
}

.thinking-indicator__waves {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 12px;
}

.thinking-indicator__waves span {
  display: block;
  width: 3px;
  height: 4px;
  border-radius: 2px;
  background: var(--color-info);
  animation: thinking-wave 0.85s ease-in-out infinite;
}

.thinking-indicator__waves span:nth-child(1) {
  animation-delay: 0s;
}
.thinking-indicator__waves span:nth-child(2) {
  animation-delay: 0.12s;
}
.thinking-indicator__waves span:nth-child(3) {
  animation-delay: 0.24s;
}
.thinking-indicator__waves span:nth-child(4) {
  animation-delay: 0.36s;
}

.thinking-indicator__label {
  font-weight: 500;
  letter-spacing: 0.02em;
  color: var(--color-text-muted);
}

@keyframes thinking-orb-pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.6;
    transform: scale(0.88);
  }
}

@keyframes thinking-wave {
  0%,
  100% {
    height: 4px;
    opacity: 0.35;
  }
  50% {
    height: 12px;
    opacity: 1;
  }
}
</style>
