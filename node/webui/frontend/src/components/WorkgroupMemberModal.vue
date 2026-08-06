<script setup>
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";

// catalog 失败时的离线兜底：与 shared catalog default=true（仅 fs）对齐；不含 bash。
const FALLBACK_TOOLS = [
  { id: "read_file", label: "读文件", hint: "read_file" },
  { id: "show_image", label: "展示图片", hint: "show_image" },
  { id: "read_image", label: "读图片", hint: "read_image（需多模态）" },
  { id: "write_file", label: "写文件", hint: "write_file" },
  { id: "glob_files", label: "列目录", hint: "glob_files" },
  { id: "grep_file", label: "单文件搜索", hint: "grep_file" },
  { id: "grep_files", label: "多文件搜索", hint: "grep_files" },
  { id: "search_replace", label: "替换内容", hint: "search_replace" },
];

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: "create" }, // create | edit
  workgroupId: { type: String, default: "" },
  memberId: { type: String, default: "" },
  defaultHomeNodeId: { type: String, default: "" },
});

const emit = defineEmits(["close", "saved"]);

const toolOptions = ref([...FALLBACK_TOOLS]);
const defaultTools = ref(FALLBACK_TOOLS.map((t) => t.id));
const draft = reactive(emptyDraft());
const busy = ref(false);
const loadingSpec = ref(false);
const error = ref("");
const advancedOpen = ref(false);
const nameInput = ref(null);

const title = computed(() => (props.mode === "create" ? "添加成员" : "配置成员"));
const primaryLabel = computed(() => {
  if (busy.value) return props.mode === "create" ? "添加中…" : "保存中…";
  return props.mode === "create" ? "添加" : "保存";
});
const canSubmit = computed(() => {
  if (busy.value || loadingSpec.value) return false;
  return String(draft.displayName || "").trim().length > 0;
});

function emptyDraft() {
  return {
    displayName: "",
    homeNodeId: "",
    tools: [...defaultTools.value],
    soulMd: "",
    customMd: "",
    llmProfileId: "",
  };
}

function toggleTool(id) {
  const set = new Set(draft.tools);
  if (set.has(id)) set.delete(id);
  else set.add(id);
  draft.tools = [...set];
}

function onBackdropClick(event) {
  if (busy.value) return;
  if (event.target === event.currentTarget) emit("close");
}

async function loadToolCatalog() {
  try {
    const catalog = await api.getMemberToolCatalog();
    const tools = Array.isArray(catalog?.tools) ? catalog.tools : [];
    const mapped = tools
      .map((t) => ({
        id: String(t.id || "").trim(),
        label: String(t.label || t.id || "").trim(),
        hint: String(t.hint || t.id || "").trim(),
      }))
      .filter((t) => t.id);
    if (mapped.length) toolOptions.value = mapped;
    const defaults = Array.isArray(catalog?.default_allow_names)
      ? catalog.default_allow_names.map(String).filter(Boolean)
      : mapped.map((t) => t.id);
    if (defaults.length) defaultTools.value = defaults;
  } catch {
    /* 保留 FALLBACK */
  }
}

async function resetFromProps() {
  error.value = "";
  busy.value = false;
  advancedOpen.value = false;
  Object.assign(draft, emptyDraft());
  draft.homeNodeId = String(props.defaultHomeNodeId || "").trim();

  if (props.mode !== "edit" || !props.workgroupId || !props.memberId) {
    await nextTick();
    nameInput.value?.focus?.();
    return;
  }

  loadingSpec.value = true;
  try {
    const spec = await api.getWorkgroupMemberSpec(props.workgroupId, props.memberId);
    draft.displayName = String(spec?.display_name || "").trim();
    draft.homeNodeId = String(spec?.home_node_id || props.defaultHomeNodeId || "").trim();
    const allow = Array.isArray(spec?.tools?.allow_names) ? spec.tools.allow_names : [];
    draft.tools = allow.length ? allow.map(String) : [...defaultTools.value];
    draft.soulMd = String(spec?.prompt?.soul_md || "");
    draft.customMd = String(spec?.prompt?.custom_md || "");
    draft.llmProfileId = String(spec?.llm_profile_id || "").trim();
    if (draft.soulMd.trim() || draft.customMd.trim() || draft.llmProfileId) {
      advancedOpen.value = true;
    }
  } catch (e) {
    error.value = e?.message || "加载成员配置失败";
  } finally {
    loadingSpec.value = false;
    await nextTick();
    nameInput.value?.focus?.();
  }
}

async function submit() {
  error.value = "";
  const name = String(draft.displayName || "").trim();
  if (!name || !props.workgroupId || busy.value) return;
  const tools = draft.tools.length ? [...draft.tools] : [...defaultTools.value];
  const body = {
    display_name: name,
    allow_tool_names: tools,
    prompt: {
      soul_md: draft.soulMd,
      user_md: "",
      custom_md: draft.customMd,
    },
  };
  const llm = String(draft.llmProfileId || "").trim();
  if (llm) {
    body.llm_profile_id = llm;
    body.llm_profile_revision = "1";
  }

  busy.value = true;
  try {
    if (props.mode === "create") {
      const home = String(draft.homeNodeId || props.defaultHomeNodeId || "").trim();
      if (home) body.home_node_id = home;
      await api.createWorkgroupMember(props.workgroupId, body);
    } else {
      await api.patchWorkgroupMember(props.workgroupId, props.memberId, body);
    }
    emit("saved", {
      mode: props.mode,
      workgroupId: props.workgroupId,
      memberId: props.memberId,
      displayName: name,
    });
    emit("close");
  } catch (e) {
    error.value = e?.message || (props.mode === "create" ? "添加失败" : "保存失败");
  } finally {
    busy.value = false;
  }
}

watch(
  () => props.open,
  (visible) => {
    if (visible) void resetFromProps();
  },
);

onMounted(() => {
  void loadToolCatalog();
});
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="wg-member-overlay" @click="onBackdropClick">
      <section
        class="wg-member-modal"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="'wg-member-title'"
      >
        <header class="wg-member-modal__header">
          <div class="wg-member-modal__heading">
            <h2 id="wg-member-title" class="wg-member-modal__title">{{ title }}</h2>
            <p class="wg-member-modal__lead">
              {{
                mode === "create"
                  ? "成员在独立工作区里协作；Supervisor 通过任务编排调用它。"
                  : "修改后会重新同步到 Home Node。"
              }}
            </p>
          </div>
          <button
            type="button"
            class="wg-member-modal__close"
            aria-label="关闭"
            :disabled="busy"
            @click="emit('close')"
          >
            ×
          </button>
        </header>

        <div class="wg-member-modal__body">
          <p v-if="loadingSpec" class="wg-member-modal__hint">加载配置…</p>
          <template v-else>
            <label class="settings-field">
              <span class="settings-field__label">显示名</span>
              <input
                ref="nameInput"
                v-model="draft.displayName"
                class="settings-field__input"
                type="text"
                maxlength="64"
                placeholder="例如：资料员、审稿助手"
                autocomplete="off"
                :disabled="busy"
                @keydown.enter.prevent="canSubmit && submit()"
              />
            </label>

            <fieldset class="wg-member-modal__tools">
              <legend class="settings-field__label">能力</legend>
              <p class="settings-field__hint">默认文件系统；Shell 无额外沙箱，需显式勾选</p>
              <div class="wg-member-modal__tool-row">
                <button
                  v-for="opt in toolOptions"
                  :key="opt.id"
                  type="button"
                  class="wg-member-modal__chip"
                  :class="{ 'wg-member-modal__chip--on': draft.tools.includes(opt.id) }"
                  :disabled="busy"
                  :title="opt.hint"
                  @click="toggleTool(opt.id)"
                >
                  {{ opt.label }}
                </button>
              </div>
            </fieldset>

            <button
              type="button"
              class="wg-member-modal__advanced-toggle"
              :aria-expanded="advancedOpen"
              :disabled="busy"
              @click="advancedOpen = !advancedOpen"
            >
              {{ advancedOpen ? "收起高级选项" : "高级选项" }}
            </button>

            <div v-if="advancedOpen" class="wg-member-modal__advanced">
              <label v-if="mode === 'create'" class="settings-field">
                <span class="settings-field__label">Home Node</span>
                <input
                  v-model="draft.homeNodeId"
                  class="settings-field__input"
                  type="text"
                  placeholder="默认本机"
                  autocomplete="off"
                  :disabled="busy"
                />
                <span class="settings-field__hint">成员工具在该 Node 上执行</span>
              </label>
              <label v-else class="settings-field">
                <span class="settings-field__label">Home Node</span>
                <input
                  class="settings-field__input"
                  type="text"
                  :value="draft.homeNodeId"
                  disabled
                />
              </label>

              <label class="settings-field">
                <span class="settings-field__label">LLM 档案 id</span>
                <input
                  v-model="draft.llmProfileId"
                  class="settings-field__input"
                  type="text"
                  placeholder="留空则跟随工作组"
                  autocomplete="off"
                  :disabled="busy"
                />
              </label>

              <label class="settings-field">
                <span class="settings-field__label">Soul</span>
                <textarea
                  v-model="draft.soulMd"
                  class="settings-field__input wg-member-modal__textarea"
                  rows="3"
                  placeholder="角色与行为准则（可选）"
                  :disabled="busy"
                />
              </label>

              <label class="settings-field">
                <span class="settings-field__label">Custom</span>
                <textarea
                  v-model="draft.customMd"
                  class="settings-field__input wg-member-modal__textarea"
                  rows="2"
                  placeholder="额外说明（可选）"
                  :disabled="busy"
                />
              </label>
            </div>
          </template>
        </div>

        <footer class="wg-member-modal__footer">
          <p v-if="error" class="wg-member-modal__error">{{ error }}</p>
          <div class="wg-member-modal__actions">
            <button type="button" class="btn btn--ghost" :disabled="busy" @click="emit('close')">
              取消
            </button>
            <button
              type="button"
              class="btn btn--primary"
              :disabled="!canSubmit"
              @click="submit"
            >
              {{ primaryLabel }}
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.wg-member-overlay {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: grid;
  place-items: center;
  padding: 24px;
  background: var(--color-overlay);
  backdrop-filter: blur(2px);
}

.wg-member-modal {
  width: min(440px, 96vw);
  max-height: min(88vh, 680px);
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  box-shadow: var(--shadow-lg, 0 16px 48px rgba(0, 0, 0, 0.28));
  overflow: hidden;
}

.wg-member-modal__header,
.wg-member-modal__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--color-border);
}

.wg-member-modal__footer {
  flex-direction: column;
  align-items: stretch;
  border-bottom: 0;
  border-top: 1px solid var(--color-border);
}

.wg-member-modal__heading {
  min-width: 0;
}

.wg-member-modal__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--color-text);
}

.wg-member-modal__lead {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.4;
  color: var(--color-text-muted);
}

.wg-member-modal__close {
  border: none;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 22px;
  line-height: 1;
  cursor: pointer;
  padding: 0 4px;
}

.wg-member-modal__close:hover {
  color: var(--color-text);
}

.wg-member-modal__body {
  flex: 1 1 auto;
  overflow: auto;
  padding: 16px 18px 8px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.wg-member-modal__hint {
  margin: 0;
  color: var(--color-text-muted);
  font-size: 13px;
}

.wg-member-modal__tools {
  margin: 0;
  padding: 0;
  border: none;
}

.wg-member-modal__tool-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}

.wg-member-modal__chip {
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  color: var(--color-text);
  border-radius: 999px;
  padding: 6px 12px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}

.wg-member-modal__chip--on {
  border-color: var(--color-primary, #0078d4);
  background: color-mix(in srgb, var(--color-primary, #0078d4) 14%, transparent);
  color: var(--color-primary-strong, var(--color-primary, #0078d4));
  font-weight: 600;
}

.wg-member-modal__chip:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.wg-member-modal__advanced-toggle {
  align-self: flex-start;
  border: none;
  background: none;
  padding: 0;
  color: var(--color-primary-strong, var(--color-primary, #0078d4));
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}

.wg-member-modal__advanced {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-top: 2px;
}

.wg-member-modal__textarea {
  resize: vertical;
  min-height: 64px;
  font-family: inherit;
  line-height: 1.4;
}

.wg-member-modal__error {
  margin: 0;
  color: var(--color-danger, #c42b1c);
  font-size: 12px;
}

.wg-member-modal__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
