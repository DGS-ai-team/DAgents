<script setup>
import { ref, watch, onUnmounted, computed, onMounted, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import { isKnownWorkgroupRealtimeEvent } from "../sse/workgroupEvents.js";
import NavRail from "../components/NavRail.vue";
import WorkgroupMemberModal from "../components/WorkgroupMemberModal.vue";
import WorkgroupApprovalCard from "../components/WorkgroupApprovalCard.vue";
import WorkgroupUserInformationCard from "../components/WorkgroupUserInformationCard.vue";
import WorkgroupDebugPanel from "../components/WorkgroupDebugPanel.vue";
import WorkgroupToolRow from "../components/WorkgroupToolRow.vue";
import WorkgroupComposer from "../components/WorkgroupComposer.vue";
import BrandActivityIndicator from "../components/BrandActivityIndicator.vue";
import ScrollToTailButton from "../components/ScrollToTailButton.vue";
import { useWorkgroupTimeline } from "../composables/useWorkgroupTimeline.js";
import { renderMarkdown } from "../utils/markdown.js";
import { workgroupApprovalItemsFromMetadata } from "../utils/workgroupApproval.js";
import { workgroupMemberInformationRequests } from "../utils/workgroupUserInformation.js";
import { createFollowTailController, distanceFromTail } from "../utils/scrollTail.js";
import { createSerializedRefresh } from "../../../../../shared/frontend/serializedRefresh.js";
import brandIcon from "@dagents-brand/brand-icon.png";
import { markWorkgroupRead, noteWorkgroupTimeline } from "../stores/unread.js";

const route = useRoute();
const router = useRouter();
const panelRef = ref(null);

const workgroupId = computed(() => String(route.params.workgroupId || "").trim());
const events = ref([]);
const draft = ref("");
const sending = ref(false);
const cancelling = ref(false);
/** @type {import('vue').Ref<Array<Record<string, any>>>} */
const humanQueueItems = ref([]);
const editingQueueId = ref("");
const editQueueDraft = ref("");
const error = ref("");
const workgroupAccessError = ref("");
const notice = ref("");
const mentionOpen = ref(false);
const mentionQuery = ref("");
/** @type {import('vue').Ref<null | { member_id: string, display_name: string }>} */
const directMember = ref(null);
const workgroupMeta = ref(null);
const selfNodeId = ref("");
const selfNodeName = ref("");
const members = ref([]);
const llmConfigs = ref([]);
const publishing = ref(false);
const bindingLlm = ref(false);
const modelMenuOpen = ref(false);
const modelMenuRoot = ref(null);
const timelineEl = ref(null);
const showScrollToTail = ref(false);
/** 本轮发送开始前的 Timeline 水位 */
const statusWatermarkSeq = ref(0);
const scrollTail = createFollowTailController({ threshold: 80 });
let timelineResizeObserver = null;
/** 编排态：成员回报折叠展开态（key = assign_id / event_id） */
const expandedMemberReports = ref({});
/** 编排态：Supervisor 成员任务详情展开态（默认折叠） */
const expandedAssignTasks = ref({});

const memberModalOpen = ref(false);
const memberModalMode = ref("create");
const memberModalWgId = ref("");
const memberModalMemberId = ref("");
const memberModalMember = computed(() =>
  (members.value || []).find(
    (member) => String(member?.member_id || "").trim() === memberModalMemberId.value,
  ) || null,
);

/** 流式：乐观用户气泡 / Supervisor 打字机 / 相位 */
const liveUser = ref(null);
const liveAssistant = ref(null);
const remoteSending = ref(false);
const remoteClientMessageId = ref("");
const localClientMessageId = ref("");
const workgroupRealtimeStatus = ref("disconnected");
const streamPhase = ref(""); // thinking | tool | streaming
const streamToolName = ref("");
const streamMode = ref(""); // leader | direct | member
const streamActorId = ref("");
/** @type {AbortController | null} */
let streamAbort = null;
/** @type {import('vue').Ref<any[]>} */
const pendingHitl = ref([]);
const hitlBusy = ref(false);
const hitlDraft = ref("");
const pendingHitlRefresh = createSerializedRefresh(
  () => api.listWorkgroupHITL(workgroupId.value, true),
  (res) => {
    const list = Array.isArray(res) ? res : res?.hitl || [];
    pendingHitl.value = Array.isArray(list) ? list : [];
  },
);

const debugOpen = ref(false);

function friendlyWorkgroupError(error, fallback = "操作失败") {
  const raw = String(error?.message || error || "").trim();
  if (/node not in workgroup ACL|not_authorized/i.test(raw)) {
    return "发送失败：当前 Node 未加入该工作组的访问控制列表，请在 Manage 的工作组 ACL 中将本机 Node 添加为协作者后重试。";
  }
  if (/workgroup is not subscribed|not_subscribed/i.test(raw)) {
    return "发送失败：当前 Node 尚未订阅该工作组，请先在 Manage 中完成订阅后重试。";
  }
  return raw || fallback;
}

function setWorkgroupError(source, fallback = "操作失败") {
  const message = friendlyWorkgroupError(source, fallback);
  if (/node not in workgroup ACL|not_authorized/i.test(String(source?.message || source || ""))) {
    workgroupAccessError.value = message;
  }
  error.value = message;
}

let timelineReqSeq = 0;
let workgroupEventSource = null;
let workgroupEventSeq = 0;
const cancelledRealtimeMessageIds = new Set();

const hasPendingInteraction = computed(() => (pendingHitl.value || []).length > 0);
const memberApprovalByAssign = computed(() => {
  const out = {};
  for (const hitl of pendingHitl.value || []) {
    const metadata = hitl?.metadata && typeof hitl.metadata === "object" ? hitl.metadata : {};
    if (String(metadata.source || "").trim() !== "agent_ref") continue;
    const assignId = String(metadata.assign_id || "").trim();
    const items = workgroupApprovalItemsFromMetadata(metadata);
    if (!assignId || items.length === 0) continue;
    // A reconnect or a slow resolve can briefly expose more than one pending
    // projection for the same assignment.  The newest request is the only
    // actionable approval; rendering all of them makes each later tool show
    // every earlier approval again.
    const current = out[assignId];
    const currentAt = String(current?.created_at || "");
    const nextAt = String(hitl?.created_at || "");
    if (!current || nextAt > currentAt || (nextAt === currentAt && String(hitl?.hitl_id || "") > String(current?.hitl_id || ""))) {
      out[assignId] = hitl;
    }
  }
  return out;
});
const supervisorHitl = computed(() => {
  return (pendingHitl.value || []).find((hitl) => {
    const metadata = hitl?.metadata && typeof hitl.metadata === "object" ? hitl.metadata : {};
    const source = String(metadata.source || hitl?.source || "").trim();
    const assignId = String(metadata.assign_id || hitl?.assign_id || "").trim();
    const hasMemberApprovalItems = workgroupApprovalItemsFromMetadata(metadata).length > 0;
    return source !== "agent_ref" && !(assignId && hasMemberApprovalItems);
  }) || null;
});
const hitlMode = computed(() => Boolean(supervisorHitl.value));
const memberInformationRequests = computed(() =>
  workgroupMemberInformationRequests(pendingHitl.value, memberNameById.value),
);
// Assigned member content is already represented by the task card. Keep the
// live bubble for Supervisor and direct @member turns, where it is the actual
// conversational reply.
const showLiveAssistant = computed(
  () =>
    Boolean(liveAssistant.value) &&
    Boolean(String(liveAssistant.value?.text || "").trim()) &&
    streamMode.value !== "member",
);
function toggleDebugPanel() {
  debugOpen.value = !debugOpen.value;
}

async function loadPendingHitl() {
  if (!workgroupId.value) {
    pendingHitl.value = [];
    return;
  }
  await pendingHitlRefresh.refresh();
}

async function submitHitlAnswer() {
  const hitl = supervisorHitl.value;
  const answer = hitlDraft.value.trim();
  if (!hitl || !workgroupId.value || !answer || hitlBusy.value) return;
  hitlBusy.value = true;
  error.value = "";
  try {
    await api.resolveWorkgroupHITL(workgroupId.value, hitl.hitl_id, { answer });
    hitlDraft.value = "";
    await loadPendingHitl();
    await loadTimeline();
  } catch (e) {
    if (/already_resolved|409/.test(String(e?.message || ""))) {
      await loadPendingHitl();
    } else {
      error.value = e?.message || "提交回答失败";
    }
  } finally {
    hitlBusy.value = false;
  }
}

async function resolveMemberInformation(request, resolution) {
  const hitlId = String(request?.hitlId || "").trim();
  if (!hitlId || !workgroupId.value || hitlBusy.value) return;
  hitlBusy.value = true;
  error.value = "";
  try {
    await api.resolveWorkgroupHITL(
      workgroupId.value,
      hitlId,
      { type: "user_information", tool_call_id: request.callId, ...resolution },
    );
    await loadPendingHitl();
    await loadTimeline();
  } catch (e) {
    if (/already_resolved|409/.test(String(e?.message || ""))) {
      await loadPendingHitl();
    } else {
      error.value = e?.message || "提交成员回答失败";
    }
  } finally {
    hitlBusy.value = false;
  }
}

async function resolveMemberApproval(approval, callId, approve) {
  const hitlId = String(approval?.hitlId || "").trim();
  if (!hitlId || !workgroupId.value || hitlBusy.value) return;
  const allIds = (approval?.allItems || approval?.items || [])
    .map((item) => String(item?.callId || "").trim())
    .filter(Boolean);
  const selectedId = String(callId || "").trim();
  const approved = selectedId
    ? approve
      ? [selectedId]
      : []
    : approve
      ? allIds
      : [];
  const rejected = selectedId
    ? approve
      ? allIds.filter((id) => id !== selectedId)
      : allIds
    : approve
      ? []
      : allIds;
  hitlBusy.value = true;
  error.value = "";
  try {
    await api.resolveWorkgroupHITL(
      workgroupId.value,
      hitlId,
      { type: "selection", approved, rejected },
    );
    await loadPendingHitl();
    await loadTimeline();
  } catch (e) {
    if (/already_resolved|409/.test(String(e?.message || ""))) {
      await loadPendingHitl();
    } else {
      error.value = e?.message || "提交审批结果失败";
    }
  } finally {
    hitlBusy.value = false;
  }
}

function newClientMessageId() {
  if (typeof crypto !== "undefined" && crypto.randomUUID) return crypto.randomUUID();
  return `cm_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`;
}

function clearLive() {
  liveUser.value = null;
  liveAssistant.value = null;
  remoteSending.value = false;
  remoteClientMessageId.value = "";
  localClientMessageId.value = "";
  streamPhase.value = "";
  streamToolName.value = "";
  streamMode.value = "";
  streamActorId.value = "";
  statusWatermarkSeq.value = 0;
}

function rememberCancelledMessageIds(...ids) {
  for (const id of ids) {
    const value = String(id || "").trim();
    if (!value) continue;
    cancelledRealtimeMessageIds.add(value);
  }
  while (cancelledRealtimeMessageIds.size > 64) {
    const oldest = cancelledRealtimeMessageIds.values().next().value;
    if (!oldest) break;
    cancelledRealtimeMessageIds.delete(oldest);
  }
}

const memberNameById = computed(() => {
  const map = {};
  for (const m of members.value || []) {
    const id = String(m?.member_id || "").trim();
    if (!id) continue;
    map[id] = String(m?.display_name || "").trim() || id;
  }
  return map;
});

async function loadSelf() {
  try {
    const boot = await api.getUIBootstrap();
    selfNodeId.value = String(
      boot?.info?.node_id || boot?.health?.node_id || boot?.info?.NodeID || "",
    ).trim();
    selfNodeName.value = String(boot?.agent?.name || "").trim();
  } catch {
    try {
      const info = await api.getAgentInfo();
      selfNodeId.value = info.node_id || info.NodeID || "";
      selfNodeName.value = String(info.name || info.Name || "").trim();
    } catch {
      selfNodeId.value = "";
      selfNodeName.value = "";
    }
  }
}

async function loadMembers() {
  if (!workgroupId.value) {
    members.value = [];
    return;
  }
  try {
    const res = await api.listWorkgroupMembers(workgroupId.value);
    members.value = Array.isArray(res) ? res : res?.members || [];
  } catch {
    members.value = [];
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
    events.value = mergeTimelineEvents(res.events || []);
    const latestSeq = events.value.reduce(
      (max, event) => Math.max(max, Number(event?.seq || 0)),
      0,
    );
    noteWorkgroupTimeline(workgroupId.value, latestSeq);
    markWorkgroupRead(workgroupId.value, latestSeq);
    if (!workgroupAccessError.value) error.value = "";
  } catch (e) {
    if (reqSeq !== timelineReqSeq) return;
    error.value = e?.message || "加载 Timeline 失败";
  }
}

function mergeTimelineEvents(incoming) {
  const current = events.value || [];
  const incomingList = Array.isArray(incoming) ? incoming : [];
  if (incomingList.length === 1 && current.length) {
    const next = incomingList[0];
    const last = current[current.length - 1];
    if (Number(next?.seq || 0) > Number(last?.seq || 0)) {
      return [...current, next];
    }
  }
  const map = new Map();
  for (const event of current) {
    if (String(event?.workgroup_id || "").trim() !== workgroupId.value) continue;
    const key = String(event?.event_id || event?.seq || "").trim();
    if (key) map.set(key, event);
  }
  for (const event of Array.isArray(incoming) ? incoming : []) {
    const key = String(event?.event_id || event?.seq || "").trim();
    if (key) map.set(key, event);
  }
  return [...map.values()].sort((a, b) => Number(a?.seq || 0) - Number(b?.seq || 0));
}

function applyTimelineEvent(event) {
  if (!event || String(event?.workgroup_id || "").trim() !== workgroupId.value) return;
  events.value = mergeTimelineEvents([event]);
  const seq = Number(event?.seq || 0);
  noteWorkgroupTimeline(workgroupId.value, seq);
  markWorkgroupRead(workgroupId.value, seq);
  if (event.type === "assign_started" || event.type === "assign_finished") {
    // Member occupancy is durable state owned by Manage. Reconcile it from
    // the authoritative assignment event instead of leaving the sidebar in
    // `busy` until a full page reload.
    void loadMembers();
  }
  if (
    !sending.value &&
    remoteSending.value &&
    String(event?.type || "") === "actor_final_text"
  ) {
    clearLive();
  }
  void nextTick().then(maybeScrollTimelineTail);
}

function applyRemoteRealtime(payload) {
  if (!payload || String(payload.workgroup_id || "").trim() !== workgroupId.value) return;
  const clientMessageId = String(payload.client_message_id || "").trim();
  const liveMessageId =
    clientMessageId || remoteClientMessageId.value || `remote-${workgroupId.value}`;
  const eventType = String(payload.event_type || "");
  const data = payload.data && typeof payload.data === "object" ? payload.data : {};
  if (cancelledRealtimeMessageIds.has(liveMessageId)) return;
  // The originating Node already receives leader/direct frames through POST
  // streaming. Member frames are emitted from the nested member run, so keep
  // those SSE frames to make the local view identical to other subscribers.
  if (
    clientMessageId &&
    clientMessageId === localClientMessageId.value &&
    String(data.mode || "") !== "member"
  ) {
    return;
  }
  const isNewRemoteTurn = remoteClientMessageId.value !== liveMessageId;
  if (eventType === "queued" || eventType === "queue") {
    applyQueuePayload(data);
    return;
  }
  if (eventType === "status") {
    remoteSending.value = true;
    remoteClientMessageId.value = liveMessageId;
    if (isNewRemoteTurn) {
      liveAssistant.value = null;
      statusWatermarkSeq.value = (events.value || []).reduce(
        (max, event) => Math.max(max, Number(event?.seq || 0)),
        0,
      );
    }
    streamPhase.value = String(data.phase || "thinking");
    streamToolName.value = String(data.purpose || "");
    streamMode.value = String(data.mode || "leader");
    streamActorId.value = String(
      data.member_id || (streamMode.value === "leader" ? "leader" : streamActorId.value),
    );
  } else if (eventType === "delta") {
    remoteSending.value = true;
    remoteClientMessageId.value = liveMessageId;
    if (isNewRemoteTurn) liveAssistant.value = null;
    streamPhase.value = "streaming";
    streamMode.value = String(data.mode || streamMode.value || "leader");
    streamActorId.value = String(
      data.member_id || (streamMode.value === "leader" ? "leader" : streamActorId.value),
    );
    liveAssistant.value = liveAssistant.value || {
      id: `live-asst-${liveMessageId}`,
      text: "",
    };
    liveAssistant.value = {
      ...liveAssistant.value,
      text: `${liveAssistant.value.text || ""}${String(data.text || "")}`,
    };
  } else if (eventType === "assistant_final") {
    if (data.timeline_event?.event_id) {
      const already = (events.value || []).some(
        (event) => event?.event_id === data.timeline_event.event_id,
      );
      if (already) return;
    }
    remoteSending.value = true;
    remoteClientMessageId.value = liveMessageId;
    streamPhase.value = "streaming";
    streamMode.value = String(data.mode || "leader");
    streamActorId.value = String(
      data.member_id || (streamMode.value === "leader" ? "leader" : streamActorId.value),
    );
    liveAssistant.value = {
      id: `live-asst-${liveMessageId}`,
      text: String(data.text || ""),
    };
  } else if (eventType === "final") {
    remoteSending.value = false;
    remoteClientMessageId.value = "";
    liveAssistant.value = null;
    statusWatermarkSeq.value = 0;
    streamPhase.value = "";
    streamToolName.value = "";
    streamActorId.value = "";
  } else if (!isKnownWorkgroupRealtimeEvent(eventType)) {
    // Unknown realtime frames must not disappear silently. They are
    // ephemeral by design, so reconcile from durable Timeline/queue state.
    void loadTimeline().catch(() => {});
    void loadPendingHitl();
    void refreshHumanQueue();
  }
  void nextTick().then(maybeScrollTimelineTail);
}

function handleWorkgroupEventMessage(raw) {
  try {
    const message = JSON.parse(raw?.data || "{}");
    const seq = Number(message?.seq || 0);
    if (seq > workgroupEventSeq) workgroupEventSeq = seq;
    const payload = message?.data || {};
    if (message?.type === "workgroup.timeline" || payload.kind === "timeline") {
      applyTimelineEvent(payload.event);
    } else if (message?.type === "workgroup.realtime") {
      applyRemoteRealtime(payload);
    }
  } catch {
    /* The next reconnect replays durable events; malformed frames are ignored. */
  }
}

function stopWorkgroupEventStream() {
  if (workgroupEventSource) {
    workgroupEventSource.close();
    workgroupEventSource = null;
  }
  workgroupRealtimeStatus.value = "disconnected";
}

function startWorkgroupEventStream() {
  stopWorkgroupEventStream();
  if (!workgroupId.value || typeof EventSource === "undefined") return;
  workgroupRealtimeStatus.value = "connecting";
  const url = api.getWorkgroupEventsURL(workgroupId.value, workgroupEventSeq);
  const source = new EventSource(url);
  workgroupEventSource = source;
  source.onopen = () => {
    if (workgroupEventSource === source) workgroupRealtimeStatus.value = "connected";
  };
  source.addEventListener("workgroup.timeline", handleWorkgroupEventMessage);
  source.addEventListener("workgroup.realtime", handleWorkgroupEventMessage);
  source.onerror = () => {
    if (workgroupEventSource !== source) return;
    workgroupRealtimeStatus.value = "disconnected";
    source.close();
    void loadTimeline().catch(() => {});
    void refreshHumanQueue();
    window.setTimeout(() => {
      if (workgroupEventSource === source && workgroupId.value) startWorkgroupEventStream();
    }, 1200);
  };
}

function onTimelineScroll() {
  scrollTail.onScroll(timelineEl.value);
  updateScrollToTailVisibility();
}

function updateScrollToTailVisibility() {
  const el = timelineEl.value;
  showScrollToTail.value = Boolean(el && !scrollTail.follow && distanceFromTail(el) > 48);
}

function maybeScrollTimelineTail() {
  nextTick(() => {
    scrollTail.pinIfFollowing(timelineEl.value);
    updateScrollToTailVisibility();
  });
}

function scrollTimelineTail() {
  renderWindowStart.value = Math.max(0, eventGroups.value.length - MAX_RENDERED_GROUPS);
  nextTick(() => {
    scrollTail.forcePin(timelineEl.value);
    updateScrollToTailVisibility();
  });
}

function bindTimelineResizeObserver() {
  const el = timelineEl.value;
  if (!el || typeof ResizeObserver === "undefined") return;
  timelineResizeObserver?.disconnect();
  timelineResizeObserver = new ResizeObserver(() => {
    if (scrollTail.follow) scrollTail.pinIfFollowing(el);
    updateScrollToTailVisibility();
  });
  const inner = el.firstElementChild || el;
  timelineResizeObserver.observe(inner);
  timelineResizeObserver.observe(el);
}

async function loadWorkgroupMeta() {
  if (!workgroupId.value) {
    workgroupMeta.value = null;
    llmConfigs.value = [];
    return;
  }
  try {
    workgroupMeta.value = await api.getWorkgroup(workgroupId.value);
  } catch {
    workgroupMeta.value = null;
  }
  try {
    const res = await api.listWorkgroupLLMConfigs(workgroupId.value);
    llmConfigs.value = Array.isArray(res?.configs) ? res.configs : Array.isArray(res) ? res : [];
  } catch {
    llmConfigs.value = [];
  }
}

const canChat = computed(() => String(workgroupMeta.value?.status || "") === "active");
const isConfiguring = computed(() => String(workgroupMeta.value?.status || "") === "configuring");

async function publishCurrent() {
  if (!workgroupId.value || publishing.value || !isConfiguring.value) return;
  publishing.value = true;
  error.value = "";
  notice.value = "";
  try {
    workgroupMeta.value = await api.publishWorkgroup(workgroupId.value);
    panelRef.value?.refreshWorkgroups?.({ force: true });
  } catch (e) {
    error.value = e?.message || "发布失败";
  } finally {
    publishing.value = false;
  }
}

const selectedSupervisorLabel = computed(() => {
  const id = String(workgroupMeta.value?.llm_profile_id || "").trim();
  if (!id) return "选择模型";
  const cfg = (llmConfigs.value || []).find((c) => String(c.id) === id);
  if (!cfg) return id;
  const name = String(cfg.name || cfg.id || "").trim() || id;
  return cfg.is_default ? `${name}（默认）` : name;
});

const canSwitchSupervisorModel = computed(
  () => !bindingLlm.value && !!workgroupMeta.value && (llmConfigs.value || []).length > 0,
);

function closeModelMenu() {
  modelMenuOpen.value = false;
}

function toggleModelMenu() {
  if (!canSwitchSupervisorModel.value) return;
  modelMenuOpen.value = !modelMenuOpen.value;
}

async function bindSupervisorLlm(cfgId) {
  const id = String(cfgId || "").trim();
  if (!workgroupId.value || !id || bindingLlm.value) return;
  if (String(workgroupMeta.value?.llm_profile_id || "") === id) return;
  bindingLlm.value = true;
  error.value = "";
  closeModelMenu();
  try {
    workgroupMeta.value = await api.patchWorkgroup(workgroupId.value, {
      llm_profile_id: id,
      llm_profile_revision: "1",
    });
  } catch (e) {
    error.value = e?.message || "绑定模型失败";
  } finally {
    bindingLlm.value = false;
  }
}

function pickSupervisorModel(cfgId) {
  void bindSupervisorLlm(cfgId);
}

function onModelMenuPointerDown(event) {
  if (!modelMenuOpen.value) return;
  const root = modelMenuRoot.value;
  if (root && !root.contains(event.target)) closeModelMenu();
}

function onModelMenuKeydown(event) {
  if (!modelMenuOpen.value) return;
  if (event.key === "Escape") {
    event.preventDefault();
    closeModelMenu();
  }
}

async function refreshHumanQueue() {
  if (!workgroupId.value) {
    humanQueueItems.value = [];
    return;
  }
  try {
    const out = await api.fetchWorkgroupHumanQueue(workgroupId.value);
    humanQueueItems.value = Array.isArray(out?.items) ? out.items : [];
  } catch {
    /* ignore */
  }
}

function applyQueuePayload(data) {
  const q = data?.queue;
  if (q && Array.isArray(q.items)) {
    humanQueueItems.value = q.items;
    return;
  }
  if (data?.queue_id) {
    const rest = (humanQueueItems.value || []).filter((x) => x.queue_id !== data.queue_id);
    humanQueueItems.value = [...rest, data].sort(
      (a, b) => Number(a.position || 0) - Number(b.position || 0),
    );
  }
}

function beginEditQueued(item) {
  editingQueueId.value = String(item?.queue_id || "");
  editQueueDraft.value = String(item?.text || "");
}

function cancelEditQueued() {
  editingQueueId.value = "";
  editQueueDraft.value = "";
}

async function saveQueuedEdit(item) {
  const qid = String(item?.queue_id || "").trim();
  const text = editQueueDraft.value.trim();
  if (!workgroupId.value || !qid || !text) return;
  try {
    const out = await api.patchWorkgroupHumanQueueItem(workgroupId.value, qid, text);
    applyQueuePayload(out);
    await refreshHumanQueue();
    cancelEditQueued();
  } catch (e) {
    error.value = e?.message || "修改排队消息失败";
  }
}

async function removeQueued(item) {
  const qid = String(item?.queue_id || "").trim();
  if (!workgroupId.value || !qid) return;
  try {
    await api.cancelWorkgroupHumanQueueItem(workgroupId.value, qid);
    await refreshHumanQueue();
    if (editingQueueId.value === qid) cancelEditQueued();
  } catch (e) {
    error.value = e?.message || "取消排队失败";
  }
}

async function loadWorkgroupAccess() {
  workgroupAccessError.value = "";
  if (!workgroupId.value || !selfNodeId.value) {
    return;
  }
  try {
    const acl = await api.getWorkgroupACL(workgroupId.value);
    const owners = Array.isArray(acl?.owners) ? acl.owners : [];
    const collaborators = Array.isArray(acl?.collaborators) ? acl.collaborators : [];
    if (
      String(workgroupMeta.value?.status || "") === "active" &&
      !owners.includes(selfNodeId.value) &&
      !collaborators.includes(selfNodeId.value)
    ) {
      workgroupAccessError.value = friendlyWorkgroupError(
        new Error("not_authorized: node not in workgroup ACL"),
      );
      error.value = workgroupAccessError.value;
    }
  } catch {
    // Keep the composer usable when an ACL read is temporarily unavailable;
    // the POST path remains authoritative and returns a normalized error.
  }
}

async function sendQueuedNow(item) {
  const qid = String(item?.queue_id || "").trim();
  if (!workgroupId.value || !qid) return;
  try {
    await api.sendWorkgroupHumanQueueItemNow(workgroupId.value, qid);
    await refreshHumanQueue();
  } catch (e) {
    setWorkgroupError(e, "立即发送失败");
  }
}

async function send() {
  let text = draft.value.trim();
  if (!text || !workgroupId.value) return;
  if (!canChat.value) {
    error.value = isConfiguring.value
      ? "工作组尚未发布，请先完成配置并点击「发布」。"
      : "当前工作组状态不允许对话。";
    return;
  }
  let directId = "";
  if (directMember.value) {
    directId = directMember.value.member_id;
    const token = `@${directMember.value.display_name}`;
    if (!text.includes(token)) {
      text = `${token} ${text}`;
    }
  }

  const clientMessageId = newClientMessageId();
  localClientMessageId.value = clientMessageId;
  const enqueueOnly = sending.value || remoteSending.value;
  error.value = "";
  draft.value = "";
  const sentDirect = directMember.value;
  directMember.value = null;

  if (enqueueOnly) {
    try {
      await api.postWorkgroupMessageStream(
        workgroupId.value,
        { text, clientMessageId, directMemberId: directId || undefined },
        {
          onEvent: (eventName, data) => {
            if (eventName === "queued") applyQueuePayload(data);
          },
        },
      );
      await refreshHumanQueue();
    } catch (e) {
      setWorkgroupError(e, "入队失败");
      draft.value = stripLeadingMention(text, sentDirect?.display_name);
      directMember.value = sentDirect;
    }
    localClientMessageId.value = "";
    return;
  }

  sending.value = true;
  cancelling.value = false;
  statusWatermarkSeq.value = (events.value || []).reduce(
    (m, ev) => Math.max(m, Number(ev?.seq || 0)),
    0,
  );

  liveUser.value = { id: `live-user-${clientMessageId}`, text, directMemberId: directId };
  liveAssistant.value = { id: `live-asst-${clientMessageId}`, text: "" };
  streamPhase.value = "thinking";
  streamToolName.value = "";
  streamMode.value = directId ? "direct" : "leader";
  streamActorId.value = directId || "leader";
  streamAbort = new AbortController();
  scrollTimelineTail();

  try {
    await api.postWorkgroupMessageStream(
      workgroupId.value,
      {
        text,
        clientMessageId,
        directMemberId: directId || undefined,
      },
      {
        signal: streamAbort.signal,
        onEvent: async (eventName, data) => {
          if (cancelledRealtimeMessageIds.has(clientMessageId)) return;
          if (eventName === "queued") {
            applyQueuePayload(data);
            clearLive();
            return;
          }
          if (eventName === "status") {
            const phase = String(data?.phase || "thinking");
            streamPhase.value =
              phase === "tool" ? "tool" : phase === "streaming" ? "streaming" : "thinking";
            streamToolName.value = String(data?.purpose || "");
            if (data?.mode) streamMode.value = String(data.mode);
            if (data?.member_id) streamActorId.value = String(data.member_id);
            else if (streamMode.value === "leader") streamActorId.value = "leader";
            if (phase === "tool") {
              await loadTimeline().catch(() => {});
              await loadPendingHitl();
            }
          } else if (eventName === "delta") {
            const piece = String(data?.text || "");
            if (!piece || !liveAssistant.value) return;
            streamPhase.value = "streaming";
            if (data?.mode) streamMode.value = String(data.mode);
            if (data?.member_id) streamActorId.value = String(data.member_id);
            else if (streamMode.value === "leader") streamActorId.value = "leader";
            liveAssistant.value = {
              ...liveAssistant.value,
              text: `${liveAssistant.value.text || ""}${piece}`,
            };
          } else if (eventName === "assistant_final") {
            const finalText = String(data?.text || "").trim();
            if (data?.mode) streamMode.value = String(data.mode);
            if (data?.member_id) streamActorId.value = String(data.member_id);
            if (liveAssistant.value && finalText) {
              liveAssistant.value = { ...liveAssistant.value, text: finalText };
            }
            streamPhase.value = "streaming";
          }
          await nextTick();
          maybeScrollTimelineTail();
        },
      },
    );
    clearLive();
    await loadTimeline();
    await loadPendingHitl();
    await refreshHumanQueue();
    scrollTimelineTail();
  } catch (e) {
    const aborted = e?.name === "AbortError" || /abort/i.test(String(e?.message || ""));
    clearLive();
    if (!aborted) {
      setWorkgroupError(e, "发送失败");
      draft.value = stripLeadingMention(text, sentDirect?.display_name);
      directMember.value = sentDirect;
    }
    await loadTimeline().catch(() => {});
    await loadPendingHitl();
  } finally {
    streamAbort = null;
    sending.value = false;
    cancelling.value = false;
    statusWatermarkSeq.value = 0;
    void refreshHumanQueue();
  }
}

function stripLeadingMention(text, displayName) {
  const name = String(displayName || "").trim();
  let out = String(text || "").trim();
  if (!name) return out;
  const token = `@${name}`;
  if (out === token) return "";
  if (out.startsWith(`${token} `)) return out.slice(token.length + 1).trimStart();
  return out.replace(/(^|[\s])@([^\s@]*)$/, "$1").trim();
}

function clearDirectMention() {
  directMember.value = null;
}

function onComposerBackspace(e) {
  if (!directMember.value) return;
  const el = e?.target;
  const start = el?.selectionStart ?? 0;
  const end = el?.selectionEnd ?? 0;
  if (start === 0 && end === 0 && !String(draft.value || "")) {
    e.preventDefault();
    clearDirectMention();
  }
}

function pickMention(member) {
  const name = String(member?.display_name || "").trim();
  const mid = String(member?.member_id || "").trim();
  if (!name || !mid) return;
  const val = draft.value;
  // 选中后从输入框移除 @query，改由芯片展示
  const replaced = val.replace(/(^|[\s])@([^\s@]*)$/, "$1");
  draft.value = replaced.replace(/[ \t]+$/g, " ").trimStart();
  directMember.value = { member_id: mid, display_name: name };
  mentionOpen.value = false;
  mentionQuery.value = "";
}

async function cancelTurn() {
  // A pending HITL may be restored after a page reload, when `sending` is
  // false even though the workgroup turn is still awaiting a decision.
  if (!workgroupId.value || (!sending.value && !hasPendingInteraction.value) || cancelling.value) return;
  cancelling.value = true;
  try {
    rememberCancelledMessageIds(localClientMessageId.value, remoteClientMessageId.value);
    await api.cancelWorkgroupTurn(workgroupId.value);
    if (streamAbort) {
      try {
        streamAbort.abort();
      } catch {
        /* ignore */
      }
    }
    sending.value = false;
    clearLive();
    await Promise.all([
      loadTimeline(),
      loadPendingHitl(),
      loadMembers(),
    ]);
  } catch (e) {
    error.value = e?.message || "取消失败";
  } finally {
    cancelling.value = false;
  }
}

const mentionCandidates = computed(() => {
  const q = mentionQuery.value.trim().toLowerCase();
  const list = (members.value || []).filter((m) => String(m?.status || "") === "ready");
  if (!q) return list.slice(0, 8);
  return list
    .filter((m) => {
      const name = String(m?.display_name || "").toLowerCase();
      const id = String(m?.member_id || "").toLowerCase();
      return name.includes(q) || id.includes(q);
    })
    .slice(0, 8);
});

function onDraftInput(e) {
  const val = String(e?.target?.value ?? draft.value);
  draft.value = val;
  const cursor = e?.target?.selectionStart ?? val.length;
  const before = val.slice(0, cursor);
  const m = before.match(/(^|[\s])@([^\s@]*)$/);
  if (m) {
    mentionOpen.value = true;
    mentionQuery.value = m[2] || "";
  } else {
    mentionOpen.value = false;
    mentionQuery.value = "";
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
  delete q.member;
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
  await loadMembers();
}

const {
  eventGroups,
  isHumanEvent,
  splitUserMentionParts,
  isMemberReportExpanded,
  toggleMemberReport,
  isAssignTaskExpanded,
  toggleAssignTask,
  taskDetailsId,
  parseNoticeTool,
  isDirectAssignEvent,
} = useWorkgroupTimeline({
  events,
  memberNameById,
  memberApprovalByAssign,
  selfNodeId,
  selfNodeName,
  liveUser,
  showLiveAssistant,
  liveAssistant,
  sending,
  streamMode,
  streamActorId,
  streamPhase,
  streamToolName,
  statusWatermarkSeq,
  expandedMemberReports,
  expandedAssignTasks,
});

const MAX_RENDERED_GROUPS = 180;
const renderWindowStart = ref(0);
const hasEarlierMessages = computed(
  () => eventGroups.value.length > MAX_RENDERED_GROUPS && renderWindowStart.value > 0,
);
const earlierMessageCount = computed(() => Math.max(0, renderWindowStart.value));
const renderedEventGroups = computed(() => {
  const groups = eventGroups.value;
  if (groups.length <= MAX_RENDERED_GROUPS) return groups;
  const start = Math.min(
    Math.max(0, renderWindowStart.value),
    Math.max(0, groups.length - MAX_RENDERED_GROUPS),
  );
  return groups.slice(start, start + MAX_RENDERED_GROUPS);
});

function loadEarlierMessages() {
  const el = timelineEl.value;
  const beforeHeight = el?.scrollHeight || 0;
  const beforeTop = el?.scrollTop || 0;
  renderWindowStart.value = Math.max(0, renderWindowStart.value - MAX_RENDERED_GROUPS);
  nextTick(() => {
    if (el) el.scrollTop = beforeTop + Math.max(0, el.scrollHeight - beforeHeight);
  });
}

const liveStatusLabel = computed(() => {
  if (!sending.value && !remoteSending.value) return "";
  const actorId = streamActorId.value || (streamMode.value === "leader" ? "leader" : "");
  const actorLabel =
    actorId === "leader" ? "Supervisor" : memberNameById.value[actorId] || actorId || "成员";
  if (streamPhase.value === "streaming" && liveAssistant.value?.text) {
    return `${actorLabel} 回复中…`;
  }
  if (streamPhase.value === "tool") {
    const tool = String(streamToolName.value || "").trim();
    if (tool === "分派成员任务") return "成员执行任务…";
    if (tool === "询问用户") return "等待你的回答…";
    if (tool.startsWith("直达") || streamMode.value === "direct") return "直连成员工作中…";
    return tool ? `${tool}…` : "工具执行中…";
  }
  if (hitlMode.value) return "Supervisor 正在询问…";
  const watermark = statusWatermarkSeq.value || 0;
  const list = events.value || [];
  let sawDirect = streamMode.value === "direct";
  for (let i = list.length - 1; i >= 0; i -= 1) {
    const ev = list[i];
    const seq = Number(ev?.seq || 0);
    if (seq && seq <= watermark) break;
    const t = String(ev?.type || "");
    if (t === "assign_started" && isDirectAssignEvent(ev)) sawDirect = true;
    if (t === "system_notice") {
      return parseNoticeTool(ev?.text).summary || "成员工作中…";
    }
    if (t === "assign_started") {
      const txt = String(ev?.text || "").trim();
      if (isDirectAssignEvent(ev)) return txt || "直连执行中…";
      return "成员执行任务…";
    }
    if (t === "assign_finished") {
      const txt = String(ev?.text || "").trim();
      if (txt && txt !== "已完成") return txt;
      return sawDirect || isDirectAssignEvent(ev) ? "成员已完成" : "成员已完成，Supervisor 汇总中…";
    }
    if (t === "actor_final_text" && String(ev?.actor_id || "") !== "leader") {
      return sawDirect ? "成员已完成" : "Supervisor 汇总中…";
    }
    if (t === "human_message") break;
  }
  if (streamMode.value === "direct" || sawDirect) return "直连成员工作中…";
  return "思考中…";
});

const composerRuntimeLabel = computed(() => {
  if (hitlMode.value) {
    return hitlBusy.value ? "提交回答中…" : "等待你的回答…";
  }
  if (memberInformationRequests.value.length) {
    return hitlBusy.value ? "提交成员回答中…" : "成员正在等待你的回答…";
  }
  if (Object.keys(memberApprovalByAssign.value).length) return "等待工具审批…";
  if (sending.value || remoteSending.value) {
    return liveStatusLabel.value || "协作进行中…";
  }
  return "";
});

const toolbarTitle = computed(() => {
  const name = String(workgroupMeta.value?.display_name || "").trim();
  if (name) return name;
  return "工作组";
});

/** 内容变化时跟随滚底（含进度文案变化，不只看条数） */
const timelineTailKey = computed(() => {
  const list = events.value || [];
  const parts = [
    list.length,
    sending.value || remoteSending.value ? 1 : 0,
    liveAssistant.value?.text?.length || 0,
    streamPhase.value,
  ];
  if (list.length) {
    const last = list[list.length - 1];
    parts.push(last?.event_id || last?.seq || "", last?.type || "", (last?.text || "").length);
  }
  return parts.join("\0");
});

watch(timelineTailKey, () => {
  maybeScrollTimelineTail();
});

let previousEventGroupCount = 0;
watch(eventGroups, (groups) => {
  const count = groups.length;
  if (scrollTail.follow && count > previousEventGroupCount) {
    renderWindowStart.value = Math.max(0, count - MAX_RENDERED_GROUPS);
  } else {
    renderWindowStart.value = Math.min(
      renderWindowStart.value,
      Math.max(0, count - MAX_RENDERED_GROUPS),
    );
  }
  previousEventGroupCount = count;
}, { immediate: true });

watch(
  workgroupId,
  async (id) => {
    closeModelMenu();
    stopWorkgroupEventStream();
    workgroupEventSeq = 0;
    renderWindowStart.value = 0;
    previousEventGroupCount = 0;
    scrollTail.setFollow(true);
    // 切换工作组时复位输入/直连/发送态，避免底部状态条残留
    draft.value = "";
    directMember.value = null;
    mentionOpen.value = false;
    mentionQuery.value = "";
    sending.value = false;
    cancelling.value = false;
    statusWatermarkSeq.value = 0;
    expandedMemberReports.value = {};
    expandedAssignTasks.value = {};
    clearLive();
    if (streamAbort) {
      try {
        streamAbort.abort();
      } catch {
        /* ignore */
      }
      streamAbort = null;
    }
    pendingHitlRefresh.reset();
    pendingHitl.value = [];
    hitlDraft.value = "";
    hitlBusy.value = false;
    debugOpen.value = false;
    error.value = "";
    workgroupAccessError.value = "";
    await Promise.all([
      loadSelf(),
      loadTimeline(),
      loadWorkgroupMeta(),
      loadMembers(),
      loadPendingHitl(),
      refreshHumanQueue(),
    ]);
    await loadWorkgroupAccess();
    scrollTimelineTail();
    if (id) {
      startWorkgroupEventStream();
    }
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

onMounted(() => {
  bindTimelineResizeObserver();
  document.addEventListener("pointerdown", onModelMenuPointerDown, true);
  document.addEventListener("keydown", onModelMenuKeydown, true);
});
watch(timelineEl, (el) => {
  if (el) bindTimelineResizeObserver();
});
onUnmounted(() => {
  stopWorkgroupEventStream();
  timelineResizeObserver?.disconnect();
  timelineResizeObserver = null;
  document.removeEventListener("pointerdown", onModelMenuPointerDown, true);
  document.removeEventListener("keydown", onModelMenuKeydown, true);
});
</script>

<template>
  <div class="app__body app__body--chat-v61">
    <aside class="app__col app__col--agents">
      <NavRail
        ref="panelRef"
        :realtime-status="workgroupRealtimeStatus"
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
          返回智能体
        </button>
      </div>
      <template v-else>
        <header class="chat__header wg-chat__header">
          <div class="chat__title">
            <span class="chat__title-main" :title="toolbarTitle">{{ toolbarTitle }}</span>
          </div>
          <div class="chat__header-meta">
            <div v-if="llmConfigs.length" ref="modelMenuRoot" class="wg-chat__model">
              <span class="wg-chat__model-label">model</span>
              <button
                type="button"
                class="wg-chat__model-trigger"
                :class="{ 'wg-chat__model-trigger--open': modelMenuOpen }"
                :disabled="!canSwitchSupervisorModel"
                :aria-expanded="modelMenuOpen"
                aria-haspopup="listbox"
                :title="selectedSupervisorLabel"
                @click="toggleModelMenu"
              >
                <span class="wg-chat__model-trigger-label">{{ selectedSupervisorLabel }}</span>
                <svg
                  class="wg-chat__model-chevron"
                  viewBox="0 0 12 12"
                  width="12"
                  height="12"
                  aria-hidden="true"
                >
                  <path
                    d="M3 4.5L6 7.5L9 4.5"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.4"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                </svg>
              </button>
              <div
                v-if="modelMenuOpen"
                class="wg-chat__model-menu"
                role="listbox"
                aria-label="选择模型"
              >
                <button
                  v-for="cfg in llmConfigs"
                  :key="cfg.id"
                  type="button"
                  class="wg-chat__model-option"
                  role="option"
                  :aria-selected="String(cfg.id) === String(workgroupMeta?.llm_profile_id || '')"
                  :class="{
                    'wg-chat__model-option--active':
                      String(cfg.id) === String(workgroupMeta?.llm_profile_id || ''),
                  }"
                  @click="pickSupervisorModel(cfg.id)"
                >
                  <span class="wg-chat__model-option-label">
                    {{ cfg.name || cfg.id }}{{ cfg.is_default ? "（默认）" : "" }}
                  </span>
                  <span
                    v-if="String(cfg.id) === String(workgroupMeta?.llm_profile_id || '')"
                    class="wg-chat__model-option-check"
                    aria-hidden="true"
                  >✓</span>
                </button>
              </div>
            </div>
            <button
              type="button"
              class="chat__header-btn"
              :class="{ 'chat__header-btn--active': debugOpen }"
              title="RunHistory / LLM 调试"
              aria-label="调试"
              @click="toggleDebugPanel"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
                <path d="M7 4.5h10a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-11a2 2 0 0 1 2-2Z" stroke="currentColor" stroke-width="1.7"/>
                <path d="M8.5 8h7M8.5 11.5h7M8.5 15h4" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>
                <path d="m15.8 15.2 1.3 1.3 2.5-2.7" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </button>
          </div>
        </header>

        <div v-if="isConfiguring" class="wg-chat__setup-banner" role="status">
          <p>
            当前为<strong>配置中</strong>：请确认 model 与成员后点击「发布」，方可对话。
          </p>
          <button type="button" class="btn btn--primary btn--sm" :disabled="publishing" @click="publishCurrent">
            {{ publishing ? "发布中…" : "发布工作组" }}
          </button>
        </div>

        <div class="wg-chat__body" :class="{ 'wg-chat__body--debug': debugOpen }">
          <div class="wg-chat__timeline-wrap">
            <ScrollToTailButton :visible="showScrollToTail" @click="scrollTimelineTail" />
            <div
              ref="timelineEl"
              class="wg-chat__timeline chat__stream"
              @scroll="onTimelineScroll"
            >
            <button
              v-if="hasEarlierMessages"
              type="button"
              class="wg-chat__load-earlier"
              @click="loadEarlierMessages"
            >
              加载更早的 {{ earlierMessageCount }} 条消息
            </button>
            <article
              v-for="group in renderedEventGroups"
              :key="group.key"
              v-memo="[group.renderKey]"
              class="msg"
              :class="[
                group.role === 'user' ? 'msg--user' : 'msg--assistant',
                group.items.every((it) => it.kind === 'progress') ? 'msg--progress' : '',
              ]"
            >
              <div
                class="msg__body"
                :class="{
                  'msg__body--hint-only': group.items.every((it) => it.kind === 'progress'),
                  'msg__body--grouped': true,
                }"
              >
                <div class="msg__hint wg-chat__message-hint">
                  <span v-if="group.role !== 'user'" class="wg-chat__message-mark" aria-hidden="true">
                    <img :src="brandIcon" alt="" />
                  </span>
                  <span>{{ group.label }}</span>
                </div>
                <template v-for="item in group.items" :key="item.key">
                  <div
                    v-if="item.kind === 'assign'"
                    class="wg-task"
                    :class="{
                      'wg-task--done': item.done && !item.failed,
                      'wg-task--failed': item.failed,
                      'wg-task--running': !item.done,
                    }"
                  >
                    <div v-if="item.mention" class="wg-task__mention">
                      <span class="wg-task__at">@{{ item.mention }}</span>
                    </div>
                    <div class="wg-task__card">
                      <div class="wg-task__head">
                        <button
                          type="button"
                          class="wg-task__toggle"
                          :aria-expanded="isAssignTaskExpanded(item.taskToggleKey)"
                          :aria-controls="taskDetailsId(item)"
                          :aria-label="
                            isAssignTaskExpanded(item.taskToggleKey)
                              ? '收起任务详情'
                              : '展开任务详情'
                          "
                          @click="toggleAssignTask(item.taskToggleKey)"
                        >
                          <span class="wg-task__label">任务</span>
                          <span class="wg-task__preview">
                            {{
                              isAssignTaskExpanded(item.taskToggleKey)
                                ? item.taskText
                                : item.taskPreview
                            }}
                          </span>
                          <span class="wg-task__chevron" aria-hidden="true">
                            {{ isAssignTaskExpanded(item.taskToggleKey) ? "▾" : "▸" }}
                          </span>
                        </button>
                        <span class="wg-task__status">
                          <BrandActivityIndicator
                            v-if="!item.done"
                            class="wg-task__dots"
                            mode="tool"
                            :show-label="false"
                            compact
                          />
                          <span v-if="item.done && !item.failed" class="wg-task__check" aria-hidden="true">✓</span>
                          <span v-else-if="item.failed" class="wg-task__mark" aria-hidden="true">−</span>
                          {{ item.statusText }}
                        </span>
                      </div>
                      <div
                        v-if="isAssignTaskExpanded(item.taskToggleKey)"
                        :id="taskDetailsId(item)"
                        class="wg-task__details"
                      >
                        <div class="wg-task__body">{{ item.taskText }}</div>
                      </div>
                      <div
                        v-if="item.steps?.length"
                        class="wg-task__steps"
                      >
                        <template v-for="step in item.steps" :key="step.key">
                          <div
                            v-if="step.kind === 'content'"
                            class="wg-task__pre-tool tool-exec-bubble__markdown assistant-msg__md"
                            v-html="renderMarkdown(step.text)"
                          />
                          <WorkgroupApprovalCard
                            v-else-if="step.kind === 'approval'"
                            :approval="step.approval"
                            :hitl-busy="hitlBusy"
                            @resolve="(callId, approve) => resolveMemberApproval(step.approval, callId, approve)"
                          />
                          <WorkgroupToolRow
                            v-else
                            :item="step"
                            :hitl-busy="hitlBusy"
                            @resolve="(callId, approve) => resolveMemberApproval(step.approval, callId, approve)"
                          />
                        </template>
                      </div>
                      <div v-if="item.hasReport" class="wg-task__report">
                        <button
                          type="button"
                          class="wg-task__report-bar"
                          :aria-expanded="isMemberReportExpanded(item.reportToggleKey)"
                          :aria-label="
                            isMemberReportExpanded(item.reportToggleKey)
                              ? '收起成员结论'
                              : '展开成员结论'
                          "
                          @click="toggleMemberReport(item.reportToggleKey)"
                        >
                          <span class="wg-task__report-kind">成员结论</span>
                          <span
                            v-if="!isMemberReportExpanded(item.reportToggleKey)"
                            class="wg-task__report-preview"
                          >
                            {{ item.reportPreview }}
                          </span>
                          <span class="wg-task__report-chevron" aria-hidden="true">
                            {{ isMemberReportExpanded(item.reportToggleKey) ? "▾" : "▸" }}
                          </span>
                        </button>
                        <div
                          v-if="isMemberReportExpanded(item.reportToggleKey)"
                          class="wg-task__report-body tool-exec-bubble__markdown assistant-msg__md"
                          v-html="renderMarkdown(item.reportText)"
                        />
                      </div>
                    </div>
                  </div>
                  <WorkgroupToolRow
                    v-else-if="item.kind === 'tool'"
                    :item="item"
                    :hitl-busy="hitlBusy"
                    @resolve="(callId, approve) => resolveMemberApproval(item.approval, callId, approve)"
                  />
                  <div
                    v-else-if="item.kind === 'live_user'"
                    class="msg__bubble msg__bubble--user"
                  >
                    <template
                      v-for="(part, pi) in splitUserMentionParts(item.text, item.directMemberId)"
                      :key="pi"
                    >
                      <span v-if="part.type === 'mention'" class="wg-msg-at">{{ part.text }}</span>
                      <template v-else>{{ part.text }}</template>
                    </template>
                  </div>
                  <div
                    v-else-if="item.kind === 'live_assistant'"
                    class="msg__bubble msg__bubble--assistant-md"
                  >
                    <pre class="assistant-msg__stream-plain">{{ item.text }}</pre>
                  </div>
                  <div
                    v-else
                    class="msg__bubble"
                    :class="
                      isHumanEvent(item.ev)
                        ? 'msg__bubble--user'
                        : 'msg__bubble--assistant-md'
                    "
                  >
                    <template v-if="isHumanEvent(item.ev)">
                      <template
                        v-for="(part, pi) in splitUserMentionParts(item.ev.text, item.ev.direct_member_id)"
                        :key="pi"
                      >
                        <span v-if="part.type === 'mention'" class="wg-msg-at">{{ part.text }}</span>
                        <template v-else>{{ part.text }}</template>
                      </template>
                    </template>
                    <div
                      v-else
                      class="tool-exec-bubble__markdown assistant-msg__md"
                      v-html="renderMarkdown(item.ev.text || '')"
                    />
                  </div>
                </template>
              </div>
            </article>
            <article
              v-for="request in memberInformationRequests"
              :key="request.key"
              class="msg msg--assistant"
            >
              <div class="msg__body msg__body--grouped">
                <div class="msg__hint wg-chat__message-hint">
                  <span class="wg-chat__message-mark" aria-hidden="true">
                    <img :src="brandIcon" alt="" />
                  </span>
                  <span>{{ request.memberLabel }}</span>
                </div>
                <WorkgroupUserInformationCard
                  :request="request"
                  :busy="hitlBusy"
                  @resolve="(resolution) => resolveMemberInformation(request, resolution)"
                />
              </div>
            </article>
            <article
              v-if="supervisorHitl"
              class="msg msg--assistant"
            >
              <div class="msg__body msg__body--grouped">
                <div class="msg__hint wg-chat__message-hint">
                  <span class="wg-chat__message-mark" aria-hidden="true">
                    <img :src="brandIcon" alt="" />
                  </span>
                  <span>Supervisor</span>
                </div>
                <div class="wg-hitl-bubble">
                  <div class="wg-hitl-bubble__badge">询问</div>
                  <p class="wg-hitl-bubble__prompt">{{ supervisorHitl.prompt }}</p>
                  <p class="wg-hitl-bubble__hint">在下方输入框回答后 Enter 提交</p>
                </div>
              </div>
            </article>
            <div v-if="!events.length && !sending && !remoteSending" class="chat__empty">
              <div class="chat__empty-inner">
                <img class="wg-chat__empty-mark" :src="brandIcon" alt="" aria-hidden="true" />
                <div class="chat__empty-title">开始对话</div>
                <div class="chat__empty-hint">向工作组发言，Leader 会编排成员协作</div>
              </div>
            </div>
            </div>
          </div>
          <WorkgroupDebugPanel
            v-if="debugOpen"
            :workgroup-id="workgroupId"
          />
        </div>

          <WorkgroupComposer
            :human-queue-items="humanQueueItems"
            :editing-queue-id="editingQueueId"
            :edit-queue-draft="editQueueDraft"
            :draft="draft"
            :composer-runtime-label="composerRuntimeLabel"
            :mention-open="mentionOpen"
            :mention-candidates="mentionCandidates"
            :hitl-mode="hitlMode"
            :pending-interaction="hasPendingInteraction"
            :direct-member="directMember"
            :hitl-draft="hitlDraft"
            :can-chat="canChat"
            :is-configuring="isConfiguring"
            :sending="sending"
            :cancelling="cancelling"
            :hitl-busy="hitlBusy"
            @update:edit-queue-draft="editQueueDraft = $event"
            @draft-input="onDraftInput"
            @draft-backspace="onComposerBackspace"
            @update:mention-open="mentionOpen = $event"
            @update:hitl-draft="hitlDraft = $event"
            @send="send"
            @cancel="cancelTurn"
            @submit-hitl-answer="submitHitlAnswer"
            @pick-mention="pickMention"
            @clear-direct-mention="clearDirectMention"
            @begin-edit-queued="beginEditQueued"
            @cancel-edit-queued="cancelEditQueued"
            @save-queued-edit="saveQueuedEdit"
            @send-queued-now="sendQueuedNow"
            @remove-queued="removeQueued"
          />
      </template>
    </div>

    <WorkgroupMemberModal
      :open="memberModalOpen"
      :mode="memberModalMode"
      :workgroup-id="memberModalWgId"
      :member-id="memberModalMemberId"
      :member="memberModalMember"
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
  /* 与 .app__main-col > .chat.panel 一致：标题栏与对话区同色 */
  background: var(--color-editor);
  color: var(--color-text);
}
.wg-chat__header {
  flex: 0 0 auto;
  background: var(--color-editor);
}
.wg-chat :deep(.chat__title-main) {
  font-family: var(--font-ui);
  font-size: 14px;
  font-weight: 600;
  line-height: 1.25;
  color: var(--color-text);
}
.wg-chat__model {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--color-text-muted, #6b7280);
}
.wg-chat__model-label {
  flex: 0 0 auto;
  font-weight: 500;
  letter-spacing: 0.01em;
  text-transform: lowercase;
}
.wg-chat__model-trigger {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 200px;
  min-height: 28px;
  padding: 2px 4px;
  border: 0;
  border-radius: var(--radius-md, 6px);
  background: transparent;
  color: var(--color-text, #111827);
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  line-height: 1.25;
  cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.wg-chat__model-trigger:hover:not(:disabled),
.wg-chat__model-trigger:focus-visible,
.wg-chat__model-trigger--open {
  background: var(--color-surface-hover, rgba(0, 0, 0, 0.04));
  outline: none;
}
.wg-chat__model-trigger:disabled {
  cursor: default;
  opacity: 0.7;
}
.wg-chat__model-trigger-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wg-chat__model-chevron {
  flex: 0 0 auto;
  opacity: 0.55;
  transition: transform 0.15s ease, opacity 0.15s ease;
}
.wg-chat__model-trigger--open .wg-chat__model-chevron {
  transform: rotate(180deg);
  opacity: 1;
}
.wg-chat__model-menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 40;
  min-width: 180px;
  max-width: min(280px, 70vw);
  max-height: 240px;
  overflow: auto;
  padding: 6px;
  border-radius: 10px;
  border: 1px solid var(--color-border, #d1d5db);
  background: var(--color-surface, #fff);
  box-shadow: var(--shadow-md, 0 8px 24px rgba(0, 0, 0, 0.12));
}
.wg-chat__model-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  padding: 7px 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--color-text, #111827);
  font: inherit;
  font-size: 12px;
  text-align: left;
  cursor: pointer;
}
.wg-chat__model-option:hover,
.wg-chat__model-option:focus-visible {
  background: var(--color-surface-hover, rgba(0, 0, 0, 0.04));
  outline: none;
}
.wg-chat__model-option--active {
  background: var(--color-primary-soft, color-mix(in srgb, var(--color-primary, #0078d4) 12%, transparent));
  color: var(--color-primary-strong, var(--color-primary, #0078d4));
}
.wg-chat__model-option-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wg-chat__model-option-check {
  flex: 0 0 auto;
  font-size: 11px;
  opacity: 0.9;
}
.wg-chat__setup-banner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin: 0;
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--color-border, #d1d5db);
  background: color-mix(in srgb, var(--color-primary, #0078d4) 8%, var(--color-surface, #fff));
  font-size: 13px;
  color: var(--color-text, #111827);
}
.wg-chat__setup-banner p {
  margin: 0;
  flex: 1 1 220px;
}
.wg-chat__body {
  position: relative;
  flex: 1;
  display: flex;
  min-height: 0;
  background: var(--color-editor, #fff);
}
.wg-chat__timeline-wrap {
  position: relative;
  display: flex;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
}
.wg-chat__body--debug .wg-chat__timeline {
  flex: 1 1 auto;
}
.wg-chat__body--debug .wg-chat__timeline-wrap {
  flex: 1 1 58%;
}
.wg-chat__timeline.chat__stream {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow: auto;
}
.wg-chat__load-earlier {
  align-self: center;
  margin: 10px auto 4px;
  padding: 5px 10px;
  border: 1px solid var(--color-border, #d1d5db);
  border-radius: 999px;
  background: var(--color-surface, #fff);
  color: var(--color-text-muted, #6b7280);
  font-size: 11px;
  cursor: pointer;
}
.wg-chat__load-earlier:hover {
  border-color: var(--color-primary, #0078d4);
  color: var(--color-primary, #0078d4);
}
.wg-chat__timeline .msg__body {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}
.msg__body--grouped {
  gap: 6px;
}
.msg--progress .msg__hint {
  opacity: 0.85;
}
.msg--progress .msg__hint--stream-meta,
.msg__body--grouped .msg__hint--stream-meta {
  margin-top: 0;
  opacity: 0.8;
  font-size: 12px;
  line-height: 1.45;
  color: var(--color-text-muted, #6b7280);
}
.wg-msg-at {
  color: var(--color-primary, #0078d4);
  font-weight: 600;
}
.wg-hitl-bubble {
  width: 100%;
  max-width: 100%;
  margin: 4px 0 8px;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid var(--color-border, #d1d5db);
  border-left: 3px solid var(--color-primary, #0078d4);
  background: color-mix(in srgb, var(--color-primary, #0078d4) 5%, var(--color-surface, #fff));
}
.wg-hitl-bubble__badge {
  display: inline-flex;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--color-primary, #0078d4);
  margin-bottom: 6px;
}
.wg-hitl-bubble__prompt {
  margin: 0 0 6px;
  font-size: 13.5px;
  line-height: 1.5;
  color: var(--color-text, #111827);
  white-space: pre-wrap;
  word-break: break-word;
}
.wg-hitl-bubble__hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-muted, #6b7280);
}
.assistant-msg__stream-plain {
  margin: 0;
  padding: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font: inherit;
  color: inherit;
  background: transparent;
  border: 0;
}
.wg-task {
  width: 100%;
  max-width: 100%;
  margin: 4px 0 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.wg-task__mention {
  display: flex;
  align-items: center;
  min-width: 0;
}
.wg-task__at {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
  color: var(--color-primary, #0078d4);
  background: color-mix(in srgb, var(--color-primary, #0078d4) 10%, var(--color-surface, #fff));
  border: 1px solid color-mix(in srgb, var(--color-primary, #0078d4) 22%, transparent);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.wg-task__card {
  width: 100%;
  max-width: 100%;
  border-radius: 10px;
  border: 1px solid var(--color-border, #d1d5db);
  background: var(--color-surface, #fff);
  overflow: hidden;
}
.wg-task--running .wg-task__card {
  border-color: var(--color-border, #d1d5db);
}
.wg-task--failed .wg-task__card {
  border-color: color-mix(in srgb, #dc2626 28%, var(--color-border, #d1d5db));
}
.wg-task__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  padding: 8px 12px;
}
.wg-task__toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1 1 auto;
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.wg-task__toggle:hover .wg-task__preview,
.wg-task__toggle:focus-visible .wg-task__preview {
  color: var(--color-primary, #0078d4);
}
.wg-task__toggle:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--color-primary, #0078d4) 45%, transparent);
  outline-offset: 3px;
  border-radius: 4px;
}
.wg-task__label {
  flex: 0 0 auto;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--color-text-subtle, #9ca3af);
}
.wg-task__preview {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-muted, #6b7280);
  font-size: 12.5px;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 0.12s ease;
}
.wg-task__chevron {
  flex: 0 0 auto;
  color: var(--color-text-subtle, #9ca3af);
  font-size: 12px;
  line-height: 1;
}
.wg-task__status {
  flex: 0 1 auto;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
  max-width: min(9rem, 42%);
  overflow: hidden;
  font-size: 11px;
  color: var(--color-text-subtle, #9ca3af);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wg-task__dots {
  --stream-meta-dot-size: 4px;
  --stream-meta-dot-gap: 2px;
}
.wg-task__check {
  color: var(--color-success, #0f7b0f);
  opacity: 0.9;
}
.wg-task__mark {
  color: var(--color-text-subtle, #9ca3af);
  opacity: 0.75;
}
.wg-task__body {
  padding: 6px 12px 10px;
  font-size: 13.5px;
  line-height: 1.55;
  color: var(--color-text, #111827);
  white-space: pre-wrap;
  word-break: break-word;
}
.wg-task__details {
  border-top: 1px solid var(--color-border, #e5e7eb);
}
.wg-task__details .wg-task__body {
  padding-top: 10px;
}
.wg-task__steps {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0 8px 8px;
  border-top: 1px solid var(--color-border, #e5e7eb);
  padding-top: 8px;
}
.wg-task__steps .wg-tool-row {
  margin: 0;
  background: color-mix(in srgb, var(--color-text, #111827) 1.5%, var(--color-surface, #fff));
}
.wg-task__pre-tool {
  padding: 2px 4px 6px;
  color: var(--color-text, #111827);
  font-size: 13px;
  line-height: 1.55;
}
.wg-task__pre-tool > :first-child {
  margin-top: 0;
}
.wg-task__pre-tool > :last-child {
  margin-bottom: 0;
}
.wg-task__report {
  border-top: 1px solid var(--color-border, #e5e7eb);
}
.wg-task__report-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-width: 0;
  padding: 8px 12px;
  margin: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.wg-task__report-bar:hover {
  background: color-mix(in srgb, var(--color-text, #111827) 2.5%, transparent);
}
.wg-task__report-bar:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--color-primary, #0078d4) 45%, transparent);
  outline-offset: -2px;
}
.wg-task__report-kind {
  flex: 0 0 auto;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: uppercase;
  line-height: 1.4;
  padding: 1px 5px;
  border-radius: 3px;
  color: var(--color-text-subtle, #9ca3af);
  border: 1px solid var(--color-border, #d1d5db);
  background: var(--color-surface-elevated, #f8fafc);
}
.wg-task__report-preview {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  line-height: 1.35;
  color: var(--color-text-muted, #6b7280);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.wg-task__report-chevron {
  flex: 0 0 auto;
  font-size: 10px;
  color: var(--color-text-subtle, #9ca3af);
}
.wg-task__report-body {
  padding: 0 12px 10px 12px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--color-text, #111827);
}
.wg-task__report-body :deep(p:first-child) {
  margin-top: 0;
}
.wg-task__report-body :deep(p:last-child) {
  margin-bottom: 0;
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
</style>
