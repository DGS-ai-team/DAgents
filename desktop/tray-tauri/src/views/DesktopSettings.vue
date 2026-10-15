<script setup>
import { invoke } from "@tauri-apps/api/core";
import { computed, onMounted, reactive } from "vue";
import SettingsPageHeader from "../../../../node/webui/frontend/src/components/SettingsPageHeader.vue";
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
    <p v-if="state.saved && !dirty" class="settings-section__desc" role="status">已保存。请重启 Desktop，使连接方式、地址、通知和更新检查设置生效。</p>

    <fieldset class="desktop-settings__fields settings-embedded-panel" :disabled="!state.loaded || state.loading || state.saving">
      <section class="settings-section">
        <h2 class="settings-section__title">Node 连接</h2>
        <p class="settings-section__desc">隧道模式只使用你预先建立的 SSH 本地转发，不会自动创建或管理 SSH 隧道。两种模式都只接受 loopback HTTP 地址。隧道模式下请手动填写 Node 机器上的工作区路径，或通过工作区目录 API 浏览；Desktop 不会在本机替远程 Node 打开目录选择器。</p>
        <div class="setup-config-panel__field-grid">
          <label class="settings-field">
            <span><span class="settings-field__label">连接方式</span><small class="settings-field__hint">隧道模式只连接已运行的 Node。</small></span>
            <select v-model="settings.connection_mode" class="settings-field__input">
              <option value="local">本机 Node（Desktop 可管理本机进程）</option>
              <option value="tunnel">已有 SSH 隧道（只连接，不管理 Node）</option>
            </select>
          </label>
          <label class="settings-field">
            <span><span class="settings-field__label">Node loopback 地址</span><small class="settings-field__hint">例如 http://127.0.0.1:18765；隧道模式填本地 SSH 转发监听地址。</small></span>
            <input v-model="settings.node_endpoint" class="settings-field__input" type="url" autocomplete="url" />
          </label>
        </div>
        <p class="settings-section__desc">当前运行连接：{{ state.runtimeConnectionMode === "tunnel" ? "已有 SSH 隧道" : "本机 Node" }} · {{ state.runtimeEndpoint || "—" }}。连接方式或地址保存后需重启 Desktop 生效。</p>
      </section>

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
