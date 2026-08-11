<script setup>
import brandIcon from "../assets/brand-icon.png";

defineProps({
  label: { type: String, default: "思考中" },
  mode: { type: String, default: "thinking" },
  showLabel: { type: Boolean, default: true },
  compact: { type: Boolean, default: false },
});
</script>

<template>
  <span
    class="brand-activity"
    :class="{ 'brand-activity--compact': compact, 'brand-activity--icon-only': !showLabel }"
    :data-mode="mode"
    role="status"
    :aria-label="label"
  >
    <span class="brand-activity__frame" aria-hidden="true">
      <img class="brand-activity__mark" :src="brandIcon" alt="" />
    </span>
    <span v-if="showLabel" class="brand-activity__label">{{ label }}</span>
  </span>
</template>

<style scoped>
.brand-activity {
  display: inline-flex;
  align-items: center;
  column-gap: 8px;
  color: var(--color-text-subtle, var(--chat-text-muted, #8893a7));
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.45;
  font-weight: 500;
}

.brand-activity__frame {
  display: inline-grid;
  flex: 0 0 28px;
  width: 28px;
  height: 28px;
  place-items: center;
  line-height: 0;
}

.brand-activity__mark {
  display: block;
  width: 23px;
  height: 23px;
  object-fit: contain;
  transform-origin: center;
  animation: brand-activity-think 1.8s ease-in-out infinite;
}

.brand-activity__label {
  letter-spacing: 0.04em;
}

.brand-activity--compact {
  column-gap: 4px;
}

.brand-activity--compact .brand-activity__frame {
  flex-basis: 20px;
  width: 20px;
  height: 20px;
}

.brand-activity--compact .brand-activity__mark {
  width: 17px;
  height: 17px;
}

.brand-activity--icon-only {
  vertical-align: middle;
}

.brand-activity--icon-only .brand-activity__frame {
  margin: 0;
}

.brand-activity[data-mode="generating"] .brand-activity__mark {
  animation-name: brand-activity-generate;
  animation-duration: 1.35s;
}

.brand-activity[data-mode="tool"] .brand-activity__mark {
  animation-name: brand-activity-tool;
  animation-duration: 1.15s;
}

@keyframes brand-activity-think {
  0%,
  100% {
    opacity: 0.82;
    transform: translate3d(0, 0, 0) rotate(0deg) scale(0.98);
    filter: drop-shadow(0 0 0 transparent);
  }
  22% {
    opacity: 0.96;
    transform: translate3d(0, -0.5px, 0) rotate(0deg) scale(1);
    filter: drop-shadow(0 0 3px color-mix(in srgb, var(--primary, #4f8cff) 34%, transparent));
  }
  46% {
    opacity: 1;
    transform: translate3d(0, -0.5px, 0) rotate(3deg) scale(1.04);
    filter: drop-shadow(0 0 5px color-mix(in srgb, var(--primary, #4f8cff) 40%, transparent));
  }
  68% {
    opacity: 0.94;
    transform: translate3d(0, 0, 0) rotate(0deg) scale(1.01);
    filter: drop-shadow(0 0 3px color-mix(in srgb, var(--primary, #4f8cff) 30%, transparent));
  }
}

@keyframes brand-activity-generate {
  0%,
  100% {
    opacity: 0.82;
    transform: translate3d(0, 0, 0) rotate(0deg) scale(0.98);
    filter: drop-shadow(0 0 0 transparent);
  }
  14% {
    opacity: 0.96;
    transform: translate3d(0, -0.5px, 0) rotate(0deg) scale(1);
  }
  28% {
    transform: translate3d(0, -0.5px, 0) rotate(8deg) scale(1.03);
    filter: drop-shadow(0 0 4px color-mix(in srgb, var(--primary, #4f8cff) 38%, transparent));
  }
  42% {
    transform: translate3d(0, 0, 0) rotate(16deg) scale(0.99);
  }
  56% {
    opacity: 1;
    transform: translate3d(0, -0.5px, 0) rotate(24deg) scale(1.04);
    filter: drop-shadow(0 0 5px color-mix(in srgb, var(--primary, #4f8cff) 42%, transparent));
  }
  70% {
    transform: translate3d(0, 0, 0) rotate(32deg) scale(1.01);
  }
  84% {
    transform: translate3d(0, -0.5px, 0) rotate(40deg) scale(1.03);
    filter: drop-shadow(0 0 4px color-mix(in srgb, var(--primary, #4f8cff) 34%, transparent));
  }
}

@keyframes brand-activity-tool {
  0%,
  100% {
    opacity: 0.82;
    transform: translate3d(0, 0, 0) rotate(0deg) scale(0.98);
  }
  18% {
    opacity: 0.96;
    transform: translate3d(0, -0.5px, 0) rotate(-4deg) scale(1.01);
  }
  36% {
    transform: translate3d(0, 0.5px, 0) rotate(4deg) scale(0.99);
  }
  54% {
    opacity: 1;
    transform: translate3d(0, -0.5px, 0) rotate(-4deg) scale(1.02);
    filter: drop-shadow(0 0 4px color-mix(in srgb, var(--primary, #4f8cff) 36%, transparent));
  }
  72% {
    transform: translate3d(0, 0, 0) rotate(0deg) scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .brand-activity__mark {
    animation: none !important;
    opacity: 0.9;
    transform: none;
    filter: none;
  }
}
</style>
