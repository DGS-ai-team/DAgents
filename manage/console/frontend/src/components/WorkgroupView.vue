<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  archiveWorkgroup,
  createWorkgroup,
  createWorkgroupMember,
  fetchAgents,
  fetchAuthMe,
  fetchLLMConfigs,
  fetchWorkgroupACL,
  fetchWorkgroupLLMConfigs,
  fetchWorkgroupMemberSpec,
  fetchWorkgroupMembers,
  fetchWorkgroups,
  patchWorkgroup,
  patchWorkgroupACL,
  publishWorkgroup,
} from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast", "open-chat", "page-meta"]);

const loading = ref(false);
const loadingDetail = ref(false);
const creating = ref(false);
const deletingId = ref("");
const publishingId = ref("");
const bindingLlmKey = ref("");
const addingCollaborator = ref(false);
const creatingMember = ref(false);
const memberFormOpen = ref(false);
const error = ref("");

const workgroups = ref([]);
const selectedId = ref("");
const llmConfigs = ref([]);
const members = ref([]);
/** @type {import('vue').Ref<Record<string, any>>} */
const memberSpecs = ref({});
const acl = ref(null);
const nodeOptions = ref([]);
const createOpen = ref(false);
const ownerId = ref("console");
const ownerLabel = ref("未登录");
const authKind = ref(""); // admin | node | ""
const authGroups = ref([]);
const collabDraft = ref("");

const MEMBER_TOOL_CHOICES = ["read_file"];

const createForm = reactive({
  displayName: "",
});

const memberForm = reactive({
  displayName: "",
  homeNodeId: "",
  soulMd: "",
  userMd: "",
  customMd: "",
  tools: ["read_file"],
});

const isNodeSession = computed(() => authKind.value === "node");

const selectedWorkgroup = computed(
  () => workgroups.value.find((item) => item.workgroup_id === selectedId.value) || null,
);

const showSettings = computed(() => Boolean(selectedWorkgroup.value));

const sortedMembers = computed(() => {
  const list = [...(members.value || [])];
  list.sort((a, b) => String(a.display_name || "").localeCompare(String(b.display_name || ""), "zh"));
  return list;
});

/** Supervisor（leader）+ Agent 成员，统一按「成员」展示配置。 */
const configMembers = computed(() => {
  const wg = selectedWorkgroup.value;
  const rows = [];
  if (wg) {
    rows.push({
      kind: "supervisor",
      key: "leader",
      display_name: "Supervisor",
      member_id: "leader",
      status: wg.status === "active" ? "ready" : wg.status === "configuring" ? "provisioning" : wg.status,
      home_node_id: "manage",
      member_generation: null,
      llm_profile_id: wg.llm_profile_id,
      llm_profile_revision: wg.llm_profile_revision,
      max_tool_loops: null,
      allow_tool_names: ["assign_workgroup_task"],
      hint: "工作组编排者；对话与分派由 Manage 侧 leader 执行",
    });
  }
  for (const m of sortedMembers.value) {
    const spec = memberSpecs.value[m.member_id] || null;
    rows.push({
      kind: "member",
      key: m.member_id,
      display_name: m.display_name,
      member_id: m.member_id,
      status: m.status,
      home_node_id: m.home_node_id,
      member_generation: m.member_generation,
      llm_profile_id: spec?.llm_profile_id || "",
      llm_profile_revision: spec?.llm_profile_revision || "",
      max_tool_loops: spec?.max_tool_loops ?? null,
      allow_tool_names: Array.isArray(spec?.tools?.allow_names) ? spec.tools.allow_names : [],
      hint: "",
    });
  }
  return rows;
});

function syncPageMeta() {
  if (!props.active) {
    emit("page-meta", null);
    return;
  }
  if (showSettings.value && selectedWorkgroup.value) {
    emit("page-meta", {
      title: "工作组",
      trail: "配置",
      subtitle: "",
      showBack: true,
    });
    return;
  }
  emit("page-meta", null);
}

function statusLabel(status) {
  const map = {
    configuring: "配置中",
    active: "进行中",
    archiving: "归档中",
    archived: "已归档",
  };
  return map[status] || status || "—";
}

function canChat(item) {
  return String(item?.status || "") === "active";
}

function formatTime(iso) {
  if (!iso) return "—";
  const ts = Date.parse(iso);
  if (!Number.isFinite(ts)) return String(iso);
  return new Date(ts).toLocaleString();
}

async function resolveCreatorDefault() {
  try {
    const me = await fetchAuthMe();
    if (me?.authenticated) {
      authKind.value = String(me.kind || "");
      authGroups.value = Array.isArray(me.discovery_groups)
        ? me.discovery_groups.filter((g) => g && g !== "*")
        : [];
      if (me.kind === "node") {
        const id = String(me.agent_id || me.subject || "").trim() || "console";
        ownerId.value = id;
        ownerLabel.value = `Node · ${id}`;
        return;
      }
      if (me.kind === "admin") {
        const id = String(me.subject || "admin").trim() || "admin";
        ownerId.value = id;
        ownerLabel.value = `管理员 · ${id}`;
        return;
      }
    }
  } catch {
    /* ignore */
  }
  authKind.value = "";
  authGroups.value = [];
  ownerId.value = "console";
  ownerLabel.value = "console";
}

function memberStatusLabel(status) {
  const map = {
    requested: "已请求",
    provisioning: "配置中",
    ready: "就绪",
    busy: "忙碌",
    archived: "已归档",
    error: "错误",
  };
  return map[status] || status || "—";
}

function isAclNode(nodeId) {
  const nid = String(nodeId || "").trim();
  if (!nid || !acl.value) return false;
  const owners = Array.isArray(acl.value.owners) ? acl.value.owners : [];
  const collaborators = Array.isArray(acl.value.collaborators) ? acl.value.collaborators : [];
  return owners.includes(nid) || collaborators.includes(nid);
}

function toggleMemberTool(name) {
  const set = new Set(memberForm.tools);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  memberForm.tools = [...set];
}

function resetMemberForm() {
  memberForm.displayName = "";
  memberForm.homeNodeId = "";
  memberForm.soulMd = "";
  memberForm.userMd = "";
  memberForm.customMd = "";
  memberForm.tools = ["read_file"];
}

function openMemberForm() {
  memberFormOpen.value = true;
  if (!memberForm.homeNodeId.trim()) {
    if (isNodeSession.value && nodeOptions.value.length === 1) {
      memberForm.homeNodeId = nodeOptions.value[0];
    } else if (isNodeSession.value && ownerId.value) {
      memberForm.homeNodeId = ownerId.value;
    }
  }
}

function cancelMemberForm() {
  memberFormOpen.value = false;
  resetMemberForm();
}

async function ensureHomeInAcl(workgroupId, homeNodeId) {
  const home = String(homeNodeId || "").trim();
  if (!home) throw new Error("Home Node 不能为空");
  if (isAclNode(home)) return acl.value;
  const current = acl.value;
  if (!current?.revision) throw new Error("无法读取工作组 ACL");
  const collaborators = [...(current.collaborators || [])];
  if (!collaborators.includes(home) && !(current.owners || []).includes(home)) {
    collaborators.push(home);
  }
  const updated = await patchWorkgroupACL(workgroupId, {
    collaborators,
    expected_revision: current.revision,
  });
  acl.value = updated;
  return updated;
}

async function loadNodeOptions() {
  try {
    if (isNodeSession.value) {
      const groups = authGroups.value.length ? authGroups.value : [];
      if (!groups.length) {
        nodeOptions.value = ownerId.value ? [ownerId.value] : [];
        return;
      }
      const pages = await Promise.all(
        groups.map((g) =>
          fetchAgents({ page: 1, page_size: 100, status: "online", discovery_group: g }).catch(() => ({
            agents: [],
          })),
        ),
      );
      const ids = new Set();
      for (const data of pages) {
        const agents = Array.isArray(data?.agents) ? data.agents : [];
        for (const a of agents) {
          const id = String(a.agent_id || a.node_id || "").trim();
          if (id) ids.add(id);
        }
      }
      if (ownerId.value) ids.add(ownerId.value);
      nodeOptions.value = [...ids].sort((a, b) => a.localeCompare(b));
      return;
    }
    const data = await fetchAgents({ page: 1, page_size: 100, status: "all" });
    const agents = Array.isArray(data?.agents) ? data.agents : Array.isArray(data) ? data : [];
    const ids = [
      ...new Set(
        agents
          .map((a) => String(a.agent_id || a.node_id || "").trim())
          .filter(Boolean),
      ),
    ];
    nodeOptions.value = ids.sort((a, b) => a.localeCompare(b));
  } catch {
    nodeOptions.value = [];
  }
}

async function addCollaborator() {
  const nid = collabDraft.value.trim();
  const wg = selectedWorkgroup.value;
  if (!nid || !wg?.workgroup_id || addingCollaborator.value) return;
  if (isAclNode(nid)) {
    emit("toast", { message: `${nid} 已在 ACL 中`, type: "info" });
    collabDraft.value = "";
    return;
  }
  addingCollaborator.value = true;
  try {
    const current = acl.value;
    if (!current?.revision) throw new Error("无法读取工作组 ACL");
    const collaborators = [...(current.collaborators || []), nid];
    acl.value = await patchWorkgroupACL(wg.workgroup_id, {
      collaborators,
      expected_revision: current.revision,
    });
    collabDraft.value = "";
    emit("toast", { message: `已添加协作者 ${nid}`, type: "success" });
  } catch (err) {
    emit("toast", { message: err.message || "添加协作者失败", type: "error" });
  } finally {
    addingCollaborator.value = false;
  }
}

async function submitCreateMember() {
  const wg = selectedWorkgroup.value;
  const displayName = memberForm.displayName.trim();
  const home = memberForm.homeNodeId.trim();
  if (!wg?.workgroup_id || !displayName || !home || creatingMember.value) return;
  creatingMember.value = true;
  try {
    await ensureHomeInAcl(wg.workgroup_id, home);
    const tools = memberForm.tools.length ? [...memberForm.tools] : ["read_file"];
    const body = {
      display_name: displayName,
      home_node_id: home,
      allow_tool_names: tools,
      prompt: {
        soul_md: memberForm.soulMd,
        user_md: memberForm.userMd,
        custom_md: memberForm.customMd,
      },
    };
    if (wg.llm_profile_id) {
      body.llm_profile_id = wg.llm_profile_id;
      body.llm_profile_revision = wg.llm_profile_revision || "1";
    }
    await createWorkgroupMember(wg.workgroup_id, body);
    cancelMemberForm();
    await loadSettings();
    emit("toast", {
      message: `已添加成员「${displayName}」，正在配置到 ${home}`,
      type: "success",
    });
  } catch (err) {
    emit("toast", { message: err.message || "添加成员失败", type: "error" });
  } finally {
    creatingMember.value = false;
  }
}

function resolveLlmLabel(profileId, revision) {
  const id = String(profileId || "").trim();
  if (!id) return "—";
  const cfg = (llmConfigs.value || []).find((c) => c.id === id || c.name === id);
  if (cfg) {
    const rev = revision ? `@${revision}` : "";
    return `${cfg.name}${rev} · ${cfg.provider} / ${cfg.model}`;
  }
  return revision ? `${id}@${revision}` : id;
}

function isLlmActive(profileId, cfg) {
  const id = String(profileId || "").trim();
  return Boolean(id) && (cfg.id === id || cfg.name === id);
}

function pickCreateLlmProfile(configs) {
  const list = Array.isArray(configs) ? configs : [];
  const preferred = list.find((c) => c.is_default) || list[0];
  if (!preferred) {
    return { llm_profile_id: "default", llm_profile_revision: "1" };
  }
  return {
    llm_profile_id: preferred.id,
    llm_profile_revision: "1",
  };
}

async function bindSupervisorLlm(cfg) {
  const wg = selectedWorkgroup.value;
  if (!wg?.workgroup_id || !cfg?.id || bindingLlmKey.value) return;
  if (isLlmActive(wg.llm_profile_id, cfg)) return;
  bindingLlmKey.value = `leader:${cfg.id}`;
  try {
    const updated = await patchWorkgroup(wg.workgroup_id, {
      llm_profile_id: cfg.id,
    });
    workgroups.value = (workgroups.value || []).map((item) =>
      item.workgroup_id === updated.workgroup_id ? updated : item,
    );
    emit("toast", { message: `Supervisor 已绑定 ${cfg.name}`, type: "success" });
  } catch (err) {
    emit("toast", { message: err.message || "绑定 LLM 失败", type: "error" });
  } finally {
    bindingLlmKey.value = "";
  }
}

async function loadWorkgroups() {
  loading.value = true;
  error.value = "";
  try {
    workgroups.value = await fetchWorkgroups();
    if (selectedId.value && !workgroups.value.some((w) => w.workgroup_id === selectedId.value)) {
      selectedId.value = "";
      llmConfigs.value = [];
      members.value = [];
      memberSpecs.value = {};
      acl.value = null;
    }
  } catch (err) {
    error.value = err.message || "加载工作组失败";
    emit("toast", { message: error.value, type: "error" });
  } finally {
    loading.value = false;
  }
}

async function loadSettings() {
  if (!selectedId.value) {
    llmConfigs.value = [];
    members.value = [];
    memberSpecs.value = {};
    acl.value = null;
    return;
  }
  loadingDetail.value = true;
  try {
    const [configs, memberList, aclData] = await Promise.all([
      fetchWorkgroupLLMConfigs(selectedId.value),
      fetchWorkgroupMembers(selectedId.value),
      fetchWorkgroupACL(selectedId.value).catch(() => null),
    ]);
    llmConfigs.value = Array.isArray(configs) ? configs : [];
    members.value = Array.isArray(memberList) ? memberList : [];
    acl.value = aclData;
    loadNodeOptions();
    const specs = {};
    await Promise.all(
      members.value.map(async (m) => {
        try {
          specs[m.member_id] = await fetchWorkgroupMemberSpec(selectedId.value, m.member_id);
        } catch {
          specs[m.member_id] = null;
        }
      }),
    );
    memberSpecs.value = specs;
  } catch (err) {
    emit("toast", { message: err.message || "加载配置失败", type: "error" });
  } finally {
    loadingDetail.value = false;
  }
}

function openChat(item) {
  if (!canChat(item)) {
    emit("toast", { message: "请先在配置页发布工作组后再对话", type: "error" });
    return;
  }
  createOpen.value = false;
  emit("open-chat", {
    workgroupId: item.workgroup_id,
    displayName: item.display_name || item.workgroup_id,
  });
}

async function onPublish(item) {
  const id = item?.workgroup_id;
  if (!id || publishingId.value || item.status !== "configuring") return;
  publishingId.value = id;
  try {
    const updated = await publishWorkgroup(id);
    workgroups.value = (workgroups.value || []).map((row) =>
      row.workgroup_id === updated.workgroup_id ? updated : row,
    );
    emit("toast", { message: "工作组已发布，可以开始对话", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message || "发布失败", type: "error" });
  } finally {
    publishingId.value = "";
  }
}

function openSettings(id) {
  selectedId.value = id;
  createOpen.value = false;
}

function backToGrid() {
  selectedId.value = "";
  llmConfigs.value = [];
  members.value = [];
  memberSpecs.value = {};
  acl.value = null;
  nodeOptions.value = [];
  collabDraft.value = "";
  cancelMemberForm();
}

defineExpose({ backToGrid });

function openCreateCard() {
  createOpen.value = true;
  createForm.displayName = "";
}

function cancelCreate() {
  createOpen.value = false;
  createForm.displayName = "";
}

async function submitCreate() {
  const displayName = createForm.displayName.trim();
  const createdBy = ownerId.value.trim() || "console";
  if (!displayName) return;
  creating.value = true;
  try {
    let llmProfile = { llm_profile_id: "default", llm_profile_revision: "1" };
    try {
      llmProfile = pickCreateLlmProfile(await fetchLLMConfigs());
    } catch {
      /* 无 LLM 列表时仍允许创建，回退 default */
    }
    await createWorkgroup({
      display_name: displayName,
      created_by_node_id: createdBy,
      ...llmProfile,
    });
    createOpen.value = false;
    createForm.displayName = "";
    await loadWorkgroups();
    emit("toast", { message: "工作组已创建", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message || "创建失败", type: "error" });
  } finally {
    creating.value = false;
  }
}

async function onDelete(item) {
  const id = item?.workgroup_id;
  if (!id || deletingId.value) return;
  const name = item.display_name || id;
  if (!window.confirm(`确定删除工作组「${name}」？\n将归档该工作组，此操作不可撤销。`)) {
    return;
  }
  deletingId.value = id;
  try {
    await archiveWorkgroup(id);
    if (selectedId.value === id) backToGrid();
    await loadWorkgroups();
    emit("toast", { message: "工作组已删除", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message || "删除失败", type: "error" });
  } finally {
    deletingId.value = "";
  }
}

watch(
  () => selectedId.value,
  () => {
    if (props.active && selectedId.value) loadSettings();
    syncPageMeta();
  },
);

watch(
  () => props.active,
  (active) => {
    if (active) {
      resolveCreatorDefault().then(() => loadWorkgroups());
      syncPageMeta();
    } else {
      emit("page-meta", null);
    }
  },
);

watch(showSettings, () => {
  syncPageMeta();
});

onMounted(async () => {
  if (props.active) {
    await resolveCreatorDefault();
    await loadWorkgroups();
    syncPageMeta();
  }
});
</script>

<template>
  <section class="workgroup-page">
    <div v-if="!showSettings" class="workgroup-grid-wrap">
      <div class="panel-head workgroup-grid-head">
        <h2 class="panel-title">工作组</h2>
      </div>

      <p v-if="error" class="state state-error">{{ error }}</p>
      <p v-else-if="loading" class="state">加载中…</p>

      <div v-else class="workgroup-card-grid">
        <article
          v-for="item in workgroups"
          :key="item.workgroup_id"
          class="wg-card"
        >
          <div class="wg-card__top">
            <span class="wg-card__status" :data-status="item.status">{{ statusLabel(item.status) }}</span>
          </div>
          <h3 class="wg-card__title" :title="item.display_name">{{ item.display_name }}</h3>
          <p class="wg-card__id" :title="item.workgroup_id">{{ item.workgroup_id }}</p>
          <div class="wg-card__meta">
            <span>归属人 {{ item.created_by_node_id || "—" }}</span>
            <span>{{ formatTime(item.created_at) }}</span>
          </div>

          <div class="wg-card__actions" @click.stop>
            <button
              type="button"
              class="wg-card__action"
              title="对话"
              aria-label="对话"
              :disabled="!canChat(item)"
              @click="openChat(item)"
            >
              <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
                <path
                  d="M3.5 5.5A2 2 0 015.5 3.5h9A2 2 0 0116.5 5.5v6a2 2 0 01-2 2H9l-3.5 2.5V13.5h-0.5a2 2 0 01-2-2v-6z"
                  stroke="currentColor"
                  stroke-width="1.4"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button
              type="button"
              class="wg-card__action"
              title="配置"
              aria-label="配置"
              @click="openSettings(item.workgroup_id)"
            >
              <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
                <path
                  d="M11.6 2.6h-3.2l-.35 1.55a5.5 5.5 0 00-1.2.7L5.4 4.4 3.9 5.9l.45 1.45a5.5 5.5 0 00-.7 1.2L2.1 8.9v3.2l1.55.35c.16.43.4.83.7 1.2L3.9 15.1l1.5 1.5 1.45-.45c.37.3.77.54 1.2.7l.35 1.55h3.2l.35-1.55c.43-.16.83-.4 1.2-.7l1.45.45 1.5-1.5-.45-1.45c.3-.37.54-.77.7-1.2l1.55-.35V8.9l-1.55-.35a5.5 5.5 0 00-.7-1.2L17.1 5.9 15.6 4.4l-1.45.45a5.5 5.5 0 00-1.2-.7L11.6 2.6z"
                  stroke="currentColor"
                  stroke-width="1.3"
                  stroke-linejoin="round"
                />
                <circle cx="10" cy="10" r="2.35" stroke="currentColor" stroke-width="1.3" />
              </svg>
            </button>
            <button
              type="button"
              class="wg-card__action wg-card__action--danger"
              title="删除"
              aria-label="删除"
              :disabled="deletingId === item.workgroup_id || item.status === 'archived'"
              @click="onDelete(item)"
            >
              <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
                <path
                  d="M4.5 6h11M8 6V4.5h4V6M6.5 6l.6 9h6l.6-9"
                  stroke="currentColor"
                  stroke-width="1.4"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
          </div>
        </article>

        <div class="wg-card wg-card--create" :class="{ 'wg-card--create-open': createOpen }">
          <button
            v-if="!createOpen"
            type="button"
            class="wg-card__create-btn"
            @click="openCreateCard"
          >
            <span class="wg-card__plus" aria-hidden="true">+</span>
            <span class="wg-card__create-label">新增工作组</span>
          </button>
          <form v-else class="wg-card__create-form" @submit.prevent="submitCreate">
            <label class="field">
              <span>显示名称</span>
              <input
                v-model="createForm.displayName"
                type="text"
                placeholder="例如：运维协作组"
                autofocus
                :disabled="creating"
              />
            </label>
            <div class="field">
              <span>归属人</span>
              <p class="wg-card__owner-readonly" :title="ownerId">{{ ownerLabel }}</p>
            </div>
            <div class="wg-card__create-actions">
              <button type="button" class="btn btn-ghost" :disabled="creating" @click="cancelCreate">
                取消
              </button>
              <button
                type="submit"
                class="btn btn-primary"
                :disabled="creating || !createForm.displayName.trim()"
              >
                {{ creating ? "创建中…" : "创建" }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>

    <div v-else class="workgroup-detail">
      <header class="wg-detail-header">
        <div class="wg-detail-header__row">
          <div>
            <h2 class="wg-detail-header__title">{{ selectedWorkgroup.display_name }}</h2>
            <div class="wg-detail-header__meta">
              <span class="wg-card__status" :data-status="selectedWorkgroup.status">
                {{ statusLabel(selectedWorkgroup.status) }}
              </span>
            </div>
          </div>
          <div class="wg-detail-header__actions">
            <button
              v-if="selectedWorkgroup.status === 'configuring'"
              type="button"
              class="btn btn-primary"
              :disabled="publishingId === selectedWorkgroup.workgroup_id"
              @click="onPublish(selectedWorkgroup)"
            >
              {{ publishingId === selectedWorkgroup.workgroup_id ? "发布中…" : "发布" }}
            </button>
            <button
              type="button"
              class="btn btn-ghost"
              :disabled="!canChat(selectedWorkgroup)"
              :title="canChat(selectedWorkgroup) ? '打开对话' : '发布后可对话'"
              @click="openChat(selectedWorkgroup)"
            >
              对话
            </button>
          </div>
        </div>
        <p v-if="selectedWorkgroup.status === 'configuring'" class="muted wg-detail-header__hint">
          当前为配置中：请完成 Supervisor / 成员配置后点击「发布」，方可开始对话。
        </p>
      </header>

      <p v-if="loadingDetail" class="state">加载配置中…</p>

      <template v-else>
        <section class="wg-detail-section" aria-label="通用配置">
          <h3 class="wg-detail-section__title">通用配置</h3>
          <dl class="wg-settings-kv">
            <div>
              <dt>显示名称</dt>
              <dd>{{ selectedWorkgroup.display_name }}</dd>
            </div>
            <div>
              <dt>工作组 ID</dt>
              <dd class="cell-wrap">{{ selectedWorkgroup.workgroup_id }}</dd>
            </div>
            <div>
              <dt>状态</dt>
              <dd>{{ statusLabel(selectedWorkgroup.status) }}</dd>
            </div>
            <div>
              <dt>归属人</dt>
              <dd>{{ selectedWorkgroup.created_by_node_id || "—" }}</dd>
            </div>
            <div>
              <dt>Owners</dt>
              <dd>{{ (acl?.owners || []).join(", ") || "—" }}</dd>
            </div>
            <div>
              <dt>Collaborators</dt>
              <dd>{{ (acl?.collaborators || []).join(", ") || "—" }}</dd>
            </div>
          </dl>
          <form class="wg-inline-form" @submit.prevent="addCollaborator">
            <label class="field">
              <span>添加协作者 Node</span>
              <input
                v-model="collabDraft"
                type="text"
                list="wg-node-options"
                placeholder="输入 node_id"
                :disabled="addingCollaborator"
              />
            </label>
            <button
              type="submit"
              class="btn btn-ghost"
              :disabled="addingCollaborator || !collabDraft.trim()"
            >
              {{ addingCollaborator ? "添加中…" : "添加" }}
            </button>
          </form>
          <p class="muted wg-detail-section__note">
            Home Node 必须在 ACL（Owners / Collaborators）中。新增成员时若尚未加入，会自动补进 Collaborators。
            LLM 默认继承 Supervisor。配置完成后请点击右上角「发布」。
          </p>
        </section>

        <section class="wg-detail-section" aria-label="成员配置">
          <div class="wg-detail-section__head">
            <h3 class="wg-detail-section__title">成员配置</h3>
            <div class="wg-detail-section__head-actions">
              <span class="muted">{{ configMembers.length }} 人</span>
              <button
                v-if="!memberFormOpen"
                type="button"
                class="btn btn-primary btn-sm"
                @click="openMemberForm"
              >
                新增成员
              </button>
            </div>
          </div>

          <form
            v-if="memberFormOpen"
            class="wg-member-create"
            @submit.prevent="submitCreateMember"
          >
            <div class="wg-member-create__grid">
              <label class="field">
                <span>显示名称</span>
                <input
                  v-model="memberForm.displayName"
                  type="text"
                  placeholder="例如：代码员"
                  required
                  autofocus
                  :disabled="creatingMember"
                />
              </label>
              <label class="field">
                <span>Home Node</span>
                <select
                  v-if="isNodeSession || nodeOptions.length"
                  v-model="memberForm.homeNodeId"
                  required
                  :disabled="creatingMember"
                >
                  <option value="" disabled>请选择执行节点</option>
                  <option v-for="nid in nodeOptions" :key="nid" :value="nid">{{ nid }}</option>
                </select>
                <input
                  v-else
                  v-model="memberForm.homeNodeId"
                  type="text"
                  list="wg-node-options"
                  placeholder="执行该成员的 Node ID"
                  required
                  :disabled="creatingMember"
                />
                <span v-if="isNodeSession" class="muted" style="font-size: 12px">
                  仅可选择与你同 discovery_group 的 Node
                  <template v-if="authGroups.length">（{{ authGroups.join(", ") }}）</template>
                </span>
              </label>
            </div>

            <fieldset class="wg-member-create__tools">
              <legend>工具白名单</legend>
              <label
                v-for="t in MEMBER_TOOL_CHOICES"
                :key="t"
                class="wg-member-create__check"
              >
                <input
                  type="checkbox"
                  :checked="memberForm.tools.includes(t)"
                  :disabled="creatingMember"
                  @change="toggleMemberTool(t)"
                />
                {{ t }}
              </label>
            </fieldset>

            <details class="wg-member-create__prompt">
              <summary>高级：Prompt 侧车（可选）</summary>
              <label class="field">
                <span>Soul</span>
                <textarea
                  v-model="memberForm.soulMd"
                  rows="3"
                  placeholder="soul.md 正文（可空）"
                  :disabled="creatingMember"
                />
              </label>
              <label class="field">
                <span>User</span>
                <textarea
                  v-model="memberForm.userMd"
                  rows="2"
                  placeholder="user.md 正文（可空）"
                  :disabled="creatingMember"
                />
              </label>
              <label class="field">
                <span>Custom</span>
                <textarea
                  v-model="memberForm.customMd"
                  rows="2"
                  placeholder="custom.md 正文（可空）"
                  :disabled="creatingMember"
                />
              </label>
            </details>

            <div class="wg-member-create__actions">
              <button
                type="button"
                class="btn btn-ghost"
                :disabled="creatingMember"
                @click="cancelMemberForm"
              >
                取消
              </button>
              <button
                type="submit"
                class="btn btn-primary"
                :disabled="
                  creatingMember ||
                  !memberForm.displayName.trim() ||
                  !memberForm.homeNodeId.trim()
                "
              >
                {{ creatingMember ? "创建中…" : "创建成员" }}
              </button>
            </div>
          </form>

          <datalist id="wg-node-options">
            <option v-for="nid in nodeOptions" :key="nid" :value="nid" />
          </datalist>

          <div class="wg-member-config-list">
            <article
              v-for="m in configMembers"
              :key="m.key"
              class="wg-member-config"
              :class="{ 'wg-member-config--supervisor': m.kind === 'supervisor' }"
            >
              <header class="wg-member-config__head">
                <div>
                  <h4 class="wg-member-config__title">
                    {{ m.display_name }}
                    <span v-if="m.kind === 'supervisor'" class="wg-member-config__badge">编排</span>
                  </h4>
                  <p class="wg-member-config__id muted">{{ m.member_id }}</p>
                  <p v-if="m.hint" class="wg-member-config__hint muted">{{ m.hint }}</p>
                </div>
                <div class="wg-member-config__head-right">
                  <span class="wg-chat-rail__status" :data-status="m.status">
                    {{ memberStatusLabel(m.status) }}
                  </span>
                </div>
              </header>
              <dl class="wg-settings-kv">
                <div>
                  <dt>{{ m.kind === 'supervisor' ? '运行位置' : 'Home Node' }}</dt>
                  <dd>{{ m.home_node_id || "—" }}</dd>
                </div>
                <div>
                  <dt>Generation</dt>
                  <dd>{{ m.member_generation ?? "—" }}</dd>
                </div>
                <div class="wg-settings-kv__wide">
                  <dt>LLM 配置</dt>
                  <dd>{{ resolveLlmLabel(m.llm_profile_id, m.llm_profile_revision) }}</dd>
                </div>
                <div>
                  <dt>Max tool loops</dt>
                  <dd>{{ m.max_tool_loops ?? "—" }}</dd>
                </div>
                <div>
                  <dt>允许工具</dt>
                  <dd>{{ (m.allow_tool_names || []).join(", ") || "—" }}</dd>
                </div>
              </dl>

              <div class="wg-member-config__llm">
                <h5 class="wg-member-config__subtitle">可选 LLM</h5>
                <ul v-if="llmConfigs.length" class="wg-llm-list">
                  <li
                    v-for="cfg in llmConfigs"
                    :key="`${m.key}-${cfg.id}`"
                    class="wg-llm-item"
                    :class="{
                      'wg-llm-item--active': isLlmActive(m.llm_profile_id, cfg),
                      'wg-llm-item--action': m.kind === 'supervisor',
                      'wg-llm-item--busy': bindingLlmKey === `leader:${cfg.id}`,
                    }"
                    :role="m.kind === 'supervisor' ? 'button' : undefined"
                    :tabindex="m.kind === 'supervisor' ? 0 : undefined"
                    :aria-pressed="
                      m.kind === 'supervisor' ? isLlmActive(m.llm_profile_id, cfg) : undefined
                    "
                    @click="m.kind === 'supervisor' && bindSupervisorLlm(cfg)"
                    @keydown.enter.prevent="m.kind === 'supervisor' && bindSupervisorLlm(cfg)"
                  >
                    <strong class="wg-llm-item__name">{{ cfg.name }}</strong>
                    <span class="wg-llm-item__meta muted">{{ cfg.provider }} / {{ cfg.model }}</span>
                    <span
                      v-if="m.kind === 'supervisor' && isLlmActive(m.llm_profile_id, cfg)"
                      class="wg-llm-item__tag"
                    >当前</span>
                  </li>
                </ul>
                <p v-else class="muted">暂无可见 LLM 配置，请先在「管理 › 配置 › LLM」中创建。</p>
                <p v-if="m.kind === 'supervisor'" class="muted wg-member-config__hint">
                  点击上方条目可为 Supervisor 绑定 LLM；未绑定真实配置时对话会回退到 mock 回声。
                </p>
              </div>
            </article>
          </div>

          <p v-if="sortedMembers.length === 0 && !memberFormOpen" class="muted wg-detail-section__empty">
            除 Supervisor 外暂无 Agent 成员；点击「新增成员」创建。
          </p>

          <p class="muted wg-detail-section__note">
            创建成员后会自动向 Home Node 下发配置；Agent 成员的私有配置在线编辑将在后续版本接入。
          </p>
        </section>
      </template>
    </div>
  </section>
</template>
