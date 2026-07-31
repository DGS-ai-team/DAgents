<script setup>
import { ref, watch, onUnmounted, computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import WorkgroupPanel from "../components/WorkgroupPanel.vue";

const route = useRoute();
const router = useRouter();
const panelRef = ref(null);

const workgroupId = computed(() => String(route.params.workgroupId || "").trim());
const selfNodeId = ref("");
const events = ref([]);
const members = ref([]);
const grants = ref([]);
const hitlList = ref([]);
const draft = ref("");
const sending = ref(false);
const error = ref("");
const pollTimer = ref(null);
const acl = ref(null);
const collabDraft = ref("");
const collabBusy = ref(false);

const memberName = ref("");
const memberHome = ref("");
const memberBusy = ref(false);
const hitlPrompt = ref("");
const hitlBusy = ref(false);
const hitlAnswers = ref({});

async function loadSelf() {
  try {
    const info = await api.getAgentInfo();
    selfNodeId.value = info.node_id || info.NodeID || "";
  } catch {
    selfNodeId.value = "";
  }
}

async function loadTimeline() {
  if (!workgroupId.value) {
    events.value = [];
    return;
  }
  try {
    const res = await api.getWorkgroupTimeline(workgroupId.value);
    events.value = res.events || [];
    error.value = "";
  } catch (e) {
    error.value = e?.message || "加载 Timeline 失败";
  }
}

async function loadACL() {
  if (!workgroupId.value) {
    acl.value = null;
    return;
  }
  try {
    acl.value = await api.getWorkgroupACL(workgroupId.value);
  } catch {
    acl.value = null;
  }
}

async function loadMembersGrantsHitl() {
  if (!workgroupId.value) {
    members.value = [];
    grants.value = [];
    hitlList.value = [];
    return;
  }
  try {
    const [m, g, h] = await Promise.all([
      api.listWorkgroupMembers(workgroupId.value),
      api.listWorkgroupGrants(workgroupId.value),
      api.listWorkgroupHITL(workgroupId.value, true),
    ]);
    members.value = m.members || [];
    if (m.node_id) selfNodeId.value = m.node_id;
    grants.value = g.grants || [];
    hitlList.value = h.hitl || [];
  } catch (e) {
    // 非致命：Timeline 仍可用
    if (!error.value) error.value = e?.message || "";
  }
}

function startPoll() {
  stopPoll();
  pollTimer.value = window.setInterval(async () => {
    await Promise.all([loadTimeline(), loadMembersGrantsHitl()]);
  }, 3000);
}

function stopPoll() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value);
    pollTimer.value = null;
  }
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

async function addCollaborator() {
  const nid = collabDraft.value.trim();
  if (!nid || !workgroupId.value || collabBusy.value) return;
  collabBusy.value = true;
  error.value = "";
  try {
    acl.value = await api.addWorkgroupCollaborator(workgroupId.value, nid);
    collabDraft.value = "";
    panelRef.value?.refresh?.();
  } catch (e) {
    error.value = e?.message || "添加协作者失败";
  } finally {
    collabBusy.value = false;
  }
}

async function createMember() {
  const name = memberName.value.trim();
  const home = memberHome.value.trim() || selfNodeId.value;
  if (!name || !home || memberBusy.value) return;
  memberBusy.value = true;
  error.value = "";
  try {
    await api.createWorkgroupMember(workgroupId.value, {
      display_name: name,
      home_node_id: home,
      allow_tool_names: ["read_file"],
      prompt: { soul_md: name },
    });
    memberName.value = "";
    await loadMembersGrantsHitl();
  } catch (e) {
    error.value = e?.message || "创建成员失败";
  } finally {
    memberBusy.value = false;
  }
}

async function inviteGrant(memberId) {
  error.value = "";
  try {
    await api.inviteWorkgroupGrant(workgroupId.value, memberId, ["read_file"]);
    await loadMembersGrantsHitl();
  } catch (e) {
    error.value = e?.message || "邀请 Grant 失败";
  }
}

async function acceptGrant(grant) {
  error.value = "";
  try {
    await api.acceptWorkgroupGrant(
      workgroupId.value,
      grant.grant_id,
      grant.member_spec_digest,
    );
    await loadMembersGrantsHitl();
  } catch (e) {
    error.value = e?.message || "接受 Grant 失败";
  }
}

async function createHitl() {
  const prompt = hitlPrompt.value.trim();
  if (!prompt || hitlBusy.value) return;
  hitlBusy.value = true;
  error.value = "";
  try {
    await api.createWorkgroupHITL(workgroupId.value, prompt);
    hitlPrompt.value = "";
    await loadMembersGrantsHitl();
  } catch (e) {
    error.value = e?.message || "创建 HITL 失败";
  } finally {
    hitlBusy.value = false;
  }
}

async function resolveHitl(hitlId) {
  const answer = String(hitlAnswers.value[hitlId] || "").trim();
  if (!answer) return;
  error.value = "";
  try {
    await api.resolveWorkgroupHITL(workgroupId.value, hitlId, answer);
    delete hitlAnswers.value[hitlId];
    await loadMembersGrantsHitl();
  } catch (e) {
    error.value = e?.message || "决议 HITL 失败";
  }
}

function eventLabel(ev) {
  if (ev.type === "human_message") return ev.actor_id || "human";
  if (ev.type === "actor_final_text") return ev.actor_id || "member";
  return ev.type || "event";
}

const pendingGrantsForSelf = computed(() =>
  grants.value.filter(
    (g) => g.status === "invited" && g.home_node_id === selfNodeId.value,
  ),
);

const aclSummary = computed(() => {
  if (!acl.value) return "";
  const owners = (acl.value.owners || []).join(", ");
  const collab = (acl.value.collaborators || []).join(", ") || "—";
  return `owners: ${owners} · collaborators: ${collab}`;
});

watch(
  workgroupId,
  async (id) => {
    stopPoll();
    await Promise.all([loadTimeline(), loadACL(), loadMembersGrantsHitl()]);
    if (id) startPoll();
  },
  { immediate: true },
);

onMounted(loadSelf);
onUnmounted(stopPoll);
</script>

<template>
  <div class="app__body app__body--chat-v61">
    <aside class="app__col app__col--agents">
      <WorkgroupPanel ref="panelRef" />
    </aside>
    <div class="app__main-col wg-chat">
      <div v-if="error" class="chat-error-banner">{{ error }}</div>
      <div v-if="!workgroupId" class="chat-empty-agent">
        <p>选择左侧已订阅工作组，或点击 + 新建。</p>
        <button type="button" class="wg-chat__link" @click="router.push({ name: 'agents' })">
          返回 Agents
        </button>
      </div>
      <template v-else>
        <div class="wg-chat__toolbar">
          <div class="wg-chat__acl" :title="aclSummary">{{ aclSummary || "加载 ACL…" }}</div>
          <form class="wg-chat__collab" @submit.prevent="addCollaborator">
            <input
              v-model="collabDraft"
              type="text"
              placeholder="添加协作者 node_id"
              :disabled="collabBusy"
            />
            <button type="submit" :disabled="collabBusy || !collabDraft.trim()">添加</button>
          </form>
        </div>

        <div class="wg-chat__body">
          <div class="wg-chat__timeline">
            <div v-for="ev in events" :key="ev.event_id || ev.seq" class="wg-chat__event">
              <div class="wg-chat__event-meta">
                <span>{{ eventLabel(ev) }}</span>
                <span v-if="ev.seq">#{{ ev.seq }}</span>
              </div>
              <div class="wg-chat__event-text">{{ ev.text }}</div>
            </div>
            <p v-if="!events.length" class="wg-chat__empty">暂无消息</p>
          </div>

          <aside class="wg-side">
            <section class="wg-side__sec">
              <h4>成员</h4>
              <ul>
                <li v-for="m in members" :key="m.member_id">
                  <div class="wg-side__row">
                    <span>{{ m.display_name }} · {{ m.status }}</span>
                    <button
                      v-if="m.status === 'requested'"
                      type="button"
                      @click="inviteGrant(m.member_id)"
                    >
                      邀请 Grant
                    </button>
                  </div>
                  <div class="wg-side__meta">{{ m.home_node_id }}</div>
                </li>
              </ul>
              <form class="wg-side__form" @submit.prevent="createMember">
                <input v-model="memberName" placeholder="成员显示名" />
                <input v-model="memberHome" :placeholder="`home node（默认 ${selfNodeId || '本机'}）`" />
                <button type="submit" :disabled="memberBusy || !memberName.trim()">创建成员</button>
              </form>
            </section>

            <section v-if="pendingGrantsForSelf.length" class="wg-side__sec">
              <h4>待接受 Grant</h4>
              <ul>
                <li v-for="g in pendingGrantsForSelf" :key="g.grant_id">
                  <div class="wg-side__row">
                    <span>{{ g.member_id.slice(0, 12) }}…</span>
                    <button type="button" @click="acceptGrant(g)">接受</button>
                  </div>
                </li>
              </ul>
            </section>

            <section class="wg-side__sec">
              <h4>信息型 HITL</h4>
              <ul>
                <li v-for="h in hitlList" :key="h.hitl_id">
                  <div class="wg-side__meta">{{ h.prompt }}</div>
                  <form class="wg-side__form" @submit.prevent="resolveHitl(h.hitl_id)">
                    <input v-model="hitlAnswers[h.hitl_id]" placeholder="回答" />
                    <button type="submit">提交</button>
                  </form>
                </li>
              </ul>
              <form class="wg-side__form" @submit.prevent="createHitl">
                <input v-model="hitlPrompt" placeholder="发起信息请求…" />
                <button type="submit" :disabled="hitlBusy || !hitlPrompt.trim()">发起</button>
              </form>
            </section>
          </aside>
        </div>

        <form class="wg-chat__composer" @submit.prevent="send">
          <input
            v-model="draft"
            class="wg-chat__input"
            type="text"
            placeholder="向工作组发言…"
            :disabled="sending"
          />
          <button type="submit" class="wg-chat__send" :disabled="sending || !draft.trim()">
            发送
          </button>
        </form>
      </template>
    </div>
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
.wg-chat__acl {
  color: var(--text-muted, #6b7280);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 55%;
}
.wg-chat__collab {
  display: flex;
  gap: 0.35rem;
}
.wg-chat__collab input,
.wg-side__form input {
  padding: 0.3rem 0.5rem;
  border: 1px solid #d1d5db;
  border-radius: 4px;
  font: inherit;
  min-width: 8rem;
}
.wg-chat__collab button,
.wg-side__form button,
.wg-side__row button {
  border: 1px solid #d1d5db;
  background: #fff;
  border-radius: 4px;
  padding: 0.3rem 0.6rem;
  cursor: pointer;
  font: inherit;
  font-size: 0.75rem;
}
.wg-chat__body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.wg-chat__timeline {
  flex: 1;
  overflow: auto;
  padding: 1rem 1.25rem;
}
.wg-side {
  width: 260px;
  border-left: 1px solid var(--border-subtle, #e5e7eb);
  overflow: auto;
  padding: 0.75rem;
  font-size: 0.8rem;
  background: var(--surface-muted, #fafafa);
}
.wg-side__sec {
  margin-bottom: 1rem;
}
.wg-side__sec h4 {
  margin: 0 0 0.4rem;
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #9ca3af;
}
.wg-side__sec ul {
  list-style: none;
  margin: 0 0 0.5rem;
  padding: 0;
}
.wg-side__sec li {
  margin-bottom: 0.45rem;
}
.wg-side__row {
  display: flex;
  justify-content: space-between;
  gap: 0.35rem;
  align-items: center;
}
.wg-side__meta {
  font-size: 0.7rem;
  color: #6b7280;
  margin-top: 0.1rem;
  word-break: break-all;
}
.wg-side__form {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.wg-chat__event {
  margin-bottom: 0.9rem;
}
.wg-chat__event-meta {
  display: flex;
  gap: 0.75rem;
  font-size: 0.75rem;
  color: var(--text-muted, #6b7280);
  margin-bottom: 0.2rem;
}
.wg-chat__event-text {
  white-space: pre-wrap;
  font-size: 0.9rem;
  line-height: 1.45;
}
.wg-chat__empty {
  color: var(--text-muted, #6b7280);
  font-size: 0.875rem;
}
.wg-chat__composer {
  display: flex;
  gap: 0.5rem;
  padding: 0.75rem 1rem;
  border-top: 1px solid var(--border-subtle, #e5e7eb);
}
.wg-chat__input {
  flex: 1;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--border-subtle, #d1d5db);
  border-radius: 6px;
  font: inherit;
}
.wg-chat__send {
  padding: 0.5rem 1rem;
  border: none;
  border-radius: 6px;
  background: var(--accent, #2563eb);
  color: #fff;
  cursor: pointer;
}
.wg-chat__send:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.wg-chat__link {
  margin-top: 0.75rem;
  border: none;
  background: none;
  color: var(--accent, #2563eb);
  cursor: pointer;
}
@media (max-width: 900px) {
  .wg-side {
    display: none;
  }
}
</style>
