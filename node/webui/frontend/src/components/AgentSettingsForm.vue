<script setup>
/**
 * Agent 设置表单。
 * mode: full（设置页）| create-basics（创建：身份）| create-capabilities（创建：工具组）
 */
import { computed, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import {
  LONG_TERM_SCOPES,
  TOOL_GROUPS,
  memoryEnabledFromToolGroups,
  skillsEnabledFromToolGroups,
} from "../utils/agentTemplateForm.js";
import ToolGroupIcon from "./ToolGroupIcon.vue";
import UiSelect from "./UiSelect.vue";

const props = defineProps({
  draft: { type: Object, required: true },
  llmProfiles: { type: Array, default: () => [] },
  showAdvanced: { type: Boolean, default: false },
  compact: { type: Boolean, default: false },
  /**
   * full：设置页完整表单
   * create-basics：创建弹窗身份步
   * create-capabilities：创建弹窗能力步
   * lite：兼容旧用法，等同 create-basics
   */
  mode: {
    type: String,
    default: "full",
    validator: (v) => ["full", "create-basics", "create-capabilities", "lite"].includes(v),
  },
  /** @deprecated 使用 mode="create-basics" */
  lite: { type: Boolean, default: false },
  /** 创建校验：替换对应字段标签文案 */
  fieldErrors: {
    type: Object,
    default: () => ({ name: "", llm: "" }),
  },
  /** Node 当前可勾选工具组（已按 browser/wecom 进程开关过滤）；缺省用全量 TOOL_GROUPS */
  availableToolGroups: {
    type: Array,
    default: null,
  },
});

const emit = defineEmits(["update:showAdvanced", "clear-field-error"]);

const resolvedMode = computed(() => {
  if (props.mode === "lite" || props.lite) return "create-basics";
  return props.mode;
});
const isCreateBasics = computed(() => resolvedMode.value === "create-basics");
const isCreateCapabilities = computed(() => resolvedMode.value === "create-capabilities");
const isFull = computed(() => resolvedMode.value === "full");
const useConversationalLabels = computed(() => isCreateBasics.value || isCreateCapabilities.value);
const visibleToolGroups = computed(() => {
  const list = Array.isArray(props.availableToolGroups) ? props.availableToolGroups : null;
  if (list && list.length) {
    return list.filter((g) => g && g.name);
  }
  return TOOL_GROUPS;
});

const nameLabel = computed(() => {
  if (props.fieldErrors?.name) return props.fieldErrors.name;
  if (useConversationalLabels.value) return "你的智能体叫什么呢";
  return "显示名称";
});
const llmLabel = computed(() => {
  if (props.fieldErrors?.llm) return props.fieldErrors.llm;
  if (useConversationalLabels.value) return "需要以哪个模型来运行呢";
  return "模型配置";
});

function onNameInput() {
  if (props.fieldErrors?.name) emit("clear-field-error", "name");
}
function onLlmChange() {
  if (props.fieldErrors?.llm) emit("clear-field-error", "llm");
}

const advancedOpen = computed({
  get: () => props.showAdvanced,
  set: (v) => emit("update:showAdvanced", v),
});

const catalogSkills = ref(/** @type {{ skill_name: string, description: string }[]} */ ([]));
const catalogLoading = ref(false);
const catalogError = ref("");

async function loadCatalog() {
  catalogLoading.value = true;
  catalogError.value = "";
  try {
    const data = await api.listNodeSkillsCatalog();
    catalogSkills.value = Array.isArray(data?.available_skills)
      ? data.available_skills
          .map((s) => ({
            skill_name: String(s.skill_name || s.name || "").trim(),
            description: String(s.description || "").trim(),
          }))
          .filter((s) => s.skill_name)
      : [];
  } catch (e) {
    catalogError.value = e?.message || "加载 skills 目录失败";
    catalogSkills.value = [];
  } finally {
    catalogLoading.value = false;
  }
}

onMounted(() => {
  if (isFull.value) loadCatalog();
});

watch(advancedOpen, (open) => {
  if (isFull.value && open && !catalogSkills.value.length && !catalogLoading.value) {
    loadCatalog();
  }
});

function toggleGroup(name) {
  const set = new Set(props.draft.toolGroups || []);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  props.draft.toolGroups = [...set].sort();
}

function isSkillVisible(name) {
  if (props.draft.visibleSkills === null || props.draft.visibleSkills === undefined) {
    return true;
  }
  return Array.isArray(props.draft.visibleSkills) && props.draft.visibleSkills.includes(name);
}

function toggleSkillVisible(name) {
  const allNames = catalogSkills.value.map((s) => s.skill_name);
  let current =
    props.draft.visibleSkills === null || props.draft.visibleSkills === undefined
      ? [...allNames]
      : [...(props.draft.visibleSkills || [])];
  if (current.includes(name)) {
    current = current.filter((x) => x !== name);
  } else {
    current.push(name);
  }
  current = [...new Set(current.map((x) => String(x || "").trim()).filter(Boolean))];
  if (allNames.length > 0 && current.length === allNames.length && allNames.every((n) => current.includes(n))) {
    props.draft.visibleSkills = null;
  } else {
    props.draft.visibleSkills = current;
  }
}

function selectAllSkills() {
  props.draft.visibleSkills = null;
}

function clearAllSkills() {
  props.draft.visibleSkills = [];
}

const skillsToolEnabled = computed(() => skillsEnabledFromToolGroups(props.draft.toolGroups));
const memoryToolEnabled = computed(() => memoryEnabledFromToolGroups(props.draft.toolGroups));

const llmProfileOptions = computed(() =>
  (props.llmProfiles || []).map((p) => ({
    value: p.id,
    label: `${p.id}${p.model ? ` · ${p.model}` : ""}`,
  })),
);

const longTermScopeOptions = computed(() =>
  LONG_TERM_SCOPES.map((opt) => ({ value: opt.value, label: opt.label })),
);
</script>

<template>
  <div
    class="agent-settings-form"
    :class="{
      'agent-settings-form--compact': compact,
      'agent-settings-form--lite': useConversationalLabels,
      'agent-settings-form--create-basics': isCreateBasics,
      'agent-settings-form--create-capabilities': isCreateCapabilities,
    }"
  >
    <section
      v-if="!isCreateCapabilities"
      class="agent-settings-section"
      :class="{ 'agent-settings-section--flat': isFull }"
    >
      <h3 v-if="isFull" class="agent-settings-section__title">基础信息</h3>
      <div :class="{ 'agent-settings-section__body': isFull }">
        <label class="agent-settings-field">
          <span
            :class="{
              'agent-settings-field__label': true,
              'agent-settings-field__label--error': !!fieldErrors?.name,
            }"
          >
            {{ nameLabel }}
          </span>
          <input
            v-model="draft.displayName"
            type="text"
            class="agent-settings-input"
            :class="{ 'agent-settings-input--error': !!fieldErrors?.name }"
            :placeholder="useConversationalLabels ? '' : '智能体名称'"
            @input="onNameInput"
          />
        </label>
        <label class="agent-settings-field" :class="{ 'agent-settings-field--grow': isCreateBasics }">
          <span class="agent-settings-field__label">
            {{ useConversationalLabels ? "介绍一下你的智能体吧，如果你愿意的话" : "简介" }}
          </span>
          <textarea
            v-model="draft.description"
            class="agent-settings-input agent-settings-input--area"
            :rows="isCreateBasics ? 4 : 2"
            :placeholder="useConversationalLabels ? '' : '可选，简短说明它擅长什么'"
          />
        </label>
        <label class="agent-settings-field">
          <span
            :class="{
              'agent-settings-field__label': true,
              'agent-settings-field__label--error': !!fieldErrors?.llm,
            }"
          >
            {{ llmLabel }}
          </span>
          <UiSelect
            v-model="draft.llmProfileId"
            :options="llmProfileOptions"
            placeholder="请选择"
            :disabled="!llmProfileOptions.length"
            @update:model-value="onLlmChange"
          />
        </label>
        <p v-if="!llmProfiles.length" class="agent-settings-hint">
          {{
            useConversationalLabels
              ? "还没有模型配置，先去「设置 › 连接」加一条吧。"
              : "请先在「设置 › 连接」中添加模型配置"
          }}
        </p>
        <p v-if="isCreateBasics" class="agent-settings-hint agent-settings-hint--later">
          角色设定、技能白名单等，创建后可在智能体设置里慢慢调。
        </p>
      </div>
    </section>

    <template v-if="isCreateCapabilities">
      <section class="agent-settings-section">
        <div class="agent-settings-toggles agent-settings-toggles--fill">
          <button
            v-for="g in visibleToolGroups"
            :key="g.name"
            type="button"
            class="agent-settings-tile"
            :class="{ 'agent-settings-tile--active': draft.toolGroups?.includes(g.name) }"
            :aria-pressed="draft.toolGroups?.includes(g.name) ? 'true' : 'false'"
            :title="g.hint || undefined"
            @click="toggleGroup(g.name)"
          >
            <span class="agent-settings-tile__icon" aria-hidden="true">
              <ToolGroupIcon :name="g.name" />
            </span>
            <span class="agent-settings-tile__label">
              {{ g.label }}{{ g.beta ? "（Beta）" : "" }}
            </span>
          </button>
        </div>
      </section>
    </template>

    <template v-if="isFull">
      <section class="agent-settings-section agent-settings-section--flat">
        <div class="agent-settings-section__title agent-settings-section__title--inline">能力与角色</div>

        <div class="agent-settings-advanced agent-settings-section__body">
          <div class="agent-settings-advanced__block">
            <h4 class="agent-settings-subsection__title">工具能力</h4>
            <p class="agent-settings-hint">点选启用；都不选则不开放任何工具。</p>
            <div class="agent-settings-toggles agent-settings-toggles--tiles">
              <button
                v-for="g in visibleToolGroups"
                :key="g.name"
                type="button"
                class="agent-settings-tile agent-settings-tile--compact"
                :class="{ 'agent-settings-tile--active': draft.toolGroups?.includes(g.name) }"
                :aria-pressed="draft.toolGroups?.includes(g.name) ? 'true' : 'false'"
                :title="g.hint || undefined"
                @click="toggleGroup(g.name)"
              >
                <span class="agent-settings-tile__icon" aria-hidden="true">
                  <ToolGroupIcon :name="g.name" />
                </span>
                <span class="agent-settings-tile__label">
                  {{ g.label }}{{ g.beta ? "（Beta）" : "" }}
                </span>
              </button>
            </div>
          </div>

          <div v-if="skillsToolEnabled" class="agent-settings-advanced__block">
            <h4 class="agent-settings-subsection__title">可见技能</h4>
            <p class="agent-settings-hint">
              选择该智能体可用的技能。全选表示不限制（新技能会自动出现）；否则仅白名单内可用。
            </p>
            <div class="agent-settings-field__head">
              <span class="agent-settings-hint" style="margin: 0">
                <template v-if="draft.visibleSkills === null">当前：不限制（全部）</template>
                <template v-else>当前：已选 {{ draft.visibleSkills.length }} 项</template>
              </span>
              <div class="agent-settings-skill-actions">
                <button type="button" class="btn btn--ghost btn--sm" :disabled="catalogLoading" @click="selectAllSkills">
                  全选
                </button>
                <button type="button" class="btn btn--ghost btn--sm" :disabled="catalogLoading" @click="clearAllSkills">
                  全不选
                </button>
              </div>
            </div>
            <p v-if="catalogLoading" class="agent-settings-hint">加载目录中…</p>
            <p v-else-if="catalogError" class="agent-settings-hint">{{ catalogError }}</p>
            <p v-else-if="!catalogSkills.length" class="agent-settings-hint">暂无可用技能。</p>
            <div v-else class="agent-settings-skill-list">
              <label
                v-for="s in catalogSkills"
                :key="s.skill_name"
                class="agent-settings-check agent-settings-check--skill"
              >
                <input
                  type="checkbox"
                  :checked="isSkillVisible(s.skill_name)"
                  @change="toggleSkillVisible(s.skill_name)"
                />
                <span>
                  <strong>{{ s.skill_name }}</strong>
                  <em v-if="s.description">{{ s.description }}</em>
                </span>
              </label>
            </div>
          </div>

          <div class="agent-settings-advanced__block">
            <h4 class="agent-settings-subsection__title">角色与记忆</h4>
            <p class="agent-settings-hint">
              填写后会注入到系统提示；留空则不注入。开启「记忆」工具后可选择记忆范围。
            </p>
            <label class="agent-settings-field">
              <span class="agent-settings-field__label">角色设定</span>
              <textarea
                v-model="draft.promptSoulMd"
                class="agent-settings-input agent-settings-input--area"
                rows="4"
                placeholder="它应该是怎样的角色、语气与边界…"
              />
            </label>
            <label class="agent-settings-field">
              <span class="agent-settings-field__label">临时指令</span>
              <textarea
                v-model="draft.promptCustomMd"
                class="agent-settings-input agent-settings-input--area"
                rows="3"
                placeholder="仅对当前智能体生效的补充说明…"
              />
            </label>
            <label v-if="memoryToolEnabled" class="agent-settings-field">
              <span class="agent-settings-field__label">记忆范围</span>
              <UiSelect v-model="draft.promptLongTermScope" :options="longTermScopeOptions" />
            </label>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.agent-settings-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.agent-settings-section {
  padding: 12px 14px;
  border-radius: 10px;
  border: 1px solid var(--color-border);
  background: var(--color-surface-muted);
}

.agent-settings-section--flat {
  padding: 0 0 4px;
  border: 0;
  border-radius: 0;
  background: transparent;
}

/* 区块标题顶格；字段内容缩进一级，避免与标题同级 */
.agent-settings-section__body {
  padding-left: 12px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.agent-settings-disclosure {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  margin: 0 0 6px;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  text-align: left;
}

.agent-settings-disclosure__chevron {
  font-size: 12px;
  color: var(--color-text-muted);
  font-weight: 400;
}

.agent-settings-subsection__title {
  margin: 0 0 6px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--color-text);
}

.agent-settings-advanced__block + .agent-settings-advanced__block {
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid color-mix(in srgb, var(--color-border) 70%, transparent);
}

.agent-settings-form--lite {
  gap: 14px;
  flex: 1 1 auto;
  min-height: 0;
  height: 100%;
}

.agent-settings-form--lite .agent-settings-section {
  padding: 0;
  border: none;
  border-radius: 0;
  background: transparent;
}

.agent-settings-form--create-basics .agent-settings-section,
.agent-settings-form--create-capabilities .agent-settings-section {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.agent-settings-form--create-basics .agent-settings-field {
  gap: 6px;
  margin-bottom: 0;
  font-size: inherit;
  color: inherit;
  flex: 0 0 auto;
}

.agent-settings-form--create-basics .agent-settings-field--grow {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.agent-settings-form--create-basics .agent-settings-field--grow .agent-settings-input--area {
  flex: 1 1 auto;
  min-height: 96px;
  height: 100%;
  resize: none;
}

.agent-settings-form--lite .agent-settings-field :deep(.ui-select__trigger),
.agent-settings-form--lite .agent-settings-input {
  background: var(--color-input, var(--color-surface));
}

.agent-settings-form--create-basics .agent-settings-hint--later {
  margin-top: auto;
  margin-bottom: 0;
  padding-top: 4px;
}

.agent-settings-toggles--fill {
  flex: 1 1 auto;
  min-height: 0;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  grid-auto-rows: 1fr;
  gap: 8px;
  align-content: stretch;
}

.agent-settings-tile {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  gap: 8px;
  margin: 0;
  min-height: 52px;
  width: 100%;
  padding: 14px 16px;
  border-radius: 10px;
  border: 1px solid var(--color-border);
  background: var(--color-input, var(--color-surface));
  color: var(--color-text);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.agent-settings-tile:hover {
  border-color: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 40%, var(--color-border));
}

.agent-settings-tile--active {
  border-color: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 55%, var(--color-border));
  background: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 8%, var(--color-surface));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 18%, transparent);
}

.agent-settings-tile__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
}

.agent-settings-tile--active .agent-settings-tile__icon {
  color: var(--color-text);
}

.agent-settings-tile__label {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.3;
}

.agent-settings-field__label {
  margin: 0;
  font-size: 12.5px;
  font-weight: 500;
  line-height: 1.4;
  color: var(--color-text-muted);
}

.agent-settings-field__label--error {
  color: var(--color-danger);
}

.agent-settings-input--error {
  border-color: color-mix(in srgb, var(--color-danger) 55%, var(--color-border));
}

.agent-settings-section__title {
  margin: 0 0 12px;
  font-size: 15px;
  font-weight: 600;
  color: var(--color-text);
}

.agent-settings-section__title--inline {
  margin: 0;
}

.agent-settings-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 8px;
  font-size: 11.5px;
  color: var(--color-text-subtle);
}

.agent-settings-field :deep(.ui-select) {
  max-width: none;
}

.agent-settings-field :deep(.ui-select__trigger) {
  padding: 7px 12px;
  min-height: 34px;
  border-radius: 8px;
  background: var(--color-surface);
}

.agent-settings-input {
  padding: 7px 10px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  color: var(--color-text);
  font-size: 13px;
}

.agent-settings-input--area {
  resize: vertical;
  min-height: 52px;
}

.agent-settings-check {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 12.5px;
  color: var(--color-text);
}

.agent-settings-check--skill {
  align-items: flex-start;
}

.agent-settings-check--skill span {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.agent-settings-check--skill strong {
  font-weight: 600;
  font-size: 12.5px;
}

.agent-settings-check--skill em {
  font-style: normal;
  font-size: 11px;
  color: var(--color-text-subtle);
  line-height: 1.35;
}

.agent-settings-hint {
  margin: 0 0 8px;
  font-size: 11.5px;
  line-height: 1.45;
  color: var(--color-text-subtle);
}

.agent-settings-hint--later {
  margin-top: 4px;
  margin-bottom: 0;
}

.agent-settings-hint--inline {
  margin: 0;
}

.agent-settings-field__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.agent-settings-skill-actions {
  display: flex;
  gap: 4px;
}

.agent-settings-skill-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 220px;
  overflow: auto;
  padding: 6px 8px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
}

.agent-settings-toggles {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 4px 10px;
}

.agent-settings-toggles--tiles {
  grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
  gap: 8px;
}

.agent-settings-tile--compact {
  flex-direction: row;
  align-items: center;
  min-height: 44px;
  padding: 10px 12px;
  gap: 10px;
}

.agent-settings-advanced {
  display: flex;
  flex-direction: column;
  gap: 0;
  margin-top: 8px;
}
</style>
