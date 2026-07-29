<script setup>
/**
 * Agent 设置表单：基础信息 + 可展开的高级设置（工具 / 沙箱 / 侧车）。
 * 创建与设置页共用。
 */
import { computed, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import { LONG_TERM_SCOPES, TOOL_GROUPS, skillsEnabledFromToolGroups } from "../utils/agentTemplateForm.js";

const props = defineProps({
  draft: { type: Object, required: true },
  llmProfiles: { type: Array, default: () => [] },
  showAdvanced: { type: Boolean, default: false },
  compact: { type: Boolean, default: false },
});

const emit = defineEmits(["update:showAdvanced"]);

const advancedOpen = computed({
  get: () => props.showAdvanced,
  set: (v) => emit("update:showAdvanced", v),
});

const catalogSkills = ref(/** @type {{ skill_name: string, description: string }[]} */ ([]));
const catalogLoading = ref(false);
const catalogError = ref("");
const catalogEnabled = ref(true);

async function loadCatalog() {
  catalogLoading.value = true;
  catalogError.value = "";
  try {
    const data = await api.listNodeSkillsCatalog();
    catalogEnabled.value = data?.enabled !== false;
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
  loadCatalog();
});

watch(advancedOpen, (open) => {
  if (open && !catalogSkills.value.length && !catalogLoading.value) {
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
  // 全选时回到「不限制」语义，便于日后新增 skill 自动可见。
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

const longTermScopeLabel = computed(() =>
  props.draft.promptLongTermScope === "global" ? "全局长期记忆" : "独立长期记忆",
);

const skillsToolEnabled = computed(() => skillsEnabledFromToolGroups(props.draft.toolGroups));

const activeLongTermEntries = computed({
  get() {
    return props.draft.promptLongTermScope === "global"
      ? props.draft.promptGlobalLongTermEntries
      : props.draft.promptLongTermEntries;
  },
  set(v) {
    if (props.draft.promptLongTermScope === "global") {
      props.draft.promptGlobalLongTermEntries = v;
    } else {
      props.draft.promptLongTermEntries = v;
    }
  },
});

function addLongTermEntry() {
  const list = Array.isArray(activeLongTermEntries.value) ? [...activeLongTermEntries.value] : [];
  list.push({ id: "", content: "" });
  activeLongTermEntries.value = list;
}

function removeLongTermEntry(index) {
  const list = Array.isArray(activeLongTermEntries.value) ? [...activeLongTermEntries.value] : [];
  list.splice(index, 1);
  activeLongTermEntries.value = list;
}

/** 启用沙箱时若仍是历史 process，改为本机 Docker；关闭时保持 process 语义给 payload。 */
watch(
  () => props.draft.sandboxEnabled,
  (enabled) => {
    if (!enabled) return;
    const backend = String(props.draft.sandboxBackend || "").trim();
    if (!backend || backend === "process") {
      props.draft.sandboxBackend = "docker";
      props.draft.fsRootIsolation = true;
    }
  },
);

watch(
  () => props.draft.sandboxBackend,
  (backend) => {
    if (!props.draft.sandboxEnabled) return;
    if (backend === "docker") {
      props.draft.fsRootIsolation = true;
    }
  },
);
</script>

<template>
  <div class="agent-settings-form" :class="{ 'agent-settings-form--compact': compact }">
    <section class="agent-settings-section">
      <h3 class="agent-settings-section__title">基础信息</h3>
      <label class="agent-settings-field">
        <span>显示名称</span>
        <input v-model="draft.displayName" type="text" class="agent-settings-input" placeholder="Agent 名称" />
      </label>
      <label class="agent-settings-field">
        <span>简介</span>
        <textarea v-model="draft.description" class="agent-settings-input agent-settings-input--area" rows="2" placeholder="可选描述" />
      </label>
      <label class="agent-settings-field">
        <span>使用的 LLM 配置</span>
        <select v-model="draft.llmProfileId" class="agent-settings-input">
          <option disabled value="">请选择</option>
          <option v-for="p in llmProfiles" :key="p.id" :value="p.id">
            {{ p.id }}{{ p.model ? ` · ${p.model}` : "" }}
          </option>
        </select>
      </label>
      <p v-if="!llmProfiles.length" class="agent-settings-hint">请先在「设置 › 连接」中添加 LLM 配置</p>
      <label class="agent-settings-check">
        <input v-model="draft.sandboxEnabled" type="checkbox" />
        <span>启用沙箱</span>
      </label>
      <p class="agent-settings-hint">
        关闭：在宿主机运行，由工具组与策略约束保证安全。开启：在隔离环境中执行，需选择沙箱模式并配置参数。
      </p>
      <template v-if="draft.sandboxEnabled">
        <p class="agent-settings-hint">
          沙箱模式：本机 Docker（Linux 容器）。需安装 Docker；命令行在容器内执行。镜像见 packaging/sandbox。
          在同组其他 Node 上创建 Agent 属于 Placement，与沙箱无关（见 docs/design/remote-agent-placement.md）。
        </p>
        <label class="agent-settings-field">
          <span>镜像</span>
          <input v-model="draft.sandboxImage" type="text" class="agent-settings-input" placeholder="dagents-sandbox:latest" />
        </label>
        <label class="agent-settings-field">
          <span>网络</span>
          <input v-model="draft.sandboxNetwork" type="text" class="agent-settings-input" placeholder="none" />
        </label>
        <label class="agent-settings-field">
          <span>内存上限</span>
          <input v-model="draft.sandboxMemory" type="text" class="agent-settings-input" placeholder="512m（可选）" />
        </label>
        <label class="agent-settings-field">
          <span>CPU 上限</span>
          <input v-model="draft.sandboxCpus" type="text" class="agent-settings-input" placeholder="1.0（可选）" />
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.allowBash" type="checkbox" />
          <span>允许命令行工具</span>
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.allowNetworkTools" type="checkbox" />
          <span>允许网络类工具（浏览器 / A2A）</span>
        </label>
        <p class="agent-settings-hint">隔离工作区在启用 Docker 沙箱时强制开启（agents/&lt;id&gt;/data）。</p>
      </template>
    </section>

    <div class="agent-settings-advanced-toggle">
      <button type="button" class="btn btn--ghost btn--sm" @click="advancedOpen = !advancedOpen">
        {{ advancedOpen ? "收起高级设置" : "高级设置" }}
      </button>
    </div>

    <div v-if="advancedOpen" class="agent-settings-advanced">
      <section class="agent-settings-section">
        <h3 class="agent-settings-section__title">工具组</h3>
        <p class="agent-settings-hint">本 Agent 可用的内置工具组。不勾选表示不额外收窄（由运行时按沙箱等约束决定）。</p>
        <div class="agent-settings-toggles">
          <label v-for="g in TOOL_GROUPS" :key="g.name" class="agent-settings-check">
            <input
              type="checkbox"
              :checked="draft.toolGroups?.includes(g.name)"
              @change="toggleGroup(g.name)"
            />
            <span>{{ g.label }}{{ g.beta ? "（Beta）" : "" }}</span>
          </label>
        </div>
      </section>

      <section class="agent-settings-section">
        <h3 class="agent-settings-section__title">能力开关</h3>
        <p class="agent-settings-hint">技能能力由上方工具组「技能」控制；Node 进程总闸仍在设置 › 能力。</p>
        <label class="agent-settings-check">
          <input v-model="draft.childAgentsEnabled" type="checkbox" />
          <span>子 Agent</span>
        </label>
        <label class="agent-settings-field">
          <span>单条消息工具步上限</span>
          <input v-model.number="draft.maxToolLoops" type="number" min="1" class="agent-settings-input" />
        </label>
      </section>

      <section v-if="skillsToolEnabled" class="agent-settings-section">
        <h3 class="agent-settings-section__title">可见 Skills</h3>
        <p class="agent-settings-hint">
          勾选本 Agent 可发现 / 加载的 skills。全选表示不限制（Node 目录新增 skill 自动可见）；取消勾选则仅白名单内可用。
        </p>
        <div class="agent-settings-field__head">
          <span class="agent-settings-hint" style="margin: 0">
            <template v-if="draft.visibleSkills === null">当前：不限制（全部）</template>
            <template v-else>当前：已选 {{ draft.visibleSkills.length }} 项</template>
          </span>
          <div class="agent-settings-skill-actions">
            <button type="button" class="btn btn--ghost btn--sm" :disabled="catalogLoading" @click="selectAllSkills">全选</button>
            <button type="button" class="btn btn--ghost btn--sm" :disabled="catalogLoading" @click="clearAllSkills">全不选</button>
          </div>
        </div>
        <p v-if="catalogLoading" class="agent-settings-hint">加载目录中…</p>
        <p v-else-if="catalogError" class="agent-settings-hint">{{ catalogError }}</p>
        <p v-else-if="!catalogEnabled" class="agent-settings-hint">Node 未启用 skills（settings.skills.enabled）。</p>
        <p v-else-if="!catalogSkills.length" class="agent-settings-hint">目录为空（.runtime/skills）。</p>
        <div v-else class="agent-settings-skill-list">
          <label v-for="s in catalogSkills" :key="s.skill_name" class="agent-settings-check agent-settings-check--skill">
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
      </section>

      <section class="agent-settings-section">
        <h3 class="agent-settings-section__title">侧车与长期记忆</h3>
        <p class="agent-settings-hint">
          正文按 Agent 存于 SQLite；开关控制是否注入到 system prompt。
        </p>
        <label class="agent-settings-check">
          <input v-model="draft.promptSoulEnabled" type="checkbox" />
          <span>接入 soul（角色设定）</span>
        </label>
        <label v-if="draft.promptSoulEnabled" class="agent-settings-field">
          <span>soul 正文（数据库）</span>
          <textarea v-model="draft.promptSoulMd" class="agent-settings-input agent-settings-input--area" rows="4" placeholder="角色设定…" />
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.promptUserEnabled" type="checkbox" />
          <span>接入 user（用户偏好）</span>
        </label>
        <label v-if="draft.promptUserEnabled" class="agent-settings-field">
          <span>user 正文（数据库）</span>
          <textarea v-model="draft.promptUserMd" class="agent-settings-input agent-settings-input--area" rows="3" placeholder="用户偏好…" />
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.promptCustomEnabled" type="checkbox" />
          <span>接入 custom（临时指令）</span>
        </label>
        <label v-if="draft.promptCustomEnabled" class="agent-settings-field">
          <span>custom 正文（数据库）</span>
          <textarea v-model="draft.promptCustomMd" class="agent-settings-input agent-settings-input--area" rows="3" placeholder="临时指令…" />
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.promptLongTermEnabled" type="checkbox" />
          <span>接入 long_term（长期记忆）</span>
        </label>
        <template v-if="draft.promptLongTermEnabled">
          <label class="agent-settings-field">
            <span>长期记忆作用域</span>
            <select v-model="draft.promptLongTermScope" class="agent-settings-input">
              <option v-for="opt in LONG_TERM_SCOPES" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
          </label>
          <div class="agent-settings-field">
            <div class="agent-settings-field__head">
              <span>{{ longTermScopeLabel }}（结构化条目；Agent 可用 remember 工具写入）</span>
              <button type="button" class="btn btn--ghost btn--sm" @click="addLongTermEntry">+ 添加条目</button>
            </div>
            <p v-if="!(activeLongTermEntries || []).length" class="agent-settings-hint">
              暂无记忆条目。可手动添加，或由 Agent 通过 remember 工具写入。
            </p>
            <div
              v-for="(entry, idx) in activeLongTermEntries || []"
              :key="entry.id || `new-${idx}`"
              class="agent-settings-longterm-entry"
            >
              <input
                v-model="entry.id"
                class="agent-settings-input agent-settings-input--mono"
                placeholder="条目 ID（可留空自动生成）"
              />
              <textarea
                v-model="entry.content"
                class="agent-settings-input agent-settings-input--area"
                rows="2"
                placeholder="记忆内容…"
              />
              <button type="button" class="btn btn--ghost btn--sm" @click="removeLongTermEntry(idx)">删除</button>
            </div>
          </div>
        </template>
      </section>
    </div>
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

.agent-settings-section__title {
  margin: 0 0 10px;
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-muted);
}

.agent-settings-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 8px;
  font-size: 11.5px;
  color: var(--color-text-subtle);
}

.agent-settings-input {
  padding: 7px 10px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
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

.agent-settings-longterm-entry {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 6px;
  margin-bottom: 10px;
  padding: 8px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
}

.agent-settings-longterm-entry textarea {
  grid-column: 1 / -1;
}

.agent-settings-input--mono {
  font-family: var(--font-mono);
  font-size: 11.5px;
}

.agent-settings-toggles {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 4px 10px;
}

.agent-settings-advanced-toggle {
  display: flex;
}

.agent-settings-advanced {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
