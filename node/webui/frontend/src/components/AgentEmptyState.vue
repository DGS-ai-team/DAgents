<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";
import brandIcon from "@dagents-brand/brand-icon.png";
import UiIcon from "./UiIcon.vue";

const emit = defineEmits(["create", "pick-template"]);

const loading = ref(false);
const templates = ref([]);

const ICON_BY_ID = {
  general: "general",
  "code-reviewer": "code",
  "ops-runner": "ops",
};

const hint = computed(() =>
  !loading.value && templates.value.length === 0
    ? "点击加号创建你的智能体吧"
    : "点一下加号或者下面的模板，让智能体为你工作"
);

const showTemplates = computed(() => loading.value || templates.value.length > 0);

function iconKind(id) {
  return ICON_BY_ID[String(id || "").trim()] || "general";
}

function iconName(id) {
  return { general: "message-circle", code: "code-2", ops: "briefcase" }[iconKind(id)] || "message-circle";
}

async function loadTemplates() {
  loading.value = true;
  try {
    const res = await api.listAgentTemplates();
    templates.value = res.templates || [];
  } catch {
    templates.value = [];
  } finally {
    loading.value = false;
  }
}

onMounted(loadTemplates);
</script>

<template>
  <div class="agent-empty">
    <div class="agent-empty__inner">
      <img class="agent-empty__brand" :src="brandIcon" alt="" aria-hidden="true" />
      <h2 class="agent-empty__title">还没有创建智能体呢</h2>
      <p class="agent-empty__hint">{{ hint }}</p>
      <button
        type="button"
        class="agent-empty__cta"
        title="新建智能体"
        aria-label="新建智能体"
        @click="emit('create')"
      >
        <UiIcon name="circle-plus" :size="48" />
      </button>

      <div v-if="showTemplates" class="agent-empty__templates">
        <p class="agent-empty__templates-label">推荐模板</p>
        <div v-if="loading" class="agent-empty__loading">加载模板…</div>
        <div v-else class="agent-empty__grid">
          <button
            v-for="tpl in templates"
            :key="tpl.id"
            type="button"
            class="agent-empty-tile"
            :title="tpl.description || tpl.display_name || tpl.id"
            @click="emit('pick-template', tpl.id)"
          >
            <span class="agent-empty-tile__icon" :data-kind="iconKind(tpl.id)" aria-hidden="true">
              <UiIcon :name="iconName(tpl.id)" :size="20" />
            </span>
            <span class="agent-empty-tile__name">{{ tpl.display_name || tpl.id }}</span>
            <span class="agent-empty-tile__desc">{{ tpl.description || "从模板创建" }}</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.agent-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
  min-height: min(70vh, 640px);
  padding: 32px 24px;
}

.agent-empty__inner {
  width: min(640px, 100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}

.agent-empty__brand {
  width: 84px;
  height: 84px;
  margin-bottom: 12px;
  object-fit: contain;
  filter: drop-shadow(0 10px 18px color-mix(in srgb, var(--color-primary) 16%, transparent));
}

.agent-empty__title {
  margin: 0;
  font-size: 22px;
  font-weight: 650;
  letter-spacing: -0.02em;
  color: var(--color-text);
}

.agent-empty__hint {
  margin: 8px 0 0;
  font-size: 14px;
  line-height: 1.5;
  color: var(--color-text-muted);
  max-width: 28em;
}

.agent-empty__cta {
  display: grid;
  place-items: center;
  width: 56px;
  height: 56px;
  margin-top: 20px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--color-primary-strong);
  cursor: pointer;
  transition: color 0.15s ease, transform 0.15s ease, background 0.15s ease;
}

.agent-empty__cta svg {
  width: 100%;
  height: 100%;
}

.agent-empty__cta:hover {
  color: var(--color-primary);
  background: color-mix(in srgb, var(--color-primary-strong) 10%, transparent);
  transform: scale(1.04);
}

.agent-empty__cta:focus-visible {
  outline: 2px solid var(--color-primary-strong);
  outline-offset: 3px;
}

.agent-empty__templates {
  width: 100%;
  margin-top: 36px;
}

.agent-empty__templates-label {
  margin: 0 0 12px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--color-text-subtle, var(--color-text-muted));
}

.agent-empty__loading {
  font-size: 13px;
  color: var(--color-text-muted);
}

.agent-empty__grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.agent-empty-tile {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 14px 14px 16px;
  border: 1px solid var(--color-border);
  border-radius: 12px;
  background: var(--color-surface);
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease;
}

.agent-empty-tile:hover {
  border-color: color-mix(in srgb, var(--color-primary-strong) 45%, var(--color-border));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--color-primary-strong) 18%, transparent);
  transform: translateY(-1px);
}

.agent-empty-tile__icon {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  color: var(--color-primary-strong);
  background: color-mix(in srgb, var(--color-primary-strong) 12%, transparent);
}

.agent-empty-tile__icon[data-kind="code"] {
  color: var(--color-info);
  background: var(--color-info-soft);
}

.agent-empty-tile__icon[data-kind="ops"] {
  color: var(--color-warning);
  background: var(--color-warning-soft);
}

.agent-empty-tile__icon svg {
  width: 20px;
  height: 20px;
}

.agent-empty-tile__name {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
}

.agent-empty-tile__desc {
  font-size: 12px;
  line-height: 1.4;
  color: var(--color-text-muted);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

@media (max-width: 720px) {
  .agent-empty__grid {
    grid-template-columns: 1fr;
  }
}
</style>
