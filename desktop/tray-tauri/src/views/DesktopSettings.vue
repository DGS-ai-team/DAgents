<script setup>
import { invoke } from "@tauri-apps/api/core";
import { computed, onMounted, reactive } from "vue";
import SettingsPageHeader from "@desktop-ui/components/SettingsPageHeader.vue";
import { createDesktopSettingsState, desktopSettingsDirty, loadDesktopSettings, saveDesktopSettings } from "../desktop-settings-state.js";

const state = reactive(createDesktopSettingsState());
const settings = state.settings;
const dirty = computed(() => desktopSettingsDirty(state));
const canSave = computed(() => state.loaded && !state.loading && !state.saving && dirty.value);

async function load() { await loadDesktopSettings(state, invoke); }
async function save() { await saveDesktopSettings(state, invoke); }

onMounted(load);
</script>

<template>
  <section class="settings-page settings-embedded desktop-settings">
    <SettingsPageHeader
      title="Desktop 设置"
      eyebrow="本机应用"
      description="这些选项保存在当前用户的 Desktop 配置中，与 Node 服务设置分开。"
    >
      <template #actions>
        <button class="btn btn--primary" type="button" :disabled="!canSave" @click="save">
          {{ state.saving ? "保存中…" : "保存设置" }}
        </button>
      </template>
    </SettingsPageHeader>

    <p v-if="state.loading" class="settings-section__desc">正在读取本机设置…</p>
    <div v-if="state.error" class="config-panel-error" role="alert">
      <span>{{ state.error }}</span>
      <button class="btn btn--ghost btn--sm" type="button" :disabled="state.loading || state.saving" @click="load">重新读取</button>
    </div>
    <p v-if="state.saved && !dirty" class="settings-section__desc" role="status">已保存。通知与更新检查设置已生效。</p>

    <fieldset class="desktop-settings__fields settings-embedded-panel" :disabled="!state.loaded || state.loading || state.saving">
      <section class="settings-section">
        <h2 class="settings-section__title">通知</h2>
        <label class="settings-toggle">
          <span><span class="settings-field__label">桌面通知</span><small class="settings-field__hint">控制待处理与更新的系统通知。</small></span>
          <input v-model="settings.notification_enabled" type="checkbox" />
        </label>
      </section>

      <section class="settings-section">
        <h2 class="settings-section__title">Desktop 更新</h2>
        <label class="settings-toggle">
          <span><span class="settings-field__label">自动检查更新</span><small class="settings-field__hint">更新状态由 Node 查询，Manage 凭据由 Node 保管。</small></span>
          <input v-model="settings.update_check_enabled" type="checkbox" />
        </label>
        <div class="setup-config-panel__field-grid">
          <label class="settings-field">
            <span><span class="settings-field__label">自动检查间隔</span><small class="settings-field__hint">范围为 1 分钟至 7 天。</small></span>
            <span class="desktop-settings__interval"><input v-model.number="state.intervalMinutes" class="settings-field__input" type="number" min="1" max="10080" /><span>分钟</span></span>
          </label>
        </div>
      </section>
    </fieldset>
  </section>
</template>

<style scoped>
.desktop-settings { gap: 0; }
.desktop-settings__fields { min-width: 0; margin: 0; padding: 0; border: 0; }
.desktop-settings__interval { display: flex; align-items: center; gap: 8px; max-width: 240px; }
.desktop-settings__interval input { width: 130px; }
.config-panel-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; color: var(--color-danger, #b42318); margin: 0 0 14px; }
</style>
