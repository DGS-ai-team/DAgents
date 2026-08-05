<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";

const emit = defineEmits(["completed"]);

const loading = ref(true);
const saving = ref(false);
const error = ref("");
const preferredName = ref("");
const nodeName = ref("");
const description = ref("");

const canSubmit = computed(() => {
  return preferredName.value.trim().length > 0 && nodeName.value.trim().length > 0 && !saving.value;
});

onMounted(async () => {
  loading.value = true;
  error.value = "";
  try {
    const boot = await api.getUIBootstrap();
    preferredName.value = String(boot?.user?.preferred_name || "").trim();
    nodeName.value = String(boot?.agent?.name || "").trim() || "local-assistant";
    const setup = await api.getSetupConfig();
    if (setup?.agent?.description) {
      description.value = String(setup.agent.description);
    }
    if (setup?.user?.preferred_name) {
      preferredName.value = String(setup.user.preferred_name);
    }
    if (setup?.agent?.name) {
      nodeName.value = String(setup.agent.name);
    }
  } catch (e) {
    error.value = e.message || "加载失败";
  } finally {
    loading.value = false;
  }
});

async function submit() {
  if (!canSubmit.value) return;
  saving.value = true;
  error.value = "";
  try {
    await api.patchSetupConfig({
      user: { preferred_name: preferredName.value.trim() },
      agent: {
        name: nodeName.value.trim(),
        description: description.value || "",
      },
      onboarding: { node_profile_completed: true },
    });
    emit("completed");
  } catch (e) {
    error.value = e.message || "保存失败";
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="first-run">
    <div class="first-run__glow" aria-hidden="true" />
    <div class="first-run__panel">
      <p class="first-run__brand">DAgents</p>
      <h1 class="first-run__title">开始使用</h1>
      <p class="first-run__lead">先告诉我们怎么称呼你，以及这台 Node 的名称。</p>

      <div v-if="loading" class="first-run__hint">加载中…</div>
      <form v-else class="first-run__form" @submit.prevent="submit">
        <label class="first-run__field">
          <span class="first-run__label">怎么称呼你</span>
          <input
            v-model="preferredName"
            class="first-run__input"
            type="text"
            maxlength="64"
            placeholder="例如：小明"
            autocomplete="nickname"
            autofocus
          />
        </label>
        <label class="first-run__field">
          <span class="first-run__label">Node 名称</span>
          <input
            v-model="nodeName"
            class="first-run__input"
            type="text"
            maxlength="64"
            placeholder="注册到 Manage 后的展示名"
            autocomplete="off"
          />
        </label>
        <p v-if="error" class="first-run__error">{{ error }}</p>
        <button class="first-run__cta" type="submit" :disabled="!canSubmit">
          {{ saving ? "保存中…" : "开始使用" }}
        </button>
      </form>
    </div>
  </div>
</template>

<style scoped>
.first-run {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: var(--space-6);
  position: relative;
  overflow: hidden;
  background:
    radial-gradient(1200px 600px at 12% -10%, rgba(0, 120, 212, 0.22), transparent 55%),
    radial-gradient(900px 500px at 90% 110%, rgba(96, 205, 255, 0.12), transparent 50%),
    linear-gradient(160deg, #1a1a1a 0%, #202020 45%, #181818 100%);
}

.first-run__glow {
  position: absolute;
  inset: auto auto 18% 50%;
  width: 42rem;
  height: 42rem;
  transform: translateX(-50%);
  background: radial-gradient(circle, rgba(0, 120, 212, 0.14), transparent 65%);
  pointer-events: none;
  animation: first-run-pulse 6s ease-in-out infinite;
}

.first-run__panel {
  width: min(420px, 100%);
  position: relative;
  z-index: 1;
  animation: first-run-rise 0.55s ease-out both;
}

.first-run__brand {
  margin: 0 0 var(--space-3);
  font-size: 1.75rem;
  font-weight: 650;
  letter-spacing: 0.04em;
  color: var(--color-text);
}

.first-run__title {
  margin: 0 0 var(--space-2);
  font-size: 1.125rem;
  font-weight: 600;
  color: var(--color-text-muted);
}

.first-run__lead {
  margin: 0 0 var(--space-6);
  color: var(--color-text-subtle);
  line-height: 1.55;
}

.first-run__form {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.first-run__field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.first-run__label {
  font-size: 0.8125rem;
  color: var(--color-text-muted);
}

.first-run__input {
  height: 40px;
  padding: 0 var(--space-3);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: var(--color-input);
  color: var(--color-text);
  font: inherit;
  outline: none;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}

.first-run__input:focus {
  border-color: var(--color-primary);
  box-shadow: 0 0 0 1px var(--color-primary-soft);
}

.first-run__cta {
  margin-top: var(--space-2);
  height: 40px;
  border: none;
  border-radius: var(--radius-md);
  background: var(--color-primary);
  color: #fff;
  font: inherit;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s ease, transform 0.15s ease, opacity 0.15s ease;
}

.first-run__cta:hover:not(:disabled) {
  background: var(--color-primary-strong);
  transform: translateY(-1px);
}

.first-run__cta:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.first-run__error {
  margin: 0;
  color: var(--color-danger);
  font-size: 0.875rem;
}

.first-run__hint {
  color: var(--color-text-subtle);
}

@keyframes first-run-rise {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes first-run-pulse {
  0%,
  100% {
    opacity: 0.7;
    transform: translateX(-50%) scale(1);
  }
  50% {
    opacity: 1;
    transform: translateX(-50%) scale(1.06);
  }
}

@media (max-width: 520px) {
  .first-run {
    place-items: start center;
    padding-top: 18vh;
  }
}
</style>
