<script setup>
import { ref, watch, onUnmounted, computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import NavRail from "../components/NavRail.vue";
import WorkgroupMemberModal from "../components/WorkgroupMemberModal.vue";
import { renderMarkdown } from "../utils/markdown.js";

const route = useRoute();
const router = useRouter();
const panelRef = ref(null);

const workgroupId = computed(() => String(route.params.workgroupId || "").trim());
const events = ref([]);
const draft = ref("");
const sending = ref(false);
const error = ref("");
const notice = ref("");
const pollTimer = ref(null);
const workgroupMeta = ref(null);
const selfNodeId = ref("");

const memberModalOpen = ref(false);
const memberModalMode = ref("create");
const memberModalWgId = ref("");
const memberModalMemberId = ref("");

let timelineReqSeq = 0;
let pollInFlight = false;

async function loadSelf() {
  try {
    const info = await api.getAgentInfo();
    selfNodeId.value = info.node_id || info.NodeID || "";
  } catch {
    selfNodeId.value = "";
  }
}

async function loadTimeline() {
  const reqSeq = ++timelineReqSeq;
  if (!workgroupId.value) {
    events.value = [];
    return;
  }
  try {
    const res = await api.getWorkgroupTimeline(workgroupId.value);
    if (reqSeq !== timelineReqSeq) return;
    events.value = res.events || [];
    error.value = "";
  } catch (e) {
    if (reqSeq !== timelineReqSeq) return;
    error.value = e?.message || "加载 Timeline 失败";
  }
}

async function loadWorkgroupMeta() {
  if (!workgroupId.value) {
    workgroupMeta.value = null;
    return;
  }
  try {
    const [sub, aclList] = await Promise.all([
      api.listWorkgroups({ scope: "subscribed" }),
      api.listWorkgroups({ scope: "acl" }),
    ]);
    const all = [...(sub.workgroups || []), ...(aclList.workgroups || [])];
    workgroupMeta.value =
      all.find((w) => String(w.workgroup_id || "").trim() === workgroupId.value) || null;
  } catch {
    workgroupMeta.value = null;
  }
}

function startPoll() {
  stopPoll();
  pollTimer.value = window.setInterval(async () => {
    if (pollInFlight) return;
    pollInFlight = true;
    try {
      await loadTimeline();
    } finally {
      pollInFlight = false;
    }
  }, 3000);
}

function stopPoll() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value);
    pollTimer.value = null;
  }
  pollInFlight = false;
}

async function send() {
  const text = draft.value.trim();
  if (!text || !workgroupId.value || sending.value) return;
  sending.value = true;
  error.value = "";
  try {
    await api.postWorkgroupMessage(workgroupId.value, text);
    draft.value = "";
    await loadTimeline();
  } catch (e) {
    error.value = e?.message || "发送失败";
  } finally {
    sending.value = false;
  }
}

async function onRailDeleteAgent(payload) {
  const aid = String(typeof payload === "string" ? payload : payload?.id || "").trim();
  if (!aid) return;
  const agent = typeof payload === "object" && payload?.agent ? payload.agent : { agent_id: aid };
  const label = agent.display_name || agent.DisplayName || aid;
  if (!window.confirm(`确定删除 Agent「${label}」？\n\n将停止该实例并归档记录，不可恢复。`)) return;
  try {
    await api.deleteAgent(aid);
    await panelRef.value?.refresh?.();
  } catch (e) {
    error.value = e?.message || "删除失败";
  }
}

function openMemberCreate(wgId) {
  const wid = String(wgId || workgroupId.value || "").trim();
  if (!wid) return;
  memberModalMode.value = "create";
  memberModalWgId.value = wid;
  memberModalMemberId.value = "";
  memberModalOpen.value = true;
  const target = {
    name: "workgroups",
    params: { workgroupId: wid },
    query: { createMember: "1" },
  };
  if (workgroupId.value !== wid) router.push(target);
  else router.replace(target);
}

function openMemberEdit(payload) {
  const wid = String(payload?.workgroupId || workgroupId.value || "").trim();
  const mid = String(payload?.memberId || "").trim();
  if (!wid || !mid) return;
  memberModalMode.value = "edit";
  memberModalWgId.value = wid;
  memberModalMemberId.value = mid;
  memberModalOpen.value = true;
  router.push({
    name: "workgroups",
    params: { workgroupId: wid },
    query: { member: mid, editMember: "1" },
  });
}

function closeMemberModal() {
  memberModalOpen.value = false;
  const wid = memberModalWgId.value || workgroupId.value;
  const q = { ...route.query };
  delete q.createMember;
  delete q.editMember;
  router.replace({
    name: "workgroups",
    params: wid ? { workgroupId: wid } : {},
    query: q,
  });
}

async function onMemberSaved(payload) {
  const name = payload?.displayName || "成员";
  notice.value = payload?.mode === "create" ? `已添加成员「${name}」` : `已更新成员「${name}」`;
  window.setTimeout(() => {
    if (notice.value.includes(name)) notice.value = "";
  }, 3200);
  await panelRef.value?.refreshWorkgroups?.({ force: true });
  const wid = String(payload?.workgroupId || memberModalWgId.value || "").trim();
  if (wid) await panelRef.value?.loadMembers?.(wid, true);
}

function eventLabel(ev) {
  if (ev.type === "human_message") return ev.actor_id || "human";
  if (ev.type === "actor_final_text") {
    if (ev.actor_id === "leader") return "Supervisor";
    return ev.actor_id || "member";
  }
  return ev.type || "event";
}

function isHumanEvent(ev) {
  return String(ev?.type || "") === "human_message";
}

const toolbarTitle = computed(() => {
  const name = String(workgroupMeta.value?.display_name || "").trim();
  return name ? `工作组 · ${name}` : "工作组";
});

watch(
  workgroupId,
  async (id) => {
    stopPoll();
    await Promise.all([loadTimeline(), loadWorkgroupMeta()]);
    if (id) startPoll();
  },
  { immediate: true },
);

watch(
  () => [route.query.createMember, route.query.editMember, route.query.member, workgroupId.value],
  ([createMember, editMember, member, wid]) => {
    if (!wid) return;
    if (String(createMember || "") === "1") {
      memberModalMode.value = "create";
      memberModalWgId.value = wid;
      memberModalMemberId.value = "";
      memberModalOpen.value = true;
      return;
    }
    const mid = String(member || "").trim();
    if (String(editMember || "") === "1" && mid) {
      memberModalMode.value = "edit";
      memberModalWgId.value = wid;
      memberModalMemberId.value = mid;
      memberModalOpen.value = true;
    }
  },
  { immediate: true },
);

onMounted(loadSelf);
onUnmounted(stopPoll);
</script>

<template>
  <div class="app__body app__body--chat-v61">
    <aside class="app__col app__col--agents">
      <NavRail
        ref="panelRef"
        @switch="(id) => router.push({ name: 'agents', params: { agentId: id } })"
        @create="router.push({ name: 'agents', query: { createAgent: '1' } })"
        @delete="onRailDeleteAgent"
        @create-member="openMemberCreate"
        @configure-member="openMemberEdit"
      />
    </aside>
    <div class="app__main-col wg-chat">
      <div v-if="error" class="chat-error-banner">{{ error }}</div>
      <div v-else-if="notice" class="chat-notice-banner">{{ notice }}</div>
      <div v-if="!workgroupId" class="chat-empty-agent">
        <p>选择左侧已订阅工作组，或点击 + 新建。</p>
        <button type="button" class="wg-chat__link" @click="router.push({ name: 'agents' })">
          返回 Agents
        </button>
      </div>
      <template v-else>
        <div class="wg-chat__toolbar">
          <div class="wg-chat__title" :title="toolbarTitle">{{ toolbarTitle }}</div>
          <button
            type="button"
            class="wg-chat__toolbar-btn"
            title="添加成员"
            @click="openMemberCreate(workgroupId)"
          >
            添加成员
          </button>
        </div>

        <div class="wg-chat__body">
          <div class="wg-chat__timeline chat__stream">
            <article
              v-for="ev in events"
              :key="ev.event_id || ev.seq"
              class="msg"
              :class="isHumanEvent(ev) ? 'msg--user' : 'msg--assistant'"
            >
              <div class="msg__body">
                <div class="msg__hint">
                  {{ eventLabel(ev) }}
                  <span v-if="ev.seq"> · #{{ ev.seq }}</span>
                </div>
                <div
                  class="msg__bubble"
                  :class="isHumanEvent(ev) ? 'msg__bubble--user' : 'msg__bubble--assistant-md'"
                >
                  <template v-if="isHumanEvent(ev)">{{ ev.text }}</template>
                  <div
                    v-else
                    class="tool-exec-bubble__markdown assistant-msg__md"
                    v-html="renderMarkdown(ev.text || '')"
                  />
                </div>
              </div>
            </article>
            <div v-if="!events.length" class="chat__empty">
              <div class="chat__empty-inner">
                <div class="chat__empty-title">开始对话</div>
                <div class="chat__empty-hint">向工作组发言，Leader 会编排成员协作</div>
                <button
                  type="button"
                  class="btn btn--ghost chat__empty-action"
                  @click="openMemberCreate(workgroupId)"
                >
                  先添加一个成员
                </button>
              </div>
            </div>
          </div>
        </div>

        <footer class="chat__composer">
          <div class="chat__composer-pill">
            <div class="chat__composer-pill-center">
              <input
                v-model="draft"
                class="chat__textarea"
                type="text"
                placeholder="向工作组发言…"
                :disabled="sending"
                @keydown.enter.prevent="send"
              />
            </div>
            <div class="chat__composer-pill-right">
              <button
                type="button"
                class="chat__composer-send"
                title="发送"
                aria-label="发送"
                :disabled="sending || !draft.trim()"
                @click="send"
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
              <span class="chat__input-strip-left">Enter 发送</span>
            </div>
          </div>
        </footer>
      </template>
    </div>

    <WorkgroupMemberModal
      :open="memberModalOpen"
      :mode="memberModalMode"
      :workgroup-id="memberModalWgId"
      :member-id="memberModalMemberId"
      :default-home-node-id="selfNodeId"
      @close="closeMemberModal"
      @saved="onMemberSaved"
    />
  </div>
</template>

<style scoped>
.wg-chat {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.wg-chat__toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  align-items: center;
  justify-content: space-between;
  padding: 0.6rem 1rem;
  border-bottom: 1px solid var(--border-subtle, #e5e7eb);
  font-size: 0.75rem;
}
.wg-chat__title {
  color: var(--color-text, #111827);
  font-size: 13px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}
.wg-chat__toolbar-btn {
  border: 1px solid var(--color-border, #d1d5db);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111827);
  border-radius: 6px;
  padding: 4px 10px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.wg-chat__toolbar-btn:hover {
  border-color: var(--color-primary, #0078d4);
  color: var(--color-primary-strong, var(--color-primary, #0078d4));
}
.wg-chat__body {
  flex: 1;
  display: flex;
  min-height: 0;
  background: var(--color-editor, #fff);
}
.wg-chat__timeline.chat__stream {
  flex: 1;
  min-width: 0;
}
.wg-chat__timeline .msg__body {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}
.wg-chat__link {
  margin-top: 0.5rem;
  border: none;
  background: none;
  color: var(--color-primary-strong, var(--accent, #242424));
  cursor: pointer;
  font-size: inherit;
  padding: 0;
  text-decoration: underline;
}
.chat-empty-agent {
  padding: 2rem;
  color: var(--color-text-muted, #6b7280);
}
.chat-notice-banner {
  padding: 0.5rem 1rem;
  background: color-mix(in srgb, var(--color-primary, #0078d4) 12%, transparent);
  color: var(--color-text, #111827);
  font-size: 12px;
}
.chat__empty-action {
  margin-top: 12px;
}
.wg-chat :deep(.chat__composer-pill) {
  padding-left: 16px;
}
.wg-chat :deep(.chat__textarea) {
  padding-left: 6px;
  padding-right: 8px;
}
</style>
