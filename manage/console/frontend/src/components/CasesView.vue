<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  createCase,
  deleteCase,
  deleteCaseAttachment,
  deleteCaseMessage,
  fetchCases,
  fetchExternalToolCatalog,
  fetchPluginCatalog,
  fetchSkillCatalog,
  insertCaseMessage,
  parseCaseJsonl,
  patchCase,
  replaceCaseMessages,
  updateCaseMessage,
  uploadCaseAttachment,
  blobDownloadUrl,
} from "../api.js";
import CaseMessageList from "./CaseMessageList.vue";
import ResourcePicker from "./ResourcePicker.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const PAGE_SIZE_OPTIONS = [6, 12, 24, 48];

const cases = ref([]);
const selectedId = ref("");
const viewMode = ref("list"); // list | detail | edit | create
const createStep = ref("upload"); // upload | edit
const loading = ref(false);
const saving = ref(false);
const parsing = ref(false);
const error = ref("");
const uploadFileInput = ref(null);
const attachmentFileInput = ref(null);

const skillCatalog = ref([]);
const pluginCatalog = ref([]);
const externaltoolCatalog = ref([]);

const listFilters = reactive({
  q: "",
  page: 1,
  pageSize: 12,
});

const draftMeta = reactive({
  name: "",
  description: "",
  skill_ids: [],
  plugin_ids: [],
  externaltool_ids: [],
});
const draftMessages = ref([]);
const messagesEditing = ref(false);
const draftMessagesEditing = ref(false);

const editMeta = reactive({
  name: "",
  description: "",
  skill_ids: [],
  plugin_ids: [],
  externaltool_ids: [],
});

const messageEditor = reactive({
  open: false,
  mode: "edit",
  index: null,
  id: "",
  role: "user",
  recorded_at: "",
  content: "",
  target: "case", // case | draft
});

const selectedCase = computed(() => cases.value.find((c) => c.case_id === selectedId.value) || null);

const attachmentTags = computed(() => selectedCase.value?.resources?.attachments || []);

function catalogOptions(items, idKey) {
  const byId = new Map();
  for (const item of items || []) {
    const id = item[idKey];
    if (!id) continue;
    const prev = byId.get(id);
    if (!prev || (item.catalog_seq || 0) > (prev.catalog_seq || 0)) {
      byId.set(id, item);
    }
  }
  return [...byId.values()]
    .map((item) => ({ id: item[idKey], version: item.version, name: item.name }))
    .sort((a, b) => a.id.localeCompare(b.id));
}

const skillOptions = computed(() => catalogOptions(skillCatalog.value, "skill_id"));
const pluginOptions = computed(() => catalogOptions(pluginCatalog.value, "plugin_id"));
const externaltoolOptions = computed(() => catalogOptions(externaltoolCatalog.value, "tool_id"));
const skillTags = computed(() => (selectedCase.value?.resources?.skill_ids || []).filter(Boolean));
const pluginTags = computed(() => (selectedCase.value?.resources?.plugin_ids || []).filter(Boolean));
const externaltoolTags = computed(() =>
  (selectedCase.value?.resources?.externaltool_ids || []).filter(Boolean),
);

const filteredCases = computed(() => {
  const q = listFilters.q.trim().toLowerCase();
  if (!q) return cases.value;
  return cases.value.filter((item) => {
    const haystack = [
      item.case_id,
      item.name,
      item.description,
      ...(item.resources?.skill_ids || []),
      ...(item.resources?.plugin_ids || []),
      ...(item.resources?.externaltool_ids || []),
    ]
      .join(" ")
      .toLowerCase();
    return haystack.includes(q);
  });
});

const totalFiltered = computed(() => filteredCases.value.length);
const totalPages = computed(() => Math.max(1, Math.ceil(totalFiltered.value / listFilters.pageSize)));
const paginatedCases = computed(() => {
  const page = Math.min(listFilters.page, totalPages.value);
  const start = (page - 1) * listFilters.pageSize;
  return filteredCases.value.slice(start, start + listFilters.pageSize);
});

const listSummary = computed(() => {
  if (loading.value && !cases.value.length) return "加载中…";
  if (!cases.value.length) return "暂无案例";
  if (listFilters.q.trim()) {
    return `共 ${cases.value.length} 个，匹配 ${totalFiltered.value} 个`;
  }
  return `共 ${cases.value.length} 个案例`;
});

const pagerLabel = computed(() => {
  const page = Math.min(listFilters.page, totalPages.value);
  return `第 ${page} / ${totalPages.value} 页 · 共 ${totalFiltered.value} 条`;
});

function caseSkills(item) {
  return (item.resources?.skill_ids || []).filter(Boolean);
}

function casePlugins(item) {
  return (item.resources?.plugin_ids || []).filter(Boolean);
}

function caseExternalTools(item) {
  return (item.resources?.externaltool_ids || []).filter(Boolean);
}

function syncEditMeta(item) {
  editMeta.name = item.name || "";
  editMeta.description = item.description || "";
  editMeta.skill_ids = [...(item.resources?.skill_ids || [])];
  editMeta.plugin_ids = [...(item.resources?.plugin_ids || [])];
  editMeta.externaltool_ids = [...(item.resources?.externaltool_ids || [])];
}

function openDetail(item) {
  selectedId.value = item.case_id;
  syncEditMeta(item);
  messagesEditing.value = false;
  viewMode.value = "detail";
}

function backToList() {
  viewMode.value = "list";
  selectedId.value = "";
  messagesEditing.value = false;
}

function startEdit() {
  if (!selectedCase.value) return;
  syncEditMeta(selectedCase.value);
  messagesEditing.value = false;
  viewMode.value = "edit";
}

function cancelEdit() {
  if (!selectedCase.value) {
    backToList();
    return;
  }
  syncEditMeta(selectedCase.value);
  messagesEditing.value = false;
  viewMode.value = "detail";
}

function resetDraft() {
  draftMeta.name = "";
  draftMeta.description = "";
  draftMeta.skill_ids = [];
  draftMeta.plugin_ids = [];
  draftMeta.externaltool_ids = [];
  draftMessages.value = [];
}

function startCreate() {
  resetDraft();
  draftMessagesEditing.value = false;
  createStep.value = "upload";
  viewMode.value = "create";
  if (uploadFileInput.value) uploadFileInput.value.value = "";
}

function cancelCreate() {
  viewMode.value = "list";
  resetDraft();
  if (uploadFileInput.value) uploadFileInput.value.value = "";
}

function onPagePrev() {
  if (listFilters.page > 1) listFilters.page -= 1;
}

function onPageNext() {
  if (listFilters.page < totalPages.value) listFilters.page += 1;
}

function onPageSizeChange() {
  listFilters.page = 1;
}

watch(
  () => listFilters.q,
  () => {
    listFilters.page = 1;
  },
);

watch(totalPages, (pages) => {
  if (listFilters.page > pages) listFilters.page = pages;
});

async function loadResourceCatalogs() {
  try {
    const [skills, plugins, tools] = await Promise.all([
      fetchSkillCatalog(),
      fetchPluginCatalog(),
      fetchExternalToolCatalog(),
    ]);
    skillCatalog.value = skills;
    pluginCatalog.value = plugins;
    externaltoolCatalog.value = tools;
  } catch (err) {
    emit("toast", { message: `资源目录加载失败: ${err.message}`, type: "error" });
  }
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    await Promise.all([fetchCases().then((r) => { cases.value = r; }), loadResourceCatalogs()]);
    if (viewMode.value === "create") return;
    if (selectedId.value) {
      const found = cases.value.find((c) => c.case_id === selectedId.value);
      if (found) {
        syncEditMeta(found);
      } else if (viewMode.value !== "list") {
        backToList();
      }
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
    createStep.value = "edit";
    emit("toast", { message: `已解析 ${messages.length} 条消息，请填写案例信息`, type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    parsing.value = false;
    if (uploadFileInput.value) uploadFileInput.value.value = "";
  }
}

async function onFinalizeCreate() {
  if (!draftMeta.name.trim()) {
    emit("toast", { message: "名称必填", type: "error" });
    return;
  }
  saving.value = true;
  try {
    const created = await createCase({
      name: draftMeta.name.trim(),
      description: draftMeta.description.trim(),
      skillIds: draftMeta.skill_ids,
      pluginIds: draftMeta.plugin_ids,
      externaltoolIds: draftMeta.externaltool_ids,
    });
    const updated = await replaceCaseMessages(created.case_id, draftMessages.value);
    cases.value = [updated, ...cases.value.filter((c) => c.case_id !== updated.case_id)];
    resetDraft();
    openDetail(updated);
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
        skill_ids: [...editMeta.skill_ids],
        plugin_ids: [...editMeta.plugin_ids],
        externaltool_ids: [...editMeta.externaltool_ids],
        attachments: selectedCase.value.resources?.attachments || [],
      },
    });
    cases.value = cases.value.map((c) => (c.case_id === updated.case_id ? updated : c));
    syncEditMeta(updated);
    messagesEditing.value = false;
    viewMode.value = "detail";
    emit("toast", { message: "案例信息已保存", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    saving.value = false;
  }
}

async function onUploadAttachment(e) {
  const file = e.target.files?.[0];
  if (!file || !selectedCase.value) return;
  saving.value = true;
  try {
    const updated = await uploadCaseAttachment(selectedCase.value.case_id, file);
    cases.value = cases.value.map((c) => (c.case_id === updated.case_id ? updated : c));
    syncEditMeta(updated);
    emit("toast", { message: `已上传附件 ${file.name}`, type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    saving.value = false;
    if (attachmentFileInput.value) attachmentFileInput.value.value = "";
  }
}

async function onDeleteAttachment(att) {
  if (!selectedCase.value) return;
  if (!window.confirm(`删除附件 ${att.filename || att.blob_id.slice(0, 12)}？`)) return;
  try {
    const updated = await deleteCaseAttachment(selectedCase.value.case_id, att.blob_id);
    cases.value = cases.value.map((c) => (c.case_id === updated.case_id ? updated : c));
    syncEditMeta(updated);
    emit("toast", { message: "附件已删除", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

async function onDeleteCase() {
  if (!selectedCase.value) return;
  const id = selectedCase.value.case_id;
  if (!window.confirm(`删除案例 ${id}？`)) return;
  try {
    await deleteCase(id);
    cases.value = cases.value.filter((c) => c.case_id !== id);
    backToList();
    emit("toast", { message: `已删除 ${id}`, type: "success" });
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
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
    syncEditMeta(updated);
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
    syncEditMeta(updated);
    emit("toast", { message: "消息已删除", type: "success" });
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
  <div class="cases-page">
    <!-- 列表 -->
    <template v-if="viewMode === 'list'">
      <section class="panel cases-toolbar">
        <div class="cases-toolbar__main">
          <div class="cases-toolbar__intro">
            <h2 class="panel-title">案例库</h2>
            <span class="cases-toolbar__summary">{{ listSummary }}</span>
          </div>
          <label class="cases-toolbar__search">
            <span class="visually-hidden">搜索案例</span>
            <input
              v-model="listFilters.q"
              type="search"
              placeholder="搜索名称、case_id、描述或关联资源…"
              autocomplete="off"
            />
          </label>
          <div class="cases-toolbar__actions">
            <button type="button" class="btn btn-primary btn-sm" @click="startCreate">新建案例</button>
            <button type="button" class="btn btn-ghost btn-sm" :disabled="loading" @click="load">刷新</button>
          </div>
        </div>
      </section>

      <p v-if="error" class="banner banner-error" role="alert">{{ error }}</p>
      <p v-else-if="loading && !cases.length" class="panel-meta cases-empty-hint">加载中…</p>
      <p v-else-if="!cases.length" class="panel-meta cases-empty-hint">暂无案例，点击「新建案例」添加</p>
      <p v-else-if="!paginatedCases.length" class="panel-meta cases-empty-hint">没有匹配的案例，请调整搜索条件</p>

      <div v-else class="case-card-grid">
        <article
          v-for="item in paginatedCases"
          :key="item.case_id"
          class="case-card"
          tabindex="0"
          role="button"
          @click="openDetail(item)"
          @keydown.enter="openDetail(item)"
        >
          <h3 class="case-card__title">{{ item.name }}</h3>
          <p class="case-card__desc">{{ item.description || "暂无描述" }}</p>
          <div v-if="caseSkills(item).length" class="case-card__row">
            <span class="case-card__label">Skills</span>
            <div class="case-card__tags">
              <span v-for="id in caseSkills(item)" :key="'s-' + id" class="tag-pill tag-pill--skill">{{ id }}</span>
            </div>
          </div>
          <div v-if="casePlugins(item).length" class="case-card__row">
            <span class="case-card__label">Plugins</span>
            <div class="case-card__tags">
              <span v-for="id in casePlugins(item)" :key="'p-' + id" class="tag-pill tag-pill--plugin">{{ id }}</span>
            </div>
          </div>
          <div v-if="caseExternalTools(item).length" class="case-card__row">
            <span class="case-card__label">External Tools</span>
            <div class="case-card__tags">
              <span
                v-for="id in caseExternalTools(item)"
                :key="'e-' + id"
                class="tag-pill tag-pill--externaltool"
              >
                {{ id }}
              </span>
            </div>
          </div>
          <footer class="case-card__footer">
            <span class="case-card__count">{{ item.messages?.length || 0 }} 条消息</span>
            <span class="case-card__id mono">{{ item.case_id }}</span>
          </footer>
        </article>
      </div>

      <footer v-if="totalFiltered > 0" class="cases-pager">
        <div class="cases-pager__nav">
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            :disabled="listFilters.page <= 1"
            @click="onPagePrev"
          >
            上一页
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            :disabled="listFilters.page >= totalPages"
            @click="onPageNext"
          >
            下一页
          </button>
        </div>
        <span class="cases-pager__info">{{ pagerLabel }}</span>
        <label class="cases-pager__size">
          <span>每页</span>
          <select v-model.number="listFilters.pageSize" @change="onPageSizeChange">
            <option v-for="n in PAGE_SIZE_OPTIONS" :key="n" :value="n">{{ n }}</option>
          </select>
        </label>
      </footer>
    </template>

    <!-- 新建：上传 JSONL -->
    <section v-else-if="viewMode === 'create' && createStep === 'upload'" class="panel detail-panel create-flow">
      <div class="panel-head panel-head--inset">
        <div>
          <h2 class="panel-title">新建案例</h2>
          <span class="panel-meta">步骤 1 / 2 · 上传 Node history JSONL</span>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="cancelCreate">返回列表</button>
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

    <!-- 新建：填写信息 -->
    <section v-else-if="viewMode === 'create' && createStep === 'edit'" class="panel detail-panel create-flow">
      <div class="panel-head panel-head--inset">
        <div>
          <h2 class="panel-title">新建案例</h2>
          <span class="panel-meta">步骤 2 / 2 · 填写信息并调整消息（{{ draftMessages.length }} 条）</span>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="cancelCreate">返回列表</button>
        </div>
      </div>

      <div class="panel-inset">
        <div class="form-block">
          <h3 class="form-block__title">案例信息</h3>
          <div class="form-grid">
            <label>
              <span>名称</span>
              <input v-model="draftMeta.name" placeholder="服务重启演示" />
            </label>
            <label class="form-grid__wide">
              <span>描述</span>
              <input v-model="draftMeta.description" placeholder="可选说明" />
            </label>
            <ResourcePicker
              v-model="draftMeta.skill_ids"
              label="关联 Skills（已发布）"
              :options="skillOptions"
              pill-class="tag-pill--skill"
            />
            <ResourcePicker
              v-model="draftMeta.plugin_ids"
              label="关联 Plugins（已发布）"
              :options="pluginOptions"
              pill-class="tag-pill--plugin"
            />
            <ResourcePicker
              v-model="draftMeta.externaltool_ids"
              label="关联 External Tools（已发布）"
              :options="externaltoolOptions"
              pill-class="tag-pill--externaltool"
            />
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
            <button v-else type="button" class="btn btn-ghost btn-sm" @click="draftMessagesEditing = true">
              编辑消息
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

    <!-- 详情（只读） -->
    <section v-else-if="viewMode === 'detail' && selectedCase" class="panel detail-panel case-detail">
      <header class="case-detail-header">
        <div class="case-detail-header__row">
          <div class="case-detail-header__main">
            <div class="case-detail-header__title-line">
              <h2 class="case-detail-header__title">{{ selectedCase.name }}</h2>
              <span class="case-detail-header__id mono">{{ selectedCase.case_id }}</span>
              <span class="case-detail-header__stat">{{ selectedCase.messages?.length || 0 }} 条消息</span>
            </div>
            <p v-if="selectedCase.description" class="case-detail-header__desc">
              {{ selectedCase.description }}
            </p>
            <div
              v-if="skillTags.length || pluginTags.length || externaltoolTags.length"
              class="case-detail-header__tags detail-meta-row"
            >
              <span v-for="id in skillTags" :key="'s-' + id" class="tag-pill tag-pill--skill">{{ id }}</span>
              <span v-for="id in pluginTags" :key="'p-' + id" class="tag-pill tag-pill--plugin">{{ id }}</span>
              <span v-for="id in externaltoolTags" :key="'e-' + id" class="tag-pill tag-pill--externaltool">
                {{ id }}
              </span>
            </div>
            <div v-if="attachmentTags.length" class="case-detail-header__attachments">
              <span class="case-card__label">附件</span>
              <a
                v-for="att in attachmentTags"
                :key="att.blob_id"
                class="tag-pill mono"
                :href="blobDownloadUrl(att.blob_id)"
                target="_blank"
                rel="noopener"
              >
                {{ att.filename || att.blob_id.slice(0, 12) }}
              </a>
            </div>
          </div>
          <div class="case-detail-header__actions">
            <button type="button" class="btn btn-primary btn-sm" @click="startEdit">编辑</button>
            <button type="button" class="btn btn-danger btn-sm" @click="onDeleteCase">删除</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="backToList">返回</button>
          </div>
        </div>
      </header>

      <div class="panel-inset case-detail-body">
        <CaseMessageList :messages="selectedCase.messages || []" :editing="false" />
      </div>
    </section>

    <!-- 编辑 -->
    <section v-else-if="viewMode === 'edit' && selectedCase" class="panel detail-panel">
      <div class="panel-head panel-head--inset">
        <div>
          <h2 class="panel-title">编辑案例</h2>
          <span class="panel-meta mono">{{ selectedCase.case_id }}</span>
        </div>
        <div class="panel-actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="cancelEdit">取消</button>
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
            <ResourcePicker
              v-model="editMeta.skill_ids"
              label="Skills（已发布目录）"
              :options="skillOptions"
              pill-class="tag-pill--skill"
            />
            <ResourcePicker
              v-model="editMeta.plugin_ids"
              label="Plugins（已发布目录）"
              :options="pluginOptions"
              pill-class="tag-pill--plugin"
            />
            <ResourcePicker
              v-model="editMeta.externaltool_ids"
              label="External Tools（已发布目录）"
              :options="externaltoolOptions"
              pill-class="tag-pill--externaltool"
            />
          </div>
          <div v-if="selectedCase" class="form-block">
            <h3 class="form-block__title">附件</h3>
            <ul v-if="attachmentTags.length" class="case-attachments">
              <li v-for="att in attachmentTags" :key="att.blob_id" class="case-attachments__item">
                <a class="mono" :href="blobDownloadUrl(att.blob_id)" target="_blank" rel="noopener">
                  {{ att.filename || att.blob_id.slice(0, 12) + "…" }}
                </a>
                <span class="muted">{{ att.size ? `${att.size} B` : "" }}</span>
                <button type="button" class="btn btn-ghost btn-sm" @click="onDeleteAttachment(att)">
                  删除
                </button>
              </li>
            </ul>
            <p v-else class="muted">暂无附件</p>
            <label class="upload-zone__picker btn btn-ghost btn-sm">
              上传附件
              <input
                ref="attachmentFileInput"
                type="file"
                hidden
                @change="onUploadAttachment"
              />
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
                编辑消息
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
