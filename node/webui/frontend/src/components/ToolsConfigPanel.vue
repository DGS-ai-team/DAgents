<script setup>
import { onMounted } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const TOOL_GROUPS = [
  { name: "a2a" },
  { name: "bash" },
  { name: "browser", beta: true },
  { name: "child_agents" },
  { name: "fs" },
  { name: "hitl" },
  { name: "skills" },
  { name: "triggers" },
];

const { loading, saving, error, statusMessage, configPath, configWritable, form, load, save } =
  useSetupConfig();

function toggleGroup(name) {
  const set = new Set(form.tools.enabled_groups || []);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  form.tools.enabled_groups = [...set].sort();
}

async function saveTools() {
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
    title="工具与 Hook"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    @refresh="load"
    @save="saveTools"
  >
    <section class="settings-section">
      <h2 class="settings-section__title">内置工具组</h2>
      <p class="setup-config-panel__hint">留空表示启用全部；勾选为允许列表。标注 Beta 的组功能仍在试验中。</p>
      <div class="setup-config-panel__toggles">
        <label v-for="g in TOOL_GROUPS" :key="g.name" class="settings-toggle">
          <input
            type="checkbox"
            :checked="form.tools.enabled_groups?.includes(g.name)"
            @change="toggleGroup(g.name)"
          />
          <span class="settings-toggle__label">
            {{ g.name }}
            <span v-if="g.beta" class="badge badge--beta" title="试验功能，接口与稳定性可能变更">Beta</span>
          </span>
        </label>
      </div>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">编码</h2>
      <label class="settings-field">
        <span class="settings-field__label">bash 输出编码</span>
        <input v-model="form.tools.bash_output_encoding" class="settings-field__input" type="text" placeholder="utf-8" autocomplete="off" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">文件工具默认编码</span>
        <input v-model="form.tools.file_encoding" class="settings-field__input" type="text" placeholder="utf-8" autocomplete="off" />
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">bash 输出清洗</h2>
      <label class="settings-toggle">
        <input v-model="form.tools.bash_compress_enabled" type="checkbox" />
        <span>启用 bash_compress</span>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">stdout 最大字符</span>
        <input v-model.number="form.tools.bash_compress_max_output_chars" class="settings-field__input" type="number" min="0" />
      </label>
      <label class="settings-field">
        <span class="settings-field__label">stderr 最大字符</span>
        <input v-model.number="form.tools.bash_compress_max_stderr_chars" class="settings-field__input" type="number" min="0" />
      </label>
    </section>

    <section class="settings-section">
      <h2 class="settings-section__title">重复 tool call 检测</h2>
      <label class="settings-toggle">
        <input v-model="form.hooks.duplicate_tool_call_enabled" type="checkbox" />
        <span>启用 duplicate_tool_call Hook</span>
      </label>
      <label class="settings-field">
        <span class="settings-field__label">检测窗口（秒）</span>
        <input v-model.number="form.hooks.duplicate_tool_call_window_seconds" class="settings-field__input" type="number" min="1" />
      </label>
    </section>
  </ConfigPanelShell>
</template>
