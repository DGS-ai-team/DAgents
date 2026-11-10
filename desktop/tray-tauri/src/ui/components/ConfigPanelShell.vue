<script setup>
import SettingsSaveBar from "./SettingsSaveBar.vue";
import SettingsPageHeader from "./SettingsPageHeader.vue";

defineProps({
  title: { type: String, required: true },
  subtitle: { type: String, default: "" },
  loading: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  configWritable: { type: Boolean, default: false },
  configPath: { type: String, default: "" },
  error: { type: String, default: "" },
  statusMessage: { type: String, default: "" },
  dirty: { type: Boolean, default: false },
});

const emit = defineEmits(["save"]);
</script>

<template>
  <section class="panel settings-embedded-panel setup-config-panel">
    <SettingsPageHeader
      class="config-panel-shell__header"
      :title="title"
      :description="[subtitle, !configWritable ? '只读' : ''].filter(Boolean).join(' · ')"
    />

    <div class="panel__body setup-config-panel__body">
      <div v-if="loading && !configPath" class="command-panel__loading">加载中…</div>
      <template v-else>
        <fieldset class="config-panel-shell__fields" :disabled="loading || saving || !configWritable">
          <slot v-if="configPath || configWritable" />
        </fieldset>
      </template>
    </div>
    <SettingsSaveBar :status="statusMessage || (dirty ? '有未保存的更改' : '修改后请保存。')" :error="error" :saving="saving" :disabled="loading || !configWritable" @save="emit('save')" />
  </section>
</template>

<style scoped>
.config-panel-shell__fields { min-width: 0; margin: 0; padding: 0; border: 0; }
</style>
