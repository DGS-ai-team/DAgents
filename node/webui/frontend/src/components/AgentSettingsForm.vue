<script setup>
/**
 * Agent 设置表单：基础信息 + 可展开的高级设置（工具 / 沙箱 / 侧车）。
 * 创建与设置页共用。
 */
import { computed } from "vue";
import { TOOL_GROUPS } from "../utils/agentTemplateForm.js";

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

function toggleGroup(name) {
  const set = new Set(props.draft.toolGroups || []);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  props.draft.toolGroups = [...set].sort();
}
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
        <span>沙箱运行</span>
      </label>
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
        <label class="agent-settings-check">
          <input v-model="draft.skillsEnabled" type="checkbox" />
          <span>技能</span>
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.childAgentsEnabled" type="checkbox" />
          <span>子 Agent</span>
        </label>
        <label class="agent-settings-field">
          <span>单条消息工具步上限</span>
          <input v-model.number="draft.maxToolLoops" type="number" min="1" class="agent-settings-input" />
        </label>
      </section>

      <section class="agent-settings-section">
        <h3 class="agent-settings-section__title">沙箱详情</h3>
        <label class="agent-settings-field">
          <span>后端</span>
          <select v-model="draft.sandboxBackend" class="agent-settings-input">
            <option value="process">process（应用层隔离）</option>
            <option value="docker">docker（bash 进容器）</option>
          </select>
        </label>
        <template v-if="draft.sandboxBackend === 'docker'">
          <p class="agent-settings-hint">
            需本机 Docker。Agent 在内存时预创建常驻容器（Alpine Linux）；bash 经 docker exec；空闲 15 分钟回收。镜像见 packaging/sandbox。
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
        </template>
        <label class="agent-settings-check">
          <input v-model="draft.fsRootIsolation" type="checkbox" :disabled="draft.sandboxBackend === 'docker'" />
          <span>隔离工作区（agents/&lt;id&gt;/data）{{ draft.sandboxBackend === 'docker' ? '（docker 强制）' : '' }}</span>
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.allowBash" type="checkbox" />
          <span>允许命令行工具</span>
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.allowNetworkTools" type="checkbox" />
          <span>允许网络类工具（浏览器 / A2A）</span>
        </label>
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
          <span>soul.md 正文</span>
          <textarea v-model="draft.promptSoulMd" class="agent-settings-input agent-settings-input--area" rows="4" placeholder="角色设定…" />
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.promptUserEnabled" type="checkbox" />
          <span>接入 user（用户偏好）</span>
        </label>
        <label v-if="draft.promptUserEnabled" class="agent-settings-field">
          <span>user.md 正文</span>
          <textarea v-model="draft.promptUserMd" class="agent-settings-input agent-settings-input--area" rows="3" placeholder="用户偏好…" />
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.promptCustomEnabled" type="checkbox" />
          <span>接入 custom（临时指令）</span>
        </label>
        <label v-if="draft.promptCustomEnabled" class="agent-settings-field">
          <span>custom.md 正文</span>
          <textarea v-model="draft.promptCustomMd" class="agent-settings-input agent-settings-input--area" rows="3" placeholder="临时指令…" />
        </label>
        <label class="agent-settings-check">
          <input v-model="draft.promptLongTermEnabled" type="checkbox" />
          <span>接入 long_term（长期记忆）</span>
        </label>
        <label v-if="draft.promptLongTermEnabled" class="agent-settings-field">
          <span>long_term.md 正文</span>
          <textarea v-model="draft.promptLongTermMd" class="agent-settings-input agent-settings-input--area" rows="4" placeholder="长期记忆…" />
        </label>
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

.agent-settings-hint {
  margin: 0 0 8px;
  font-size: 11.5px;
  line-height: 1.45;
  color: var(--color-text-subtle);
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
