<script setup>
import { computed, defineAsyncComponent, onMounted, onUnmounted, onActivated, onDeactivated, ref, watch, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import { connectStream, shouldIgnoreSSEForAgent } from "../sse/stream.js";
import { getAgentStreamEventPolicy } from "../sse/agentEvents.js";
import MainChatPanel from "../components/MainChatPanel.vue";
import NavRail from "../components/NavRail.vue";
import AgentCreatePage from "../components/AgentCreatePage.vue";
import AgentEmptyState from "../components/AgentEmptyState.vue";
const TerminalWorkbench = defineAsyncComponent(() => import("../components/TerminalWorkbench.vue"));
import {
  agentStore,
  persistAgentId,
  ensureAgent,
  beginSubmit,
  beginImplicitTurn,
  isStaleEvent,
  isDuplicateEvent,
  observeEventContinuity,
  markEventApplied,
  shouldAckSSEEvent,
  resetEventTracking,
} from "../stores/agent.js";
import {
  transcriptStore,
  addUser,
  markSideEffectsApplied,
  markSideEffectsStale,
  addSystem,
  appendAssistant,
  appendReasoning,
  finalizeAssistant,
  finalizeReasoning,
  markHistoryCommitted,
  upsertToolCallFromSSE,
  applyToolResult,
  clearTranscript,
  setShowReasoning,
  finalizePartialToolCalls,
} from "../stores/transcript.js";
import {
  hitlStore,
  getHitlAt,
  dequeueHitlAt,
  peekHitl,
  clearHitl,
  buildApprovalResume,
  buildApprovalOneResume,
  buildUserInfoSubmitResume,
  buildMemoryConflictResume,
  enqueueHitlRequired,
  shouldSkipChildRuntimeDisplay,
} from "../stores/hitl.js";
import { consumeStartupURL, hydrateAgent, invalidateHydration } from "../stores/hydrate.js";
import {
  recordSSEEvent,
  startPerformanceSpan,
} from "../stores/performanceDiagnostics.js";
import {
  startDesktopFocusHeartbeat,
  stopDesktopFocusHeartbeat,
  pulseDesktopFocus,
} from "../stores/desktopFocus.js";
import { classifyCancelOutcome } from "../stores/cancelState.js";
import { createTurnWatchdog } from "../stores/turnWatchdog.js";
import { COMPOSER_DRAFT_KEY } from "../utils/helpCommands.js";
import { chromeStore, setUsageFromSSE, resetUsageStrip } from "../stores/chrome.js";
import {
  startStatus,
  finishStatus,
  hasStatus,
  resetStatusLines,
  syncTurnStatus,
} from "../stores/statusLines.js";
import {
  turnStateStore,
  applyTurnState,
  setOutputChannel,
  markTurnAccepted,
  failTurnSubmission,
  beginTurnCancellation,
  markTurnCancellationConfirmed,
  markTurnCancellationFailed,
  resetTurnState,
  isTurnProcessing,
  isTurnTerminal,
} from "../stores/turnState.js";
import { resetToolStream } from "../stores/toolStream.js";
import {
  onChildCreated,
  onChildProgress,
  onChildFinished,
  resetRemoteWorkers,
  setChildAwaitingApproval,
  syncChildAgentsFromApi,
} from "../stores/remoteWorkers.js";
import { runSlashCommand } from "../utils/commands.js";
import { agentDisplayTitle, agentRecordId } from "../utils/format.js";
import { canToggleThinking, hasThinkingSecondaryControl } from "../utils/llmControls.js";

const router = useRouter();
const route = useRoute();

const routeNotice = ref("");
let routeNoticeTimer = null;
const hitlSelected = ref([]);
const cancelling = ref(false);
const streamHandle = ref(null);
const agentPanelRef = ref(null);
const showAgentCreatePage = ref(false);
const createPageTemplateId = ref("");
const agentListCount = ref(null);
const agentList = ref([]);
const currentAgentDisplayName = ref("");
const chatPanelRef = ref(null);
const selectedTerminalId = ref("");
const selectedTerminalMeta = ref(null);
const terminalRevision = ref(0);
let agentNameSyncToken = 0;
let sseResyncToken = 0;
let sseResyncInFlight = false;
let hitlReconcileInFlight = false;
let hitlReconciledTurnId = "";
let agentSwitchToken = 0;
let agentSwitchPromise = null;

const turnWatchdog = createTurnWatchdog({
  isAwaiting: () => isTurnProcessing(),
  hasStuckStatus: () =>
    hasStatus("queued") ||
    hasStatus("model_generating") ||
    hasStatus("thinking") ||
    hasStatus("assistant_generating"),
  onStuck: () => resyncAfterSSEGap("watchdog"),
});

const entries = computed(() => transcriptStore.entries);
const hitlKind = computed(() => peekHitl()?.kind || "");
const hasUserInfoHitl = computed(() => hitlKind.value === "user_information");
const canSend = computed(() => {
  if (hitlStore.busy) return false;
  if (hasUserInfoHitl.value) return true;
  if (hitlKind.value) return false;
  return !isTurnProcessing();
});
const sending = computed(() => isTurnProcessing());
const thinkingSupported = computed(() => !!chromeStore.llmSettings?.thinking_supported);
const showNoAgentWelcome = computed(
  () => !agentStore.agentId && agentListCount.value === 0
);
const currentAgentTitle = computed(() => {
  if (!String(agentStore.agentId || "").trim()) return "";
  return String(currentAgentDisplayName.value || "").trim() || "未命名 Agent";
});

async function syncCurrentAgentDisplayName() {
  const id = String(agentStore.agentId || "").trim();
  const token = ++agentNameSyncToken;
  if (!id) {
    currentAgentDisplayName.value = "";
    return;
  }
  const fromList = agentList.value.find((a) => agentRecordId(a) === id);
  if (fromList) {
    const name = String(fromList.display_name || fromList.DisplayName || "").trim();
    if (token === agentNameSyncToken) {
      currentAgentDisplayName.value = name || "未命名 Agent";
    }
    return;
  }
  try {
    const agent = await api.getAgent(id);
    if (token !== agentNameSyncToken) return;
    const name = String(agent?.display_name || agent?.DisplayName || "").trim();
    currentAgentDisplayName.value = name || "未命名 Agent";
  } catch {
    if (token !== agentNameSyncToken) return;
    currentAgentDisplayName.value = "未命名 Agent";
  }
}

function syncRouteAgent(agentId) {
  const id = String(agentId || "").trim();
  if (route.params.agentId === id) return;
  router.replace({ name: "agents", params: id ? { agentId: id } : {} });
}

function syncReasoningDisplay(_llm) {
  // 思考内容不再在对话区展示；思考开关仅控制 LLM thinking 模式。
  setShowReasoning(false);
}

function restartStream() {
  streamHandle.value?.close();
  if (!agentStore.agentId) {
    streamHandle.value = null;
    return;
  }
  streamHandle.value = connectStream({
    getAgentId: () => agentStore.agentId,
    getAfterSeq: () => transcriptStore.lastSeq,
    getAfterAgentSeq: () => transcriptStore.lastAgentSeq,
    onStatus: (s) => {
      chromeStore.sseStatus = s;
    },
    onEvent: handleEvent,
    onReconnect: () => {
      terminalRevision.value += 1;
      void resyncAfterSSEGap("reconnect");
    },
  });
}

/** SSE 断线重连或状态卡住时：hydrate 对账 turn/status/HITL（不重启已恢复的流）。 */
async function resyncAfterSSEGap(reason) {
  if (!agentStore.agentId || sseResyncInFlight) return;
  sseResyncInFlight = true;
  const token = ++sseResyncToken;
  const agentId = agentStore.agentId;
  turnWatchdog.noteActivity();
  try {
    const data = await hydrateAgent();
    if (data === null) return;
    if (token !== sseResyncToken || agentStore.agentId !== agentId) return;
    turnWatchdog.noteActivity();
    // A Node restart can complete while the NavRail is still serving its
    // cached agent list. Reconcile it after the stream has reconnected so a
    // newly created/registered Agent becomes selectable without waiting for
    // the periodic 30-second rail refresh.
    if (reason === "reconnect") {
      await agentPanelRef.value?.refresh?.({ force: true });
    }
  } catch (e) {
    if (token !== sseResyncToken || agentStore.agentId !== agentId) return;
    agentStore.error = e.message || String(e);
  } finally {
    sseResyncInFlight = false;
  }
}

async function activateAgentStream() {
  if (!agentStore.agentId) {
    clearTranscript();
    clearHitl();
    resetTurnState();
    streamHandle.value?.close();
    streamHandle.value = null;
    chromeStore.sseStatus = "idle";
    return;
  }
  const prev = agentStore.agentId;
  const data = await hydrateAgent();
  if (data === null) return;
  if (agentStore.agentId !== prev || !streamHandle.value) {
    restartStream();
  }
  await syncChildAgentsFromApi();
  terminalRevision.value += 1;
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
}

/** 发消息时：流已在则只 ensure runtime，避免每次全量 hydrate。 */
async function ensureStreamReady() {
  // Sidebar/route switching is asynchronous. A message submitted during the
  // switch must wait until the target Agent has finished hydrate and stream
  // setup; otherwise it can be posted against a half-reset global transcript
  // and the old stream may consume the visible lifecycle events.
  if (agentSwitchPromise) {
    await agentSwitchPromise;
  }
  if (!agentStore.agentId) {
    await activateAgentStream();
    return;
  }
  if (!streamHandle.value) {
    await activateAgentStream();
    return;
  }
  await ensureAgent();
}

async function refreshAfterPageRestore() {
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetUsageStrip();
  resetEventTracking();
  await activateAgentStream();
  await refreshLLMSettings();
  refreshContextTokens();
  agentPanelRef.value?.refresh?.();
}

function onPageShow(event) {
  if (event?.persisted) {
    void refreshAfterPageRestore();
  }
}

async function refreshMeta() {
  try {
    const boot = await api.getUIBootstrap();
    chromeStore.agentInfo = { ...(boot.health || {}), ...(boot.info || {}) };
    chromeStore.llmSettings = boot.llm || null;
    syncReasoningDisplay(chromeStore.llmSettings);
  } catch (e) {
    agentStore.error = e.message || "无法加载 Node 状态";
  }
}

async function refreshContextTokens() {
  if (!agentStore.agentId) return;
  try {
    const ctx = await api.getAgentContext(agentStore.agentId);
    chromeStore.contextTokens = Number(ctx.messages_total_tokens ?? -1);
  } catch {
    /* keep last */
  }
}

function handleEvent(ev) {
  // 防御：忽略非当前 Agent 的事件（切 Agent 时旧 EventSource 可能仍投递）
  if (shouldIgnoreSSEForAgent(ev?.agentId, agentStore.agentId)) return;

  const continuity = observeEventContinuity(ev.agentSeq, ev.epoch);
  if (continuity.epochChanged) {
    void resyncAfterSSEGap("epoch-change");
    return;
  }
  if (continuity.gap) {
    void resyncAfterSSEGap("sequence-gap");
  }
  if (isStaleEvent(ev.seq, ev.agentSeq, ev.epoch) || isDuplicateEvent(ev.seq, ev.agentSeq, ev.epoch)) return;
  recordSSEEvent(ev.type, ev.seq);
  const eventSpan = startPerformanceSpan("sse.handle", { type: ev.type });
  try {
    turnWatchdog.noteActivity();
    if (["terminal.opened", "terminal.updated", "terminal.closed"].includes(ev.type)) {
      terminalRevision.value += 1;
    }
    const skipRender = shouldSkipChildRuntimeDisplay(ev.type, ev.data);

    if (!skipRender) {
      switch (ev.type) {
    case "turn_state":
      if (applyTurnState(ev.data, { source: "event" })) {
        syncTurnStatus(turnStateStore);
        if (
          ["tool_waiting", "waiting_user"].includes(String(turnStateStore.phase || "")) &&
          hitlStore.queue.length === 0
        ) {
          void reconcilePendingHitl(ev.data);
        }
        if (isTurnTerminal()) {
          // A terminal lifecycle event supersedes any reconnect/watchdog
          // hydrate that may still be in flight. Its response must not be
          // allowed to replace the now-committed final assistant snapshot.
          invalidateHydration();
          sseResyncToken += 1;
          // HITL cards are projections of the current turn's pending
          // approval. Once the authoritative lifecycle reaches a terminal
          // phase, no approval can remain actionable, even when the
          // approval was resolved from another tab or connection.
          clearHitl();
          hitlReconciledTurnId = "";
          markHistoryCommitted(turnStateStore.historyRevision);
          if (["requesting", "confirmed"].includes(turnStateStore.cancelState)) {
            markTurnCancellationConfirmed();
          }
          finalizeAssistant();
          finalizeReasoning();
          finalizePartialToolCalls({ interrupted: turnStateStore.phase !== "completed" });
          resetToolStream();
          syncChildAgentsFromApi();
          refreshContextTokens();
        }
      }
      break;
    case "assistant":
      setOutputChannel("assistant");
      syncTurnStatus(turnStateStore);
      appendAssistant(String(ev.data.content || ""));
      break;
    case "reasoning":
      setOutputChannel("reasoning");
      syncTurnStatus(turnStateStore);
      appendReasoning(String(ev.data.content || ""));
      break;
    case "tool_call":
      if (ev.data?.partial) {
        setOutputChannel("tool_call");
        syncTurnStatus(turnStateStore);
      }
      upsertToolCallFromSSE(ev.data);
      break;
    case "tool_result":
      applyToolResult(ev.data);
      break;
    case "usage":
      setUsageFromSSE(ev.data);
      break;
    case "error":
      addSystem(`error: ${ev.data.message || "unknown"}`);
      break;
    case "system_notice":
      addSystem(String(ev.data?.message || "工具集已变更"));
      break;
    case "runtime/config-changed":
      agentPanelRef.value?.refresh?.();
      addSystem(
        ev.data?.applied === false
          ? "运行时配置已更新，将在当前回合结束后生效。"
          : "运行时配置已更新。",
      );
      break;
    case "memory/changed":
      addSystem(
        ev.data?.next_turn === true || ev.data?.turn_boundary === "next_turn"
          ? "长期记忆已更新，将在下一轮生效。"
          : "长期记忆已更新。",
      );
      break;
    case "skills/changed":
      if (typeof window !== "undefined") {
        window.dispatchEvent(new CustomEvent("dagents:skills-changed", { detail: ev.data || {} }));
      }
      addSystem(
        ev.data?.change === "loaded_set"
          ? ev.data?.applied_boundary === "next_model_step"
            ? "技能状态已更新，将在下一步模型请求生效。"
            : "技能状态已更新，将在下一轮生效。"
          : ev.data?.applied_boundary === "next_model_step"
            ? "技能目录已更新，将在上下文重建后的下一步模型请求生效。"
            : "技能目录已变化，将在下一轮上下文边界更新。",
      );
      break;
    case "mcp/catalog-changed":
      addSystem(
        ev.data?.applied === false
          ? "MCP 工具目录已变化，将在当前回合结束后生效。"
          : "MCP 工具目录已更新。",
      );
      break;
    case "turn_finished":
      finalizeAssistant();
      finalizeReasoning();
      finalizePartialToolCalls({ interrupted: true });
      refreshContextTokens();
      break;
    case "resync_required":
      void resyncAfterSSEGap("server-resync");
      break;
    case "hitl_required":
      finalizeAssistant();
      finalizeReasoning();
      {
        const { approval } = enqueueHitlRequired(ev.data);
        if (approval?.child_agent_id) setChildAwaitingApproval(approval.child_agent_id, true);
      }
      break;
    case "temporary_agent_created":
      onChildCreated(ev.data);
      break;
    case "temporary_agent_progress":
      onChildProgress(ev.data);
      break;
    case "temporary_agent_completed":
    case "temporary_agent_cancelled":
      onChildFinished(ev.data);
      break;
    case "context_compression_blocking":
    case "context_compression_silent":
      handleCompressionEvent(ev.type, ev.data);
      refreshContextTokens();
      break;
    case "side_effect_turn_start":
      beginImplicitTurn();
      syncTurnStatus({ phase: "queued" });
      turnWatchdog.noteActivity();
      break;
    case "side_effect_applied":
      markSideEffectsApplied(Array.isArray(ev.data.seqs) ? ev.data.seqs : []);
      break;
    case "side_effects_cleared":
      markSideEffectsStale(Array.isArray(ev.data.seqs) ? ev.data.seqs : []);
      break;
        default:
          break;
      }
    }

    // The registry and the view switch are intentionally checked together at
    // runtime. It protects against adding a transport event and forgetting
    // to give it a UI policy/handler in the same change.
    if (getAgentStreamEventPolicy(ev.type) === "unknown") {
      addSystem(`收到未识别的 Agent 事件：${String(ev.type || "unknown")}`);
    }

    markEventApplied(ev.seq, {
      agentSeq: ev.agentSeq,
      epoch: ev.epoch,
      ack: !skipRender && shouldAckSSEEvent(ev.type, ev.data),
    });
  } finally {
    eventSpan.end();
  }
}

function handleCompressionEvent(type, data) {
  const phase = String(data.phase || "");
  if (phase === "start") {
    startStatus("compression");
    return;
  }
  if (phase === "end") {
    finishStatus("compression");
  }
}

/**
 * HITL 卡片是 hydrate 的权威投影。正常情况下 hitl_required SSE 会直接入队，
 * 但在 tool_call / turn_state 紧邻发布、重连或序号对账时，客户端可能只收到
 * tool_waiting 而漏掉 hitl_required。此时只对当前 Turn 做一次 hydrate 对账，
 * 避免把“待审批”误显示成普通的待执行工具，也避免循环请求。
 */
async function reconcilePendingHitl(turnState) {
  if (hitlReconcileInFlight || hitlStore.queue.length > 0) return;
  const turnId = String(turnState?.turn_id || turnStateStore.turnId || "").trim();
  if (turnId && hitlReconciledTurnId === turnId) return;
  hitlReconcileInFlight = true;
  try {
    const data = await hydrateAgent();
    if (data !== null && turnId) hitlReconciledTurnId = turnId;
  } catch {
    // SSE 仍是主通道；hydrate 失败时交给重连/看门狗再次对账。
  } finally {
    hitlReconcileInFlight = false;
  }
}

function markSubmissionAccepted() {
  markTurnAccepted();
  // The POST acknowledgement means the turn is accepted, but its first
  // durable turn_state event may still be in flight. Show the queue state
  // during that gap instead of leaving the user with a blank busy composer.
  syncTurnStatus({ phase: "queued" });
}

async function submitHitlApproval(approveAll, hitlIndex = 0) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "approval") return;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  const resume = buildApprovalResume(item.data, { approveAll });
  try {
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_agent_id) setChildAwaitingApproval(item.data.child_agent_id, false);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    markSubmissionAccepted();
    turnWatchdog.noteActivity();
  } catch (e) {
    agentStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function submitHitlOne(payload, approve) {
  const hitlIndex = typeof payload === "object" ? payload.index : 0;
  const callId = typeof payload === "object" ? payload.callId : payload;
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "approval") return;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  const resume = buildApprovalOneResume(item.data, callId, approve);
  try {
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_agent_id) setChildAwaitingApproval(item.data.child_agent_id, false);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    markSubmissionAccepted();
    turnWatchdog.noteActivity();
  } catch (e) {
    agentStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function submitHitlMemoryConflict(hitlIndex, decision, { cancelled = false } = {}) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "memory_conflict") return;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  const resume = buildMemoryConflictResume(item.data, decision, { cancelled });
  try {
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    markSubmissionAccepted();
    turnWatchdog.noteActivity();
  } catch (e) {
    agentStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

function onHitlUserInfoSelected(v) {
  // 必须在 <script> 里写 ref.value；模板中顶层 ref 已自动解包，写 .value 不会更新选中态。
  hitlSelected.value = Array.isArray(v) ? [...v] : Number(v);
}

async function submitHitlUserInfo(hitlIndex, text) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "user_information") return;
  const built = buildUserInfoSubmitResume(item.data, {
    text,
    selected: hitlSelected.value,
  });
  if (!built.ok) {
    agentStore.error = built.error;
    return;
  }
  const resume = built.resume;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  try {
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_agent_id) setChildAwaitingApproval(item.data.child_agent_id, false);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    hitlSelected.value = [];
    beginSubmit();
    markSubmissionAccepted();
    turnWatchdog.noteActivity();
  } catch (e) {
    agentStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function onSendMessage(payload) {
  agentStore.error = "";
  const text = typeof payload === "string" ? payload : String(payload?.text || "").trim();
  const contentParts = typeof payload === "string" ? null : payload?.contentParts;
  const images = typeof payload === "string" ? [] : payload?.images || [];
  const fileRefs = typeof payload === "string" ? [] : payload?.fileRefs || [];

  if (text.startsWith("/")) {
    await handleCommand(text);
    return;
  }

  const hitl = peekHitl();
  if (hitl?.kind === "user_information") {
    await submitHitlUserInfo(0, text);
    return;
  }
  if (hitl?.kind === "memory_conflict") {
    agentStore.error = "请先处理长期记忆冲突确认";
    return;
  }

  if (isTurnProcessing()) {
    agentStore.error = "上一回合尚未结束";
    return;
  }

  await ensureStreamReady();
  clearHitl();
  addUser(text, images, fileRefs);
  beginSubmit();
  turnWatchdog.noteActivity();
  try {
    await api.submitMessage(agentStore.agentId, text, contentParts, fileRefs);
    markSubmissionAccepted();
  } catch (e) {
    failTurnSubmission();
    resetStatusLines();
    resetTurnState();
    agentStore.error = e.message;
  }
}

async function handleCommand(cmd) {
  const res = await runSlashCommand(cmd);
  if (res.error) {
    agentStore.error = res.error;
    return;
  }
  if (res.action === "clear") {
    await api.clearContext(await ensureAgent());
    // clearContext is a durable boundary. Remove the old local projection
    // before hydrate so the dirty-history guard cannot preserve pre-clear
    // messages when the response races with the clear acknowledgement.
    invalidateHydration();
    clearTranscript();
    resetTurnState();
    resetEventTracking();
    resetStatusLines();
    resetToolStream();
    resetUsageStrip();
    resetRemoteWorkers();
    chromeStore.contextTokens = 0;
    const data = await hydrateAgent();
    if (data === null) return;
    restartStream();
    addSystem("已清空对话上下文，并终止未完成命令与临时子 Agent");
    return;
  }
  if (res.action === "compress") {
    startStatus("compression");
    try {
      const out = await api.compressContext(await ensureAgent());
      if (out.status && out.status !== "applied" && out.status !== "done") {
        addSystem(`压缩: ${out.status}`);
      }
    } finally {
      finishStatus("compression");
    }
    refreshContextTokens();
    return;
  }
  if (res.action === "thinking") {
    await handleThinkingCommand(res.arg);
    return;
  }
  if (res.action === "upload") {
    await handleUploadCommand(res.upload);
    return;
  }
}

async function handleUploadCommand(spec) {
  if (!spec || spec.error) {
    agentStore.error = spec?.error || "upload 参数无效";
    return;
  }
  const { kind, path, id, version, name, platform, publish } = spec;
  try {
    let out;
    if (kind === "skill" || kind === "skills") {
      out = await api.uploadSkillToManage({ path, skillId: id, version, name, publish });
    } else if (["externaltool", "externaltools", "tool", "tools"].includes(kind)) {
      out = await api.uploadExternalToolToManage({ path, toolId: id, version, name, platform, publish });
    } else if (kind === "plugin" || kind === "plugins") {
      out = await api.uploadPluginToManage({ path, pluginId: id, version, name, platform, publish });
    } else {
      agentStore.error = `未知 upload 类型: ${kind}`;
      return;
    }
    const note = out.message ? ` — ${out.message}` : "";
    addSystem(`已上传 ${out.kind} ${out.id}@${out.version}（${out.status}${note}）`);
  } catch (err) {
    agentStore.error = err.message;
  }
}

async function openCreateWizard(templateId = "") {
  createPageTemplateId.value = String(templateId || "").trim();
  showAgentCreatePage.value = true;
}

function onAgentsUpdated(list) {
  agentList.value = Array.isArray(list) ? list.slice() : [];
  agentListCount.value = agentList.value.length;
  void syncCurrentAgentDisplayName();
}

function onCreatePageCancel() {
  showAgentCreatePage.value = false;
  createPageTemplateId.value = "";
}

async function onAgentCreated(created) {
  const id = agentRecordId(created);
  if (!id) return;
  showAgentCreatePage.value = false;
  createPageTemplateId.value = "";
  const createdName = String(created?.display_name || created?.DisplayName || "").trim();
  if (createdName) currentAgentDisplayName.value = createdName;
  persistAgentId(id);
  clearTranscript();
  resetUsageStrip();
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetEventTracking();
  clearHitl();
  resetTurnState();
  chromeStore.panel = null;
  restartStream();
  syncRouteAgent(id);
  pulseDesktopFocus();
  agentPanelRef.value?.refresh?.();
  try {
    const data = await hydrateAgent();
    if (data === null) return;
    await refreshLLMSettings();
  } catch (e) {
    agentStore.error = e.message;
  }
  await syncCurrentAgentDisplayName();
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
}

async function switchAgent(id) {
  const targetID = String(id || "").trim();
  if (!targetID) return;
  if (targetID === agentStore.agentId && streamHandle.value) return;
  const token = ++agentSwitchToken;
  const run = (async () => {
  // 先断开旧 SSE，避免切 Agent 间隙里旧连接继续改全局 status/transcript
  invalidateHydration();
  sseResyncToken += 1;
  streamHandle.value?.close();
  streamHandle.value = null;
  resetTurnState();
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetUsageStrip();
  resetEventTracking();
  clearHitl();
  chromeStore.panel = null;
  // transcriptStore is shared by the KeepAlive ChatView. It must be cleared
  // before the target hydrate; otherwise historyDirty/historyRevision can
  // intentionally reject the target snapshot and leave the previous Agent's
  // messages on screen.
  clearTranscript();
  currentAgentDisplayName.value = "";
  persistAgentId(targetID);
  void syncCurrentAgentDisplayName();
  turnWatchdog.noteActivity();
  try {
    const data = await hydrateAgent();
    if (data === null || token !== agentSwitchToken || agentStore.agentId !== targetID) return;
    await refreshLLMSettings();
    if (token !== agentSwitchToken || agentStore.agentId !== targetID) return;
  } catch (e) {
    if (token !== agentSwitchToken || agentStore.agentId !== targetID) return;
    agentStore.error = e.message;
    clearTranscript();
    clearHitl();
    resetEventTracking();
    resetTurnState();
    resetStatusLines();
    return;
  }
  restartStream();
  if (token !== agentSwitchToken || agentStore.agentId !== targetID) return;
  refreshContextTokens();
  await syncChildAgentsFromApi();
  if (token !== agentSwitchToken || agentStore.agentId !== targetID) return;
  syncRouteAgent(targetID);
  pulseDesktopFocus();
  agentPanelRef.value?.refresh?.();
  await syncCurrentAgentDisplayName();
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
  })();
  agentSwitchPromise = run;
  try {
    await run;
  } finally {
    if (agentSwitchPromise === run) agentSwitchPromise = null;
  }
}

async function deleteAgentById(payload) {
  const aid = String(typeof payload === "string" ? payload : payload?.id || "").trim();
  if (!aid) {
    agentStore.error = "无法删除：Agent ID 无效";
    return;
  }
  const agent = typeof payload === "object" && payload?.agent ? payload.agent : { agent_id: aid };
  const label = agentDisplayTitle(agent);
  if (!window.confirm(`确定删除 Agent「${label}」？\n\n将停止该实例并归档记录，不可恢复。`)) return;
  const deletingCurrent = agentStore.agentId === aid;
  agentStore.error = "";
  agentPanelRef.value?.setDeleting?.(aid);
  try {
    await api.deleteAgent(aid);
    streamHandle.value?.close();
    streamHandle.value = null;

    if (deletingCurrent) {
      resetTurnState();
      hitlStore.busy = false;
      clearTranscript();
      clearHitl();
      resetStatusLines();
      resetToolStream();
      resetRemoteWorkers();
      resetEventTracking();
      persistAgentId("");

      const res = await api.listAgents();
      const remaining = (res.agents || []).filter((row) => agentRecordId(row) !== aid);
      if (remaining.length > 0) {
        await switchAgent(agentRecordId(remaining[0]));
      } else {
        syncRouteAgent("");
        chromeStore.sseStatus = "idle";
        agentList.value = [];
        agentListCount.value = 0;
        currentAgentDisplayName.value = "";
        // 最后一个 Agent 被删时 switchAgent 不会跑，需显式刷新侧栏列表
        await agentPanelRef.value?.refresh?.();
      }
    } else {
      await agentPanelRef.value?.refresh?.();
    }
  } catch (e) {
    agentStore.error = e.message;
    if (deletingCurrent && agentStore.agentId === aid) {
      await activateAgentStream();
    }
  } finally {
    agentPanelRef.value?.setDeleting?.("");
  }
}

async function toggleThinkingMode() {
  if (!chromeStore.llmSettings?.thinking_supported) {
    agentStore.error = "当前 provider 不支持 thinking 控制";
    return;
  }
  if (!canToggleThinking(chromeStore.llmSettings)) {
    agentStore.error = "当前模型固定开启思考，无法切换";
    return;
  }
  agentStore.error = "";
  const t = String(chromeStore.llmSettings.thinking || "").toLowerCase();
  const enabled = !["disabled", "off"].includes(t);
  try {
    chromeStore.llmSettings = await api.patchLLMSettings({ thinking: enabled ? "disabled" : "enabled" });
    syncReasoningDisplay(chromeStore.llmSettings);
  } catch (e) {
    agentStore.error = e.message;
  }
}

async function cycleThinkingEffort() {
  if (
    !chromeStore.llmSettings?.thinking_supported ||
    !hasThinkingSecondaryControl(chromeStore.llmSettings)
  ) return;
  agentStore.error = "";
  const current = String(chromeStore.llmSettings.reasoning_effort || "high").toLowerCase();
  const next = current === "max" ? "high" : "max";
  try {
    chromeStore.llmSettings = await api.patchLLMSettings({ reasoning_effort: next });
  } catch (e) {
    agentStore.error = e.message;
  }
}

async function refreshLLMSettings() {
  try {
    chromeStore.llmSettings = await api.getLLMSettings();
    syncReasoningDisplay(chromeStore.llmSettings);
  } catch {
    /* best-effort */
  }
}

async function switchLLMProfile(id) {
  const profileId = String(id || "").trim();
  if (!profileId) return;
  if (!agentStore.agentId) {
    agentStore.error = "请先选择 Agent";
    return;
  }
  if (profileId === chromeStore.llmSettings?.active_profile) return;
  agentStore.error = "";
  try {
    // 绑定到当前 Agent；ensure/reload 时会应用到进程 LLM（含多模态）。
    await api.patchAgent(agentStore.agentId, { defaults: { llm: { active: profileId } } });
    await refreshLLMSettings();
    try {
      chromeStore.agentInfo = await api.getAgentInfo();
    } catch {
      /* agent info refresh best-effort */
    }
  } catch (e) {
    agentStore.error = e.message;
  }
}

async function handleThinkingCommand(arg) {
  const parts = String(arg || "").trim().split(/\s+/);
  const patch = {};
  if (!parts[0]) {
    addSystem(`thinking: ${chromeStore.llmSettings?.thinking || "-"}`);
    return;
  }
  if (["on", "enabled", "true", "1"].includes(parts[0])) patch.thinking = "enabled";
  else if (["off", "disabled", "false", "0"].includes(parts[0])) patch.thinking = "disabled";
  else if (parts[0] === "effort" && ["high", "max"].includes(parts[1])) patch.reasoning_effort = parts[1];
  else {
    agentStore.error = "用法: /thinking on|off 或 /thinking effort high|max";
    return;
  }
  chromeStore.llmSettings = await api.patchLLMSettings(patch);
  addSystem(`thinking: ${chromeStore.llmSettings.thinking || "-"}`);
}

function closePanel() {
  chromeStore.panel = null;
}

function consumeComposerDraft() {
  try {
    const cmd = sessionStorage.getItem(COMPOSER_DRAFT_KEY);
    if (!cmd) return;
    sessionStorage.removeItem(COMPOSER_DRAFT_KEY);
    nextTick(() => chatPanelRef.value?.setDraft?.(cmd));
  } catch {
    /* ignore */
  }
}


async function bootstrapAgentFromRoute() {
  const fromRoute = String(route.params.agentId || "").trim();
  if (fromRoute) {
    persistAgentId(fromRoute);
    return;
  }
  consumeStartupURL();
  if (agentStore.agentId) {
    // 校验持久化 id 是否仍存在
    try {
      await api.getAgent(agentStore.agentId);
      syncRouteAgent(agentStore.agentId);
    } catch {
      persistAgentId("");
    }
  }
}

async function cancelTurn() {
  // A user-information request owns the active Turn even if hydrate/SSE has
  // not propagated its interaction phase into turnState yet. The pending HITL
  // item is therefore also a valid cancellation signal; it must never be
  // mistaken for an answer submission.
  if (!agentStore.agentId || cancelling.value || (!isTurnProcessing() && !hasUserInfoHitl.value)) return;
  cancelling.value = true;
  beginTurnCancellation();
  agentStore.error = "";
  try {
    const response = await api.cancelAgentTurn(agentStore.agentId);
    let hydrate = null;
    try {
      hydrate = await hydrateAgent();
    } catch {
      // The cancellation acknowledgement remains usable if reconciliation
      // briefly fails; the next SSE/hydrate cycle will repair the view.
    }
    const outcome = classifyCancelOutcome(response, hydrate);
    if (outcome === "not_cancelled" || outcome === "invalid_scope") {
      markTurnCancellationFailed();
      agentStore.error = hydrate
        ? "turn 仍在执行，取消未生效，请稍后重试"
        : "取消状态未确认，请稍后重试";
      return;
    }

    if (outcome === "cancel_requested" && !isTurnTerminal()) {
      markTurnCancellationConfirmed();
      addSystem("正在取消本轮…");
      return;
    }
    resetStatusLines();
    finalizePartialToolCalls({ interrupted: true });
    clearHitl();
    finalizeAssistant();
    finalizeReasoning();
    resetToolStream();
    addSystem(outcome === "cancelled" ? "turn 已取消" : "当前没有正在执行的 turn");
  } catch (e) {
    markTurnCancellationFailed();
    agentStore.error = e.message;
  } finally {
    cancelling.value = false;
  }
}

function onKeydown(e) {
  if (e.key === "Escape" && chromeStore.panel) {
    closePanel();
  }
}

onMounted(async () => {
  await bootstrapAgentFromRoute();
  await syncCurrentAgentDisplayName();
  await refreshMeta();
  await activateAgentStream();
  // ensure 会按 Agent 绑定应用 LLM；再拉一次设置，避免刷新后仍显示进程默认档案。
  await refreshLLMSettings();
  refreshContextTokens();
  consumeComposerDraft();
  startDesktopFocusHeartbeat(() => agentStore.agentId);
  turnWatchdog.start();
  window.addEventListener("keydown", onKeydown);
  window.addEventListener("pageshow", onPageShow);
});

watch(
  () => agentStore.agentId,
  () => {
    void syncCurrentAgentDisplayName();
    selectedTerminalId.value = "";
    selectedTerminalMeta.value = null;
  },
);

const terminalOpen = computed(
  () =>
    Boolean(
      agentStore.agentId &&
        String(route.query.view || "") === "terminal",
    ),
);

function terminalRouteQuery(terminalId = "") {
  const query = { ...route.query };
  const id = String(terminalId || "").trim();
  if (id) {
    query.view = "terminal";
    query.terminal_id = id;
  } else {
    delete query.view;
    delete query.terminal_id;
  }
  return query;
}

function openTerminalRouteQuery() {
  const query = { ...route.query, view: "terminal" };
  delete query.terminal_id;
  return query;
}

function selectTerminal(item) {
  const id = String(item?.terminal_id || "").trim();
  if (!id) return;
  selectedTerminalId.value = id;
  selectedTerminalMeta.value = item;
  void router.replace({ name: "agents", params: { agentId: agentStore.agentId }, query: terminalRouteQuery(id) });
}

function closeTerminal() {
  // Leaving the workbench only hides its view. The selected session remains
  // resumable through the status bar and is cleared only by an authoritative
  // terminal.closed/terminated event or an explicit session removal.
  chromeStore.panel = null;
  void router.replace({ name: "agents", params: { agentId: agentStore.agentId }, query: terminalRouteQuery() });
  nextTick(() => {
    document.querySelector(".chat__textarea")?.focus();
  });
}

function clearTerminalSelection() {
  selectedTerminalId.value = "";
  selectedTerminalMeta.value = null;
  chromeStore.panel = null;
  void router.replace({
    name: "agents",
    params: { agentId: agentStore.agentId },
    query: openTerminalRouteQuery(),
  });
}

const workspaceView = computed(() => {
  return terminalOpen.value ? "terminal" : "messages";
});

function switchWorkspace(view) {
  const next = String(view || "messages");
  chromeStore.panel = null;
  if (next === "terminal") {
    void router.replace({
      name: "agents",
      params: { agentId: agentStore.agentId },
      query: openTerminalRouteQuery(),
    });
    return;
  }
  if (terminalOpen.value) closeTerminal();
}

watch(
  () => [route.query.view, route.query.terminal_id, agentStore.agentId],
  ([view, terminalId]) => {
    if (String(view || "") !== "terminal") {
      return;
    }
    const id = String(terminalId || "").trim();
    if (id && id !== selectedTerminalId.value) {
      selectedTerminalId.value = id;
      selectedTerminalMeta.value = null;
    }
  },
  { immediate: true },
);

onActivated(() => {
  turnWatchdog.start();
  if (agentStore.agentId) {
    void activateAgentStream();
  }
  consumeComposerDraft();
  startDesktopFocusHeartbeat(() => agentStore.agentId);
});

onDeactivated(() => {
  // KeepAlive 切到设置页时：停心跳/轮询/看门狗，并断开 SSE，避免焦点与状态串台
  invalidateHydration();
  sseResyncToken += 1;
  turnWatchdog.stop();
  stopDesktopFocusHeartbeat();
  streamHandle.value?.close();
  streamHandle.value = null;
  chromeStore.sseStatus = "idle";
});

watch(
  () => route.query.notice,
  (value) => {
    if (String(value || "") !== "workgroup-disabled") return;
    routeNotice.value = "工作组尚未启用，已返回智能体工作区。";
    const query = { ...route.query };
    delete query.notice;
    void router.replace({ query });
    if (routeNoticeTimer) clearTimeout(routeNoticeTimer);
    routeNoticeTimer = setTimeout(() => {
      routeNotice.value = "";
      routeNoticeTimer = null;
    }, 4000);
  },
  { immediate: true },
);

watch(
  () => route.params.agentId,
  async (id) => {
    const aid = String(id || "").trim();
    if (!aid) {
      if (agentStore.agentId) {
        // 允许停留在 /agents 空态
        return;
      }
      return;
    }
    // The route value and the hydrated Agent can temporarily diverge during
    // KeepAlive/router transitions. The switch token suppresses the duplicate
    // watcher triggered by syncRouteAgent(), while this guard only skips an
    // already-active Agent.
    if (aid === agentStore.agentId) return;
    await switchAgent(aid);
  },
);

watch(
  () => route.query.createAgent,
  (v) => {
    if (String(v || "") === "1") {
      openCreateWizard();
      const q = { ...route.query };
      delete q.createAgent;
      router.replace({ query: q });
    }
  },
  { immediate: true },
);

onUnmounted(() => {
  if (routeNoticeTimer) clearTimeout(routeNoticeTimer);
  invalidateHydration();
  sseResyncToken += 1;
  turnWatchdog.stop();
  stopDesktopFocusHeartbeat();
  streamHandle.value?.close();
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("pageshow", onPageShow);
});
</script>

<template>
  <div class="app__body app__body--chat-v61">
    <aside class="app__col app__col--agents">
      <NavRail
        ref="agentPanelRef"
        @switch="switchAgent"
        @create="openCreateWizard()"
        @delete="deleteAgentById"
        @agents-updated="onAgentsUpdated"
        @create-member="
          (id) =>
            router.push({
              name: 'workgroups',
              params: { workgroupId: id },
              query: { createMember: '1' },
            })
        "
        @configure-member="
          (p) =>
            router.push({
              name: 'workgroups',
              params: { workgroupId: p.workgroupId },
              query: { member: p.memberId, editMember: '1' },
            })
        "
      />
    </aside>

    <div class="app__main-col">
      <Transition name="chat-surface" mode="out-in">
        <AgentCreatePage
          v-if="showAgentCreatePage"
          :initial-template-id="createPageTemplateId"
          @cancel="onCreatePageCancel"
          @created="onAgentCreated"
        />

        <div v-else class="chat-surface">
          <div v-if="routeNotice" class="chat-notice-banner" role="status">{{ routeNotice }}</div>
          <div v-if="agentStore.error && !agentStore.agentId" class="chat-error-banner">{{ agentStore.error }}</div>
          <AgentEmptyState
            v-if="showNoAgentWelcome"
            @create="openCreateWizard()"
            @pick-template="openCreateWizard"
          />
          <div v-else-if="!agentStore.agentId" class="chat-empty-agent">
            <p>选择左侧 Agent，或点击 + 从模板新建。</p>
          </div>

          <div v-else class="chat-workspace">
        <MainChatPanel
          v-show="!terminalOpen"
          ref="chatPanelRef"
          :entries="entries"
          :hitl-queue="hitlStore.queue"
          :tool-verbose="transcriptStore.toolFoldVerbose"
          :disabled="!canSend && !sending"
          :sending="sending"
          :cancelling="cancelling"
          :error="agentStore.error"
          :hitl-busy="hitlStore.busy"
          :hitl-busy-index="hitlStore.busyIndex"
          :thinking-supported="thinkingSupported"
          :llm-settings="chromeStore.llmSettings"
          :agent-title="currentAgentTitle"
          :agent-id="agentStore.agentId"
          :terminal-refresh-key="terminalRevision"
          :workspace-view="workspaceView"
          @workspace-change="switchWorkspace"
          @send="onSendMessage"
          @cancel="cancelTurn"
          @toggle-thinking="toggleThinkingMode"
          @cycle-effort="cycleThinkingEffort"
          @switch-profile="switchLLMProfile"
          @approve-all="(idx) => submitHitlApproval(true, idx)"
          @reject-all="(idx) => submitHitlApproval(false, idx)"
          @approve-one="(payload) => submitHitlOne(payload, true)"
          @reject-one="(payload) => submitHitlOne(payload, false)"
          @user-info-submit="(idx) => submitHitlUserInfo(idx, '')"
          @user-info-selected="onHitlUserInfoSelected"
          @memory-conflict-decide="(payload) => submitHitlMemoryConflict(payload.index, payload.decision)"
          @memory-conflict-cancel="(idx) => submitHitlMemoryConflict(idx, 'cancelled', { cancelled: true })"
        />
        <TerminalWorkbench
          v-if="terminalOpen"
          :agent-id="agentStore.agentId"
          :selected-terminal-id="selectedTerminalId"
          :selected-terminal-meta="selectedTerminalMeta"
          :refresh-key="terminalRevision"
          :entries="entries"
          :hitl-queue="hitlStore.queue"
          :tool-verbose="transcriptStore.toolFoldVerbose"
          :agent-disabled="!canSend && !sending"
          :agent-input-disabled="!agentStore.agentId || hitlStore.busy || cancelling"
          :sending="sending"
          :cancelling="cancelling"
          :error="agentStore.error"
          :hitl-busy="hitlStore.busy"
          :hitl-busy-index="hitlStore.busyIndex"
          :thinking-supported="thinkingSupported"
          :llm-settings="chromeStore.llmSettings"
          :agent-title="currentAgentTitle"
          @close="closeTerminal"
          @terminal-selected="selectTerminal"
          @terminal-cleared="clearTerminalSelection"
          @send-agent="onSendMessage"
          @workspace-change="switchWorkspace"
          @cancel-agent="cancelTurn"
          @toggle-thinking="toggleThinkingMode"
           @cycle-effort="cycleThinkingEffort"
          @switch-profile="switchLLMProfile"
          @approve-all="(idx) => submitHitlApproval(true, idx)"
          @reject-all="(idx) => submitHitlApproval(false, idx)"
          @approve-one="(payload) => submitHitlOne(payload, true)"
          @reject-one="(payload) => submitHitlOne(payload, false)"
          @user-info-submit="(idx) => submitHitlUserInfo(idx, '')"
          @user-info-selected="onHitlUserInfoSelected"
          @memory-conflict-decide="(payload) => submitHitlMemoryConflict(payload.index, payload.decision)"
          @memory-conflict-cancel="(idx) => submitHitlMemoryConflict(idx, 'cancelled', { cancelled: true })"
          />
          </div>
        </div>
      </Transition>

    </div>
  </div>
</template>

<style scoped>
.chat-surface {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  flex-direction: column;
}

.chat-surface-enter-active,
.chat-surface-leave-active {
  transition: opacity 280ms ease, transform 280ms ease;
}

.chat-surface-enter-from {
  opacity: 0;
  transform: translateY(8px);
}

.chat-surface-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .chat-surface-enter-active,
  .chat-surface-leave-active {
    transition: none;
  }
}

.chat-workspace { display: flex; flex: 1; min-height: 0; flex-direction: column; }
.chat-workspace > :deep(.main-chat-panel) { min-height: 0; }
</style>
