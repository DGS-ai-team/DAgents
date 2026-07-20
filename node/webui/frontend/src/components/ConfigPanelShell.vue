<script setup>
defineProps({
  title: { type: String, required: true },
  subtitle: { type: String, default: "" },
  loading: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  configWritable: { type: Boolean, default: false },
  configPath: { type: String, default: "" },
  error: { type: String, default: "" },
  statusMessage: { type: String, default: "" },
});

const emit = defineEmits(["refresh", "save"]);
</script>

<template>
  <section class="panel settings-embedded-panel setup-config-panel">
    <header class="panel__header">
      <div>
        <div class="panel__title">{{ title }}</div>
        <div v-if="subtitle || !configWritable" class="setup-config-panel__subtitle">
          <template v-if="subtitle">{{ subtitle }}</template>
          <span v-if="!configWritable">只读</span>
        </div>
      </div>
      <div class="setup-config-panel__actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading || saving" @click="emit('refresh')">
          刷新
        </button>
        <button
          type="button"
          class="btn btn--primary btn--sm"
          :disabled="loading || saving || !configWritable"
          @click="emit('save')"
        >
          {{ saving ? "保存中…" : "保存" }}
        </button>
      </div>
    </header>

    <div class="panel__body setup-config-panel__body">
      <div v-if="loading && !configPath" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <template v-else>
        <p v-if="statusMessage" class="setup-config-panel__status">{{ statusMessage }}</p>
        <slot />
      </template>
    </div>
  </section>
</template>
