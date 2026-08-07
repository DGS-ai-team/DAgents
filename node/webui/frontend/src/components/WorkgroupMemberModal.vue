<script setup>
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";

// catalog 失败时的离线兜底：与 shared catalog groups（fs 默认开、bash 默认关）对齐。
const FALLBACK_GROUPS = [
  {
    id: "fs",
    label: "文件系统",
    hint: "读/写/搜索等工作区文件工具",
    defaultOn: true,
    toolIds: [
      "read_file",
      "show_image",
      "read_image",
      "write_file",
      "glob_files",
      "grep_file",
      "grep_files",
      "search_replace",
    ],
  },
  {
    id: "bash",
    label: "Shell",
    hint: "bash_run 等（无额外沙箱，默认不勾选）",
    defaultOn: false,
    toolIds: ["bash_run", "background_job_status", "background_job_cancel"],
  },
];

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: "create" }, // create | edit
  workgroupId: { type: String, default: "" },
  memberId: { type: String, default: "" },
  defaultHomeNodeId: { type: String, default: "" },
});

const emit = defineEmits(["close", "saved"]);

const groupOptions = ref(FALLBACK_GROUPS.map((g) => ({ ...g, toolIds: [...g.toolIds] })));
const draft = reactive(emptyDraft());
const busy = ref(false);
const loadingSpec = ref(false);
const catalogError = ref("");
const error = ref("");
const advancedOpen = ref(false);
const nameInput = ref(null);

const title = computed(() => (props.mode === "create" ? "添加成员" : "配置成员"));
const lead = computed(() =>
  props.mode === "create"
    ? "成员在独立工作区协作，由 Supervisor 编排调用。"
    : "修改后会重新同步到 Home Node。",
);
const primaryLabel = computed(() => {
  if (busy.value) return props.mode === "create" ? "添加中…" : "保存中…";
  return props.mode === "create" ? "添加" : "保存";
});
const canSubmit = computed(() => {
  if (busy.value || loadingSpec.value) return false;
  return String(draft.displayName || "").trim().length > 0;
});

function defaultGroupIds(groups = groupOptions.value) {
  return (groups || []).filter((g) => g.defaultOn).map((g) => g.id);
}

function expandGroupsToTools(groupIds, groups = groupOptions.value) {
  const want = new Set((groupIds || []).map(String));
  const out = [];
  const seen = new Set();
  for (const g of groups || []) {
    if (!want.has(g.id)) continue;
    for (const id of g.toolIds || []) {
      if (seen.has(id)) continue;
      seen.add(id);
      out.push(id);
    }
  }
  return out;
}

function groupsFromAllowNames(allowNames, groups = groupOptions.value) {
  const allow = new Set((allowNames || []).map(String));
  return (groups || [])
    .filter((g) => (g.toolIds || []).some((id) => allow.has(id)))
    .map((g) => g.id);
}

function emptyDraft() {
  return {
    displayName: "",
    homeNodeId: "",
    groups: defaultGroupIds(),
    soulMd: "",
    customMd: "",
    llmProfileId: "",
  };
}

function toggleGroup(id) {
  const set = new Set(draft.groups);
  if (set.has(id)) set.delete(id);
  else set.add(id);
  draft.groups = [...set];
}

function onBackdropClick(event) {
  if (busy.value) return;
  if (event.target === event.currentTarget) emit("close");
}

async function loadToolCatalog() {
  catalogError.value = "";
  try {
    // 始终从本 Node 拉取成员可用工具组（嵌入 catalog，不经 Manage）。
    const catalog = await api.getMemberToolCatalog();
    const tools = Array.isArray(catalog?.tools) ? catalog.tools : [];
    const groups = Array.isArray(catalog?.groups) ? catalog.groups : [];
    const byGroup = new Map();
    for (const t of tools) {
      const gid = String(t.group || "").trim() || "other";
      if (!byGroup.has(gid)) {
        byGroup.set(gid, {
          id: gid,
          label: String(t.group_label || gid).trim() || gid,
          hint: "",
          defaultOn: false,
          toolIds: [],
        });
      }
      const entry = byGroup.get(gid);
      const tid = String(t.id || "").trim();
      if (tid) entry.toolIds.push(tid);
      if (t.default) entry.defaultOn = true;
      if (!entry.hint && t.hint) entry.hint = String(t.hint);
    }
    const ordered = [];
    const seen = new Set();
    for (const g of groups) {
      const id = String(g.id || "").trim();
      if (!id || !byGroup.has(id)) continue;
      const entry = byGroup.get(id);
      if (g.label) entry.label = String(g.label);
      ordered.push(entry);
      seen.add(id);
    }
    for (const [id, entry] of byGroup) {
      if (!seen.has(id)) ordered.push(entry);
    }
    if (ordered.length) {
      groupOptions.value = ordered;
      if (props.mode === "create" && !props.open) {
        draft.groups = defaultGroupIds(ordered);
      }
    }
  } catch (e) {
    catalogError.value = e?.message || "加载 Node 工具组失败，已用离线兜底";
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
    draft.groups = allow.length
      ? groupsFromAllowNames(allow.map(String))
      : defaultGroupIds();
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
  const selected = draft.groups.length ? [...draft.groups] : defaultGroupIds();
  const tools = expandGroupsToTools(selected);
  const body = {
    display_name: name,
    allow_tool_names: tools,
    prompt: {
      soul_md: draft.soulMd,
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
    if (visible) {
      void (async () => {
        await loadToolCatalog();
        await resetFromProps();
      })();
    }
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
            <p class="wg-member-modal__lead">{{ lead }}</p>
          </div>
          <button
            type="button"
            class="wg-member-modal__close"
            aria-label="关闭"
            :disabled="busy"
            @click="emit('close')"
          >
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path
                d="M4 4l8 8M12 4l-8 8"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
              />
            </svg>
          </button>
        </header>

        <div class="wg-member-modal__body">
          <p v-if="loadingSpec" class="wg-member-modal__hint">加载配置…</p>
          <template v-else>
            <label class="wg-member-modal__field">
              <span class="wg-member-modal__label">显示名</span>
              <input
                ref="nameInput"
                v-model="draft.displayName"
                class="wg-member-modal__input"
                type="text"
                maxlength="64"
                placeholder="例如：资料员、审稿助手"
                autocomplete="off"
                :disabled="busy"
                @keydown.enter.prevent="canSubmit && submit()"
              />
            </label>

            <fieldset class="wg-member-modal__tools">
              <legend class="wg-member-modal__label">工具组</legend>
              <p class="wg-member-modal__hint-text">
                由本 Node 提供；默认文件系统，Shell 需显式开启。
              </p>
              <p v-if="catalogError" class="wg-member-modal__hint-text wg-member-modal__hint-text--warn">
                {{ catalogError }}
              </p>
              <div class="wg-member-modal__tool-list">
                <button
                  v-for="opt in groupOptions"
                  :key="opt.id"
                  type="button"
                  class="wg-member-modal__tool"
                  :class="{ 'wg-member-modal__tool--on': draft.groups.includes(opt.id) }"
                  :disabled="busy"
                  :title="opt.hint || opt.id"
                  :aria-pressed="draft.groups.includes(opt.id)"
                  @click="toggleGroup(opt.id)"
                >
                  <span class="wg-member-modal__tool-check" aria-hidden="true">
                    <svg v-if="draft.groups.includes(opt.id)" viewBox="0 0 12 12" width="11" height="11">
                      <path
                        d="M2.5 6.2L4.8 8.5 9.5 3.5"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.6"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                    </svg>
                  </span>
                  <span class="wg-member-modal__tool-text">
                    <span class="wg-member-modal__tool-name">{{ opt.label }}</span>
                    <span v-if="opt.hint" class="wg-member-modal__tool-hint">{{ opt.hint }}</span>
                  </span>
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
              <span>{{ advancedOpen ? "收起高级选项" : "高级选项" }}</span>
              <svg
                class="wg-member-modal__advanced-chevron"
                :class="{ 'wg-member-modal__advanced-chevron--open': advancedOpen }"
                viewBox="0 0 12 12"
                width="12"
                height="12"
                aria-hidden="true"
              >
                <path
                  d="M3 4.5L6 7.5L9 4.5"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.4"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>

            <div v-if="advancedOpen" class="wg-member-modal__advanced">
              <label v-if="mode === 'create'" class="wg-member-modal__field">
                <span class="wg-member-modal__label">Home Node</span>
                <input
                  v-model="draft.homeNodeId"
                  class="wg-member-modal__input"
                  type="text"
                  placeholder="默认本机"
                  autocomplete="off"
                  :disabled="busy"
                />
                <span class="wg-member-modal__hint-text">成员工具在该 Node 上执行</span>
              </label>
              <label v-else class="wg-member-modal__field">
                <span class="wg-member-modal__label">Home Node</span>
                <input
                  class="wg-member-modal__input"
                  type="text"
                  :value="draft.homeNodeId"
                  disabled
                />
              </label>

              <label class="wg-member-modal__field">
                <span class="wg-member-modal__label">LLM 档案 id</span>
                <input
                  v-model="draft.llmProfileId"
                  class="wg-member-modal__input"
                  type="text"
                  placeholder="留空则跟随工作组"
                  autocomplete="off"
                  :disabled="busy"
                />
              </label>

              <label class="wg-member-modal__field">
                <span class="wg-member-modal__label">Soul</span>
                <textarea
                  v-model="draft.soulMd"
                  class="wg-member-modal__input wg-member-modal__textarea"
                  rows="3"
                  placeholder="角色与行为准则（可选）"
                  :disabled="busy"
                />
              </label>

              <label class="wg-member-modal__field">
                <span class="wg-member-modal__label">Custom</span>
                <textarea
                  v-model="draft.customMd"
                  class="wg-member-modal__input wg-member-modal__textarea"
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
  width: min(460px, 96vw);
  max-height: min(88vh, 720px);
  display: flex;
  flex-direction: column;
  border-radius: 14px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  box-shadow: var(--shadow-lg, 0 16px 48px rgba(0, 0, 0, 0.28));
  overflow: hidden;
}

.wg-member-modal__header {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--color-border);
}

.wg-member-modal__footer {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
  padding: 12px 20px;
  border-top: 1px solid var(--color-border);
}

.wg-member-modal__heading {
  min-width: 0;
}

.wg-member-modal__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  line-height: 1.3;
  color: var(--color-text);
}

.wg-member-modal__lead {
  margin: 6px 0 0;
  font-size: 13px;
  line-height: 1.45;
  color: var(--color-text-muted);
}

.wg-member-modal__close {
  flex: 0 0 auto;
  width: 30px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin: -4px -6px 0 0;
  border: none;
  border-radius: var(--radius-md, 6px);
  background: transparent;
  color: var(--color-text-muted);
  cursor: pointer;
  transition: color 0.12s ease, background 0.12s ease;
}

.wg-member-modal__close:hover:not(:disabled) {
  color: var(--color-text);
  background: var(--color-surface-hover, rgba(0, 0, 0, 0.04));
}

.wg-member-modal__close:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.wg-member-modal__body {
  flex: 1 1 auto;
  overflow: auto;
  padding: 18px 20px 12px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.wg-member-modal__hint {
  margin: 0;
  color: var(--color-text-muted);
  font-size: 13px;
}

.wg-member-modal__field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.wg-member-modal__label {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-muted);
  letter-spacing: 0.01em;
}

.wg-member-modal__input {
  width: 100%;
  box-sizing: border-box;
  padding: 9px 11px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  border-radius: 8px;
  background: var(--color-surface);
  color: var(--color-text);
  font: inherit;
  font-size: 13px;
  line-height: 1.4;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}

.wg-member-modal__input:focus {
  outline: none;
  border-color: var(--color-primary, #0078d4);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #0078d4) 18%, transparent);
}

.wg-member-modal__input:disabled {
  opacity: 0.65;
  cursor: not-allowed;
}

.wg-member-modal__hint-text {
  margin: 0;
  font-size: 12px;
  line-height: 1.4;
  color: var(--color-text-subtle, var(--color-text-muted));
}

.wg-member-modal__hint-text--warn {
  color: var(--color-warning, #b45309);
}

.wg-member-modal__tools {
  margin: 0;
  padding: 0;
  border: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.wg-member-modal__tool-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.wg-member-modal__tool {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  border-radius: 10px;
  background: var(--color-surface);
  color: var(--color-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.12s ease, background 0.12s ease;
}

.wg-member-modal__tool:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--color-primary, #0078d4) 45%, var(--color-border));
  background: var(--color-surface-hover, rgba(0, 0, 0, 0.02));
}

.wg-member-modal__tool--on {
  border-color: color-mix(in srgb, var(--color-primary, #0078d4) 55%, var(--color-border));
  background: color-mix(in srgb, var(--color-primary, #0078d4) 8%, var(--color-surface));
}

.wg-member-modal__tool:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.wg-member-modal__tool-check {
  flex: 0 0 auto;
  width: 18px;
  height: 18px;
  margin-top: 1px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 5px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  color: var(--color-primary-strong, var(--color-primary, #0078d4));
}

.wg-member-modal__tool--on .wg-member-modal__tool-check {
  border-color: var(--color-primary, #0078d4);
  background: color-mix(in srgb, var(--color-primary, #0078d4) 14%, transparent);
}

.wg-member-modal__tool-text {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.wg-member-modal__tool-name {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.3;
}

.wg-member-modal__tool-hint {
  font-size: 12px;
  line-height: 1.4;
  color: var(--color-text-muted);
}

.wg-member-modal__advanced-toggle {
  align-self: flex-start;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: none;
  padding: 2px 0;
  color: var(--color-text-muted);
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

.wg-member-modal__advanced-toggle:hover:not(:disabled) {
  color: var(--color-text);
}

.wg-member-modal__advanced-toggle:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.wg-member-modal__advanced-chevron {
  opacity: 0.7;
  transition: transform 0.15s ease;
}

.wg-member-modal__advanced-chevron--open {
  transform: rotate(180deg);
}

.wg-member-modal__advanced {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding-top: 2px;
}

.wg-member-modal__textarea {
  resize: vertical;
  min-height: 72px;
  font-family: inherit;
  line-height: 1.45;
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
