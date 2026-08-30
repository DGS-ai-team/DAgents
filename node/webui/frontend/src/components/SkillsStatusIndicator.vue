<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({
  agentId: { type: String, default: "" },
});

const open = ref(false);
const rootRef = ref(null);
const loading = ref(false);
const error = ref("");
const skills = ref([]);
const skillsEnabled = ref(false);
const visibleSkillsRestricted = ref(false);
let loadSequence = 0;
let refreshTimer = null;

const summary = computed(() => String(skills.value.length));
const unrestricted = computed(() => skillsEnabled.value && !visibleSkillsRestricted.value);
const summaryLabel = computed(() => {
  if (!skillsEnabled.value) return "未启用";
  return unrestricted.value ? "全部" : `${summary.value} 个`;
});
const countBadge = computed(() => {
  if (unrestricted.value) return "全";
  return skills.value.length > 9 ? "9+" : summary.value;
});

async function load() {
  const agentId = String(props.agentId || "").trim();
  const sequence = ++loadSequence;
  if (!agentId) {
    skills.value = [];
    skillsEnabled.value = false;
    visibleSkillsRestricted.value = false;
    error.value = "";
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const result = await api.listSkills(agentId);
    if (sequence !== loadSequence || String(props.agentId || "").trim() !== agentId) return;
    skillsEnabled.value = result?.skills_enabled === true;
    visibleSkillsRestricted.value = result?.visible_skills_restricted === true;
    skills.value = Array.isArray(result?.visible_skills)
      ? result.visible_skills.map((name) => String(name || "").trim()).filter(Boolean)
      : [];
    error.value = "";
  } catch (e) {
    if (sequence !== loadSequence) return;
    skills.value = [];
    skillsEnabled.value = false;
    visibleSkillsRestricted.value = false;
    error.value = e?.message || "技能读取失败";
  } finally {
    if (sequence === loadSequence) loading.value = false;
  }
}

function toggle() {
  open.value = !open.value;
  if (open.value) void load();
}

function onDocumentPointerDown(event) {
  if (open.value && !rootRef.value?.contains(event.target)) open.value = false;
}

function onDocumentKeydown(event) {
  if (event.key === "Escape") open.value = false;
}

function onSkillsChanged(event) {
  const eventAgentId = String(event?.detail?.agent_id || "").trim();
  const agentId = String(props.agentId || "").trim();
  if (!eventAgentId || eventAgentId === agentId) void load();
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointerDown);
  document.addEventListener("keydown", onDocumentKeydown);
  window.addEventListener("dagents:skills-changed", onSkillsChanged);
  void load();
  refreshTimer = setInterval(() => void load(), 30_000);
});

watch(() => props.agentId, () => void load());

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onDocumentKeydown);
  window.removeEventListener("dagents:skills-changed", onSkillsChanged);
  if (refreshTimer) clearInterval(refreshTimer);
});
</script>

<template>
  <div ref="rootRef" class="skills-status-indicator">
    <button
      type="button"
      class="skills-status-indicator__trigger"
      :aria-expanded="open"
      :aria-label="`当前 Agent 的技能，${summaryLabel}，点击查看`"
      :title="`当前 Agent 的技能 · ${summaryLabel}`"
      @click="toggle"
    >
      <span class="skills-status-indicator__icon" aria-hidden="true">
        <svg viewBox="0 0 20 20" fill="none">
          <path d="m10 2.8 1.75 4.05 4.45.4-3.35 2.95.98 4.35L10 12.3l-3.83 2.25.98-4.35L3.8 7.25l4.45-.4L10 2.8Z" stroke="currentColor" stroke-width="1.35" stroke-linejoin="round" />
        </svg>
      </span>
      <span v-if="skillsEnabled" class="skills-status-indicator__count" aria-hidden="true">{{ countBadge }}</span>
    </button>

    <div v-if="open" class="skills-status-indicator__popover" role="dialog" aria-label="当前 Agent 的技能">
      <div class="skills-status-indicator__popover-head">
        <strong>当前 Agent 的技能</strong>
        <span>{{ summaryLabel }}</span>
      </div>
      <p v-if="loading && !skills.length" class="skills-status-indicator__muted">读取中…</p>
      <p v-else-if="error" class="skills-status-indicator__error" role="alert">{{ error }}</p>
      <p v-else-if="!skillsEnabled" class="skills-status-indicator__muted">当前 Agent 未启用技能。</p>
      <p v-else-if="unrestricted" class="skills-status-indicator__muted">当前 Agent 可发现全部技能。</p>
      <p v-else-if="!skills.length" class="skills-status-indicator__muted">当前没有绑定可见技能。</p>
      <ul v-else class="skills-status-indicator__list">
        <li v-for="skill in skills" :key="skill" class="skills-status-indicator__item">
          <strong>{{ skill }}</strong>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.skills-status-indicator { position: relative; display: inline-flex; flex: 0 0 auto; min-width: 26px; }
.skills-status-indicator__trigger { position: relative; display: inline-flex; width: 26px; min-width: 26px; height: 26px; flex: 0 0 26px; align-items: center; justify-content: center; padding: 0; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--color-text-muted); cursor: pointer; }
.skills-status-indicator__trigger:hover, .skills-status-indicator__trigger:focus-visible { border-color: var(--color-border); background: var(--color-surface-alt, #f5f7f9); }
.skills-status-indicator__icon, .skills-status-indicator__icon svg { display: block; width: 18px; height: 18px; }
.skills-status-indicator__count { position: absolute; right: -3px; bottom: -3px; display: inline-flex; min-width: 12px; height: 12px; align-items: center; justify-content: center; padding: 0 2px; border: 1px solid var(--color-surface, #fff); border-radius: 999px; background: var(--color-text-muted, #64748b); color: #fff; font-size: 8px; font-weight: 700; line-height: 10px; }
.skills-status-indicator__popover { position: absolute; left: 0; bottom: calc(100% + 8px); z-index: 30; width: min(260px, calc(100vw - 24px)); padding: 10px; border: 1px solid var(--color-border); border-radius: 9px; background: var(--color-surface, #fff); box-shadow: 0 10px 28px rgb(20 35 50 / 16%); }
.skills-status-indicator__popover-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.skills-status-indicator__popover-head strong { color: var(--color-text); font-size: 12px; }
.skills-status-indicator__popover-head span, .skills-status-indicator__muted { color: var(--color-text-subtle); font-size: 10px; }
.skills-status-indicator__error { margin: 12px 0 2px; color: var(--color-danger, #c45757); font-size: 11px; }
.skills-status-indicator__muted { margin: 12px 0 2px; text-align: center; }
.skills-status-indicator__list { display: grid; gap: 5px; max-height: 280px; margin: 9px 0 0; padding: 0; overflow: auto; list-style: none; }
.skills-status-indicator__item { padding: 8px; border: 1px solid var(--color-border); border-radius: 7px; }
.skills-status-indicator__item strong { display: block; overflow: hidden; color: var(--color-text); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
</style>
