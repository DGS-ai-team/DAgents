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
  parseCaseJsonl,
  patchCase,
  replaceCaseMessages,
  updateCaseMessage,
} from "../api.js";
import CaseMessageList from "./CaseMessageList.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const cases = ref([]);
const selectedId = ref("");
const loading = ref(false);
const saving = ref(false);
const parsing = ref(false);
const error = ref("");
const createMode = ref(null); // null | 'upload' | 'edit'
const uploadFileInput = ref(null);
const importFileInput = ref(null);

const draftMeta = reactive({
  case_id: "",
  name: "",
  description: "",
  skill_ids: "",
  plugin_ids: "",
  externaltool_ids: "",
});
const draftMessages = ref([]);
const messagesEditing = ref(false);
const draftMessagesEditing = ref(false);

const editMeta = reactive({
  name: "",
  description: "",
  skill_ids: "",
  plugin_ids: "",
  externaltool_ids: "",
});

const messageEditor = reactive({
  open: false,
  mode: "edit",
  index: null,
  id: "",
  role: "user",
  recorded_at: "",
  content: "",
  target: "case", // 'case' | 'draft'
});

const selectedCase = computed(() => cases.value.find((c) => c.case_id === selectedId.value) || null);

const skillTags = computed(() =>
  (selectedCase.value?.resources?.skill_ids || []).filter(Boolean),
);
const pluginTags = computed(() =>
  (selectedCase.value?.resources?.plugin_ids || []).filter(Boolean),
);
const externaltoolTags = computed(() =>
  (selectedCase.value?.resources?.externaltool_ids || []).filter(Boolean),
);

function selectCase(item) {
  selectedId.value = item.case_id;
  messagesEditing.value = false;
  editMeta.name = item.name || "";
  editMeta.description = item.description || "";
  editMeta.skill_ids = (item.resources?.skill_ids || []).join(", ");
  editMeta.plugin_ids = (item.resources?.plugin_ids || []).join(", ");
  editMeta.externaltool_ids = (item.resources?.externaltool_ids || []).join(", ");
}

function resetDraft() {
  draftMeta.case_id = "";
  draftMeta.name = "";
  draftMeta.description = "";
  draftMeta.skill_ids = "";
  draftMeta.plugin_ids = "";
  draftMeta.externaltool_ids = "";
  draftMessages.value = [];
}

function startCreate() {
  resetDraft();
  draftMessagesEditing.value = false;
  createMode.value = "upload";
  if (uploadFileInput.value) uploadFileInput.value.value = "";
}

function cancelCreate() {
  createMode.value = null;
  resetDraft();
  if (uploadFileInput.value) uploadFileInput.value.value = "";
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    cases.value = await fetchCases();
    if (createMode.value) return;
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

async function onUploadJsonl(e) {
  const file = e.target.files?.[0];
  if (!file) return;
  parsing.value = true;
  try {
    const messages = await parseCaseJsonl(file);
    if (!messages.length) {
      emit("toast", { message: "JSONL 文件中没有有效消息", type: "error" });
      return;
    }
    draftMessages.value = messages;
    draftMessagesEditing.value = false;
    createMode.value = "edit";
    emit("toast", { message: `已解析 ${messages.length} 条消息，请填写案例信息`, type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    parsing.value = false;
    if (uploadFileInput.value) uploadFileInput.value.value = "";
  }
}

async function onFinalizeCreate() {
  if (!draftMeta.case_id.trim() || !draftMeta.name.trim()) {
    emit("toast", { message: "case_id 与名称必填", type: "error" });
    return;
  }
  saving.value = true;
  try {
    const created = await createCase({
      caseId: draftMeta.case_id.trim(),
      name: draftMeta.name.trim(),
      description: draftMeta.description.trim(),
      skillIds: draftMeta.skill_ids,
      pluginIds: draftMeta.plugin_ids,
      externaltoolIds: draftMeta.externaltool_ids,
    });
    const updated = await replaceCaseMessages(created.case_id, draftMessages.value);
    cases.value = [updated, ...cases.value.filter((c) => c.case_id !== updated.case_id)];
    selectCase(updated);
    createMode.value = null;
    resetDraft();
    emit("toast", { message: `已创建案例 ${updated.case_id}`, type: "success" });
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
        externaltool_ids: editMeta.externaltool_ids.split(",").map((s) => s.trim()).filter(Boolean),
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

function openInsertMessage(index = null, target = "case") {
  messageEditor.open = true;
  messageEditor.mode = "insert";
  messageEditor.index = index;
  messageEditor.id = "";
  messageEditor.role = "user";
  messageEditor.recorded_at = "";
  messageEditor.content = "";
  messageEditor.target = target;
}

function openEditMessage(msg, index, target = "case") {
  messageEditor.open = true;
  messageEditor.mode = "edit";
  messageEditor.index = index;
  messageEditor.id = msg.id;
  messageEditor.role = msg.role || "user";
  messageEditor.recorded_at = msg.recorded_at || "";
  messageEditor.content = msg.content || "";
  messageEditor.target = target;
}

function closeMessageEditor() {
  messageEditor.open = false;
}

async function onSaveMessage() {
  saving.value = true;
  const payload = {
    id: messageEditor.id || crypto.randomUUID(),
    role: messageEditor.role,
    recorded_at: messageEditor.recorded_at,
    content: messageEditor.content,
    raw: null,
  };
  try {
    if (messageEditor.target === "draft") {
      const msgs = [...draftMessages.value];
      if (messageEditor.mode === "insert") {
        const idx = messageEditor.index ?? msgs.length;
        msgs.splice(Math.max(0, idx), 0, payload);
      } else {
        const i = msgs.findIndex((m) => m.id === messageEditor.id);
        if (i >= 0) msgs[i] = { ...msgs[i], ...payload };
      }
      draftMessages.value = msgs;
      closeMessageEditor();
      emit("toast", { message: "消息已更新", type: "success" });
      return;
    }
    if (!selectedCase.value) return;
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

async function onDeleteMessage(msg, target = "case") {
  if (target === "draft") {
    if (!window.confirm(`删除 ${msg.role} 消息？`)) return;
    draftMessages.value = draftMessages.value.filter((m) => m.id !== msg.id);
    emit("toast", { message: "消息已删除", type: "success" });
    return;
  }
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

function exitMessagesEditing() {
  messagesEditing.value = false;
  draftMessagesEditing.value = false;
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
  <div class="split-layout" :class="{ 'split-layout--create': createMode }">
    <aside v-if="!createMode" class="panel split-sidebar">
      <div class="panel-head panel-head--inset">
        <div>
          <h2 class="panel-title">案例列表</h2>
          <span class="panel-meta">{{ loading ? "加载中…" : `${cases.length} 个案例` }}</span>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-primary btn-sm" @click="startCreate">新建</button>
          <button type="button" class="btn btn-ghost btn-sm" :disabled="loading" @click="load">
            刷新
          </button>
        </div>
      </div>

      <p v-if="error" class="banner banner-error panel-inset" role="alert">{{ error }}</p>
      <p v-else-if="loading && !cases.length" class="panel-meta panel-inset">加载中…</p>
      <p v-else-if="!cases.length" class="panel-meta panel-inset">暂无案例，点击「新建」添加</p>

      <ul v-else class="list-nav">
        <li
          v-for="item in cases"
          :key="item.case_id"
          :class="{ active: item.case_id === selectedId }"
        >
          <button type="button" class="list-nav-btn" @click="selectCase(item)">
            <strong>{{ item.name }}</strong>
            <span>{{ item.case_id }} · {{ item.messages?.length || 0 }} 条消息</span>
          </button>
        </li>
      </ul>
    </aside>

    <section v-if="createMode === 'upload'" class="panel detail-panel create-flow">
      <div class="panel-head panel-head--inset">
        <div>
          <h2 class="panel-title">新建案例</h2>
          <span class="panel-meta">步骤 1 / 2 · 上传 Node history JSONL</span>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="cancelCreate">取消</button>
        </div>
      </div>
      <div class="panel-inset">
        <div class="upload-zone">
          <p class="upload-zone__hint">选择 JSONL 文件后系统将自动解析消息列表</p>
          <label class="upload-zone__picker btn btn-primary" :class="{ 'is-disabled': parsing }">
            {{ parsing ? "解析中…" : "选择 JSONL 文件" }}
            <input
              ref="uploadFileInput"
              type="file"
              accept=".jsonl,.json,.txt"
              hidden
              :disabled="parsing"
              @change="onUploadJsonl"
            />
          </label>
          <p class="muted upload-zone__note">支持 Node 会话导出的 message journal 格式</p>
        </div>
      </div>
    </section>

    <section v-else-if="createMode === 'edit'" class="panel detail-panel create-flow">
      <div class="panel-head panel-head--inset">
        <div>
          <h2 class="panel-title">新建案例</h2>
          <span class="panel-meta">步骤 2 / 2 · 填写信息并调整消息（{{ draftMessages.length }} 条）</span>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="cancelCreate">取消</button>
        </div>
      </div>

      <div class="panel-inset">
        <div class="form-block">
          <h3 class="form-block__title">案例信息</h3>
          <div class="form-grid">
            <label>
              <span>case_id</span>
              <input v-model="draftMeta.case_id" placeholder="demo-restart-service" />
            </label>
            <label>
              <span>名称</span>
              <input v-model="draftMeta.name" placeholder="服务重启演示" />
            </label>
            <label class="form-grid__wide">
              <span>描述</span>
              <input v-model="draftMeta.description" placeholder="可选说明" />
            </label>
            <label class="form-grid__wide">
              <span>关联 Skills（逗号分隔）</span>
              <input v-model="draftMeta.skill_ids" placeholder="service-restart, ops-runbook" />
            </label>
            <label class="form-grid__wide">
              <span>关联 Plugins（Hook 插件，逗号分隔）</span>
              <input v-model="draftMeta.plugin_ids" placeholder="protect-loaded-skill" />
            </label>
            <label class="form-grid__wide">
              <span>关联 External Tools（外置 CLI，逗号分隔）</span>
              <input v-model="draftMeta.externaltool_ids" placeholder="officecli, my-tool" />
            </label>
          </div>
          <div class="panel-actions">
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="saving"
              @click="onFinalizeCreate"
            >
              创建案例
            </button>
            <template v-if="draftMessagesEditing">
              <button type="button" class="btn btn-ghost btn-sm" @click="openInsertMessage(null, 'draft')">
                插入消息
              </button>
              <button type="button" class="btn btn-ghost btn-sm" @click="exitMessagesEditing">完成编辑</button>
            </template>
            <button
              v-else
              type="button"
              class="btn btn-ghost btn-sm"
              @click="draftMessagesEditing = true"
            >
              编辑
            </button>
          </div>
        </div>

        <div class="table-block">
          <div class="panel-head">
            <h3 class="table-block__title">消息列表</h3>
            <span class="panel-meta">{{ draftMessages.length }} 条</span>
          </div>
          <CaseMessageList
            :messages="draftMessages"
            :editing="draftMessagesEditing"
            @insert="(idx) => openInsertMessage(idx, 'draft')"
            @edit="(msg, idx) => openEditMessage(msg, idx, 'draft')"
            @delete="(msg) => onDeleteMessage(msg, 'draft')"
          />
        </div>
      </div>
    </section>

    <section v-else-if="selectedCase" class="panel detail-panel">
      <div class="panel-head panel-head--inset">
        <div>
          <h2 class="panel-title">{{ selectedCase.name }}</h2>
          <span class="panel-meta mono">{{ selectedCase.case_id }}</span>
          <div v-if="skillTags.length || pluginTags.length || externaltoolTags.length" class="detail-meta-row">
            <span v-for="id in skillTags" :key="'s-' + id" class="tag-pill tag-pill--skill">{{ id }}</span>
            <span v-for="id in pluginTags" :key="'p-' + id" class="tag-pill tag-pill--plugin">{{ id }}</span>
            <span v-for="id in externaltoolTags" :key="'e-' + id" class="tag-pill tag-pill--externaltool">{{ id }}</span>
          </div>
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

      <div class="panel-inset">
        <div class="form-block">
          <h3 class="form-block__title">案例信息</h3>
          <div class="form-grid">
            <label>
              <span>名称</span>
              <input v-model="editMeta.name" />
            </label>
            <label class="form-grid__wide">
              <span>描述</span>
              <input v-model="editMeta.description" />
            </label>
            <label class="form-grid__wide">
              <span>Skills</span>
              <input v-model="editMeta.skill_ids" placeholder="skill-a, skill-b" />
            </label>
            <label class="form-grid__wide">
              <span>Plugins（Hook 插件）</span>
              <input v-model="editMeta.plugin_ids" placeholder="protect-loaded-skill" />
            </label>
            <label class="form-grid__wide">
              <span>External Tools（外置 CLI）</span>
              <input v-model="editMeta.externaltool_ids" placeholder="officecli, my-tool" />
            </label>
          </div>
          <div class="panel-actions">
            <button type="button" class="btn btn-primary btn-sm" :disabled="saving" @click="onSaveMeta">
              保存信息
            </button>
          </div>
        </div>

        <div class="table-block">
          <div class="panel-head">
            <h3 class="table-block__title">消息列表</h3>
            <div class="panel-actions">
              <span class="panel-meta">{{ selectedCase.messages?.length || 0 }} 条</span>
              <template v-if="messagesEditing">
                <button type="button" class="btn btn-ghost btn-sm" @click="openInsertMessage()">插入消息</button>
                <button type="button" class="btn btn-ghost btn-sm" @click="exitMessagesEditing">完成编辑</button>
              </template>
              <button v-else type="button" class="btn btn-ghost btn-sm" @click="messagesEditing = true">
                编辑
              </button>
            </div>
          </div>
          <CaseMessageList
            :messages="selectedCase.messages || []"
            :editing="messagesEditing"
            @insert="(idx) => openInsertMessage(idx)"
            @edit="(msg, idx) => openEditMessage(msg, idx)"
            @delete="(msg) => onDeleteMessage(msg)"
          />
        </div>
      </div>
    </section>

    <section v-else-if="!createMode" class="panel detail-panel detail-panel--empty">
      <p class="panel-meta">选择左侧案例，或点击「新建」创建</p>
    </section>

    <div v-if="messageEditor.open" class="modal-backdrop" @click.self="closeMessageEditor">
      <div class="panel modal-panel">
        <div class="panel-head">
          <h3 class="panel-title">{{ messageEditor.mode === "insert" ? "插入消息" : "编辑消息" }}</h3>
        </div>
        <div class="form-grid">
          <label>
            <span>role</span>
            <select v-model="messageEditor.role">
              <option value="user">user</option>
              <option value="assistant">assistant</option>
              <option value="system">system</option>
              <option value="tool">tool</option>
            </select>
          </label>
          <label class="form-grid__wide">
            <span>recorded_at</span>
            <input v-model="messageEditor.recorded_at" placeholder="2026-06-29T15:04:05+08:00" />
          </label>
          <label class="form-grid__wide">
            <span>content</span>
            <textarea v-model="messageEditor.content" rows="8" />
          </label>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-primary" :disabled="saving" @click="onSaveMessage">保存</button>
          <button type="button" class="btn btn-ghost" @click="closeMessageEditor">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>
