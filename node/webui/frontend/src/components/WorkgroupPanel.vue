<script setup>
import { ref, onMounted, computed } from "vue";
import { useRouter, useRoute } from "vue-router";
import * as api from "../api/node.js";

const router = useRouter();
const route = useRoute();
const emit = defineEmits(["select", "created"]);

const workgroups = ref([]);
const available = ref([]);
const loading = ref(false);
const error = ref("");
const creating = ref(false);
const createOpen = ref(false);
const createName = ref("");
const createBusy = ref(false);

const selectedId = computed(() => String(route.params.workgroupId || "").trim());

const availableOnly = computed(() => {
  const sub = new Set(workgroups.value.map((w) => w.workgroup_id));
  return available.value.filter((w) => !sub.has(w.workgroup_id));
});

async function refresh() {
  loading.value = true;
  error.value = "";
  try {
    const [subRes, aclRes] = await Promise.all([
      api.listWorkgroups({ scope: "subscribed" }),
      api.listWorkgroups({ scope: "acl" }),
    ]);
    workgroups.value = subRes.workgroups || [];
    available.value = aclRes.workgroups || [];
  } catch (e) {
    workgroups.value = [];
    available.value = [];
    error.value = e?.message || "无法加载工作组";
  } finally {
    loading.value = false;
  }
}

function select(id) {
  emit("select", id);
  router.push({ name: "workgroups", params: { workgroupId: id } });
}

async function subscribe(id) {
  try {
    await api.subscribeWorkgroup(id);
    await refresh();
    select(id);
  } catch (e) {
    error.value = e?.message || "订阅失败";
  }
}

function goAgents() {
  router.push({ name: "agents" });
}

function openCreate() {
  createOpen.value = true;
  createName.value = "";
}

async function submitCreate() {
  const name = createName.value.trim();
  if (!name || createBusy.value) return;
  createBusy.value = true;
  error.value = "";
  try {
    const res = await api.createWorkgroup(name);
    const wg = res.workgroup || res;
    const id = wg.workgroup_id;
    createOpen.value = false;
    creating.value = false;
    emit("created", id);
    await refresh();
    if (id) select(id);
  } catch (e) {
    error.value = e?.message || "建组失败";
  } finally {
    createBusy.value = false;
  }
}

onMounted(refresh);

defineExpose({ refresh });
</script>

<template>
  <div class="wg-panel">
    <div class="wg-panel__head">
      <button type="button" class="wg-panel__tab" @click="goAgents">Agents</button>
      <span class="wg-panel__tab wg-panel__tab--active">工作组</span>
      <button type="button" class="wg-panel__icon" title="新建工作组" @click="openCreate">+</button>
      <button type="button" class="wg-panel__icon" title="刷新" @click="refresh">↻</button>
    </div>
    <p v-if="loading" class="wg-panel__hint">加载中…</p>
    <p v-else-if="error" class="wg-panel__hint wg-panel__hint--err">{{ error }}</p>
    <template v-else>
      <div class="wg-panel__section-label">已订阅</div>
      <p v-if="!workgroups.length" class="wg-panel__hint">暂无已订阅工作组</p>
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
      <div v-if="availableOnly.length" class="wg-panel__section-label">可订阅（ACL 内）</div>
      <ul v-if="availableOnly.length" class="wg-panel__list">
        <li v-for="wg in availableOnly" :key="'a-' + wg.workgroup_id" class="wg-panel__item wg-panel__item--avail">
          <div class="wg-panel__avail-row">
            <div>
              <div class="wg-panel__title">{{ wg.display_name || wg.workgroup_id }}</div>
              <div class="wg-panel__meta">{{ wg.status }}</div>
            </div>
            <button type="button" class="wg-panel__sub-btn" @click.stop="subscribe(wg.workgroup_id)">
              订阅
            </button>
          </div>
        </li>
      </ul>
    </template>

    <div v-if="createOpen" class="wg-panel__modal" @click.self="createOpen = false">
      <form class="wg-panel__modal-card" @submit.prevent="submitCreate">
        <h3>新建工作组</h3>
        <input
          v-model="createName"
          class="wg-panel__input"
          type="text"
          placeholder="显示名称"
          maxlength="128"
          autofocus
        />
        <div class="wg-panel__modal-actions">
          <button type="button" @click="createOpen = false">取消</button>
          <button type="submit" :disabled="createBusy || !createName.trim()">创建</button>
        </div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.wg-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  border-top: 1px solid var(--border-subtle, #e5e7eb);
  position: relative;
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
.wg-panel__icon {
  margin-left: 0.15rem;
  border: none;
  background: none;
  cursor: pointer;
  color: var(--text-muted, #6b7280);
  font-size: 1rem;
}
.wg-panel__icon:first-of-type {
  margin-left: auto;
}
.wg-panel__section-label {
  padding: 0.5rem 0.75rem 0.25rem;
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted, #9ca3af);
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
.wg-panel__avail-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.wg-panel__sub-btn {
  flex-shrink: 0;
  border: 1px solid var(--border-subtle, #d1d5db);
  background: #fff;
  border-radius: 4px;
  padding: 0.2rem 0.5rem;
  font-size: 0.75rem;
  cursor: pointer;
}
.wg-panel__modal {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.25);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 2rem;
  z-index: 5;
}
.wg-panel__modal-card {
  background: #fff;
  border-radius: 8px;
  padding: 1rem;
  width: min(280px, 90%);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.wg-panel__modal-card h3 {
  margin: 0;
  font-size: 0.95rem;
}
.wg-panel__input {
  padding: 0.45rem 0.6rem;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font: inherit;
}
.wg-panel__modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
}
.wg-panel__modal-actions button {
  border: 1px solid #d1d5db;
  background: #fff;
  border-radius: 6px;
  padding: 0.35rem 0.7rem;
  cursor: pointer;
  font: inherit;
}
.wg-panel__modal-actions button[type="submit"] {
  background: var(--accent, #2563eb);
  border-color: var(--accent, #2563eb);
  color: #fff;
}
.wg-panel__modal-actions button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
