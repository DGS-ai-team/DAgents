<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import { agentStore } from "../stores/agent.js";

const props = defineProps({
  agentId: { type: String, default: "" },
  embedded: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const busySkill = ref("");
const error = ref("");
const data = ref(null);

const resolvedAgentId = computed(() => String(props.agentId || agentStore.agentId || "").trim());

const loadedSkills = computed(() => {
  const rows = data.value?.loaded_skills;
  return Array.isArray(rows) ? rows : [];
});

const availableSkills = computed(() => {
  const rows = data.value?.available_skills;
  return Array.isArray(rows) ? rows : [];
});

const loadedNameSet = computed(() => {
  const set = new Set();
  for (const sk of loadedSkills.value) {
    const name = skillName(sk);
    if (name) set.add(name);
  }
  return set;
});

function skillName(sk) {
  return String(sk?.skill_name || sk?.name || "").trim();
}

function skillDescription(sk) {
  return String(sk?.description || "").trim();
}

function isLoaded(name) {
  return loadedNameSet.value.has(name);
}

async function load() {
  const sid = props.agentId || agentStore.agentId;
  if (!sid) {
    data.value = null;
    error.value = "";
    loading.value = false;
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.listSkills(sid);
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

async function handleLoad(name) {
  const sid = props.agentId || agentStore.agentId;
  if (!sid || !name || busySkill.value) return;
  busySkill.value = name;
  error.value = "";
  try {
    await api.loadSkill(sid, name);
    await load();
  } catch (e) {
    error.value = e.message;
  } finally {
    busySkill.value = "";
  }
}

async function handleUnload(name) {
  const sid = props.agentId || agentStore.agentId;
  if (!sid || !name || busySkill.value) return;
  busySkill.value = name;
  error.value = "";
  try {
    await api.unloadSkill(sid, name);
    await load();
  } catch (e) {
    error.value = e.message;
  } finally {
    busySkill.value = "";
  }
}

onMounted(load);
watch(() => props.agentId || agentStore.agentId, load);

function onSkillsChanged(event) {
  const eventAgentId = String(event?.detail?.agent_id || "").trim();
  if (eventAgentId && eventAgentId !== resolvedAgentId.value) return;
  load();
}

onMounted(() => window.addEventListener("dagents:skills-changed", onSkillsChanged));
onBeforeUnmount(() => window.removeEventListener("dagents:skills-changed", onSkillsChanged));
</script>

<template>
  <section class="panel panel-overlay__card skills-panel" :class="{ 'settings-embedded-panel': embedded }">
    <header v-if="!embedded" class="panel__header skills-panel__header">
      <div>
        <div class="panel__title">技能</div>
        <div class="skills-panel__subtitle">{{ resolvedAgentId || "—" }}</div>
      </div>
      <div class="skills-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body skills-panel__body">
      <div v-if="embedded" class="skills-panel__embedded-toolbar">
        <div>
          <span class="skills-panel__context-label">当前智能体</span>
          <code>{{ resolvedAgentId || "未选择" }}</code>
        </div>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">
          {{ loading ? "刷新中…" : "刷新" }}
        </button>
      </div>
      <div v-if="resolvedAgentId" class="skills-panel__notice">
        模型会通过技能工具按需发现并启用技能；这里的操作只修改当前会话状态，下一次模型步骤生效。
      </div>
      <div v-if="!resolvedAgentId" class="skills-panel__empty">请先在对话中打开一个智能体。</div>
      <div v-else-if="loading && !data" class="skills-panel__loading">加载中…</div>
      <div v-else-if="error" class="skills-panel__error">{{ error }}</div>
      <template v-else-if="data">
        <section class="skills-section">
          <h3 class="skills-section__title">已加载 ({{ loadedSkills.length }})</h3>
          <ul v-if="loadedSkills.length" class="skills-list">
            <li v-for="sk in loadedSkills" :key="skillName(sk)" class="skills-item skills-item--loaded">
              <div class="skills-item__main">
                <div class="skills-item__name">{{ skillName(sk) }}</div>
                <div v-if="skillDescription(sk)" class="skills-item__desc">{{ skillDescription(sk) }}</div>
              </div>
              <button
                type="button"
                class="btn btn--ghost btn--sm btn--danger"
                :disabled="!!busySkill"
                @click="handleUnload(skillName(sk))"
              >
                {{ busySkill === skillName(sk) ? "…" : "卸载" }}
              </button>
            </li>
          </ul>
          <div v-else class="skills-panel__empty-card">
            <strong>暂无已加载技能</strong>
            <span>模型或你从下方目录启用技能后，会显示在这里。</span>
          </div>
        </section>

        <section class="skills-section">
          <h3 class="skills-section__title">技能目录 ({{ availableSkills.length }})</h3>
          <ul v-if="availableSkills.length" class="skills-list">
            <li
              v-for="sk in availableSkills"
              :key="skillName(sk)"
              class="skills-item"
              :class="{ 'skills-item--loaded': isLoaded(skillName(sk)) }"
            >
              <div class="skills-item__main">
                <div class="skills-item__name">
                  {{ skillName(sk) }}
                  <span v-if="isLoaded(skillName(sk))" class="skills-item__badge">已加载</span>
                </div>
                <div v-if="skillDescription(sk)" class="skills-item__desc">{{ skillDescription(sk) }}</div>
              </div>
              <button
                v-if="!isLoaded(skillName(sk))"
                type="button"
                class="btn btn--primary btn--sm"
                :disabled="!!busySkill"
                @click="handleLoad(skillName(sk))"
              >
                {{ busySkill === skillName(sk) ? "…" : "加载" }}
              </button>
            </li>
          </ul>
          <div v-else class="skills-panel__empty-card">
            <strong>暂无可用技能</strong>
            <span>技能目录为空，或当前 Node 尚未安装技能。</span>
          </div>
        </section>
      </template>
    </div>
  </section>
</template>
