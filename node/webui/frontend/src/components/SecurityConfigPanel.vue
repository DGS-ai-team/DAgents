<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const TOOL_GROUPS = [
  { name: "a2a", label: "A2A 协作" },
  { name: "bash", label: "命令行" },
  { name: "browser", label: "浏览器", beta: true },
  { name: "child_agents", label: "子 Agent" },
  { name: "fs", label: "文件" },
  { name: "hitl", label: "人工确认" },
  { name: "skills", label: "技能" },
  { name: "triggers", label: "定时任务" },
];

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

function toggleGroup(name) {
  const set = new Set(form.tools.enabled_groups || []);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  form.tools.enabled_groups = [...set].sort();
}

async function saveSecurity() {
  await save({
    tools: {
      enabled_groups: [...(form.tools.enabled_groups || [])],
      bash_output_encoding: form.tools.bash_output_encoding,
      file_encoding: form.tools.file_encoding,
      bash_compress_enabled: form.tools.bash_compress_enabled,
      bash_compress_max_output_chars: form.tools.bash_compress_max_output_chars,
      bash_compress_max_stderr_chars: form.tools.bash_compress_max_stderr_chars,
    },
    hooks: {
      duplicate_tool_call_enabled: form.hooks.duplicate_tool_call_enabled,
      duplicate_tool_call_window_seconds: form.hooks.duplicate_tool_call_window_seconds,
    },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    title="安全设置"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveSecurity"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">可用工具</h2>
      <p class="settings-section__desc">不勾选任何项表示全部可用；勾选后仅启用已选工具。</p>
      <div class="setup-config-panel__toggles">
        <label v-for="g in TOOL_GROUPS" :key="g.name" class="settings-toggle">
          <input
            type="checkbox"
            :checked="form.tools.enabled_groups?.includes(g.name)"
            @change="toggleGroup(g.name)"
          />
          <span class="settings-toggle__label">
            {{ g.label }}
            <span v-if="g.beta" class="badge badge--beta" title="试验功能">Beta</span>
          </span>
        </label>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">编码</h2>
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
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">命令输出长度</h2>
      <label class="settings-toggle">
        <input v-model="form.tools.bash_compress_enabled" type="checkbox" />
        <span>限制过长输出</span>
      </label>
      <div class="setup-config-panel__field-grid">
        <label class="settings-field">
          <span class="settings-field__label">标准输出最大字符</span>
          <input v-model.number="form.tools.bash_compress_max_output_chars" class="settings-field__input" type="number" min="0" />
        </label>
        <label class="settings-field">
          <span class="settings-field__label">错误输出最大字符</span>
          <input v-model.number="form.tools.bash_compress_max_stderr_chars" class="settings-field__input" type="number" min="0" />
        </label>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">重复操作提醒</h2>
      <p class="settings-section__desc">短时间内重复相同工具调用时提示确认。</p>
      <label class="settings-toggle">
        <input v-model="form.hooks.duplicate_tool_call_enabled" type="checkbox" />
        <span>启用重复调用检测</span>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">检测窗口（秒）</span>
        <input v-model.number="form.hooks.duplicate_tool_call_window_seconds" class="settings-field__input" type="number" min="1" />
      </label>
    </section>
  </ConfigPanelShell>
</template>
