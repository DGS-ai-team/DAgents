<script setup>
import { ref } from "vue";
import { pickPlatformDirectory } from "../api/platform.js";

const draft = defineModel("draft", { type: Object, required: true });

const props = defineProps({
  fieldError: { type: String, default: "" },
});

const emit = defineEmits(["clear-error"]);
const selecting = ref(false);
const pickerError = ref("");

function setMode(mode) {
  draft.value.workspaceMode = mode;
  if (mode === "private") draft.value.workspacePath = "";
  pickerError.value = "";
  emit("clear-error");
}

async function chooseDirectory() {
  selecting.value = true;
  pickerError.value = "";
  emit("clear-error");
  try {
    const result = await pickPlatformDirectory();
    const path = String(result?.path || "").trim();
    if (path) {
      draft.value.workspacePath = path;
      return;
    }
    if (!result?.cancelled) pickerError.value = "没有获取到所选目录，请重试或直接输入路径。";
  } catch (error) {
    const message = String(error?.message || "").trim();
    pickerError.value = message || "Node 暂时无法打开系统目录选择器，请直接输入绝对路径。";
  } finally {
    selecting.value = false;
  }
}
</script>

<template>
  <section class="workspace-picker" aria-labelledby="workspace-picker-title">
    <div class="workspace-picker__heading">
      <div>
        <h2 id="workspace-picker-title" class="workspace-picker__title">工作目录</h2>
        <p class="workspace-picker__lead">文件、命令行和本机终端会默认在这里运行。</p>
      </div>
      <span class="workspace-picker__badge">创建后不可修改</span>
    </div>

    <div class="workspace-picker__options" role="radiogroup" aria-label="工作目录类型">
      <label
        class="workspace-picker__option"
        :class="{ 'workspace-picker__option--active': draft.workspaceMode === 'private' }"
      >
        <input
          type="radio"
          name="agent-workspace-mode"
          value="private"
          :checked="draft.workspaceMode === 'private'"
          @change="setMode('private')"
        />
        <span>
          <strong>Agent 私有目录</strong>
          <small>自动创建独立目录，适合大多数任务。</small>
        </span>
      </label>
      <label
        class="workspace-picker__option"
        :class="{ 'workspace-picker__option--active': draft.workspaceMode === 'custom' }"
      >
        <input
          type="radio"
          name="agent-workspace-mode"
          value="custom"
          :checked="draft.workspaceMode === 'custom'"
          @change="setMode('custom')"
        />
        <span>
          <strong>指定本机目录</strong>
          <small>点击选择目录打开系统选择器，也可以直接输入绝对路径。</small>
        </span>
      </label>
    </div>

    <div v-if="draft.workspaceMode === 'custom'" class="workspace-picker__selection">
      <div class="workspace-picker__selection-head">
        <label
          for="agent-workspace-path"
          class="workspace-picker__label"
          :class="{ 'workspace-picker__label--error': props.fieldError }"
        >
          {{ props.fieldError || "工作目录路径" }}
        </label>
        <button
          type="button"
          class="btn btn--ghost btn--sm"
          :disabled="selecting"
          @click="chooseDirectory"
        >
          {{ selecting ? "打开中…" : "选择目录" }}
        </button>
      </div>
      <input
        id="agent-workspace-path"
        v-model="draft.workspacePath"
        class="workspace-picker__path-input"
        type="text"
        inputmode="url"
        autocomplete="off"
        placeholder="例如 C:\Projects\my-agent 或 /home/user/project"
        :aria-invalid="Boolean(props.fieldError)"
        @input="pickerError = ''; emit('clear-error')"
      />
      <p v-if="pickerError" class="workspace-picker__error">{{ pickerError }}</p>
      <p class="workspace-picker__hint">输入 Windows 或 Linux 的绝对路径，创建后不可修改。</p>
    </div>
  </section>
</template>

<style scoped>
.workspace-picker {
  display: flex;
  flex-direction: column;
  gap: 18px;
  width: 100%;
}

.workspace-picker__heading,
.workspace-picker__selection-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.workspace-picker__title {
  margin: 0;
  color: var(--color-text);
  font-size: 16px;
  font-weight: 650;
  letter-spacing: -0.015em;
}

.workspace-picker__lead {
  margin: 5px 0 0;
  color: var(--color-text-muted);
  font-size: 13px;
  line-height: 1.45;
}

.workspace-picker__badge {
  flex: 0 0 auto;
  padding: 4px 8px;
  border: 1px solid var(--color-border);
  border-radius: 999px;
  color: var(--color-text-muted);
  font-size: 11px;
  line-height: 1.2;
  white-space: nowrap;
}

.workspace-picker__options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.workspace-picker__option {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--color-border);
  border-radius: 11px;
  background: var(--color-input, var(--color-surface));
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.workspace-picker__option:hover {
  border-color: color-mix(in srgb, var(--color-primary) 40%, var(--color-border));
}

.workspace-picker__option--active {
  border-color: color-mix(in srgb, var(--color-primary) 55%, var(--color-border));
  background: color-mix(in srgb, var(--color-primary) 8%, var(--color-surface));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--color-primary) 14%, transparent);
}

.workspace-picker__option input {
  flex: 0 0 auto;
  margin-top: 2px;
  accent-color: var(--color-primary);
}

.workspace-picker__option span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.workspace-picker__option strong {
  color: var(--color-text);
  font-size: 13px;
  font-weight: 600;
}

.workspace-picker__option small {
  color: var(--color-text-muted);
  font-size: 11px;
  line-height: 1.45;
}

.workspace-picker__selection {
  display: flex;
  flex-direction: column;
  gap: 9px;
  padding-top: 2px;
}

.workspace-picker__label {
  color: var(--color-text-muted);
  font-size: 12px;
  font-weight: 600;
}

.workspace-picker__label--error {
  color: var(--color-danger);
}

.workspace-picker__error {
  margin: 0;
  color: var(--color-danger);
  font-size: 12px;
  line-height: 1.45;
}

.workspace-picker__path-input {
  width: 100%;
  box-sizing: border-box;
  display: block;
  padding: 10px 12px;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: var(--color-surface-muted);
  color: var(--color-text);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Consolas, monospace);
  font-size: 12px;
  line-height: 1.4;
}

.workspace-picker__path-input::placeholder {
  color: var(--color-text-subtle);
}

.workspace-picker__path-input:focus {
  border-color: var(--color-primary);
  outline: 2px solid color-mix(in srgb, var(--color-primary) 22%, transparent);
  outline-offset: 1px;
}

.workspace-picker__hint {
  margin: 0;
  color: var(--color-text-subtle);
  font-size: 12px;
  line-height: 1.45;
}

@media (max-width: 640px) {
  .workspace-picker__options {
    grid-template-columns: 1fr;
  }

  .workspace-picker__heading,
  .workspace-picker__selection-head {
    align-items: flex-start;
    flex-direction: column;
    gap: 9px;
  }
}
</style>
