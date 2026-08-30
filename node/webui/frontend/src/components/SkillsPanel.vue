<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import * as api from "../api/node.js";

defineProps({
  embedded: { type: Boolean, default: false },
});

const loading = ref(false);
const error = ref("");
const data = ref(null);

const availableSkills = computed(() => {
  const rows = data.value?.available_skills;
  return Array.isArray(rows) ? rows : [];
});

const catalogEnabled = computed(() => data.value?.enabled !== false);

function skillName(skill) {
  return String(skill?.skill_name || skill?.name || "").trim();
}

function skillDescription(skill) {
  return String(skill?.description || "").trim();
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.listNodeSkillsCatalog();
  } catch (cause) {
    error.value = cause?.message || "无法读取技能目录";
    data.value = null;
  } finally {
    loading.value = false;
  }
}

function onSkillsChanged() {
  void load();
}

onMounted(() => {
  void load();
  window.addEventListener("dagents:skills-changed", onSkillsChanged);
});

onBeforeUnmount(() => {
  window.removeEventListener("dagents:skills-changed", onSkillsChanged);
});
</script>

<template>
  <section class="skills-catalog" :class="{ 'settings-embedded-panel': embedded }">
    <div class="settings-section__head skills-catalog__head">
      <div>
        <h2 class="settings-section__title">技能目录</h2>
        <p class="settings-section__desc">
          展示 Node 当前发现的技能元数据。技能内容由模型在需要时读取，不需要在这里加载或卸载。
        </p>
      </div>
      <button
        type="button"
        class="btn btn--ghost btn--sm skills-catalog__refresh"
        :disabled="loading"
        :title="loading ? '正在刷新技能目录' : '刷新技能目录'"
        :aria-label="loading ? '正在刷新技能目录' : '刷新技能目录'"
        @click="load"
      >
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path d="M12.5 5.25A5 5 0 1 0 13 9" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" />
          <path d="M10.25 3.5h2.5V6" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>

    <div class="skills-catalog__lifecycle" role="note">
      <strong>会话中的更新方式</strong>
      <span>
        新会话或上下文重建时会写入最新技能元数据；会话进行中只发送变更事件，模型可调用
        <code>list_available_skills</code> 获取最新目录。
      </span>
    </div>

    <div v-if="loading && !data" class="skills-panel__loading">正在读取技能目录…</div>
    <div v-else-if="error" class="skills-panel__error" role="alert">
      <span>{{ error }}</span>
      <button type="button" class="btn btn--ghost btn--sm" @click="load">重试</button>
    </div>
    <div v-else-if="!catalogEnabled" class="settings-empty-state">
      <strong>技能能力未启用</strong>
      <span>启用 Skills 能力后，Node 会在这里展示可发现的技能。</span>
    </div>
    <ul v-else-if="availableSkills.length" class="skills-list skills-catalog__list">
      <li v-for="skill in availableSkills" :key="skillName(skill)" class="skills-item skills-catalog__item">
        <div class="skills-item__main">
          <div class="skills-item__name">{{ skillName(skill) }}</div>
          <div v-if="skillDescription(skill)" class="skills-item__desc">{{ skillDescription(skill) }}</div>
        </div>
        <span class="skills-catalog__status">可发现</span>
      </li>
    </ul>
    <div v-else class="settings-empty-state skills-catalog__empty">
      <strong>暂无可发现的技能</strong>
      <span>将技能安装到 Node 的 Skills 目录后，此处会自动更新。</span>
    </div>
  </section>
</template>
