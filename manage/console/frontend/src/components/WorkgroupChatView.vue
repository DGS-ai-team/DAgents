<script setup>
import { computed, nextTick, onMounted, ref, watch } from "vue";
import {
  fetchAuthMe,
  fetchWorkgroupACL,
  fetchWorkgroupMembers,
  fetchWorkgroupTimeline,
  postWorkgroupMessage,
} from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
  workgroupId: { type: String, default: "" },
  displayName: { type: String, default: "" },
});

const emit = defineEmits(["toast", "close"]);

const loading = ref(false);
const loadingMembers = ref(false);
const sending = ref(false);
const timeline = ref([]);
const members = ref([]);
const acl = ref(null);
const fromNodeId = ref("console");
const input = ref("");
const textareaRef = ref(null);
const streamRef = ref(null);
const followTail = ref(true);

const title = computed(() => {
  const name = String(props.displayName || "").trim() || props.workgroupId || "未命名";
  return `工作组 · ${name}`;
});

const canSubmit = computed(
  () => Boolean(input.value.trim()) && !sending.value && Boolean(fromNodeId.value.trim()),
);

const visibleEvents = computed(() =>
  (timeline.value || []).filter((event) => {
    const text = String(event?.text || "").trim();
    return Boolean(text) || Boolean(event?.type);
  }),
);

const sortedMembers = computed(() => {
  const list = [...(members.value || [])];
  list.sort((a, b) => String(a.display_name || "").localeCompare(String(b.display_name || ""), "zh"));
  return list;
});

/** Supervisor（leader）始终作为成员列表第一项。 */
const railMembers = computed(() => {
  const rows = [
    {
      member_id: "leader",
      display_name: "Supervisor",
      home_node_id: "manage",
      status: "ready",
      kind: "supervisor",
    },
  ];
  for (const m of sortedMembers.value) {
    rows.push({ ...m, kind: "member" });
  }
  return rows;
});

const aclPeople = computed(() => {
  const owners = Array.isArray(acl.value?.owners) ? acl.value.owners : [];
  const collaborators = Array.isArray(acl.value?.collaborators) ? acl.value.collaborators : [];
  const seen = new Set();
  const rows = [];
  for (const id of owners) {
    const node = String(id || "").trim();
    if (!node || seen.has(node)) continue;
    seen.add(node);
    rows.push({ id: node, role: "owner" });
  }
  for (const id of collaborators) {
    const node = String(id || "").trim();
    if (!node || seen.has(node)) continue;
    seen.add(node);
    rows.push({ id: node, role: "collaborator" });
  }
  return rows;
});

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

function initialOf(name) {
  const s = String(name || "").trim();
  return s ? s.slice(0, 1).toUpperCase() : "?";
}

async function resolveSender() {
  try {
    const me = await fetchAuthMe();
    if (me?.authenticated) {
      if (me.kind === "node") {
        fromNodeId.value = String(me.agent_id || me.subject || "").trim() || "console";
        return;
      }
      if (me.kind === "admin") {
        fromNodeId.value = String(me.subject || "admin").trim() || "admin";
        return;
      }
    }
  } catch {
    /* ignore */
  }
  fromNodeId.value = "console";
}

async function loadMembers() {
  if (!props.workgroupId) {
    members.value = [];
    acl.value = null;
    return;
  }
  loadingMembers.value = true;
  try {
    const [memberList, aclData] = await Promise.all([
      fetchWorkgroupMembers(props.workgroupId),
      fetchWorkgroupACL(props.workgroupId).catch(() => null),
    ]);
    members.value = Array.isArray(memberList) ? memberList : [];
    acl.value = aclData;
  } catch (err) {
    members.value = [];
    emit("toast", { message: err.message || "加载成员失败", type: "error" });
  } finally {
    loadingMembers.value = false;
  }
}

async function loadTimeline() {
  if (!props.workgroupId) {
    timeline.value = [];
    return;
  }
  loading.value = true;
  try {
    timeline.value = await fetchWorkgroupTimeline(props.workgroupId);
    await nextTick();
    scrollToBottom(true);
  } catch (err) {
    emit("toast", { message: err.message || "加载对话失败", type: "error" });
  } finally {
    loading.value = false;
  }
}

async function loadAll() {
  await Promise.all([loadTimeline(), loadMembers()]);
}

function eventRole(event) {
  const type = String(event?.type || "").toLowerCase();
  const actor = String(event?.actor_id || "").toLowerCase();
  if (type === "human_message") return "user";
  if (
    type === "actor_final_text" ||
    type.includes("assistant") ||
    type.includes("agent") ||
    type.includes("supervisor") ||
    actor === "leader" ||
    actor === "supervisor"
  ) {
    return "assistant";
  }
  return "user";
}

function eventActorLabel(event) {
  const actor = String(event?.actor_id || "").trim();
  if (!actor || actor === "leader") return "Supervisor";
  return actor;
}

function resizeTextarea() {
  const el = textareaRef.value;
  if (!el) return;
  el.style.height = "auto";
  el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
}

function onStreamScroll() {
  const el = streamRef.value;
  if (!el) return;
  followTail.value = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
}

function scrollToBottom(force = false) {
  const el = streamRef.value;
  if (!el) return;
  if (!force && !followTail.value) return;
  el.scrollTop = el.scrollHeight;
}

async function sendMessage() {
  const text = input.value.trim();
  const sender = fromNodeId.value.trim();
  if (!props.workgroupId || !text || !sender || sending.value) return;
  sending.value = true;
  try {
    await postWorkgroupMessage(props.workgroupId, {
      text,
      from_node_id: sender,
    });
    input.value = "";
    await nextTick();
    resizeTextarea();
    followTail.value = true;
    await loadTimeline();
  } catch (err) {
    emit("toast", { message: err.message || "发送失败", type: "error" });
  } finally {
    sending.value = false;
  }
}

function onKeydown(event) {
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    sendMessage();
  }
}

watch(
  () => [props.active, props.workgroupId],
  ([active, id]) => {
    if (active && id) loadAll();
  },
);

watch(input, () => {
  nextTick(resizeTextarea);
});

onMounted(async () => {
  await resolveSender();
  if (props.active && props.workgroupId) await loadAll();
  nextTick(resizeTextarea);
});
</script>

<template>
  <div class="wg-chat-shell">
    <aside class="wg-chat-rail" aria-label="工作组成员">
      <div class="wg-chat-rail__head">
        <strong>成员</strong>
        <span class="muted">{{ railMembers.length }}</span>
      </div>

      <div v-if="loadingMembers" class="wg-chat-rail__state muted">加载中…</div>
      <ul v-else class="wg-chat-rail__list">
        <li
          v-for="m in railMembers"
          :key="m.member_id"
          class="wg-chat-rail__item"
          :class="{ 'wg-chat-rail__item--supervisor': m.kind === 'supervisor' }"
        >
          <span class="wg-chat-rail__avatar" aria-hidden="true">{{ initialOf(m.display_name) }}</span>
          <div class="wg-chat-rail__meta">
            <strong class="wg-chat-rail__name" :title="m.display_name">
              {{ m.display_name }}
              <span v-if="m.kind === 'supervisor'" class="wg-chat-rail__badge">编排</span>
            </strong>
            <span class="wg-chat-rail__sub muted" :title="m.home_node_id">{{ m.home_node_id }}</span>
          </div>
          <span class="wg-chat-rail__status" :data-status="m.status">{{ memberStatusLabel(m.status) }}</span>
        </li>
      </ul>

      <div class="wg-chat-rail__section">
        <div class="wg-chat-rail__head">
          <strong>访问身份</strong>
          <span class="muted">{{ aclPeople.length }}</span>
        </div>
        <ul v-if="aclPeople.length" class="wg-chat-rail__list">
          <li v-for="p in aclPeople" :key="p.id" class="wg-chat-rail__item">
            <span class="wg-chat-rail__avatar wg-chat-rail__avatar--node" aria-hidden="true">
              {{ initialOf(p.id) }}
            </span>
            <div class="wg-chat-rail__meta">
              <strong class="wg-chat-rail__name" :title="p.id">{{ p.id }}</strong>
              <span class="wg-chat-rail__sub muted">
                {{ p.role === "owner" ? "归属人" : "协作者" }}
              </span>
            </div>
          </li>
        </ul>
        <p v-else class="wg-chat-rail__state muted">暂无 ACL 记录</p>
      </div>
    </aside>

    <section class="panel panel--flex chat wg-chat-page" aria-label="工作组对话">
      <header class="chat__header">
        <div class="chat__title">
          <span class="chat__title-main">{{ title }}</span>
        </div>
        <div class="chat__header-meta">
          <span class="chat__header-id" :title="workgroupId">{{ workgroupId }}</span>
        </div>
      </header>

      <div ref="streamRef" class="chat__stream" @scroll="onStreamScroll">
        <div v-if="loading" class="chat__empty">
          <div class="chat__empty-inner">
            <div class="chat__empty-hint">加载对话中…</div>
          </div>
        </div>
        <div v-else-if="!visibleEvents.length" class="chat__empty">
          <div class="chat__empty-inner">
            <div class="chat__empty-title">开始对话</div>
            <div class="chat__empty-hint">输入消息与工作组协作</div>
          </div>
        </div>
        <template v-else>
          <article
            v-for="event in visibleEvents"
            :key="event.event_id"
            class="msg"
            :class="eventRole(event) === 'user' ? 'msg--user' : 'msg--assistant'"
          >
            <div class="msg__body">
              <div
                class="msg__bubble"
                :class="eventRole(event) === 'user' ? 'msg__bubble--user' : 'msg__bubble--assistant'"
              >
                <div v-if="eventRole(event) === 'assistant'" class="msg__hint">
                  {{ eventActorLabel(event) }}
                </div>
                <div class="msg__text">{{ event.text || "（空）" }}</div>
              </div>
            </div>
          </article>
        </template>
      </div>

      <footer class="chat__composer">
        <div class="chat__composer-pill">
          <div class="chat__composer-pill-center">
            <textarea
              ref="textareaRef"
              v-model="input"
              class="chat__textarea"
              rows="1"
              placeholder="输入消息…"
              :disabled="sending"
              @keydown="onKeydown"
              @input="resizeTextarea"
            />
          </div>
          <div class="chat__composer-pill-right">
            <button
              type="button"
              class="chat__composer-send"
              title="发送"
              aria-label="发送"
              :disabled="!canSubmit"
              @click="sendMessage"
            >
              <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path
                  d="M8 12.25V3.75M8 3.75L4.5 7.25M8 3.75l3.5 3.5"
                  stroke="currentColor"
                  stroke-width="1.7"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
          </div>
        </div>
        <div class="chat__composer-statusline">
          <div class="chat__composer-statusline-left">
            <span class="chat__input-strip-left">Enter 发送 · Shift+Enter 换行</span>
          </div>
          <div class="chat__composer-statusline-right">
            <span v-if="sending" class="chat__input-strip-right">发送中…</span>
          </div>
        </div>
      </footer>
    </section>
  </div>
</template>
