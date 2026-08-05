<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  archiveWorkgroup,
  createWorkgroup,
  fetchAuthMe,
  fetchLLMConfigs,
  fetchWorkgroupACL,
  fetchWorkgroupLLMConfigs,
  fetchWorkgroupMemberSpec,
  fetchWorkgroupMembers,
  fetchWorkgroups,
  patchWorkgroup,
} from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast", "open-chat", "page-meta"]);

const loading = ref(false);
const loadingDetail = ref(false);
const creating = ref(false);
const deletingId = ref("");
const bindingLlmKey = ref("");
const error = ref("");

const workgroups = ref([]);
const selectedId = ref("");
const llmConfigs = ref([]);
const members = ref([]);
/** @type {import('vue').Ref<Record<string, any>>} */
const memberSpecs = ref({});
const acl = ref(null);
const createOpen = ref(false);
const ownerId = ref("console");
const ownerLabel = ref("未登录");

const createForm = reactive({
  displayName: "",
});

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
      status: wg.status === "active" ? "ready" : wg.status,
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
    active: "进行中",
    archiving: "归档中",
    archived: "已归档",
  };
  return map[status] || status || "—";
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
  createOpen.value = false;
  emit("open-chat", {
    workgroupId: item.workgroup_id,
    displayName: item.display_name || item.workgroup_id,
  });
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
        <h2 class="wg-detail-header__title">{{ selectedWorkgroup.display_name }}</h2>
        <p class="wg-detail-header__id" :title="selectedWorkgroup.workgroup_id">
          {{ selectedWorkgroup.workgroup_id }}
        </p>
        <div class="wg-detail-header__meta">
          <span class="wg-card__status" :data-status="selectedWorkgroup.status">
            {{ statusLabel(selectedWorkgroup.status) }}
          </span>
          <span class="muted">归属人 {{ selectedWorkgroup.created_by_node_id || "—" }}</span>
        </div>
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
          <p class="muted wg-detail-section__note">
            工作组级字段编辑与 ACL 管理将在后续版本接入。新建 Agent 成员时，LLM 默认继承 Supervisor 的配置。
          </p>
        </section>

        <section class="wg-detail-section" aria-label="成员配置">
          <div class="wg-detail-section__head">
            <h3 class="wg-detail-section__title">成员配置</h3>
            <span class="muted">{{ configMembers.length }} 人</span>
          </div>

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
                <span class="wg-chat-rail__status" :data-status="m.status">
                  {{ memberStatusLabel(m.status) }}
                </span>
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

          <p v-if="sortedMembers.length === 0" class="muted wg-detail-section__empty">
            除 Supervisor 外暂无 Agent 成员；可从 Agent 模板添加后再配置其私有项。
          </p>

          <p class="muted wg-detail-section__note">
            Agent 成员的私有配置在线编辑（切换 LLM、工具白名单等）将在后续版本接入。
          </p>
        </section>
      </template>
    </div>
  </section>
</template>
