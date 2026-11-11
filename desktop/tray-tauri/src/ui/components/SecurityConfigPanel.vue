<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import UiIcon from "./UiIcon.vue";
import SettingsDisclosure from "./SettingsDisclosure.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, dirty, configPath, configWritable, form, load, save } =
  useSetupConfig();

async function saveSecurity() {
  const outputLimit = Number(form.tools.bash_compress_max_output_chars);
  const stderrLimit = Number(form.tools.bash_compress_max_stderr_chars);
  if (!Number.isInteger(outputLimit) || outputLimit <= 0 || !Number.isInteger(stderrLimit) || stderrLimit <= 0) {
    error.value = "输出字符上限必须是大于 0 的整数";
    return;
  }
  await save({
    tools: {
      bash_output_encoding: form.tools.bash_output_encoding,
      file_encoding: form.tools.file_encoding,
      bash_compress_enabled: form.tools.bash_compress_enabled,
      bash_compress_max_output_chars: outputLimit,
      bash_compress_max_stderr_chars: stderrLimit,
    },
    hooks: {
      duplicate_tool_call_enabled: form.hooks.duplicate_tool_call_enabled,
      duplicate_tool_call_window_seconds: form.hooks.duplicate_tool_call_window_seconds,
    },
  });
}

function restoreBashCompressDefaults() {
  form.tools.bash_compress_max_output_chars = form.tools.bash_compress_default_max_output_chars;
  form.tools.bash_compress_max_stderr_chars = form.tools.bash_compress_default_max_stderr_chars;
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="保护策略"
    subtitle="保存后对新发起的工具调用生效；工具 / Shell 审批请到智能体详情配置"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    :dirty="dirty"
    @save="saveSecurity"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">命令输出长度</h2>
      <label class="settings-row">
        <span class="settings-row__copy"><span class="settings-row__title">限制过长输出</span><span class="settings-row__description">清洗并截断超过上限的命令输出。</span></span>
        <input v-model="form.tools.bash_compress_enabled" class="settings-switch-input" type="checkbox" aria-label="限制过长输出" />
      </label>
      <SettingsDisclosure class="security-config__advanced" title="输出上限">
        <div class="setup-config-panel__field-grid">
          <label class="settings-field">
            <span class="settings-field__label">标准输出最大字符</span>
            <input v-model.number="form.tools.bash_compress_max_output_chars" class="settings-field__input" type="number" min="1" step="1" />
          </label>
          <label class="settings-field">
            <span class="settings-field__label">错误输出最大字符</span>
            <input v-model.number="form.tools.bash_compress_max_stderr_chars" class="settings-field__input" type="number" min="1" step="1" />
          </label>
        </div>
        <button class="btn btn--ghost btn--sm desktop-icon-button" type="button" title="恢复默认上限" aria-label="恢复默认上限" @click="restoreBashCompressDefaults">
          <UiIcon name="refresh-cw" :size="15" />
        </button>
      </SettingsDisclosure>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">重复操作提醒</h2>
      <label class="settings-row">
        <span class="settings-row__copy"><span class="settings-row__title">重复调用检测</span><span class="settings-row__description">短时间内重复相同工具调用时提示确认。</span></span>
        <input v-model="form.hooks.duplicate_tool_call_enabled" class="settings-switch-input" type="checkbox" aria-label="重复调用检测" />
      </label>
      <SettingsDisclosure class="security-config__advanced" title="检测窗口">
        <label class="settings-field">
          <span class="settings-field__label">检测窗口（秒）</span>
          <input v-model.number="form.hooks.duplicate_tool_call_window_seconds" class="settings-field__input" type="number" min="1" />
        </label>
      </SettingsDisclosure>
    </section>

    <section class="settings-section security-config__advanced">
      <SettingsDisclosure title="编码设置">
      <p class="settings-section__desc">命令输出与文件读写的默认字节编码；通常保持 UTF-8。</p>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">命令输出编码</span>
          <input v-model="form.tools.bash_output_encoding" class="settings-field__input" type="text" placeholder="utf-8" autocomplete="off" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">文件默认编码</span>
          <input v-model="form.tools.file_encoding" class="settings-field__input" type="text" placeholder="utf-8" autocomplete="off" />
        </label>
      </div>
      </SettingsDisclosure>
    </section>
  </ConfigPanelShell>
</template>

<style scoped>
.security-config__advanced {
  margin-top: 12px;
}
.security-config__advanced .setup-config-panel__field-grid { margin-top: 12px; }
</style>
