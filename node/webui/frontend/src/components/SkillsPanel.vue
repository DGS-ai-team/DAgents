<script setup>
import { computed, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";
import { sessionStore } from "../stores/session.js";

const props = defineProps({
  sessionId: { type: String, default: "" },
});

const emit = defineEmits(["close"]);

const loading = ref(false);
const busySkill = ref("");
const error = ref("");
const data = ref(null);
const showRaw = ref(false);

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
  const sid = props.sessionId || sessionStore.sessionId;
  if (!sid) return;
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
  const sid = props.sessionId || sessionStore.sessionId;
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
  const sid = props.sessionId || sessionStore.sessionId;
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
watch(() => props.sessionId || sessionStore.sessionId, load);
</script>

<template>
  <section class="panel panel-overlay__card skills-panel">
    <header class="panel__header skills-panel__header">
      <div>
        <div class="panel__title">Skills</div>
        <div class="skills-panel__subtitle">{{ sessionId || sessionStore.sessionId || "—" }}</div>
      </div>
      <div class="skills-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" @click="showRaw = !showRaw">
          {{ showRaw ? "友好视图" : "JSON" }}
        </button>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading || !!busySkill" @click="load">
          刷新
        </button>
        <button type="button" class="btn btn--ghost btn--sm" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body skills-panel__body">
      <div v-if="loading && !data" class="skills-panel__loading">加载中…</div>
      <div v-else-if="error" class="skills-panel__error">{{ error }}</div>
      <pre v-else-if="showRaw && data" class="skills-panel__raw">{{ JSON.stringify(data, null, 2) }}</pre>
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
          <p v-else class="skills-panel__empty">当前 session 未加载任何 skill</p>
        </section>

        <section class="skills-section">
          <h3 class="skills-section__title">可用 ({{ availableSkills.length }})</h3>
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
          <p v-else class="skills-panel__empty">磁盘上未发现可用 skill</p>
        </section>
      </template>
    </div>
  </section>
</template>
