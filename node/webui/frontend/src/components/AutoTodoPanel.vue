<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({ agentId: { type: String, required: true } });
const open = ref(false);
const loading = ref(false);
const loaded = ref(false);
const saving = ref(false);
const error = ref("");
const conflict = ref("");
const todos = ref([]);
const draft = ref("");
const edits = ref({});
const orphanDrafts = computed(() => Object.entries(edits.value).filter(([id]) => !todos.value.some((todo) => todo.id === id)).map(([id, value]) => ({ id, text: value.text, status: value.status })));
let epoch = 0;

function current(id, token) { return id === props.agentId && token === epoch; }
function load({ switching = false } = {}) {
  const id = props.agentId; const token = ++epoch;
  if (switching) { draft.value = ""; edits.value = {}; saving.value = false; loaded.value = false; }
  loading.value = true; error.value = ""; conflict.value = ""; todos.value = [];
  api.listAgentTodos(id).then((data) => {
    if (!current(id, token)) return;
    todos.value = Array.isArray(data?.todos) ? data.todos : [];
    loaded.value = true;
  }).catch((e) => { if (current(id, token)) { loaded.value = false; error.value = e?.message || "待办加载失败"; } })
    .finally(() => { if (current(id, token)) loading.value = false; });
}
function create() {
  const text = draft.value.trim(); if (!text || saving.value || loading.value || !loaded.value) return;
  const id = props.agentId; const token = epoch; saving.value = true; error.value = "";
  api.createAgentTodo(id, { text }).then((todo) => {
    if (!current(id, token)) return;
    todos.value.push(todo); draft.value = "";
  }).catch((e) => { if (current(id, token)) error.value = e?.message || "待办保存失败"; })
    .finally(() => { if (current(id, token)) saving.value = false; });
}
function editFor(todo) { return edits.value[todo.id]?.text ?? todo.text; }
function statusFor(todo) { return edits.value[todo.id]?.status ?? todo.status; }
function setEdit(todo, field, value) { edits.value[todo.id] = { text: editFor(todo), status: statusFor(todo), ...edits.value[todo.id], [field]: value }; }
function save(todo) {
  const text = String(editFor(todo)).trim();
  if (!text || saving.value || loading.value || !loaded.value) return;
  const id = props.agentId; const token = epoch; saving.value = true; error.value = ""; conflict.value = "";
  api.updateAgentTodo(id, todo.id, { expected_revision: todo.revision, text, status: statusFor(todo) }).then((next) => {
    if (!current(id, token)) return;
    const index = todos.value.findIndex((item) => item.id === todo.id);
    if (index >= 0) { todos.value[index] = next; delete edits.value[todo.id]; }
  }).catch((e) => {
    if (!current(id, token)) return;
    if (e?.status === 409) { conflict.value = "该待办已被其他操作更新，请重新加载；你的草稿仍保留。"; }
    else error.value = e?.message || "待办保存失败";
  }).finally(() => { if (current(id, token)) saving.value = false; });
}
function remove(todo) {
  if (saving.value || loading.value || !loaded.value) return;
  const id = props.agentId; const token = epoch; saving.value = true; error.value = ""; conflict.value = "";
  api.deleteAgentTodo(id, todo.id, todo.revision).then(() => {
    if (current(id, token)) todos.value = todos.value.filter((item) => item.id !== todo.id);
  }).catch((e) => { if (current(id, token)) conflict.value = e?.status === 409 ? "删除冲突，请重新加载。" : (e?.message || "待办删除失败"); })
    .finally(() => { if (current(id, token)) saving.value = false; });
}
function reload() { if (!saving.value) load(); }
function recoverDraft(item) { draft.value = item.text; }
watch(() => props.agentId, () => load({ switching: true }), { immediate: true });
onBeforeUnmount(() => { epoch += 1; });
</script>

<template>
  <section class="auto-todo-panel">
    <button class="auto-todo-panel__heading" type="button" :aria-expanded="open" @click="open = !open"><span><span class="settings-kicker">Auto 专属</span><strong>待办事项</strong></span><span aria-hidden="true">{{ open ? "⌃" : "⌄" }}</span></button>
    <div v-if="open" class="auto-todo-panel__body">
      <p v-if="error" class="error" role="alert">{{ error }} <button type="button" class="btn btn--ghost" :disabled="saving || loading" @click="reload">重试</button></p>
      <p v-if="conflict" class="error" role="alert">{{ conflict }} <button type="button" class="btn btn--ghost" :disabled="saving" @click="reload">重新加载</button></p>
      <p v-if="loading" class="hint">加载中…</p>
      <p v-else-if="!todos.length" class="hint">暂无待办事项。</p>
      <ul v-else class="auto-todo-list">
        <li v-for="todo in todos" :key="todo.id" class="auto-todo-list__item">
          <input :value="editFor(todo)" aria-label="待办文本" :disabled="saving || loading" @input="setEdit(todo, 'text', $event.target.value)" @keyup.enter="save(todo)">
          <select :value="statusFor(todo)" aria-label="待办状态" :disabled="saving || loading" @change="setEdit(todo, 'status', $event.target.value)"><option value="pending">待处理</option><option value="in_progress">进行中</option><option value="completed">已完成</option></select>
          <button type="button" class="btn btn--ghost" :disabled="saving || loading" @click="save(todo)">保存</button>
          <button type="button" class="btn btn--ghost" :disabled="saving || loading" @click="remove(todo)">删除</button>
        </li>
      </ul>
      <div v-if="orphanDrafts.length" class="auto-todo-panel__recovery">
        <p class="hint">服务器已删除以下待办，本地草稿仍可恢复：</p>
        <button v-for="item in orphanDrafts" :key="item.id" type="button" class="btn btn--ghost" @click="recoverDraft(item)">恢复草稿：{{ item.text }}</button>
      </div>
      <form class="auto-todo-panel__create" @submit.prevent="create"><input v-model="draft" aria-label="新待办" placeholder="新增待办事项" :disabled="saving || loading || !loaded"><button class="btn btn--primary" type="submit" :disabled="saving || loading || !loaded || !draft.trim()">新增</button></form>
    </div>
  </section>
</template>

<style scoped>
.auto-todo-panel { display: grid; gap: 10px; padding-top: 20px; border-top: 1px solid var(--color-border); }
.auto-todo-panel__heading { display: flex; justify-content: space-between; align-items: center; border: 0; background: transparent; color: var(--color-text); text-align: left; cursor: pointer; padding: 0; }
.auto-todo-panel__heading > span { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
.auto-todo-panel__heading .settings-kicker { display: inline; color: var(--color-text-subtle); font-size: 11px; font-weight: 600; letter-spacing: .02em; white-space: nowrap; }
.auto-todo-panel__heading strong { display: inline; font-size: 15px; }
.auto-todo-panel__body { display: grid; gap: 10px; }
.auto-todo-list { display: grid; gap: 8px; list-style: none; padding: 0; margin: 0; }
.auto-todo-list__item, .auto-todo-panel__create { display: flex; gap: 8px; align-items: center; }
.auto-todo-list__item input, .auto-todo-panel__create input { min-width: 0; flex: 1; }
.auto-todo-list__item select { width: 90px; }
.hint { color: var(--color-text-subtle); font-size: 12px; }
.error { color: var(--color-danger); font-size: 12px; }
</style>
