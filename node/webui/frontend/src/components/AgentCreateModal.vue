<script setup>
import { computed, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";
import {
  TOOL_GROUPS,
  buildCreateAgentPayload,
  draftFromTemplate,
  emptyAgentDraft,
} from "../utils/agentTemplateForm.js";

const props = defineProps({
  open: { type: Boolean, default: false },
});

const emit = defineEmits(["close", "created"]);

const loading = ref(false);
const saving = ref(false);
const error = ref("");
const templates = ref([]);
const draft = reactive(emptyAgentDraft());

const selectedTemplate = computed(
  () => templates.value.find((t) => t.id === draft.templateId) || null,
);

async function loadTemplates() {
  loading.value = true;
  error.value = "";
  try {
    const [tplRes, setup] = await Promise.all([
      api.listAgentTemplates(),
      api.getSetupConfig().catch(() => null),
    ]);
    templates.value = tplRes.templates || [];
    const nodeHooks = setup?.hooks || {};
    const first = templates.value[0];
    if (first) applyTemplate(first, nodeHooks);
  } catch (e) {
    error.value = e.message || "加载模板失败";
    templates.value = [];
  } finally {
    loading.value = false;
  }
}

function applyTemplate(template, nodeHooks = {}) {
  const next = draftFromTemplate(template, nodeHooks);
  Object.assign(draft, next);
}

function onPickTemplate(template) {
  applyTemplate(template);
}

function toggleGroup(name) {
  const set = new Set(draft.enabledGroups || []);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  draft.enabledGroups = [...set].sort();
}

async function submit() {
  if (!draft.templateId) {
    error.value = "请选择一个模板";
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    const created = await api.createAgent(buildCreateAgentPayload(draft));
    emit("created", created);
    emit("close");
  } catch (e) {
    error.value = e.message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function onBackdropClick(event) {
  if (event.target === event.currentTarget && !saving.value) emit("close");
}

watch(
  () => props.open,
  (visible) => {
    if (visible) void loadTemplates();
    else {
      error.value = "";
      Object.assign(draft, emptyAgentDraft());
    }
  },
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="agent-create-overlay" @click="onBackdropClick">
      <section class="agent-create-modal" role="dialog" aria-modal="true" aria-labelledby="agent-create-title">
        <header class="agent-create-modal__header">
          <div>
            <h2 id="agent-create-title" class="agent-create-modal__title">新建 Agent</h2>
            <p class="agent-create-modal__subtitle">选择模板预设，再在下方调整工具、Hook 与沙箱配置</p>
          </div>
          <button type="button" class="agent-create-modal__close" aria-label="关闭" :disabled="saving" @click="emit('close')">
            ×
          </button>
        </header>

        <div class="agent-create-modal__body">
          <div v-if="loading" class="agent-create-modal__loading">加载模板…</div>

          <template v-else>
            <div class="agent-create-modal__templates">
              <button
                v-for="tpl in templates"
                :key="tpl.id"
                type="button"
                class="agent-create-card"
                :class="{ 'agent-create-card--active': draft.templateId === tpl.id }"
                @click="onPickTemplate(tpl)"
              >
                <div class="agent-create-card__head">
                  <span class="agent-create-card__name">{{ tpl.display_name || tpl.id }}</span>
                  <span class="agent-create-card__id">{{ tpl.id }}</span>
                </div>
                <p class="agent-create-card__desc">{{ tpl.description || "无描述" }}</p>
                <div class="agent-create-card__tags">
                  <span v-if="tpl.sandbox?.enabled" class="agent-create-tag">沙箱</span>
                  <span
                    v-for="g in (tpl.defaults?.tools?.enabled_groups || []).slice(0, 3)"
                    :key="g"
                    class="agent-create-tag agent-create-tag--muted"
                  >{{ g }}</span>
                </div>
              </button>
              <p v-if="!templates.length" class="agent-create-modal__empty">暂无可用模板</p>
            </div>

            <div class="agent-create-modal__settings">
              <div class="agent-create-settings__grid">
                <section class="agent-create-section">
                  <h3 class="agent-create-section__title">基本信息</h3>
                  <label class="agent-create-field">
                    <span>显示名称</span>
                    <input v-model="draft.displayName" type="text" class="agent-create-input" placeholder="Agent 名称" />
                  </label>
                  <p v-if="selectedTemplate?.description" class="agent-create-hint">{{ selectedTemplate.description }}</p>
                </section>

                <section class="agent-create-section">
                  <h3 class="agent-create-section__title">沙箱</h3>
                  <label class="agent-create-check">
                    <input v-model="draft.sandboxEnabled" type="checkbox" />
                    <span>沙箱运行</span>
                  </label>
                  <label class="agent-create-field">
                    <span>后端</span>
                    <select v-model="draft.sandboxBackend" class="agent-create-input">
                      <option value="process">process</option>
                      <option value="docker">docker（预留）</option>
                    </select>
                  </label>
                </section>

                <section class="agent-create-section agent-create-section--wide">
                  <h3 class="agent-create-section__title">工具组</h3>
                  <p class="agent-create-hint">基于模板预设，可在此增删允许的工具组。</p>
                  <div class="agent-create-toggles">
                    <label v-for="g in TOOL_GROUPS" :key="g.name" class="agent-create-check">
                      <input
                        type="checkbox"
                        :checked="draft.enabledGroups.includes(g.name)"
                        @change="toggleGroup(g.name)"
                      />
                      <span>
                        {{ g.label }}
                        <span v-if="g.beta" class="agent-create-beta">Beta</span>
                      </span>
                    </label>
                  </div>
                </section>

                <section class="agent-create-section">
                  <h3 class="agent-create-section__title">运行参数</h3>
                  <label class="agent-create-field">
                    <span>max_tool_loops</span>
                    <input v-model.number="draft.maxToolLoops" type="number" min="1" class="agent-create-input" />
                  </label>
                  <label class="agent-create-check">
                    <input v-model="draft.skillsEnabled" type="checkbox" />
                    <span>启用 Skills</span>
                  </label>
                  <label class="agent-create-check">
                    <input v-model="draft.childAgentsEnabled" type="checkbox" />
                    <span>启用子 Agent</span>
                  </label>
                </section>

                <section class="agent-create-section agent-create-section--wide">
                  <h3 class="agent-create-section__title">Hook</h3>
                  <div class="agent-create-toggles agent-create-toggles--hooks">
                    <label class="agent-create-check">
                      <input v-model="draft.hooks.inject_today_date_enabled" type="checkbox" />
                      <span>inject_today_date</span>
                    </label>
                    <label class="agent-create-check">
                      <input v-model="draft.hooks.tool_result_enabled" type="checkbox" />
                      <span>tool_result</span>
                    </label>
                    <label class="agent-create-field">
                      <span>tool_result 落盘阈值</span>
                      <input
                        v-model.number="draft.hooks.tool_result_spill_threshold_tokens"
                        type="number"
                        min="1"
                        step="500"
                        class="agent-create-input"
                      />
                    </label>
                    <label class="agent-create-check">
                      <input v-model="draft.hooks.duplicate_tool_call_enabled" type="checkbox" />
                      <span>duplicate_tool_call</span>
                    </label>
                    <label class="agent-create-field">
                      <span>重复检测窗口（秒）</span>
                      <input
                        v-model.number="draft.hooks.duplicate_tool_call_window_seconds"
                        type="number"
                        min="1"
                        class="agent-create-input"
                      />
                    </label>
                  </div>
                </section>
              </div>
            </div>
          </template>
        </div>

        <footer class="agent-create-modal__footer">
          <p v-if="error" class="agent-create-modal__error">{{ error }}</p>
          <div class="agent-create-modal__actions">
            <button type="button" class="btn btn--ghost" :disabled="saving" @click="emit('close')">取消</button>
            <button
              type="button"
              class="btn btn--primary"
              :disabled="saving || loading || !draft.templateId"
              @click="submit"
            >
              {{ saving ? "创建中…" : "创建 Agent" }}
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.agent-create-overlay {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(2px);
}

.agent-create-modal {
  width: min(920px, 96vw);
  max-height: min(88vh, 900px);
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  border: 1px solid var(--color-border-strong);
  background: var(--color-surface);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}

.agent-create-modal__header,
.agent-create-modal__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--color-border);
}

.agent-create-modal__footer {
  border-bottom: 0;
  border-top: 1px solid var(--color-border);
  align-items: center;
}

.agent-create-modal__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.agent-create-modal__subtitle {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--color-text-subtle);
}

.agent-create-modal__close {
  width: 32px;
  height: 32px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 22px;
  line-height: 1;
  cursor: pointer;
}

.agent-create-modal__close:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.06);
  color: var(--color-text);
}

.agent-create-modal__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 16px 20px 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.agent-create-modal__loading,
.agent-create-modal__empty {
  padding: 32px 12px;
  text-align: center;
  color: var(--color-text-subtle);
  font-size: 13px;
}

.agent-create-modal__templates {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}

.agent-create-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  text-align: left;
  padding: 14px 14px 12px;
  border-radius: 10px;
  border: 1px solid var(--color-border);
  background: var(--color-surface-muted);
  color: inherit;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
}

.agent-create-card:hover {
  border-color: var(--color-border-strong);
  background: var(--color-surface-hover);
}

.agent-create-card--active {
  border-color: rgba(55, 148, 255, 0.55);
  background: var(--color-primary-soft);
}

.agent-create-card__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.agent-create-card__name {
  font-size: 14px;
  font-weight: 600;
}

.agent-create-card__id {
  font-size: 11px;
  color: var(--color-text-subtle);
  font-family: var(--font-mono);
}

.agent-create-card__desc {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text-muted);
  min-height: 2.8em;
}

.agent-create-card__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.agent-create-tag {
  font-size: 10px;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--color-primary-soft);
  color: var(--color-primary-strong);
}

.agent-create-tag--muted {
  background: var(--color-surface-elevated);
  color: var(--color-text-subtle);
}

.agent-create-modal__settings {
  border-top: 1px solid var(--color-border);
  padding-top: 16px;
}

.agent-create-settings__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.agent-create-section {
  padding: 12px 14px;
  border-radius: 10px;
  border: 1px solid var(--color-border);
  background: var(--color-surface-muted);
}

.agent-create-section--wide {
  grid-column: 1 / -1;
}

.agent-create-section__title {
  margin: 0 0 10px;
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-muted);
  letter-spacing: 0.02em;
}

.agent-create-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 8px;
  font-size: 11.5px;
  color: var(--color-text-subtle);
}

.agent-create-input {
  padding: 7px 10px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
  color: var(--color-text);
  font-size: 13px;
}

.agent-create-check {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 12.5px;
  color: var(--color-text);
}

.agent-create-hint {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.45;
  color: var(--color-text-subtle);
}

.agent-create-toggles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 4px 12px;
}

.agent-create-toggles--hooks {
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
}

.agent-create-beta {
  margin-left: 4px;
  font-size: 10px;
  color: var(--color-warning);
}

.agent-create-modal__error {
  flex: 1 1 auto;
  margin: 0;
  font-size: 12px;
  color: var(--color-danger);
}

.agent-create-modal__actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}

@media (max-width: 720px) {
  .agent-create-settings__grid {
    grid-template-columns: 1fr;
  }
}
</style>
