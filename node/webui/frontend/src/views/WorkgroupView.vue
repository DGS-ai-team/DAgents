<script setup>
import { ref, watch, onUnmounted, computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import NavRail from "../components/NavRail.vue";

/** v1 Worker 可执行工具；其余名仅写入 Spec，provision 时与本机能力求交 */
const TOOL_CHOICES = ["read_file"];

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
const memberSoul = ref("");
const memberUser = ref("");
const memberCustom = ref("");
const memberLlmProfile = ref("default");
const memberLlmRevision = ref("1");
const memberTools = ref(["read_file"]);
const memberBusy = ref(false);
const showMemberForm = ref(false);

const selectedMemberId = ref("");
const memberSpec = ref(null);
const memberSpecBusy = ref(false);

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
    if (
      selectedMemberId.value &&
      !members.value.some((x) => x.member_id === selectedMemberId.value)
    ) {
      selectedMemberId.value = "";
      memberSpec.value = null;
    }
  } catch (e) {
    if (!error.value) error.value = e?.message || "";
  }
}

async function loadMemberSpec(memberId) {
  if (!workgroupId.value || !memberId) {
    memberSpec.value = null;
    return;
  }
  memberSpecBusy.value = true;
  try {
    memberSpec.value = await api.getWorkgroupMemberSpec(workgroupId.value, memberId);
  } catch (e) {
    memberSpec.value = null;
    if (!error.value) error.value = e?.message || "加载 MemberSpec 失败";
  } finally {
    memberSpecBusy.value = false;
  }
}

async function selectMember(memberId) {
  if (selectedMemberId.value === memberId) {
    selectedMemberId.value = "";
    memberSpec.value = null;
    if (workgroupId.value) {
      router.replace({ name: "workgroups", params: { workgroupId: workgroupId.value }, query: {} });
    }
    return;
  }
  selectedMemberId.value = memberId;
  await loadMemberSpec(memberId);
  if (workgroupId.value) {
    router.replace({
      name: "workgroups",
      params: { workgroupId: workgroupId.value },
      query: { member: memberId },
    });
  }
}

function startPoll() {
  stopPoll();
  pollTimer.value = window.setInterval(async () => {
    await Promise.all([loadTimeline(), loadMembersGrantsHitl()]);
    if (selectedMemberId.value) {
      await loadMemberSpec(selectedMemberId.value);
    }
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

function toggleTool(name) {
  const set = new Set(memberTools.value);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  memberTools.value = [...set];
}

function resetMemberForm() {
  memberName.value = "";
  memberHome.value = "";
  memberSoul.value = "";
  memberUser.value = "";
  memberCustom.value = "";
  memberLlmProfile.value = "default";
  memberLlmRevision.value = "1";
  memberTools.value = ["read_file"];
}

async function createMember() {
  const name = memberName.value.trim();
  const home = memberHome.value.trim() || selfNodeId.value;
  if (!name || !home || memberBusy.value) return;
  memberBusy.value = true;
  error.value = "";
  try {
    const tools = memberTools.value.length ? [...memberTools.value] : ["read_file"];
    const out = await api.createWorkgroupMember(workgroupId.value, {
      display_name: name,
      home_node_id: home,
      llm_profile_id: memberLlmProfile.value.trim() || "default",
      llm_profile_revision: memberLlmRevision.value.trim() || "1",
      allow_tool_names: tools,
      prompt: {
        soul_md: memberSoul.value,
        user_md: memberUser.value,
        custom_md: memberCustom.value,
      },
    });
    resetMemberForm();
    showMemberForm.value = false;
    await loadMembersGrantsHitl();
    const mid = out?.member?.member_id;
    if (mid) {
      selectedMemberId.value = mid;
      memberSpec.value = out.spec || null;
      if (!memberSpec.value) await loadMemberSpec(mid);
      router.replace({
        name: "workgroups",
        params: { workgroupId: workgroupId.value },
        query: { member: mid },
      });
    }
    panelRef.value?.refresh?.();
  } catch (e) {
    error.value = e?.message || "创建成员失败";
  } finally {
    memberBusy.value = false;
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

function toolsForGrant(memberId) {
  if (
    selectedMemberId.value === memberId &&
    memberSpec.value?.tools?.allow_names?.length
  ) {
    return [...memberSpec.value.tools.allow_names];
  }
  return ["read_file"];
}

async function inviteGrant(memberId) {
  error.value = "";
  try {
    await api.inviteWorkgroupGrant(workgroupId.value, memberId, toolsForGrant(memberId));
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
    if (grant.member_id === selectedMemberId.value) {
      await loadMemberSpec(grant.member_id);
    }
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

function promptExcerpt(text, max = 120) {
  const s = String(text || "").trim();
  if (!s) return "（空）";
  return s.length > max ? `${s.slice(0, max)}…` : s;
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

const selectedMember = computed(() =>
  members.value.find((m) => m.member_id === selectedMemberId.value) || null,
);

watch(
  workgroupId,
  async (id) => {
    stopPoll();
    selectedMemberId.value = "";
    memberSpec.value = null;
    showMemberForm.value = false;
    resetMemberForm();
    await Promise.all([loadTimeline(), loadACL(), loadMembersGrantsHitl()]);
    const memberQ = String(route.query.member || "").trim();
    if (memberQ) {
      selectedMemberId.value = memberQ;
      await loadMemberSpec(memberQ);
    }
    if (String(route.query.addMember || "") === "1") {
      showMemberForm.value = true;
    }
    if (id) startPoll();
  },
  { immediate: true },
);

watch(
  () => [route.query.member, route.query.addMember],
  async ([member, addMember]) => {
    if (!workgroupId.value) return;
    const memberQ = String(member || "").trim();
    if (memberQ && memberQ !== selectedMemberId.value) {
      selectedMemberId.value = memberQ;
      await loadMemberSpec(memberQ);
    }
    if (String(addMember || "") === "1") {
      showMemberForm.value = true;
    }
  },
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
      />
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
              <div class="wg-side__row">
                <h4>成员</h4>
                <button type="button" @click="showMemberForm = !showMemberForm">
                  {{ showMemberForm ? "收起" : "+ 资产" }}
                </button>
              </div>
              <ul>
                <li v-for="m in members" :key="m.member_id">
                  <div class="wg-side__row">
                    <button
                      type="button"
                      class="wg-side__name"
                      :class="{ 'wg-side__name--active': selectedMemberId === m.member_id }"
                      @click="selectMember(m.member_id)"
                    >
                      {{ m.display_name }} · {{ m.status }}
                    </button>
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

              <form
                v-if="showMemberForm"
                class="wg-side__form"
                @submit.prevent="createMember"
              >
                <label class="wg-side__label">显示名</label>
                <input v-model="memberName" placeholder="成员显示名" required />
                <label class="wg-side__label">Home Node</label>
                <input
                  v-model="memberHome"
                  :placeholder="`默认 ${selfNodeId || '本机'}`"
                />
                <label class="wg-side__label">Soul</label>
                <textarea v-model="memberSoul" rows="3" placeholder="soul.md 正文（可空）" />
                <label class="wg-side__label">User</label>
                <textarea v-model="memberUser" rows="2" placeholder="user.md 正文（可空）" />
                <label class="wg-side__label">Custom</label>
                <textarea v-model="memberCustom" rows="2" placeholder="custom.md 正文（可空）" />
                <label class="wg-side__label">工具白名单</label>
                <div class="wg-side__tools">
                  <label v-for="t in TOOL_CHOICES" :key="t" class="wg-side__check">
                    <input
                      type="checkbox"
                      :checked="memberTools.includes(t)"
                      @change="toggleTool(t)"
                    />
                    {{ t }}
                  </label>
                </div>
                <label class="wg-side__label">LLM 档案</label>
                <div class="wg-side__llm">
                  <input v-model="memberLlmProfile" placeholder="profile id" />
                  <input v-model="memberLlmRevision" placeholder="revision" />
                </div>
                <button type="submit" :disabled="memberBusy || !memberName.trim()">
                  创建成员资产
                </button>
              </form>
            </section>

            <section v-if="selectedMemberId" class="wg-side__sec">
              <h4>成员资产</h4>
              <p v-if="memberSpecBusy" class="wg-side__meta">加载 Spec…</p>
              <template v-else-if="memberSpec">
                <div class="wg-side__meta">
                  {{ selectedMember?.display_name || memberSpec.display_name }}
                </div>
                <div class="wg-side__spec">
                  <div><span>digest</span>{{ memberSpec.digest?.slice(0, 18) }}…</div>
                  <div>
                    <span>llm</span>{{ memberSpec.llm_profile_id }}@{{
                      memberSpec.llm_profile_revision
                    }}
                  </div>
                  <div>
                    <span>tools</span>{{ (memberSpec.tools?.allow_names || []).join(", ") || "—" }}
                  </div>
                  <div><span>soul</span>{{ promptExcerpt(memberSpec.prompt?.soul_md) }}</div>
                  <div><span>user</span>{{ promptExcerpt(memberSpec.prompt?.user_md) }}</div>
                  <div><span>custom</span>{{ promptExcerpt(memberSpec.prompt?.custom_md) }}</div>
                </div>
              </template>
              <p v-else class="wg-side__meta">无法加载 MemberSpec</p>
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
.wg-side__form input,
.wg-side__form textarea {
  padding: 0.3rem 0.5rem;
  border: 1px solid #d1d5db;
  border-radius: 4px;
  font: inherit;
  min-width: 8rem;
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
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
  width: 300px;
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
.wg-side__name {
  border: none;
  background: none;
  padding: 0;
  text-align: left;
  cursor: pointer;
  color: inherit;
  font: inherit;
}
.wg-side__name--active {
  color: var(--accent, #2563eb);
  font-weight: 600;
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
.wg-side__label {
  font-size: 0.7rem;
  color: #6b7280;
  margin-top: 0.15rem;
}
.wg-side__tools {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem 0.75rem;
}
.wg-side__check {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.75rem;
  cursor: pointer;
}
.wg-side__llm {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.35rem;
}
.wg-side__spec {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  margin-top: 0.35rem;
  font-size: 0.72rem;
  line-height: 1.35;
}
.wg-side__spec div span {
  display: inline-block;
  min-width: 3.2rem;
  color: #9ca3af;
  margin-right: 0.35rem;
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
