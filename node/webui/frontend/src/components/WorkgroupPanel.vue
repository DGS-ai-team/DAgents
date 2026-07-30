<script setup>
import { ref, onMounted, computed } from "vue";
import { useRouter, useRoute } from "vue-router";
import * as api from "../api/node.js";

const router = useRouter();
const route = useRoute();
const emit = defineEmits(["select"]);

const workgroups = ref([]);
const loading = ref(false);
const error = ref("");

const selectedId = computed(() => String(route.params.workgroupId || "").trim());

async function refresh() {
  loading.value = true;
  error.value = "";
  try {
    const res = await api.listWorkgroups({ subscribed: true });
    workgroups.value = res.workgroups || [];
  } catch (e) {
    workgroups.value = [];
    error.value = e?.message || "无法加载工作组";
  } finally {
    loading.value = false;
  }
}

function select(id) {
  emit("select", id);
  router.push({ name: "workgroups", params: { workgroupId: id } });
}

function goAgents() {
  router.push({ name: "agents" });
}

onMounted(refresh);

defineExpose({ refresh });
</script>

<template>
  <div class="wg-panel">
    <div class="wg-panel__head">
      <button type="button" class="wg-panel__tab" @click="goAgents">Agents</button>
      <span class="wg-panel__tab wg-panel__tab--active">工作组</span>
      <button type="button" class="wg-panel__refresh" title="刷新" @click="refresh">↻</button>
    </div>
    <p v-if="loading" class="wg-panel__hint">加载中…</p>
    <p v-else-if="error" class="wg-panel__hint wg-panel__hint--err">{{ error }}</p>
    <p v-else-if="!workgroups.length" class="wg-panel__hint">暂无已订阅工作组</p>
    <ul v-else class="wg-panel__list">
      <li
        v-for="wg in workgroups"
        :key="wg.workgroup_id"
        class="wg-panel__item"
        :class="{ 'wg-panel__item--active': wg.workgroup_id === selectedId }"
        @click="select(wg.workgroup_id)"
      >
        <div class="wg-panel__title">{{ wg.display_name || wg.workgroup_id }}</div>
        <div class="wg-panel__meta">{{ wg.status }}</div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.wg-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  border-top: 1px solid var(--border-subtle, #e5e7eb);
}
.wg-panel__head {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  font-size: 0.8rem;
}
.wg-panel__tab {
  background: none;
  border: none;
  color: var(--text-muted, #6b7280);
  cursor: pointer;
  padding: 0.15rem 0;
}
.wg-panel__tab--active {
  color: var(--text, #111);
  font-weight: 600;
  cursor: default;
}
.wg-panel__refresh {
  margin-left: auto;
  border: none;
  background: none;
  cursor: pointer;
  color: var(--text-muted, #6b7280);
}
.wg-panel__hint {
  padding: 0.75rem;
  font-size: 0.8rem;
  color: var(--text-muted, #6b7280);
}
.wg-panel__hint--err {
  color: #b91c1c;
}
.wg-panel__list {
  list-style: none;
  margin: 0;
  padding: 0;
  overflow: auto;
}
.wg-panel__item {
  padding: 0.6rem 0.75rem;
  cursor: pointer;
  border-left: 2px solid transparent;
}
.wg-panel__item:hover {
  background: var(--surface-hover, rgba(0, 0, 0, 0.03));
}
.wg-panel__item--active {
  border-left-color: var(--accent, #2563eb);
  background: var(--surface-active, rgba(37, 99, 235, 0.06));
}
.wg-panel__title {
  font-size: 0.875rem;
  font-weight: 500;
}
.wg-panel__meta {
  font-size: 0.75rem;
  color: var(--text-muted, #6b7280);
  margin-top: 0.15rem;
}
</style>
