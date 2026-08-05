<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  archiveWorkgroup,
  createWorkgroup,
  fetchAuthMe,
  fetchWorkgroupLLMConfigs,
  fetchWorkgroups,
  fetchWorkgroupTimeline,
  postWorkgroupMessage,
} from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const loading = ref(false);
const loadingTimeline = ref(false);
const sending = ref(false);
const creating = ref(false);
const deletingId = ref("");
const error = ref("");

const workgroups = ref([]);
const selectedId = ref("");
/** @type {import('vue').Ref<'chat' | 'settings' | ''>} */
const detailMode = ref("");
const timeline = ref([]);
const llmConfigs = ref([]);
const createOpen = ref(false);
const ownerId = ref("console");
const ownerLabel = ref("未登录");

const createForm = reactive({
  displayName: "",
});

const form = reactive({
  fromNodeId: "console",
  text: "",
  disableTools: false,
});

const selectedWorkgroup = computed(
  () => workgroups.value.find((item) => item.workgroup_id === selectedId.value) || null,
);

const showDetail = computed(() => Boolean(selectedWorkgroup.value && detailMode.value));

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
        form.fromNodeId = id;
        return;
      }
      if (me.kind === "admin") {
        const id = String(me.subject || "admin").trim() || "admin";
        ownerId.value = id;
        ownerLabel.value = `管理员 · ${id}`;
        form.fromNodeId = id;
        return;
      }
    }
  } catch {
    /* ignore */
  }
  ownerId.value = "console";
  ownerLabel.value = "console";
  form.fromNodeId = "console";
}

async function loadWorkgroups() {
  loading.value = true;
  error.value = "";
  try {
    workgroups.value = await fetchWorkgroups();
    if (selectedId.value && !workgroups.value.some((w) => w.workgroup_id === selectedId.value)) {
      selectedId.value = "";
      detailMode.value = "";
    }
  } catch (err) {
    error.value = err.message || "加载工作组失败";
    emit("toast", { message: error.value, type: "error" });
  } finally {
    loading.value = false;
  }
}

async function loadTimelineAndLLM() {
  if (!selectedId.value) {
    timeline.value = [];
    llmConfigs.value = [];
    return;
  }
  loadingTimeline.value = true;
  try {
    const tasks = [fetchWorkgroupLLMConfigs(selectedId.value)];
    if (detailMode.value === "chat") {
      tasks.unshift(fetchWorkgroupTimeline(selectedId.value));
    }
    const results = await Promise.all(tasks);
    if (detailMode.value === "chat") {
      timeline.value = results[0] || [];
      llmConfigs.value = results[1] || [];
    } else {
      timeline.value = [];
      llmConfigs.value = results[0] || [];
    }
  } catch (err) {
    emit("toast", { message: err.message || "加载工作组详情失败", type: "error" });
  } finally {
    loadingTimeline.value = false;
  }
}

function openChat(id) {
  selectedId.value = id;
  detailMode.value = "chat";
  createOpen.value = false;
}

function openSettings(id) {
  selectedId.value = id;
  detailMode.value = "settings";
  createOpen.value = false;
}

function backToGrid() {
  selectedId.value = "";
  detailMode.value = "";
  timeline.value = [];
  llmConfigs.value = [];
}

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
    await createWorkgroup({
      display_name: displayName,
      created_by_node_id: createdBy,
      llm_profile_id: "default",
      llm_profile_revision: "1",
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

async function sendMessage() {
  const text = form.text.trim();
  const fromNodeId = form.fromNodeId.trim();
  if (!selectedId.value || !text || !fromNodeId) return;
  sending.value = true;
  try {
    await postWorkgroupMessage(selectedId.value, {
      text,
      from_node_id: fromNodeId,
      disable_tools: form.disableTools,
    });
    form.text = "";
    await loadTimelineAndLLM();
    emit("toast", { message: "消息已发送", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message || "发送失败", type: "error" });
  } finally {
    sending.value = false;
  }
}

watch(
  () => [selectedId.value, detailMode.value],
  () => {
    if (props.active && selectedId.value && detailMode.value) loadTimelineAndLLM();
  },
);

watch(
  () => props.active,
  (active) => {
    if (active) {
      resolveCreatorDefault().then(() => loadWorkgroups());
    }
  },
);

onMounted(async () => {
  if (props.active) {
    await resolveCreatorDefault();
    await loadWorkgroups();
  }
});
</script>

<template>
  <section class="workgroup-page">
    <div v-if="!showDetail" class="workgroup-grid-wrap">
      <div class="panel-head workgroup-grid-head">
        <h2 class="panel-title">工作组</h2>
        <button type="button" class="btn btn-ghost" :disabled="loading" @click="loadWorkgroups">
          刷新
        </button>
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
              @click="openChat(item.workgroup_id)"
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
                <circle cx="10" cy="10" r="2.4" stroke="currentColor" stroke-width="1.4" />
                <path
                  d="M10 3.2v1.6M10 15.2v1.6M3.2 10h1.6M15.2 10h1.6M5.1 5.1l1.1 1.1M13.8 13.8l1.1 1.1M5.1 14.9l1.1-1.1M13.8 6.2l1.1-1.1"
                  stroke="currentColor"
                  stroke-width="1.4"
                  stroke-linecap="round"
                />
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

    <div v-else class="panel workgroup-detail">
      <div class="panel-head">
        <div class="workgroup-detail__title-block">
          <button type="button" class="btn btn-ghost workgroup-back" @click="backToGrid">
            ← 返回
          </button>
          <h2 class="panel-title">{{ selectedWorkgroup.display_name }}</h2>
          <span class="panel-meta">{{ selectedWorkgroup.workgroup_id }}</span>
          <span class="pill">{{ detailMode === "chat" ? "对话" : "配置" }}</span>
        </div>
        <button type="button" class="btn btn-ghost" :disabled="loadingTimeline" @click="loadTimelineAndLLM">
          刷新
        </button>
      </div>

      <div class="workgroup-meta-row">
        <span class="pill">{{ statusLabel(selectedWorkgroup.status) }}</span>
        <span class="muted">
          LLM: {{ selectedWorkgroup.llm_profile_id }}@{{ selectedWorkgroup.llm_profile_revision }}
        </span>
        <span class="muted">归属人 {{ selectedWorkgroup.created_by_node_id || "—" }}</span>
      </div>

      <template v-if="detailMode === 'settings'">
        <div class="panel subsection">
          <div class="panel-head">
            <h3 class="panel-title">可用 LLM 配置</h3>
          </div>
          <div v-if="loadingTimeline" class="state">加载中…</div>
          <div v-else class="tags">
            <span v-if="!llmConfigs.length" class="muted">无可见配置</span>
            <span v-for="cfg in llmConfigs" :key="cfg.id" class="tag">
              {{ cfg.name }} · {{ cfg.provider }} / {{ cfg.model }}
            </span>
          </div>
        </div>
        <p class="muted workgroup-settings-note">
          成员管理、ACL 与从模板加成员将在后续版本接入。
        </p>
      </template>

      <div v-else class="panel subsection">
        <div class="panel-head">
          <h3 class="panel-title">工作组对话</h3>
          <span class="panel-meta">{{ timeline.length }} 条</span>
        </div>
        <div v-if="loadingTimeline" class="state">加载对话中…</div>
        <div v-else class="timeline-list">
          <article v-for="event in timeline" :key="event.event_id" class="timeline-item">
            <header>
              <strong>{{ event.actor_id }}</strong>
              <span class="muted">{{ event.type }}</span>
            </header>
            <p>{{ event.text || "（空）" }}</p>
          </article>
          <p v-if="!timeline.length" class="muted">暂无消息</p>
        </div>
        <form class="workgroup-chat-form" @submit.prevent="sendMessage">
          <label class="field field-checkbox">
            <input v-model="form.disableTools" type="checkbox" />
            <span>禁用 supervisor 工具（纯对话）</span>
          </label>
          <label class="field">
            <span>消息内容</span>
            <textarea
              v-model="form.text"
              rows="3"
              placeholder="输入消息并发送到工作组 supervisor"
            />
          </label>
          <div class="form-actions">
            <button
              type="submit"
              class="btn btn-primary"
              :disabled="sending || !form.text.trim() || !form.fromNodeId.trim()"
            >
              {{ sending ? "发送中…" : "发送消息" }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </section>
</template>
