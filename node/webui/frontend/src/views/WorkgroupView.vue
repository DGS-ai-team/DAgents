<script setup>
import { ref, watch, onUnmounted, computed, onMounted, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import { isKnownWorkgroupRealtimeEvent } from "../sse/workgroupEvents.js";
import NavRail from "../components/NavRail.vue";
import WorkgroupMemberModal from "../components/WorkgroupMemberModal.vue";
import BrandActivityIndicator from "../components/BrandActivityIndicator.vue";
import ScrollToTailButton from "../components/ScrollToTailButton.vue";
import { renderMarkdown } from "../utils/markdown.js";
import { inferToolKind } from "../utils/toolSource.js";
import { createFollowTailController, distanceFromTail } from "../utils/scrollTail.js";
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
let queuePollTimer = null;
const error = ref("");
const workgroupAccessError = ref("");
const notice = ref("");
const mentionOpen = ref(false);
const mentionQuery = ref("");
/** @type {import('vue').Ref<null | { member_id: string, display_name: string }>} */
const directMember = ref(null);
const pollTimer = ref(null);
const workPollTimer = ref(null);
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

/** RunHistory 调试面板（mock / LLM 可观测） */
const debugOpen = ref(false);
const debugLoading = ref(false);
const debugRuns = ref([]);
const debugLlm = ref(null);
const debugSelectedRunId = ref("");
const debugHistory = ref(null);
const debugError = ref("");

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
let pollInFlight = false;
let workgroupEventSource = null;
let workgroupEventSeq = 0;

const activeHitl = computed(() => (pendingHitl.value || [])[0] || null);
const hitlMode = computed(() => Boolean(activeHitl.value));
// Assigned member content is already represented by the task card. Keep the
// live bubble for Supervisor and direct @member turns, where it is the actual
// conversational reply.
const showLiveAssistant = computed(
  () => Boolean(liveAssistant.value) && streamMode.value !== "member",
);
const debugLlmBadge = computed(() => {
  const mode = String(debugLlm.value?.mode || "").trim();
  if (mode === "mock") return "Mock · 回声/脚本";
  if (mode === "live") {
    const model = String(debugLlm.value?.model || "").trim();
    return model ? `Live · ${model}` : "Live";
  }
  return "";
});

async function loadDebugRuns() {
  if (!workgroupId.value) {
    debugRuns.value = [];
    debugLlm.value = null;
    return;
  }
  debugLoading.value = true;
  debugError.value = "";
  try {
    const res = await api.listWorkgroupRuns(workgroupId.value, { limit: 12 });
    debugRuns.value = Array.isArray(res?.runs) ? res.runs : [];
    debugLlm.value = res?.llm || null;
    if (!debugSelectedRunId.value && debugRuns.value.length) {
      await selectDebugRun(debugRuns.value[0].run_id);
    } else if (debugSelectedRunId.value) {
      const still = debugRuns.value.some((r) => r.run_id === debugSelectedRunId.value);
      if (still) await selectDebugRun(debugSelectedRunId.value);
      else if (debugRuns.value.length) await selectDebugRun(debugRuns.value[0].run_id);
      else {
        debugHistory.value = null;
        debugSelectedRunId.value = "";
      }
    }
  } catch (e) {
    debugError.value = e?.message || "加载 Run 失败";
    debugRuns.value = [];
  } finally {
    debugLoading.value = false;
  }
}

async function selectDebugRun(runId) {
  const id = String(runId || "").trim();
  if (!id || !workgroupId.value) return;
  debugSelectedRunId.value = id;
  debugLoading.value = true;
  debugError.value = "";
  try {
    const res = await api.getWorkgroupRunHistory(workgroupId.value, id);
    debugHistory.value = res?.history || null;
    if (res?.llm) debugLlm.value = res.llm;
  } catch (e) {
    debugError.value = e?.message || "加载 History 失败";
    debugHistory.value = null;
  } finally {
    debugLoading.value = false;
  }
}

async function toggleDebugPanel() {
  debugOpen.value = !debugOpen.value;
  if (debugOpen.value) await loadDebugRuns();
}

function formatDebugMsg(m) {
  const role = String(m?.role || "");
  if (role === "assistant" && Array.isArray(m?.tool_calls) && m.tool_calls.length) {
    const names = m.tool_calls.map((tc) => tc?.function?.name || tc?.name || "?").join(", ");
    const body = String(m?.content || "").trim();
    return body ? `${body}\n\ntool_calls: ${names}` : `tool_calls: ${names}`;
  }
  if (role === "tool") {
    const body = String(m?.content || "").trim();
    return body.length > 180 ? `${body.slice(0, 180)}…` : body || "(empty)";
  }
  const body = String(m?.content || "").trim();
  return body.length > 220 ? `${body.slice(0, 220)}…` : body || "(empty)";
}

async function loadPendingHitl() {
  if (!workgroupId.value) {
    pendingHitl.value = [];
    return;
  }
  try {
    const res = await api.listWorkgroupHITL(workgroupId.value, true);
    const list = Array.isArray(res) ? res : res?.hitl || [];
    pendingHitl.value = Array.isArray(list) ? list : [];
  } catch {
    /* 轮询期忽略 */
  }
}

async function submitHitlAnswer() {
  const hitl = activeHitl.value;
  const answer = hitlDraft.value.trim();
  if (!hitl || !workgroupId.value || !answer || hitlBusy.value) return;
  hitlBusy.value = true;
  error.value = "";
  try {
    await api.resolveWorkgroupHITL(workgroupId.value, hitl.hitl_id, answer);
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
    /* reconnect/resync polling handles malformed or partial frames */
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
    // 回退列表查找（旧 Manage / 权限边界）
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

function startPoll() {
  stopPoll();
  pollTimer.value = window.setInterval(async () => {
    if (pollInFlight) return;
    pollInFlight = true;
    try {
      await loadTimeline();
      await loadPendingHitl();
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

function stopWorkPoll() {
  if (workPollTimer.value) {
    clearInterval(workPollTimer.value);
    workPollTimer.value = null;
  }
}

function startWorkPoll() {
  stopWorkPoll();
  workPollTimer.value = window.setInterval(() => {
    if (!sending.value && !remoteSending.value) return;
    void loadPendingHitl();
  }, 1500);
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

function startQueuePoll() {
  stopQueuePoll();
  queuePollTimer = setInterval(() => {
    if (!workgroupId.value) return;
    if (!sending.value && !(humanQueueItems.value || []).length) return;
    void refreshHumanQueue();
  }, 1500);
}

function stopQueuePoll() {
  if (queuePollTimer) {
    clearInterval(queuePollTimer);
    queuePollTimer = null;
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
    // ACL reads can be unavailable for older Manage deployments; sending will
    // still surface the normalized authorization error from the POST path.
  }
}

async function sendQueuedNow(item) {
  const qid = String(item?.queue_id || "").trim();
  if (!workgroupId.value || !qid) return;
  try {
    await api.sendWorkgroupHumanQueueItemNow(workgroupId.value, qid);
    await refreshHumanQueue();
    startQueuePoll();
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
      startQueuePoll();
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
  startWorkPoll();
  startQueuePoll();

  try {
    let becameQueued = false;
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
          if (eventName === "queued") {
            becameQueued = true;
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
    if (debugOpen.value) await loadDebugRuns();
    scrollTimelineTail();
    if (becameQueued) startQueuePoll();
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
    if (debugOpen.value) await loadDebugRuns().catch(() => {});
  } finally {
    streamAbort = null;
    stopWorkPoll();
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
  if (!workgroupId.value || !sending.value || cancelling.value) return;
  cancelling.value = true;
  try {
    await api.cancelWorkgroupTurn(workgroupId.value);
    if (streamAbort) {
      try {
        streamAbort.abort();
      } catch {
        /* ignore */
      }
    }
    await loadTimeline();
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

function eventLabel(ev) {
  const actor = String(ev?.actor_id || "").trim();
  const type = String(ev?.type || "");
  if (type === "human_message") {
    if (actor && selfNodeId.value && actor === selfNodeId.value) {
      return selfNodeName.value || actor;
    }
    return actor || "human";
  }
  if (type === "actor_final_text" || type === "assistant_content") {
    if (actor === "leader") return "Supervisor";
    if (actor && memberNameById.value[actor]) return memberNameById.value[actor];
    return actor || "member";
  }
  if (type === "assign_started" || type === "assign_finished" || type === "system_notice") {
    if (actor === "leader") return "Supervisor";
    if (actor && memberNameById.value[actor]) return memberNameById.value[actor];
    return actor || "工作组";
  }
  return type || "event";
}

function isHumanEvent(ev) {
  return String(ev?.type || "") === "human_message";
}

/** 只有结构化直达事件才高亮 @成员；普通文本中的 @ 保持原样。 */
function splitUserMentionParts(text, directMemberId = "") {
  const raw = String(text || "");
  if (!raw) return [{ type: "text", text: "" }];
  const memberId = String(directMemberId || "").trim();
  const displayName = memberNameById.value[memberId] || "";
  const token = displayName ? `@${displayName}` : "";
  if (!memberId || !token) return [{ type: "text", text: raw }];
  const parts = [];
  let last = 0;
  let index = raw.indexOf(token);
  while (index >= 0) {
    const before = index === 0 ? " " : raw[index - 1];
    const afterIndex = index + token.length;
    const after = afterIndex >= raw.length ? " " : raw[afterIndex];
    if (/\s/.test(before) && /\s/.test(after)) {
      if (index > last) parts.push({ type: "text", text: raw.slice(last, index) });
      parts.push({ type: "mention", text: token });
      last = afterIndex;
    }
    index = raw.indexOf(token, Math.max(afterIndex, index + 1));
  }
  if (last < raw.length) parts.push({ type: "text", text: raw.slice(last) });
  if (!parts.length) parts.push({ type: "text", text: raw });
  return parts;
}

function isDirectAssignEvent(ev) {
  const actor = String(ev?.actor_id || "").trim();
  const text = String(ev?.text || "").trim();
  // 新路径：挂在成员下；旧路径：leader +「直达」前缀
  return (actor && actor !== "leader") || text.startsWith("直达");
}

function previewMemberReport(text) {
  const raw = String(text || "").trim().replace(/\s+/g, " ");
  if (!raw) return "成员结论";
  return raw.length > 72 ? `${raw.slice(0, 72)}…` : raw;
}

function previewAssignTask(text) {
  const raw = String(text || "").trim().replace(/\s+/g, " ");
  if (!raw) return "分派任务";
  return raw.length > 96 ? `${raw.slice(0, 96)}…` : raw;
}

/** 解析编排态 assign_started：新格式 `@名\\n任务`；兼容旧 `→ 名 · 摘要` */
function parseAssignStartedText(text) {
  const raw = String(text || "").trim();
  if (!raw) return { mention: "", taskText: "分派任务" };

  const atMatch = raw.match(/^@([^\n\r]+)\r?\n([\s\S]*)$/);
  if (atMatch) {
    return {
      mention: String(atMatch[1] || "").trim(),
      taskText: String(atMatch[2] || "").trim() || "分派任务",
    };
  }

  const arrow = raw.match(/^→\s*(.+?)\s*·\s*([\s\S]*)$/);
  if (arrow) {
    return {
      mention: String(arrow[1] || "").trim(),
      taskText: String(arrow[2] || "").trim() || "分派任务",
    };
  }

  if (raw.startsWith("@")) {
    const name = raw.slice(1).trim();
    return { mention: name, taskText: "分派任务" };
  }

  return { mention: "", taskText: raw };
}

function buildAssignIndex(list) {
  const directAssignIds = new Set();
  const noticeByAssign = {};
  const noticesByAssign = {};
  const finishedByAssign = {};
  const startedByAssign = {};
  const memberFinalByAssign = {};
  const assistantContentByAssign = {};
  const noticeIndexByEventId = {};
  for (const ev of list || []) {
    const t = String(ev?.type || "");
    const aid = String(ev?.assign_id || "").trim();
    if (!aid) continue;
    if (t === "assign_started") {
      startedByAssign[aid] = ev;
      if (isDirectAssignEvent(ev)) directAssignIds.add(aid);
    } else if (t === "assign_finished") {
      finishedByAssign[aid] = ev;
      if (isDirectAssignEvent(ev)) directAssignIds.add(aid);
    } else if (t === "system_notice") {
      noticeByAssign[aid] = ev;
      if (!noticesByAssign[aid]) noticesByAssign[aid] = [];
      if (ev.event_id) noticeIndexByEventId[ev.event_id] = noticesByAssign[aid].length;
      noticesByAssign[aid].push(ev);
    } else if (t === "actor_final_text") {
      const actor = String(ev?.actor_id || "").trim();
      if (actor && actor !== "leader") memberFinalByAssign[aid] = ev;
    } else if (t === "assistant_content") {
      if (!assistantContentByAssign[aid]) assistantContentByAssign[aid] = [];
      assistantContentByAssign[aid].push(ev);
    }
  }
  return {
    directAssignIds,
    noticeByAssign,
    noticesByAssign,
    finishedByAssign,
    startedByAssign,
    memberFinalByAssign,
    assistantContentByAssign,
    noticeIndexByEventId,
  };
}

function isMemberReportExpanded(key) {
  return Boolean(expandedMemberReports.value[key]);
}

function toggleMemberReport(key) {
  if (!key) return;
  expandedMemberReports.value = {
    ...expandedMemberReports.value,
    [key]: !expandedMemberReports.value[key],
  };
}

function isAssignTaskExpanded(key) {
  return Boolean(expandedAssignTasks.value[key]);
}

function toggleAssignTask(key) {
  if (!key) return;
  expandedAssignTasks.value = {
    ...expandedAssignTasks.value,
    [key]: !expandedAssignTasks.value[key],
  };
}

function taskDetailsId(item) {
  const key = String(item?.taskToggleKey || item?.key || "task").replace(/[^a-zA-Z0-9_-]/g, "-");
  return `wg-task-details-${key}`;
}

function parseNoticeTool(text) {
  const raw = String(text || "").trim();
  if (!raw) return { toolName: "tool", summary: "执行成员工具" };
  const parts = raw.split(/\s*·\s*/);
  const toolName = String(parts[0] || "tool").trim() || "tool";
  const purposeByTool = {
    read_file: "读取文件",
    show_image: "展示图片",
    read_image: "分析图片",
    write_file: "写入文件",
    glob_files: "查找文件",
    grep_file: "搜索内容",
    grep_files: "搜索内容",
    search_replace: "替换内容",
    bash_run: "执行命令",
    background_job_status: "查看后台任务",
    background_job_cancel: "取消后台任务",
  };
  const knownToolNames = new Set(Object.keys(purposeByTool));
  const purpose =
    purposeByTool[toolName] ||
    (Object.values(purposeByTool).includes(toolName)
      ? toolName
      : knownToolNames.has(toolName)
        ? "执行成员工具"
        : parts.length === 1
          ? toolName
          : "执行成员工具");
  return {
    toolName,
    summary: purpose,
  };
}

function toolKindLabel(toolName) {
  const kind = inferToolKind(toolName);
  if (kind === "fs") return "fs";
  if (kind === "shell") return "shell";
  if (kind === "terminal") return "terminal";
  if (kind === "browser") return "browser";
  if (kind === "mcp") return "mcp";
  return "tool";
}

function makeAssignItem(started, finished, notices, isDirect, memberFinal, assistantContents = []) {
  const noticeList = Array.isArray(notices) ? notices : notices ? [notices] : [];
  const lastNotice = noticeList.length ? noticeList[noticeList.length - 1] : null;
  const noticeText = lastNotice ? String(lastNotice.text || "").trim() : "";
  const parsed = parseAssignStartedText(started?.text || "");
  const fallbackSummary = String(started?.text || finished?.text || "").trim() || "分派任务";
  const taskText = parsed.taskText || fallbackSummary;
  const statusText = finished
    ? String(finished.text || "").trim() || "已完成"
    : noticeText || "执行中";
  const reportText = memberFinal ? String(memberFinal.text || "").trim() : "";
  const assignId =
    String(started?.assign_id || finished?.assign_id || memberFinal?.assign_id || "").trim();
  const reporter = memberFinal ? String(memberFinal.actor_id || "").trim() : "";
  const reportActorLabel = reporter
    ? memberNameById.value[reporter] || reporter
    : "";
  const mention =
    parsed.mention ||
    reportActorLabel ||
    "";
  const failed = finished ? /失败|中断/.test(String(finished.text || "")) : false;
  const contentList = Array.isArray(assistantContents) ? assistantContents : [];
  const activity = [
    ...contentList.map((ev) => ({ kind: "content", ev })),
    ...noticeList.map((ev) => ({ kind: "tool", ev })),
  ].sort((a, b) => Number(a.ev?.seq || 0) - Number(b.ev?.seq || 0));
  const lastToolIndex = activity.reduce(
    (last, entry, index) => (entry.kind === "tool" ? index : last),
    -1,
  );
  const steps = activity.flatMap((entry, idx) => {
    const ev = entry.ev;
    if (entry.kind === "content") {
      return [{
        key: ev.event_id || `content-${ev.seq || idx}`,
        kind: "content",
        text: String(ev.text || ""),
      }];
    }
    const p = parseNoticeTool(ev?.text);
    const isLast = idx === lastToolIndex;
    const done = Boolean(finished) || !isLast;
    return {
      key: ev.event_id || `step-${ev.seq || idx}`,
      kind: "tool",
      toolName: p.toolName,
      toolKind: toolKindLabel(p.toolName),
      summary: p.summary,
      statusText: !done ? "执行中" : failed && isLast ? "已中断" : "已完成",
      done,
      failed: Boolean(failed && done && isLast),
      inProgress: !done,
    };
  });
  return {
    key: (started || finished)?.event_id || `assign-${(started || finished)?.seq}`,
    kind: "assign",
    assignId,
    started: started || null,
    finished: finished || null,
    mention,
    taskText,
    summary: taskText,
    liveProgress: "",
    statusText,
    done: Boolean(finished),
    failed,
    direct: isDirect,
    steps,
    hasReport: Boolean(reportText),
    reportText,
    reportPreview: previewMemberReport(reportText),
    reportActorLabel,
    reportToggleKey: assignId || (started || finished)?.event_id || "",
    taskPreview: previewAssignTask(taskText),
    taskToggleKey: assignId || (started || finished)?.event_id || "",
  };
}

function makeDirectToolItem(ev, { assignFinished, isLast, failed }) {
  const parsed = parseNoticeTool(ev?.text);
  const done = Boolean(assignFinished) || !isLast;
  return {
    key: ev.event_id || `tool-${ev.seq}`,
    kind: "tool",
    toolName: parsed.toolName,
    toolKind: toolKindLabel(parsed.toolName),
    summary: parsed.summary,
    statusText: !done ? "执行中" : failed ? "已中断" : "已完成",
    done,
    failed: Boolean(failed && done && isLast),
    inProgress: !done,
  };
}

function makeLeaderToolItem(ev) {
  const parsed = parseNoticeTool(ev?.text);
  const inProgress = Boolean(
    sending.value &&
      streamMode.value === "leader" &&
      streamActorId.value === "leader" &&
      streamPhase.value === "tool" &&
      Number(ev?.seq || 0) > Number(statusWatermarkSeq.value || 0),
  );
  return {
    key: ev.event_id || `leader-tool-${ev.seq}`,
    kind: "tool",
    toolName: parsed.toolName,
    toolKind: toolKindLabel(parsed.toolName),
    summary: parsed.summary,
    statusText: inProgress ? "生成中" : "已完成",
    done: !inProgress,
    failed: false,
    inProgress,
  };
}

function mixRenderHash(hash, value) {
  let next = hash >>> 0;
  const raw = String(value ?? "");
  for (let i = 0; i < raw.length; i += 1) {
    next = Math.imul(next ^ raw.charCodeAt(i), 16777619) >>> 0;
  }
  return next;
}

function renderItemToken(item) {
  const lastStep = item?.steps?.[item.steps.length - 1];
  const expanded = item?.reportToggleKey
    ? expandedMemberReports.value[item.reportToggleKey] ? 1 : 0
    : 0;
  const taskExpanded = item?.taskToggleKey
    ? expandedAssignTasks.value[item.taskToggleKey] ? 1 : 0
    : 0;
  return [
    item?.key,
    item?.kind,
    item?.text?.length || item?.ev?.text?.length || 0,
    item?.statusText,
    item?.done ? 1 : 0,
    item?.failed ? 1 : 0,
    item?.streaming ? 1 : 0,
    item?.phase,
    item?.tool,
    item?.reportText?.length || 0,
    expanded,
    taskExpanded,
    lastStep?.key,
    lastStep?.statusText,
    lastStep?.inProgress ? 1 : 0,
    lastStep?.failed ? 1 : 0,
  ].join("|");
}

function appendGroupItem(group, item) {
  group.items.push(item);
  group.renderHash = mixRenderHash(group.renderHash, renderItemToken(item));
  group.renderKey = `${group.bucket}|${group.label}|${group.items.length}|${group.renderHash}`;
}

/** 先按 assign_id 全局收口，再按 actor 分组；直连不展示分派壳，改为工具行 */
const eventGroups = computed(() => {
  const list = events.value || [];
  const {
    directAssignIds,
    noticesByAssign,
    finishedByAssign,
    startedByAssign,
    memberFinalByAssign,
    assistantContentByAssign,
    noticeIndexByEventId,
  } = buildAssignIndex(list);
  const consumedFinished = new Set();

  const flat = [];
  for (const ev of list) {
    const t = String(ev?.type || "");
    const aid = String(ev?.assign_id || "").trim();

    if (t === "assign_started") {
      const finished = aid ? finishedByAssign[aid] || null : null;
      if (aid && finished) consumedFinished.add(aid);
      const isDirect = Boolean(aid && directAssignIds.has(aid));
      // 直连：跳过分派气泡，工具过程由 notice 转成工具行
      if (isDirect) continue;
      const notices = aid ? noticesByAssign[aid] || [] : [];
      const memberFinal = aid ? memberFinalByAssign[aid] || null : null;
      const actorId = String(ev?.actor_id || "leader").trim() || "leader";
      flat.push({
        role: "assistant",
        actorId,
        label: eventLabel({ ...ev, actor_id: actorId, type: "assign_started" }),
        item: makeAssignItem(
          ev,
          finished,
          notices,
          false,
          memberFinal,
          aid ? assistantContentByAssign[aid] || [] : [],
        ),
      });
      continue;
    }

    if (t === "assign_finished") {
      if (aid && startedByAssign[aid]) continue;
      if (aid && consumedFinished.has(aid)) continue;
      const isDirect = Boolean(aid && directAssignIds.has(aid));
      if (isDirect) continue;
      const actorId = String(ev?.actor_id || "leader").trim() || "leader";
      const notices = aid ? noticesByAssign[aid] || [] : [];
      const memberFinal = aid ? memberFinalByAssign[aid] || null : null;
      flat.push({
        role: "assistant",
        actorId,
        label: eventLabel({ ...ev, actor_id: actorId, type: "assign_finished" }),
        item: makeAssignItem(
          null,
          ev,
          notices,
          false,
          memberFinal,
          aid ? assistantContentByAssign[aid] || [] : [],
        ),
      });
      continue;
    }

    // 编排态成员回报并入分派气泡折叠展示；直连仍走普通消息
    if (t === "assistant_content" && aid && !directAssignIds.has(aid)) {
      continue;
    }

    if (t === "actor_final_text") {
      const actor = String(ev?.actor_id || "").trim();
      if (actor && actor !== "leader" && aid && !directAssignIds.has(aid)) {
        continue;
      }
    }

    if (t === "system_notice") {
      // 编排态：notice 只滚进分派气泡
      if (aid && !directAssignIds.has(aid)) continue;
      // 旧「已直达」提示不展示
      if (String(ev?.text || "").startsWith("已直达")) continue;
      const actorId = String(ev?.actor_id || "").trim();
      if (aid && directAssignIds.has(aid)) {
        const chain = noticesByAssign[aid] || [];
        const idx = ev.event_id ? noticeIndexByEventId[ev.event_id] ?? -1 : -1;
        const isLast = idx < 0 || idx === chain.length - 1;
        const finished = finishedByAssign[aid] || null;
        const failed = finished ? /失败|中断/.test(String(finished.text || "")) : false;
        flat.push({
          role: "assistant",
          actorId,
          label: eventLabel(ev),
          item: makeDirectToolItem(ev, {
            assignFinished: Boolean(finished),
            isLast,
            failed,
          }),
        });
        continue;
      }
      if (actorId === "leader") {
        flat.push({
          role: "assistant",
          actorId,
          label: eventLabel(ev),
          item: makeLeaderToolItem(ev),
        });
        continue;
      }
      flat.push({
        role: "assistant",
        actorId,
        label: eventLabel(ev),
        item: {
          key: ev.event_id || `notice-${ev.seq}`,
          kind: "progress",
          ev,
          text: String(ev?.text || ""),
        },
      });
      continue;
    }

    const role = isHumanEvent(ev) ? "user" : "assistant";
    const actorId = String(ev?.actor_id || "").trim();
    flat.push({
      role,
      actorId,
      label: eventLabel(ev),
      item: {
        key: ev.event_id || `msg-${ev.seq}`,
        kind: "message",
        ev,
      },
    });
  }

  if (liveUser.value) {
    const already = (list || []).some(
      (ev) =>
        String(ev?.type || "") === "human_message" &&
        String(ev?.text || "") === liveUser.value.text,
    );
    if (!already) {
      const actorId = selfNodeId.value || "node";
      flat.push({
        role: "user",
        actorId,
        label: selfNodeName.value || actorId,
      item: {
        key: liveUser.value.id,
        kind: "live_user",
        text: liveUser.value.text,
        directMemberId: liveUser.value.directMemberId,
      },
      });
    }
  }

  if (showLiveAssistant.value) {
    const actorId = streamActorId.value || (streamMode.value === "leader" ? "leader" : "member");
    flat.push({
      role: "assistant",
      actorId,
      label: actorId === "leader" ? "Supervisor" : memberNameById.value[actorId] || actorId,
      item: {
        key: liveAssistant.value.id,
        kind: "live_assistant",
        text: liveAssistant.value.text || "",
        streaming: true,
        phase: streamPhase.value,
        tool: streamToolName.value,
      },
    });
  }

  const groups = [];
  for (const row of flat) {
    const bucket = `${row.role}:${row.actorId || "_"}`;
    const last = groups[groups.length - 1];
    if (last && last.bucket === bucket) {
      appendGroupItem(last, row.item);
      continue;
    }
    const group = {
      key: `${bucket}-${row.item.key}`,
      bucket,
      role: row.role,
      label: row.label,
      items: [row.item],
      renderHash: mixRenderHash(2166136261, renderItemToken(row.item)),
    };
    group.renderKey = `${group.bucket}|${group.label}|1|${group.renderHash}`;
    groups.push(group);
  }
  return groups;
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
    stopPoll();
    stopWorkPoll();
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
    pendingHitl.value = [];
    hitlDraft.value = "";
    hitlBusy.value = false;
    debugOpen.value = false;
    debugRuns.value = [];
    debugLlm.value = null;
    debugSelectedRunId.value = "";
    debugHistory.value = null;
    debugError.value = "";
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
      startPoll();
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
  stopPoll();
  stopWorkPoll();
  stopQueuePoll();
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
                          <div
                            v-else
                            class="wg-tool-row"
                            :class="{
                              'wg-tool-row--progress': step.inProgress,
                              [`wg-tool-row--${step.toolKind || 'tool'}`]: true,
                            }"
                          >
                          <div class="wg-tool-row__bar">
                            <span class="wg-tool-row__glyph" aria-hidden="true">
                              <span v-if="step.inProgress" class="tool-exec-spinner" />
                              <span v-else-if="step.failed" class="wg-tool-row__mark">−</span>
                              <span v-else class="wg-tool-row__check">✓</span>
                            </span>
                            <span class="wg-tool-row__text">{{ step.summary }}</span>
                            <span class="wg-tool-row__status">
                              <BrandActivityIndicator
                                v-if="step.inProgress"
                                class="wg-tool-row__dots"
                                mode="tool"
                                :show-label="false"
                                compact
                              />
                              {{ step.statusText }}
                            </span>
                          </div>
                          </div>
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
                  <div
                    v-else-if="item.kind === 'tool'"
                    class="wg-tool-row"
                    :class="{
                      'wg-tool-row--progress': item.inProgress,
                      [`wg-tool-row--${item.toolKind || 'tool'}`]: true,
                    }"
                  >
                    <div class="wg-tool-row__bar">
                      <span class="wg-tool-row__glyph" aria-hidden="true">
                        <span v-if="item.inProgress" class="tool-exec-spinner" />
                        <span v-else-if="item.failed" class="wg-tool-row__mark">−</span>
                        <span v-else class="wg-tool-row__check">✓</span>
                      </span>
                      <span class="wg-tool-row__text">{{ item.summary }}</span>
                      <span class="wg-tool-row__status">
                        <BrandActivityIndicator
                          v-if="item.inProgress"
                          class="wg-tool-row__dots"
                          mode="tool"
                          :show-label="false"
                          compact
                        />
                        {{ item.statusText }}
                      </span>
                    </div>
                  </div>
                  <div
                    v-else-if="item.kind === 'progress'"
                    class="msg__hint msg__hint--stream-meta"
                  >
                    {{ item.text }}
                  </div>
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
                    <div
                      v-if="!item.text"
                      class="msg__hint msg__hint--stream-meta"
                      role="status"
                    >
                      <span class="msg__meta-label">{{ liveStatusLabel || "思考中…" }}</span>
                      <BrandActivityIndicator
                        :label="liveStatusLabel || '思考中…'"
                        mode="generating"
                        :show-label="false"
                        compact
                      />
                    </div>
                    <pre v-else class="assistant-msg__stream-plain">{{ item.text }}</pre>
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
              v-if="activeHitl"
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
                  <p class="wg-hitl-bubble__prompt">{{ activeHitl.prompt }}</p>
                  <p class="wg-hitl-bubble__hint">在下方输入框回答后 Enter 提交</p>
                </div>
              </div>
            </article>
            <article
              v-if="
                (sending || remoteSending) &&
                !activeHitl &&
                !showLiveAssistant
              "
              class="msg msg--assistant msg--progress"
            >
              <div class="msg__body msg__body--hint-only">
                <div
                  class="msg__hint msg__hint--stream-meta"
                  role="status"
                  :aria-label="liveStatusLabel || '协作进行中'"
                >
                  <span class="msg__meta-label">{{ liveStatusLabel || "协作进行中" }}</span>
                  <BrandActivityIndicator
                    :label="liveStatusLabel || '协作进行中'"
                    mode="generating"
                    :show-label="false"
                    compact
                  />
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
          <aside v-if="debugOpen" class="wg-debug" aria-label="RunHistory 调试">
            <header class="wg-debug__head">
              <strong>RunHistory</strong>
              <span v-if="debugLlmBadge" class="wg-debug__badge" :data-mode="debugLlm?.mode">
                {{ debugLlmBadge }}
              </span>
              <button type="button" class="wg-debug__refresh" :disabled="debugLoading" @click="loadDebugRuns">
                刷新
              </button>
            </header>
            <p v-if="debugError" class="wg-debug__error">{{ debugError }}</p>
            <p v-else-if="debugLoading && !debugRuns.length" class="wg-debug__muted">加载中…</p>
            <p v-else-if="!debugRuns.length" class="wg-debug__muted">暂无 ActorRun（发一条消息后会出现）</p>
            <ul v-else class="wg-debug__runs">
              <li v-for="r in debugRuns" :key="r.run_id">
                <button
                  type="button"
                  class="wg-debug__run"
                  :class="{ 'wg-debug__run--active': r.run_id === debugSelectedRunId }"
                  @click="selectDebugRun(r.run_id)"
                >
                  <span class="wg-debug__run-actor">{{ r.actor_id === 'leader' ? 'Supervisor' : r.actor_id }}</span>
                  <span class="wg-debug__run-status">{{ r.status }}</span>
                  <span class="wg-debug__run-id" :title="r.run_id">{{ r.run_id.slice(-8) }}</span>
                </button>
              </li>
            </ul>
            <div v-if="debugHistory" class="wg-debug__msgs">
              <div
                v-for="(m, i) in debugHistory.messages || []"
                :key="i"
                class="wg-debug__msg"
                :data-role="m.role"
              >
                <div class="wg-debug__msg-role">{{ m.role }}</div>
                <pre class="wg-debug__msg-body">{{ formatDebugMsg(m) }}</pre>
                <details
                  v-if="m.role === 'assistant' && m.tool_calls?.length"
                  class="wg-debug__details"
                >
                  <summary>工具参数</summary>
                  <pre
                    v-for="(tc, ti) in m.tool_calls"
                    :key="ti"
                    class="wg-debug__msg-body"
                  >{{ tc.function?.name || '?' }}
{{ tc.function?.arguments || '{}' }}</pre>
                </details>
              </div>
            </div>
          </aside>
        </div>

        <footer class="chat__composer">
          <div v-if="humanQueueItems.length" class="chat__queue" aria-label="排队中的消息">
            <div
              v-for="item in humanQueueItems"
              :key="item.queue_id"
              class="chat__queue-item"
            >
              <span class="chat__queue-pos">#{{ item.position }}</span>
              <template v-if="editingQueueId === item.queue_id">
                <input
                  v-model="editQueueDraft"
                  class="chat__queue-edit"
                  type="text"
                  @keydown.enter.prevent="saveQueuedEdit(item)"
                  @keydown.escape.prevent="cancelEditQueued"
                />
                <button type="button" class="chat__queue-btn" @click="saveQueuedEdit(item)">保存</button>
                <button type="button" class="chat__queue-btn chat__queue-btn--ghost" @click="cancelEditQueued">
                  取消
                </button>
              </template>
              <template v-else>
                <span class="chat__queue-text" :title="item.text">{{ item.text }}</span>
                <button type="button" class="chat__queue-btn chat__queue-btn--send" @click="sendQueuedNow(item)">
                  立即发送
                </button>
                <button type="button" class="chat__queue-btn" @click="beginEditQueued(item)">修改</button>
                <button
                  type="button"
                  class="chat__queue-btn chat__queue-btn--ghost"
                  title="取消排队"
                  @click="removeQueued(item)"
                >
                  ×
                </button>
              </template>
            </div>
          </div>
          <div class="chat__composer-pill" style="position: relative">
            <div
              v-if="mentionOpen && mentionCandidates.length && !hitlMode"
              class="wg-mention-menu"
              role="listbox"
            >
              <button
                v-for="m in mentionCandidates"
                :key="m.member_id"
                type="button"
                class="wg-mention-menu__item"
                @mousedown.prevent="pickMention(m)"
              >
                <strong>{{ m.display_name }}</strong>
                <span class="muted">{{ m.member_id }}</span>
              </button>
            </div>
            <div class="chat__composer-pill-center wg-composer-field">
              <button
                v-if="directMember && !hitlMode"
                type="button"
                class="wg-task__at wg-composer-at"
                :title="`取消 @${directMember.display_name}`"
                :aria-label="`取消 @${directMember.display_name}`"
                @click="clearDirectMention"
              >
                @{{ directMember.display_name }}
              </button>
              <input
                v-if="hitlMode"
                v-model="hitlDraft"
                class="chat__textarea"
                type="text"
                placeholder="回答 Supervisor 的问题…"
                :disabled="hitlBusy"
                @keydown.enter.prevent="submitHitlAnswer"
              />
              <input
                v-else
                v-model="draft"
                class="chat__textarea"
                type="text"
                :placeholder="
                  !canChat
                    ? isConfiguring
                      ? '请先发布工作组后再发言…'
                      : '当前状态不可对话…'
                    : directMember
                      ? '输入直达成员的任务…'
                      : '向工作组发言，输入 @ 直达成员…'
                "
                :disabled="!canChat"
                @input="onDraftInput"
                @keydown.enter.prevent="send"
                @keydown.esc="mentionOpen = false"
                @keydown.backspace="onComposerBackspace"
              />
            </div>
            <div class="chat__composer-pill-right">
              <button
                v-if="sending && !hitlMode"
                type="button"
                class="chat__composer-send chat__composer-send--cancel"
                title="取消"
                aria-label="取消"
                :disabled="cancelling"
                @click="cancelTurn"
              >
                <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                  <path
                    d="M4.5 4.5l7 7M11.5 4.5l-7 7"
                    stroke="currentColor"
                    stroke-width="1.6"
                    stroke-linecap="round"
                  />
                </svg>
              </button>
              <button
                v-else-if="hitlMode"
                type="button"
                class="chat__composer-send"
                title="提交回答"
                aria-label="提交回答"
                :disabled="!hitlDraft.trim() || hitlBusy"
                @click="submitHitlAnswer"
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
              <button
                v-else
                type="button"
                class="chat__composer-send"
                title="发送"
                aria-label="发送"
                :disabled="!draft.trim() || !canChat"
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
              <span class="chat__input-strip-left">{{
                hitlMode
                  ? hitlBusy
                    ? "提交回答中…"
                    : "回答询问 · Enter 提交"
                  : sending
                    ? cancelling
                      ? "正在取消…"
                      : liveStatusLabel || "协作进行中…"
                    : remoteSending
                      ? liveStatusLabel || "其他 Node 协作中…"
                    : directMember
                      ? "直达成员 · Enter 发送 · 点击 @ 取消"
                      : "Enter 发送 · @ 直达成员"
              }}</span>
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
.wg-debug {
  flex: 0 0 320px;
  max-width: 38%;
  min-width: 260px;
  border-left: 1px solid var(--color-border, #e5e7eb);
  background: var(--color-surface, #fafafa);
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.wg-debug__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--color-border, #e5e7eb);
  font-size: 13px;
}
.wg-debug__badge {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--color-primary, #0078d4) 12%, transparent);
  color: var(--color-primary, #0078d4);
}
.wg-debug__badge[data-mode="mock"] {
  background: color-mix(in srgb, #c50f1f 12%, transparent);
  color: #c50f1f;
}
.wg-debug__refresh {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: var(--color-text-muted, #6b7280);
  font-size: 12px;
  cursor: pointer;
}
.wg-debug__error {
  margin: 8px 12px;
  color: #c50f1f;
  font-size: 12px;
}
.wg-debug__muted {
  margin: 12px;
  color: var(--color-text-muted, #6b7280);
  font-size: 12px;
}
.wg-debug__runs {
  list-style: none;
  margin: 0;
  padding: 6px;
  border-bottom: 1px solid var(--color-border, #e5e7eb);
  max-height: 28%;
  overflow: auto;
}
.wg-debug__run {
  width: 100%;
  display: grid;
  grid-template-columns: 1fr auto auto;
  gap: 6px;
  align-items: center;
  text-align: left;
  border: 0;
  background: transparent;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12px;
  color: var(--color-text, #111827);
}
.wg-debug__run:hover,
.wg-debug__run--active {
  background: color-mix(in srgb, var(--color-primary, #0078d4) 10%, transparent);
}
.wg-debug__run-status {
  color: var(--color-text-muted, #6b7280);
}
.wg-debug__run-id {
  font-family: ui-monospace, Consolas, monospace;
  color: var(--color-text-muted, #6b7280);
}
.wg-debug__msgs {
  flex: 1;
  overflow: auto;
  padding: 8px 10px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.wg-debug__msg {
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 8px;
  padding: 6px 8px;
  background: var(--color-editor, #fff);
}
.wg-debug__msg-role {
  font-size: 10.5px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--color-text-muted, #6b7280);
  margin-bottom: 4px;
}
.wg-debug__msg-body {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.45;
  font-family: ui-monospace, Consolas, monospace;
}
.wg-debug__details {
  margin-top: 4px;
  font-size: 12px;
}
.wg-debug__details summary {
  cursor: pointer;
  color: var(--color-primary, #0078d4);
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
.wg-composer-field {
  flex-direction: row;
  align-items: center;
  gap: 6px;
}
.wg-composer-at {
  flex: 0 0 auto;
  cursor: pointer;
}
.wg-composer-at:hover {
  filter: brightness(0.97);
}
.wg-composer-field .chat__textarea {
  flex: 1 1 auto;
  min-width: 0;
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
.wg-tool-row {
  width: 100%;
  max-width: 100%;
  margin: 2px 0;
  border-radius: 6px;
  border: 1px solid var(--color-border, #d1d5db);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111827);
}
.wg-tool-row:hover,
.wg-tool-row--progress {
  border-color: var(--color-border, #d1d5db);
  background: var(--color-surface, #fff);
}
.wg-tool-row__bar {
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  gap: 6px;
  width: 100%;
  min-width: 0;
  padding: 5px 8px;
}
.wg-tool-row__glyph {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  color: var(--color-success, #0f7b0f);
  font-size: 12px;
}
.wg-tool-row__check {
  opacity: 0.9;
}
.wg-tool-row__mark {
  opacity: 0.75;
  color: var(--color-text-subtle, #9ca3af);
}
.wg-tool-row__kind {
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
.wg-tool-row--fs .wg-tool-row__kind {
  color: var(--color-success, #0f7b0f);
  border-color: rgba(137, 209, 133, 0.25);
  background: var(--color-success-soft, rgba(15, 123, 15, 0.12));
}
.wg-tool-row--shell .wg-tool-row__kind {
  color: #e2a053;
  border-color: rgba(226, 160, 83, 0.25);
  background: rgba(226, 160, 83, 0.08);
}
.wg-tool-row__text {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  line-height: 1.35;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace);
  color: var(--color-text, #111827);
}
.wg-tool-row__status {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--color-text-subtle, #9ca3af);
  white-space: nowrap;
}
.wg-tool-row__dots {
  --stream-meta-dot-size: 4px;
  --stream-meta-dot-gap: 2px;
}
.wg-mention-menu {
  position: absolute;
  left: 12px;
  right: 48px;
  bottom: calc(100% + 6px);
  z-index: 20;
  max-height: 220px;
  overflow: auto;
  border: 1px solid var(--color-border, #d1d5db);
  border-radius: 10px;
  background: var(--color-surface, #fff);
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.12);
  padding: 4px;
}
.wg-mention-menu__item {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  border: 0;
  background: transparent;
  text-align: left;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
  font: inherit;
}
.wg-mention-menu__item:hover {
  background: color-mix(in srgb, var(--color-primary, #0078d4) 10%, transparent);
}
.wg-mention-menu__item .muted {
  font-size: 11px;
  color: var(--color-text-muted, #6b7280);
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
.chat__queue {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0 0 10px;
}
.chat__queue-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 10px;
  border: 1px dashed var(--color-border, rgba(0, 0, 0, 0.14));
  background: var(--color-surface, #fff);
  font-size: 13px;
}
.chat__queue-pos {
  flex: 0 0 auto;
  font-weight: 700;
  color: var(--color-text-muted, #6b7280);
  font-variant-numeric: tabular-nums;
}
.chat__queue-text {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chat__queue-edit {
  flex: 1 1 auto;
  min-width: 0;
  border: 1px solid var(--color-border, #d1d5db);
  border-radius: 6px;
  padding: 4px 8px;
  font: inherit;
}
.chat__queue-btn {
  flex: 0 0 auto;
  border: none;
  background: transparent;
  color: var(--color-primary, #0078d4);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
  padding: 2px 4px;
}
.chat__queue-btn--ghost {
  color: var(--color-text-muted, #6b7280);
}
.chat__queue-btn--send {
  font-weight: 600;
}
.wg-chat :deep(.chat__composer-pill) {
  padding-left: 16px;
}
.wg-chat :deep(.chat__textarea) {
  padding-left: 6px;
  padding-right: 8px;
}
</style>
