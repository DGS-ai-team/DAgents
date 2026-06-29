<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  createCase,
  deleteCase,
  deleteCaseMessage,
  exportCaseJsonl,
  fetchCases,
  importCaseJsonl,
  insertCaseMessage,
  patchCase,
  updateCaseMessage,
} from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const cases = ref([]);
const selectedId = ref("");
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const createFileInput = ref(null);
const importFileInput = ref(null);

const createForm = reactive({
  case_id: "",
  name: "",
  description: "",
  skill_ids: "",
  plugin_ids: "",
  file: null,
});

const editMeta = reactive({
  name: "",
  description: "",
  skill_ids: "",
  plugin_ids: "",
});

const messageEditor = reactive({
  open: false,
  mode: "edit",
  index: null,
  id: "",
  role: "user",
  recorded_at: "",
  content: "",
});

const selectedCase = computed(() => cases.value.find((c) => c.case_id === selectedId.value) || null);

function selectCase(item) {
  selectedId.value = item.case_id;
  editMeta.name = item.name || "";
  editMeta.description = item.description || "";
  editMeta.skill_ids = (item.resources?.skill_ids || []).join(", ");
  editMeta.plugin_ids = (item.resources?.plugin_ids || []).join(", ");
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    cases.value = await fetchCases();
    if (!selectedId.value && cases.value.length) {
      selectCase(cases.value[0]);
    } else if (selectedId.value) {
      const found = cases.value.find((c) => c.case_id === selectedId.value);
      if (found) selectCase(found);
      else if (cases.value.length) selectCase(cases.value[0]);
      else selectedId.value = "";
    }
  } catch (err) {
    error.value = err.message;
    emit("toast", { message: err.message, type: "error" });
  } finally {
    loading.value = false;
  }
}

function onCreateFile(e) {
  createForm.file = e.target.files?.[0] || null;
}

async function onCreate() {
  if (!createForm.case_id.trim() || !createForm.name.trim()) {
    emit("toast", { message: "case_id 与名称必填", type: "error" });
    return;
  }
  saving.value = true;
  try {
    const created = await createCase({
      caseId: createForm.case_id.trim(),
      name: createForm.name.trim(),
      description: createForm.description.trim(),
      skillIds: createForm.skill_ids,
      pluginIds: createForm.plugin_ids,
      file: createForm.file,
    });
    cases.value = [created, ...cases.value.filter((c) => c.case_id !== created.case_id)];
    selectCase(created);
    emit("toast", { message: `已创建案例 ${created.case_id}`, type: "success" });
    createForm.case_id = "";
    createForm.name = "";
    createForm.description = "";
    createForm.skill_ids = "";
    createForm.plugin_ids = "";
    createForm.file = null;
    if (createFileInput.value) createFileInput.value.value = "";
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onSaveMeta() {
  if (!selectedCase.value) return;
  saving.value = true;
  try {
    const updated = await patchCase(selectedCase.value.case_id, {
      name: editMeta.name.trim(),
      description: editMeta.description.trim(),
      resources: {
        skill_ids: editMeta.skill_ids.split(",").map((s) => s.trim()).filter(Boolean),
        plugin_ids: editMeta.plugin_ids.split(",").map((s) => s.trim()).filter(Boolean),
      },
    });
    cases.value = cases.value.map((c) => (c.case_id === updated.case_id ? updated : c));
    selectCase(updated);
    emit("toast", { message: "案例信息已保存", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onDeleteCase() {
  if (!selectedCase.value) return;
  const id = selectedCase.value.case_id;
  if (!window.confirm(`删除案例 ${id}？`)) return;
  try {
    await deleteCase(id);
    cases.value = cases.value.filter((c) => c.case_id !== id);
    selectedId.value = cases.value[0]?.case_id || "";
    if (cases.value[0]) selectCase(cases.value[0]);
    emit("toast", { message: `已删除 ${id}`, type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

async function onImportJsonl(e) {
  if (!selectedCase.value) return;
  const file = e.target.files?.[0];
  if (!file) return;
  const replace = window.confirm("确定用上传文件替换现有消息？取消则追加到末尾。");
  try {
    const updated = await importCaseJsonl(selectedCase.value.case_id, file, { replace });
    cases.value = cases.value.map((c) => (c.case_id === updated.case_id ? updated : c));
    selectCase(updated);
    emit("toast", { message: replace ? "已替换消息列表" : "已追加消息", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    if (importFileInput.value) importFileInput.value.value = "";
  }
}

function openInsertMessage(index = null) {
  messageEditor.open = true;
  messageEditor.mode = "insert";
  messageEditor.index = index;
  messageEditor.id = "";
  messageEditor.role = "user";
  messageEditor.recorded_at = "";
  messageEditor.content = "";
}

function openEditMessage(msg, index) {
  messageEditor.open = true;
  messageEditor.mode = "edit";
  messageEditor.index = index;
  messageEditor.id = msg.id;
  messageEditor.role = msg.role || "user";
  messageEditor.recorded_at = msg.recorded_at || "";
  messageEditor.content = msg.content || "";
}

function closeMessageEditor() {
  messageEditor.open = false;
}

async function onSaveMessage() {
  if (!selectedCase.value) return;
  saving.value = true;
  const payload = {
    id: messageEditor.id || crypto.randomUUID(),
    role: messageEditor.role,
    recorded_at: messageEditor.recorded_at,
    content: messageEditor.content,
    raw: null,
  };
  try {
    let updated;
    if (messageEditor.mode === "insert") {
      updated = await insertCaseMessage(selectedCase.value.case_id, {
        index: messageEditor.index,
        message: payload,
      });
    } else {
      updated = await updateCaseMessage(selectedCase.value.case_id, messageEditor.id, payload);
    }
    cases.value = cases.value.map((c) => (c.case_id === updated.case_id ? updated : c));
    selectCase(updated);
    closeMessageEditor();
    emit("toast", { message: "消息已保存", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onDeleteMessage(msg) {
  if (!selectedCase.value) return;
  if (!window.confirm(`删除 ${msg.role} 消息？`)) return;
  try {
    const updated = await deleteCaseMessage(selectedCase.value.case_id, msg.id);
    cases.value = cases.value.map((c) => (c.case_id === updated.case_id ? updated : c));
    selectCase(updated);
    emit("toast", { message: "消息已删除", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

async function onExport() {
  if (!selectedCase.value) return;
  try {
    await exportCaseJsonl(selectedCase.value.case_id);
    emit("toast", { message: "已开始下载 JSONL", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

function preview(text, max = 80) {
  const s = String(text || "").replace(/\s+/g, " ").trim();
  if (s.length <= max) return s || "—";
  return `${s.slice(0, max)}…`;
}

watch(
  () => props.active,
  (now) => {
    if (now) load();
  },
);
onMounted(() => {
  if (props.active) load();
});
defineExpose({ load });
</script>

<template>
  <div class="cases-layout">
    <section class="panel cases-create">
      <div class="panel-head">
        <h2 class="panel-title">新建案例</h2>
        <span class="panel-meta">上传 Node history JSONL 初始化消息列表</span>
      </div>
      <div class="filters-grid">
        <label class="field">
          <span>case_id</span>
          <input v-model="createForm.case_id" placeholder="demo-restart-service" />
        </label>
        <label class="field field-grow">
          <span>名称</span>
          <input v-model="createForm.name" placeholder="服务重启演示" />
        </label>
        <label class="field field-grow">
          <span>描述</span>
          <input v-model="createForm.description" placeholder="可选说明" />
        </label>
        <label class="field field-grow">
          <span>关联 Skills（逗号分隔）</span>
          <input v-model="createForm.skill_ids" placeholder="service-restart, ops-runbook" />
        </label>
        <label class="field field-grow">
          <span>关联 Plugins（逗号分隔）</span>
          <input v-model="createForm.plugin_ids" placeholder="officecli" />
        </label>
        <label class="field field-grow">
          <span>JSONL 文件</span>
          <input ref="createFileInput" type="file" accept=".jsonl,.json,.txt" @change="onCreateFile" />
        </label>
      </div>
      <div class="panel-actions">
        <button type="button" class="btn btn-primary" :disabled="saving" @click="onCreate">
          创建案例
        </button>
      </div>
    </section>

    <div class="cases-split">
      <aside class="panel cases-list">
        <div class="panel-head">
          <h2 class="panel-title">案例列表</h2>
          <button type="button" class="btn btn-ghost btn-sm" :disabled="loading" @click="load">刷新</button>
        </div>
        <p v-if="error" class="panel-error">{{ error }}</p>
        <p v-else-if="loading && !cases.length" class="panel-meta">加载中…</p>
        <p v-else-if="!cases.length" class="panel-meta">暂无案例</p>
        <ul v-else class="cases-list-items">
          <li
            v-for="item in cases"
            :key="item.case_id"
            :class="{ active: item.case_id === selectedId }"
          >
            <button type="button" class="cases-list-btn" @click="selectCase(item)">
              <strong>{{ item.name }}</strong>
              <span>{{ item.case_id }} · {{ item.messages?.length || 0 }} 条消息</span>
            </button>
          </li>
        </ul>
      </aside>

      <section v-if="selectedCase" class="panel cases-detail">
        <div class="panel-head">
          <div>
            <h2 class="panel-title">{{ selectedCase.name }}</h2>
            <span class="panel-meta">{{ selectedCase.case_id }}</span>
          </div>
          <div class="panel-actions">
            <button type="button" class="btn btn-ghost btn-sm" @click="onExport">导出 JSONL</button>
            <label class="btn btn-ghost btn-sm">
              导入 JSONL
              <input
                ref="importFileInput"
                type="file"
                accept=".jsonl,.json,.txt"
                hidden
                @change="onImportJsonl"
              />
            </label>
            <button type="button" class="btn btn-danger btn-sm" @click="onDeleteCase">删除</button>
          </div>
        </div>

        <div class="filters-grid">
          <label class="field field-grow">
            <span>名称</span>
            <input v-model="editMeta.name" />
          </label>
          <label class="field field-grow">
            <span>描述</span>
            <input v-model="editMeta.description" />
          </label>
          <label class="field field-grow">
            <span>Skills</span>
            <input v-model="editMeta.skill_ids" placeholder="skill-a, skill-b" />
          </label>
          <label class="field field-grow">
            <span>Plugins</span>
            <input v-model="editMeta.plugin_ids" />
          </label>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-primary btn-sm" :disabled="saving" @click="onSaveMeta">
            保存信息
          </button>
          <button type="button" class="btn btn-ghost btn-sm" @click="openInsertMessage()">插入消息</button>
        </div>

        <div class="cases-messages">
          <h3 class="cases-subtitle">消息列表（{{ selectedCase.messages?.length || 0 }}）</h3>
          <table class="data-table">
            <thead>
              <tr>
                <th>#</th>
                <th>role</th>
                <th>内容预览</th>
                <th>recorded_at</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(msg, idx) in selectedCase.messages || []" :key="msg.id">
                <td>{{ idx + 1 }}</td>
                <td><code>{{ msg.role }}</code></td>
                <td>{{ preview(msg.content) }}</td>
                <td class="mono">{{ msg.recorded_at || "—" }}</td>
                <td class="cases-row-actions">
                  <button type="button" class="btn btn-ghost btn-sm" @click="openInsertMessage(idx)">前插</button>
                  <button type="button" class="btn btn-ghost btn-sm" @click="openEditMessage(msg, idx)">编辑</button>
                  <button type="button" class="btn btn-ghost btn-sm" @click="onDeleteMessage(msg)">删</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-else class="panel cases-detail cases-detail--empty">
        <p class="panel-meta">选择或创建一个案例</p>
      </section>
    </div>

    <div v-if="messageEditor.open" class="cases-modal-backdrop" @click.self="closeMessageEditor">
      <div class="cases-modal panel">
        <div class="panel-head">
          <h3 class="panel-title">{{ messageEditor.mode === "insert" ? "插入消息" : "编辑消息" }}</h3>
        </div>
        <div class="filters-grid">
          <label class="field">
            <span>role</span>
            <select v-model="messageEditor.role">
              <option value="user">user</option>
              <option value="assistant">assistant</option>
              <option value="system">system</option>
              <option value="tool">tool</option>
            </select>
          </label>
          <label class="field field-grow">
            <span>recorded_at</span>
            <input v-model="messageEditor.recorded_at" placeholder="2026-06-29T15:04:05+08:00" />
          </label>
        </div>
        <label class="field field-grow">
          <span>content</span>
          <textarea v-model="messageEditor.content" rows="8" class="cases-textarea" />
        </label>
        <div class="panel-actions">
          <button type="button" class="btn btn-primary" :disabled="saving" @click="onSaveMessage">保存</button>
          <button type="button" class="btn btn-ghost" @click="closeMessageEditor">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cases-layout {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.cases-split {
  display: grid;
  grid-template-columns: minmax(220px, 280px) 1fr;
  gap: 16px;
  align-items: start;
}
.cases-list-items {
  list-style: none;
  margin: 0;
  padding: 0;
}
.cases-list-items li.active .cases-list-btn {
  background: var(--surface-2, #f3f4f6);
  border-color: var(--accent, #6366f1);
}
.cases-list-btn {
  width: 100%;
  text-align: left;
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 8px;
  background: #fff;
  padding: 10px 12px;
  margin-bottom: 8px;
  cursor: pointer;
}
.cases-list-btn strong {
  display: block;
  font-size: 14px;
}
.cases-list-btn span {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: var(--muted, #6b7280);
}
.cases-subtitle {
  margin: 16px 0 8px;
  font-size: 14px;
}
.cases-row-actions {
  white-space: nowrap;
}
.cases-detail--empty {
  min-height: 240px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.cases-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  padding: 24px;
}
.cases-modal {
  width: min(640px, 100%);
  max-height: 90vh;
  overflow: auto;
}
.cases-textarea {
  width: 100%;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
@media (max-width: 960px) {
  .cases-split {
    grid-template-columns: 1fr;
  }
}
</style>
